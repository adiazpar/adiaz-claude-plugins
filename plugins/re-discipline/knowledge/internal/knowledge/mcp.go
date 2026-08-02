package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type MCPServer struct {
	AssetRoot        string
	InitialRoot      string
	DisableDense     bool
	DisableRerank    bool
	services         map[string]*Service
	serviceStamps    map[string]string
	preflightedRoots map[string]bool
	serviceMu        sync.Mutex
	configuredRoots  []string
	rootHints        []string
	sessionRoot      string
	rootsReceived    bool
	clientRoots      bool
	initialized      bool
	windowStart      time.Time
	windowCalls      int
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type stateToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	StateRequest
}

type queryToolInput struct {
	ProjectRoot          string   `json:"projectRoot,omitempty"`
	Query                string   `json:"query"`
	CampaignID           string   `json:"campaignId,omitempty"`
	AllowedSourceClasses []string `json:"allowedSourceClasses,omitempty"`
	AllowedReviewStates  []string `json:"allowedReviewStates,omitempty"`
	AllowedValidities    []string `json:"allowedValidities,omitempty"`
	Limit                int      `json:"limit,omitempty"`
	TokenBudget          int      `json:"tokenBudget,omitempty"`
	IncludeRaw           bool     `json:"includeRaw,omitempty"`
	RequestID            string   `json:"requestId,omitempty"`
}

type readToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	ExactReadRequest
}

type traceToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	TraceRequest
}

type contextPackMaterializeToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	ContextPackMaterializeRequest
}

type managerApplyToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	ManagerApplyRequest
}

type curationSubmitToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	CurationSubmitRequest
}

type closureApplyToolInput struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	ClosureApplyRequest
}

func (server *MCPServer) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	server.services = map[string]*Service{}
	server.serviceStamps = map[string]string{}
	server.preflightedRoots = map[string]bool{}
	server.windowStart = time.Now()
	if server.InitialRoot != "" {
		root, err := server.preflightRoot(server.InitialRoot)
		if err != nil {
			return err
		}
		server.configuredRoots = []string{root}
	}
	if value := os.Getenv("CLAUDE_PROJECT_DIR"); value != "" {
		root, err := server.preflightRoot(value)
		if err != nil {
			return err
		}
		server.configuredRoots = SortedUnique(append(server.configuredRoots, root))
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for scanner.Scan() {
		line := scanner.Bytes()
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			if err := writeRPCError(encoder, nil, -32700, "Parse error", nil); err != nil {
				return err
			}
			continue
		}
		if message.JSONRPC != "2.0" {
			if len(message.ID) > 0 {
				if err := writeRPCError(encoder, message.ID, -32600, "Invalid Request", nil); err != nil {
					return err
				}
			}
			continue
		}
		if message.Method == "" && len(message.Result) > 0 {
			server.handleClientResponse(message)
			continue
		}
		switch message.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
				Capabilities    struct {
					Roots *struct {
						ListChanged bool `json:"listChanged"`
					} `json:"roots"`
				} `json:"capabilities"`
				ClientInfo map[string]any `json:"clientInfo"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				if err := writeRPCError(encoder, message.ID, -32602, "Invalid initialize parameters", nil); err != nil {
					return err
				}
				continue
			}
			version := negotiateProtocol(params.ProtocolVersion)
			server.clientRoots = params.Capabilities.Roots != nil
			server.initialized = true
			result := map[string]any{
				"protocolVersion": version,
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": false},
				},
				"serverInfo": map[string]any{
					"name":    "re-discipline-knowledge",
					"title":   "Re-Discipline Knowledge",
					"version": RuntimeVersion,
				},
				"instructions": "Filesystem-canonical campaign state and finding-first retrieval. Use exact handles for expansion, typed mutation tools for writes, and projectRoot only when the host provides no single MCP root.",
			}
			if err := writeRPCResult(encoder, message.ID, result); err != nil {
				return err
			}
		case "notifications/initialized":
			if server.clientRoots {
				request := map[string]any{
					"jsonrpc": "2.0", "id": "rd-roots-1",
					"method": "roots/list", "params": map[string]any{},
				}
				if err := encoder.Encode(request); err != nil {
					return err
				}
			}
		case "notifications/cancelled", "notifications/roots/list_changed":
			if message.Method == "notifications/roots/list_changed" && server.clientRoots {
				request := map[string]any{
					"jsonrpc": "2.0", "id": "rd-roots-1",
					"method": "roots/list", "params": map[string]any{},
				}
				if err := encoder.Encode(request); err != nil {
					return err
				}
			}
		case "ping":
			if err := writeRPCResult(encoder, message.ID, map[string]any{}); err != nil {
				return err
			}
		case "tools/list":
			if !server.initialized {
				if err := writeRPCError(encoder, message.ID, -32002, "Server not initialized", nil); err != nil {
					return err
				}
				continue
			}
			if err := writeRPCResult(encoder, message.ID, map[string]any{"tools": toolDefinitions()}); err != nil {
				return err
			}
		case "tools/call":
			if !server.initialized {
				if err := writeRPCError(encoder, message.ID, -32002, "Server not initialized", nil); err != nil {
					return err
				}
				continue
			}
			if err := server.allowCall(); err != nil {
				if err := writeToolError(encoder, message.ID, err); err != nil {
					return err
				}
				continue
			}
			var call struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(message.Params, &call); err != nil {
				if err := writeRPCError(encoder, message.ID, -32602, "Invalid tool call", nil); err != nil {
					return err
				}
				continue
			}
			value, err := server.callTool(ctx, call.Name, call.Arguments)
			if err != nil {
				if err := writeToolError(encoder, message.ID, err); err != nil {
					return err
				}
				continue
			}
			if err := writeToolResult(encoder, message.ID, value); err != nil {
				return err
			}
		default:
			if len(message.ID) > 0 {
				if err := writeRPCError(encoder, message.ID, -32601, "Method not found", nil); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

func (server *MCPServer) allowCall() error {
	now := time.Now()
	if now.Sub(server.windowStart) >= time.Minute {
		server.windowStart = now
		server.windowCalls = 0
	}
	server.windowCalls++
	if server.windowCalls > 120 {
		return errors.New("local MCP rate limit exceeded; retry after one minute")
	}
	return nil
}

func (server *MCPServer) handleClientResponse(message rpcMessage) {
	var id string
	if err := json.Unmarshal(message.ID, &id); err != nil || id != "rd-roots-1" {
		return
	}
	var result struct {
		Roots []struct {
			URI string `json:"uri"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(message.Result, &result); err != nil {
		return
	}
	server.rootsReceived = true
	roots := []string{}
	for _, root := range result.Roots {
		path, err := fileURIPath(root.URI)
		if err != nil {
			continue
		}
		if managed, err := server.preflightRoot(path); err == nil {
			roots = append(roots, managed)
		}
	}
	server.rootHints = SortedUnique(roots)
}

func fileURIPath(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" {
		return "", errors.New("root must use file URI")
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}

func validateManagedRoot(value string) (string, error) {
	root, err := FindProjectRoot(value)
	if err != nil {
		return "", err
	}
	boundary, err := NewBoundary(root)
	if err != nil {
		return "", err
	}
	marker := filepath.Join(boundary.Root, ".re-discipline", "project-profile.md")
	if info, err := os.Stat(marker); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("project root lacks managed re-discipline marker")
	}
	body, _, err := ReadProjectFile(boundary, ".re-discipline/project-profile.md")
	if err != nil {
		return "", fmt.Errorf("read managed re-discipline marker: %w", err)
	}
	if !strings.Contains(string(body), SharedLawsMarker) {
		return "", errors.New("project root lacks the runtime-supported re-discipline shared-laws marker")
	}
	configuration := LoadConfiguration(boundary.Root)
	if configuration.Unsafe {
		return "", fmt.Errorf("project knowledge configuration is unsafe: %s",
			strings.Join(configuration.Errors, "; "))
	}
	return boundary.Root, nil
}

func (server *MCPServer) validateRoot(value string) (string, error) {
	root, err := FindProjectRoot(value)
	if err != nil {
		return "", err
	}
	return validateManagedRoot(root)
}

func (server *MCPServer) preflightRoot(value string) (string, error) {
	root, err := FindProjectRoot(value)
	if err != nil {
		return "", err
	}
	if server.preflightedRoots == nil {
		server.preflightedRoots = map[string]bool{}
	}
	if !server.preflightedRoots[root] {
		if _, recoveryErr := RecoverProject(root, filepath.Dir(server.AssetRoot)); recoveryErr != nil {
			if err := validateManagedRecoveryBoundary(root); err != nil {
				return "", fmt.Errorf(
					"managed MCP preflight recovery: %v; boundary validation: %w",
					recoveryErr, err,
				)
			}
			if _, err := validateManagedRoot(root); err != nil {
				return "", fmt.Errorf(
					"managed MCP preflight recovery: %v; project validation: %w",
					recoveryErr, err,
				)
			}
		}
		server.preflightedRoots[root] = true
	}
	return validateManagedRoot(root)
}

func validateManagedRecoveryBoundary(root string) error {
	boundary, err := NewBoundary(root)
	if err != nil {
		return err
	}
	for _, managed := range managedRecoveryFiles {
		target, err := safeMissingTarget(boundary, managed.Target)
		if err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("managed recovery target %s is not a regular file", managed.Target)
		}
	}
	for _, relative := range managedRecoveryDirectories {
		target, err := safeMissingTarget(boundary, relative)
		if err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed recovery directory %s is not a real directory", relative)
		}
	}
	return nil
}

func (server *MCPServer) resolveProject(explicit string) (string, error) {
	approved := append([]string(nil), server.configuredRoots...)
	if server.rootsReceived {
		approved = append(approved, server.rootHints...)
	}
	approved = SortedUnique(approved)
	if explicit != "" {
		root, err := server.validateRoot(explicit)
		if err != nil {
			return "", err
		}
		for _, allowed := range approved {
			if root == allowed {
				return root, nil
			}
		}
		if len(approved) > 0 {
			return "", errors.New("projectRoot was not granted by the launch configuration or MCP roots")
		}
		if server.sessionRoot == "" {
			server.sessionRoot = root
			return root, nil
		}
		if server.sessionRoot == root {
			return root, nil
		}
		return "", errors.New("projectRoot does not match this MCP session shard")
	}
	if len(approved) == 1 {
		return approved[0], nil
	}
	if len(approved) > 1 {
		return "", errors.New("multiple managed MCP roots are available; pass projectRoot explicitly")
	}
	if server.sessionRoot != "" {
		return server.sessionRoot, nil
	}
	return "", errors.New("no managed MCP root is available; pass projectRoot explicitly")
}

func (server *MCPServer) service(projectRoot string) (*Service, error) {
	root, err := server.resolveProject(projectRoot)
	if err != nil {
		return nil, err
	}
	stamp := server.serviceConfigurationStamp(root)
	server.serviceMu.Lock()
	defer server.serviceMu.Unlock()
	if server.services == nil {
		server.services = map[string]*Service{}
	}
	if server.serviceStamps == nil {
		server.serviceStamps = map[string]string{}
	}
	if cached, ok := server.services[root]; ok && server.serviceStamps[root] == stamp {
		return cached, nil
	}
	service, err := NewService(ServiceOptions{
		ProjectRoot: root, AssetRoot: server.AssetRoot,
		DisableDense: server.DisableDense, DisableRerank: server.DisableRerank,
	})
	if err != nil {
		return nil, err
	}
	server.services[root] = service
	server.serviceStamps[root] = stamp
	return service, nil
}

func (server *MCPServer) serviceConfigurationStamp(root string) string {
	projectPaths := []string{
		".re-discipline/config.json",
		".re-discipline/knowledge/policy.jsonc",
		".re-discipline/knowledge/retrieval-profile.json",
	}
	assetPaths := []string{
		"profiles/balanced-v1.json",
		"models/manifest.json",
	}
	parts := make([]string, 0, len(projectPaths)+len(assetPaths)+2)
	boundary, boundaryErr := NewBoundary(root)
	for _, path := range projectPaths {
		var body []byte
		var err error
		if boundaryErr != nil {
			err = boundaryErr
		} else {
			body, err = readProjectControlFile(boundary, path)
		}
		if err != nil {
			parts = append(parts, path+"!"+err.Error())
			continue
		}
		parts = append(parts, path+"@"+SHA256Bytes(body))
	}
	for _, path := range assetPaths {
		body, err := readContainedAsset(server.AssetRoot, path)
		if err != nil {
			parts = append(parts, path+"!"+err.Error())
			continue
		}
		parts = append(parts, path+"@"+SHA256Bytes(body))
	}
	parts = append(parts,
		fmt.Sprintf("disable-dense=%t", server.DisableDense),
		fmt.Sprintf("disable-rerank=%t", server.DisableRerank),
	)
	return SHA256String(strings.Join(parts, "\x00"))
}

func (server *MCPServer) callTool(ctx context.Context, name string, body json.RawMessage) (any, error) {
	switch name {
	case "state":
		var input stateToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.State(ctx, input.StateRequest)
	case "query":
		var input queryToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.Query(ctx, FindingQueryOptions{
			Query: input.Query, CampaignID: input.CampaignID,
			AllowedSourceClasses: input.AllowedSourceClasses,
			AllowedReviewStates:  input.AllowedReviewStates,
			AllowedValidities:    input.AllowedValidities,
			Limit:                input.Limit, TokenBudget: input.TokenBudget,
			IncludeRaw: input.IncludeRaw, RequestID: input.RequestID,
		})
	case "read":
		var input readToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.ReadExact(ctx, input.ExactReadRequest)
	case "trace":
		var input traceToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.Trace(ctx, input.TraceRequest)
	case "context_pack_materialize":
		var input contextPackMaterializeToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		if err := ValidateContextPackMaterializeRequest(
			input.ContextPackMaterializeRequest,
		); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.ContextPackMaterialize(
			ctx, input.ContextPackMaterializeRequest,
		)
	case "manager_apply":
		var input managerApplyToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.ManagerApply(ctx, input.ManagerApplyRequest)
	case "curation_submit":
		var input curationSubmitToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.CurationSubmit(ctx, input.CurationSubmitRequest)
	case "closure_apply":
		var input closureApplyToolInput
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.ClosureApply(ctx, input.ClosureApplyRequest)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func decodeToolInput(body json.RawMessage, target any) error {
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := decodeStrict(body, target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

func negotiateProtocol(requested string) string {
	switch requested {
	case "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05":
		return requested
	default:
		return "2025-11-25"
	}
}

func toolDefinitions() []map[string]any {
	projectRoot := map[string]any{
		"type": "string", "maxLength": 1000,
		"description": "Canonical initialized project root. Optional only when the host supplied exactly one validated MCP root.",
	}
	readOnly := map[string]any{
		"readOnlyHint": true, "destructiveHint": false,
		"idempotentHint": true, "openWorldHint": false,
	}
	writeOnly := map[string]any{
		"readOnlyHint": false, "destructiveHint": false,
		"idempotentHint": true, "openWorldHint": false,
	}
	closureWrite := map[string]any{
		"readOnlyHint": false, "destructiveHint": true,
		"idempotentHint": true, "openWorldHint": false,
	}

	stateSchema := reflectedToolSchema(stateToolInput{})
	setSchemaProperties(stateSchema, map[string]any{
		"projectRoot":  projectRoot,
		"mode":         enumSchema("Bounded state projection to compile.", "orient", "resume", "work", "delta", "closure"),
		"campaignId":   stringSchema("Campaign ID required by resume, work, delta, and closure modes.", 0, 64),
		"workItemId":   stringSchema("Work item ID required by work mode.", 0, 64),
		"sinceEventId": stringSchema("Exclusive event cursor required by delta mode.", 0, 128),
		"tokenBudget":  integerSchemaDescription(128, 8192, "Hard maximum estimated tokens for the state view."),
		"maxCards":     integerSchemaDescription(1, 50, "Maximum number of context cards to return."),
	})

	querySchema := reflectedToolSchema(queryToolInput{})
	setSchemaProperties(querySchema, map[string]any{
		"projectRoot": projectRoot,
		"query":       stringSchema("Question or identifier to resolve to normalized finding cards.", 1, 1000),
		"campaignId":  stringSchema("Optional campaign filter.", 0, 64),
		"allowedSourceClasses": enumArraySchema("Permitted finding durability/source classes.", 5,
			"truth", "campaign", "provisional", "history", "archive"),
		"allowedReviewStates": enumArraySchema("Permitted independent review states.", 4,
			"extracted", "curator-checked", "manager-ratified", "manager-rejected"),
		"allowedValidities": enumArraySchema("Permitted independent validity states.", 6,
			"provisional", "current", "challenged", "historical", "superseded", "invalid"),
		"limit":       integerSchemaDescription(1, 5, "Maximum normalized cards before relation expansion."),
		"tokenBudget": integerSchemaDescription(128, 8192, "Hard maximum estimated tokens for cards and trace metadata."),
		"includeRaw":  map[string]any{"type": "boolean", "description": "Permit measured raw-report fallback when policy allows it."},
		"requestId":   stringSchema("Stable request identifier used only for fallback serve accounting.", 0, 128),
	})

	readSchema := reflectedToolSchema(readToolInput{})
	setSchemaProperties(readSchema, map[string]any{
		"projectRoot": projectRoot,
		"selector":    enumSchema("Kind of exact handle in value.", "record", "finding", "evidence", "report", "path", "chunk", "uri"),
		"value":       stringSchema("Exact handle, canonical path, chunk ID, or generation URI to expand.", 1, 1000),
		"campaignId":  stringSchema("Campaign required to disambiguate campaign-local handles.", 0, 64),
		"startLine":   integerSchemaDescription(1, 10000000, "Optional first source line for path, chunk, or URI reads."),
		"endLine":     integerSchemaDescription(1, 10000000, "Optional last source line for path, chunk, or URI reads."),
		"tokenBudget": integerSchemaDescription(128, 8192, "Hard maximum estimated tokens for expanded content."),
	})

	traceSchema := reflectedToolSchema(traceToolInput{})
	setSchemaProperties(traceSchema, map[string]any{
		"projectRoot": projectRoot,
		"campaignId":  stringSchema("Campaign used to resolve the starting handle.", 0, 64),
		"startHandle": stringSchema("Exact finding, record, evidence, run, or state handle.", 1, 1000),
		"relations": enumArraySchema("Relation kinds to follow; omitted means every supported relation.", 20,
			"supports", "contradicts", "depends-on", "supersedes", "duplicates", "answers", "spawned",
			"evidence", "source-run", "work-item", "parent", "child", "blocked-by", "run", "finding",
			"review", "intake", "focus", "projection"),
		"direction":   enumSchema("Traversal direction.", "outgoing", "incoming", "both"),
		"depth":       integerSchemaDescription(1, 4, "Maximum relation depth."),
		"maxNodes":    integerSchemaDescription(1, 100, "Maximum returned nodes."),
		"tokenBudget": integerSchemaDescription(128, 8192, "Hard maximum estimated tokens for the trace."),
	})

	contextPackSchema := reflectedToolSchema(contextPackMaterializeToolInput{})
	setSchemaProperties(contextPackSchema, map[string]any{
		"projectRoot": projectRoot,
		"action": enumSchema(
			"preview compiles an active-run or recruiting-run pack without writing; materialize atomically publishes only an independently verified recruiting-run preview. Active-run publication belongs to manager_apply run.prepare.",
			"preview", "materialize"),
		"target": map[string]any{
			"type": "object", "additionalProperties": false,
			"required":    []string{"kind"},
			"description": "Run authority boundary frozen into the pack. Active runs require campaignId, workItemId, and runId; recruiting runs require candidateSlug and recruitingRunId.",
			"properties": map[string]any{
				"kind":            enumSchema("Canonical run kind.", "active-run", "recruiting-run"),
				"campaignId":      stringSchema("Active campaign ID.", 0, 64),
				"workItemId":      stringSchema("Active work-item ID.", 0, 64),
				"runId":           stringSchema("Active run ID.", 0, 64),
				"candidateSlug":   stringSchema("Recruiting candidate slug.", 0, 50),
				"recruitingRunId": stringSchema("Recruiting run ID.", 0, 128),
			},
		},
		"task": stringSchema("Concrete run task the immutable pack must support.", 1, 2000),
		"role": enumSchema("Consumer role that determines default tiers and budget ceiling.", "manager", "drafter"),
		"allowedTiers": enumArraySchema("Explicit epistemic tiers; omitted uses role defaults.", 14,
			"profile", "navigation", "truth", "history", "backlog", "active", "memory", "asset",
			"campaign", "draft", "provisional", "archive", "intake", "playbook"),
		"tokenBudget": integerSchemaDescription(512, 8192, "Requested pack budget, capped by project role policy."),
		"requiredPaths": map[string]any{
			"type": "array", "uniqueItems": true, "maxItems": 20,
			"description": "Canonical sources that must contribute a bounded card and exact handle or fail compilation.",
			"items":       stringSchema("Canonical managed source path.", 1, 500),
		},
		"expectedDigest": map[string]any{
			"type": "string", "pattern": "^sha256:[0-9a-f]{64}$",
			"description": "Digest retained independently from the preview result; materialize only.",
		},
		"expectedPackId": map[string]any{
			"type": "string", "pattern": "^context-[0-9a-f]{20}$",
			"description": "Optional independently retained pack ID; materialize only.",
		},
	})

	managerSchema := reflectedToolSchema(managerApplyToolInput{})
	setSchemaProperties(managerSchema, map[string]any{
		"projectRoot": projectRoot,
		"action": enumSchema("Typed manager transition.",
			"campaign.open", "campaign.update", "work.create", "work.update", "run.prepare", "run.start",
			"run.return", "run.complete", "review.submit", "finding.challenge", "finding.update",
			"decision.record", "reconcile.import"),
		"actor":                stringSchema("Permitted manager identity recorded in the event.", 1, 128),
		"campaignSlug":         stringSchema("Canonical campaign directory slug.", 3, 50),
		"campaignId":           stringSchema("Canonical campaign ID bound by every submitted record.", 1, 64),
		"correlationId":        stringSchema("Stable transaction correlation ID.", 1, 128),
		"idempotencyKey":       stringSchema("Stable retry key for this semantic transition.", 1, 256),
		"rationale":            stringSchema("Compact manager rationale or review reference.", 0, 2000),
		"expectedHeadRevision": integerSchemaDescription(0, 2147483647, "Exact project state-head revision observed before the transition."),
		"expectedHeadDigest":   map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$", "description": "Exact project state-head digest observed before the transition."},
	})

	curationSchema := reflectedToolSchema(curationSubmitToolInput{})
	setSchemaProperties(curationSchema, map[string]any{
		"projectRoot":          projectRoot,
		"actor":                stringSchema("Curator identity recorded in the event.", 1, 128),
		"campaignSlug":         stringSchema("Canonical campaign directory slug.", 3, 50),
		"campaignId":           stringSchema("Canonical campaign ID bound by the intake and candidates.", 1, 64),
		"correlationId":        stringSchema("Stable curation transaction correlation ID.", 1, 128),
		"idempotencyKey":       stringSchema("Stable retry key for this curation submission.", 1, 256),
		"rationale":            stringSchema("Compact extraction rationale.", 0, 2000),
		"expectedHeadRevision": integerSchemaDescription(0, 2147483647, "Exact project state-head revision observed before submission."),
		"expectedHeadDigest":   map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$", "description": "Exact project state-head digest observed before submission."},
	})

	closureSchema := reflectedToolSchema(closureApplyToolInput{})
	closureSchema["required"] = []string{"action", "campaignId"}
	setSchemaProperties(closureSchema, map[string]any{
		"projectRoot":          projectRoot,
		"action":               enumSchema("Closure transition or bounded status projection.", "start", "status", "advance", "reopen", "verify", "finalize"),
		"actor":                stringSchema("Permitted manager identity for mutating actions.", 0, 128),
		"campaignSlug":         stringSchema("Canonical campaign directory slug for mutating actions.", 0, 50),
		"campaignId":           stringSchema("Canonical campaign ID.", 1, 64),
		"correlationId":        stringSchema("Stable closure transaction correlation ID for mutating actions.", 0, 128),
		"idempotencyKey":       stringSchema("Stable retry key for mutating actions.", 0, 256),
		"rationale":            stringSchema("Compact closure rationale.", 0, 2000),
		"timestamp":            stringSchema("UTC RFC3339 timestamp for mutating actions.", 0, 64),
		"expectedHeadRevision": integerSchemaDescription(0, 2147483647, "Exact state-head revision for mutating actions."),
		"expectedHeadDigest":   map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$", "description": "Exact state-head digest for mutating actions."},
	})

	return []map[string]any{
		{
			"name": "state", "title": "Compile bounded project state",
			"description": "Return an orient, resume, work, delta, or closure view from canonical campaign records, with omissions and expansion handles.",
			"inputSchema": stateSchema,
			"annotations": readOnly,
		},
		{
			"name": "query", "title": "Query normalized findings",
			"description": "Return bounded finding-first context cards after source-class, review-state, validity, and optional campaign filtering.",
			"inputSchema": querySchema,
			"annotations": readOnly,
		},
		{
			"name": "read", "title": "Expand an exact knowledge handle",
			"description": "Read one exact finding, record, evidence object, run report, canonical path, indexed chunk, or generation URI under a hard budget.",
			"inputSchema": readSchema,
			"annotations": readOnly,
		},
		{
			"name": "trace", "title": "Trace knowledge relations",
			"description": "Follow bounded evidence, dependency, challenge, supersession, run, review, intake, work, and projection edges from one exact handle.",
			"inputSchema": traceSchema,
			"annotations": readOnly,
		},
		{
			"name": "context_pack_materialize", "title": "Preview or publish scoped context pack",
			"description": "Preview a reproducible active-run or recruiting-run pack, or atomically publish an independently verified recruiting-run preview. Active-run publication belongs to manager_apply run.prepare; every destination is derived from the target.",
			"inputSchema": contextPackSchema,
			"annotations": writeOnly,
		},
		{
			"name": "manager_apply", "title": "Apply typed manager transition",
			"description": "Validate and atomically apply one typed campaign, work, run, review, finding, decision, or reconciliation transition with optimistic concurrency.",
			"inputSchema": managerSchema,
			"annotations": writeOnly,
		},
		{
			"name": "curation_submit", "title": "Submit bounded curation packet",
			"description": "Validate and atomically publish a curator intake, normalized candidate findings, coverage rows, and explicitly declared follow-up work.",
			"inputSchema": curationSchema,
			"annotations": writeOnly,
		},
		{
			"name": "closure_apply", "title": "Apply resumable closure transition",
			"description": "Return closure status or apply one explicit start, advance, verify, reopen, or finalize transition with coverage and projection gates.",
			"inputSchema": closureSchema,
			"annotations": closureWrite,
		},
	}
}

func reflectedToolSchema(value any) map[string]any {
	return reflectedJSONSchema(reflect.TypeOf(value))
}

func reflectedJSONSchema(valueType reflect.Type) map[string]any {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	switch valueType.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for index := 0; index < valueType.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "-" {
				continue
			}
			optional := false
			for _, option := range parts[1:] {
				optional = optional || option == "omitempty"
			}
			if field.Anonymous && name == "" {
				embedded := reflectedJSONSchema(field.Type)
				if embeddedProperties, ok := embedded["properties"].(map[string]any); ok {
					for key, schema := range embeddedProperties {
						properties[key] = schema
					}
				}
				if embeddedRequired, ok := embedded["required"].([]string); ok {
					required = append(required, embeddedRequired...)
				}
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = reflectedJSONSchema(field.Type)
			if !optional {
				required = append(required, name)
			}
		}
		required = SortedUnique(required)
		sort.Strings(required)
		return objectSchema(properties, required)
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": reflectedJSONSchema(valueType.Elem())}
	case reflect.Map:
		additional := any(false)
		if valueType.Key().Kind() == reflect.String {
			item := reflectedJSONSchema(valueType.Elem())
			if len(item) == 0 {
				additional = true
			} else {
				additional = item
			}
		}
		return map[string]any{"type": "object", "additionalProperties": additional}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

func setSchemaProperties(schema map[string]any, replacements map[string]any) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, replacement := range replacements {
		properties[name] = replacement
	}
}

func stringSchema(description string, minimum, maximum int) map[string]any {
	schema := map[string]any{"type": "string", "description": description}
	if minimum > 0 {
		schema["minLength"] = minimum
	}
	if maximum > 0 {
		schema["maxLength"] = maximum
	}
	return schema
}

func enumSchema(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

func enumArraySchema(description string, maximum int, values ...string) map[string]any {
	return map[string]any{
		"type": "array", "uniqueItems": true, "maxItems": maximum,
		"description": description,
		"items":       map[string]any{"type": "string", "enum": values},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

func integerSchemaDescription(minimum, maximum int, description string) map[string]any {
	schema := integerSchema(minimum, maximum)
	schema["description"] = description
	return schema
}

func writeRPCResult(encoder *json.Encoder, id json.RawMessage, result any) error {
	var decodedID any
	if len(id) > 0 {
		_ = json.Unmarshal(id, &decodedID)
	}
	return encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": decodedID, "result": result})
}

func writeRPCError(
	encoder *json.Encoder,
	id json.RawMessage,
	code int,
	message string,
	data any,
) error {
	var decodedID any
	if len(id) > 0 {
		_ = json.Unmarshal(id, &decodedID)
	}
	body := map[string]any{"code": code, "message": message}
	if data != nil {
		body["data"] = data
	}
	return encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": decodedID, "error": body})
}

func writeToolResult(encoder *json.Encoder, id json.RawMessage, value any) error {
	object, err := toObject(value)
	if err != nil {
		return writeToolError(encoder, id, err)
	}
	body, _ := json.Marshal(object)
	text := string(body)
	if len(body) > 1024 {
		text = compactCompatibilityText(object)
	}
	return writeRPCResult(encoder, id, map[string]any{
		"content":           []map[string]any{{"type": "text", "text": text}},
		"structuredContent": object, "isError": false,
	})
}

func compactCompatibilityText(object map[string]any) string {
	summary := map[string]any{
		"message": "Full typed result is available in structuredContent.",
	}
	for _, key := range []string{
		"schemaVersion", "id", "packId", "digest", "query", "role",
		"generation", "effectiveProfile", "requestedProfile", "estimatedTokens",
	} {
		if value, ok := object[key]; ok {
			summary[key] = value
		}
	}
	for _, key := range []string{"results", "passages", "restored", "profiles"} {
		if value, ok := object[key].([]any); ok {
			summary[key+"Count"] = len(value)
		}
	}
	body, err := json.Marshal(summary)
	if err != nil || len(body) > 1024 {
		return `{"message":"Full typed result is available in structuredContent."}`
	}
	return string(body)
}

func writeToolError(encoder *json.Encoder, id json.RawMessage, err error) error {
	return writeRPCResult(encoder, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": err.Error()}},
		"isError": true,
	})
}

func toObject(value any) (map[string]any, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, errors.New("tool result must be a JSON object")
	}
	return object, nil
}
