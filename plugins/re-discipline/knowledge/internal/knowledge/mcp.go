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
	"runtime"
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
				"instructions": "Project-owned, tier-aware retrieval. Pending memory proposals never participate in normal retrieval. Pass projectRoot when the host provides no MCP roots.",
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
		return "", errors.New("project root lacks supported re-discipline shared-laws v0.7.0 marker")
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
	case "status":
		var input struct {
			ProjectRoot string `json:"projectRoot"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.Status(ctx)
	case "orient":
		var input struct {
			ProjectRoot     string `json:"projectRoot"`
			Role            string `json:"role"`
			TokenBudget     int    `json:"tokenBudget"`
			Verbosity       string `json:"verbosity"`
			SinceGeneration string `json:"sinceGeneration"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		pack, err := service.OrientVerbosity(
			ctx, input.Role, input.TokenBudget, input.Verbosity)
		if err != nil {
			return nil, err
		}
		if input.SinceGeneration == "" {
			return pack, nil
		}
		// The delta rides beside the pack, never inside it. A pack's digest
		// covers a citable artifact reproducible from its generation; what one
		// particular caller last saw is neither citable nor reproducible, and
		// folding it in would give two callers asking the same question two
		// different pack identities over identical evidence.
		object, err := toObject(pack)
		if err != nil {
			return nil, err
		}
		object["delta"] = service.GenerationDeltaSince(ctx, input.SinceGeneration)
		return object, nil
	case "search":
		var input struct {
			ProjectRoot  string   `json:"projectRoot"`
			Query        string   `json:"query"`
			QueryClass   string   `json:"queryClass"`
			AllowedTiers []string `json:"allowedTiers"`
			Limit        int      `json:"limit"`
			TokenBudget  int      `json:"tokenBudget"`
			Verbosity    string   `json:"verbosity"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		if input.Limit == 0 {
			input.Limit = service.Configuration.Settings.Budgets.MaxPassages
		}
		if input.TokenBudget == 0 {
			input.TokenBudget = service.Configuration.Settings.Budgets.SearchTokens
		}
		if len(input.AllowedTiers) == 0 {
			input.AllowedTiers = []string{"profile", "navigation", "truth", "memory"}
		}
		return service.Search(ctx, SearchOptions{
			Query: input.Query, QueryClass: input.QueryClass,
			AllowedTiers: input.AllowedTiers, Limit: input.Limit,
			TokenBudget: input.TokenBudget, Verbosity: input.Verbosity,
		})
	case "read":
		var input struct {
			ProjectRoot string `json:"projectRoot"`
			Path        string `json:"path"`
			ChunkID     string `json:"chunkId"`
			URI         string `json:"uri"`
			StartLine   int    `json:"startLine"`
			EndLine     int    `json:"endLine"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.Read(ctx, ReadOptions{
			Path: input.Path, ChunkID: input.ChunkID, URI: input.URI,
			StartLine: input.StartLine, EndLine: input.EndLine,
		})
	case "context_pack":
		var input struct {
			ProjectRoot   string   `json:"projectRoot"`
			Task          string   `json:"task"`
			Role          string   `json:"role"`
			AllowedTiers  []string `json:"allowedTiers"`
			TokenBudget   int      `json:"tokenBudget"`
			RequiredPaths []string `json:"requiredPaths"`
			Verbosity     string   `json:"verbosity"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.ContextPackOptions(ctx, ContextPackRequest{
			Task: input.Task, Role: input.Role, Tiers: input.AllowedTiers,
			TokenBudget: input.TokenBudget, RequiredPaths: input.RequiredPaths,
			Verbosity: input.Verbosity,
		})
	case "context_pack_materialize":
		var input struct {
			ProjectRoot    string   `json:"projectRoot"`
			Task           string   `json:"task"`
			Role           string   `json:"role"`
			AllowedTiers   []string `json:"allowedTiers"`
			TokenBudget    int      `json:"tokenBudget"`
			RequiredPaths  []string `json:"requiredPaths"`
			Verbosity      string   `json:"verbosity"`
			Path           string   `json:"path"`
			ExpectedDigest string   `json:"expectedDigest"`
			ExpectedPackID string   `json:"expectedPackId"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		pack, err := service.ContextPackOptions(ctx, ContextPackRequest{
			Task: input.Task, Role: input.Role, Tiers: input.AllowedTiers,
			TokenBudget: input.TokenBudget, RequiredPaths: input.RequiredPaths,
			Verbosity: input.Verbosity,
		})
		if err != nil {
			return nil, err
		}
		if err := service.MaterializeContextPackExpected(
			input.Path, pack, input.ExpectedDigest, input.ExpectedPackID,
		); err != nil {
			return nil, err
		}
		return map[string]any{
			"path": input.Path, "packId": pack.PackID, "digest": pack.Digest,
			"materialized": true,
		}, nil
	case "recall_propose":
		var input struct {
			ProjectRoot string   `json:"projectRoot"`
			Title       string   `json:"title"`
			Content     string   `json:"content"`
			SourceLinks []string `json:"sourceLinks"`
		}
		if err := decodeToolInput(body, &input); err != nil {
			return nil, err
		}
		service, err := server.service(input.ProjectRoot)
		if err != nil {
			return nil, err
		}
		return service.RecallPropose(ctx, input.Title, input.Content, input.SourceLinks)
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
		"type":        "string",
		"description": "Canonical initialized project root. Optional only when the host supplied exactly one validated MCP root.",
	}
	tiers := map[string]any{
		"type": "array", "uniqueItems": true,
		"items": map[string]any{
			"type": "string",
			"enum": []string{"profile", "navigation", "truth", "history", "backlog", "active", "memory", "asset"},
		},
	}
	readOnly := map[string]any{
		"readOnlyHint": true, "destructiveHint": false,
		"idempotentHint": true, "openWorldHint": false,
	}
	verbosity := map[string]any{
		"type": "string", "enum": []string{"compact", "verbose"},
		"default": "compact",
		"description": "compact (default) omits re-derivable citation and provenance metadata " +
			"so the token budget is spent on evidence; verbose carries the document hash, " +
			"the generation URI, the prelude hash and the full retrieval provenance.",
	}
	requiredPaths := map[string]any{
		"type": "array", "uniqueItems": true, "maxItems": 20,
		"items": map[string]any{
			"type": "string", "minLength": 1, "maxLength": 500,
			"description": "Exact managed source path whose best-matching indexed passage " +
				"must be present in the pack, or the call fails. The document is served as " +
				"the one chunk selected by the same ranking as ordinary retrieval, so a " +
				"source larger than the budget can still be pinned; read the path for the rest.",
		},
	}
	return []map[string]any{
		{
			"name": "status", "title": "Knowledge status",
			"description": "Validate configuration, index freshness, model checksums, and requested/effective retrieval profiles.",
			"inputSchema": objectSchema(map[string]any{"projectRoot": projectRoot}, nil),
			"annotations": readOnly,
		},
		{
			"name": "orient", "title": "Bounded project orientation",
			"description": "Compile a small tier-labeled orientation pack from the canonical profile, indexes, current truth, active campaign, and accepted memory.",
			"inputSchema": objectSchema(map[string]any{
				"projectRoot": projectRoot,
				"role":        map[string]any{"type": "string", "enum": []string{"manager", "drafter"}, "default": "manager"},
				"tokenBudget": integerSchema(512, 8192),
				"verbosity":   verbosity,
				"sinceGeneration": map[string]any{
					"type": "string", "pattern": "^generation-[0-9a-f]{20}$",
					"description": "A generation ID this caller has already seen. The response then " +
						"carries a sibling `delta` object listing the documents added, changed, and " +
						"removed since then, with their tiers. The delta sits beside the pack and is " +
						"not part of its digest. When the recorded history does not reach back that " +
						"far the delta reports itself unavailable rather than implying nothing changed.",
				},
			}, nil),
			"annotations": readOnly,
		},
		{
			"name": "search", "title": "Tier-aware knowledge search",
			"description": "Run deterministic exact, FTS5, graph, local dense, and optional reranking lanes after hard epistemic-tier filtering.",
			"inputSchema": objectSchema(map[string]any{
				"projectRoot":  projectRoot,
				"query":        map[string]any{"type": "string", "minLength": 1, "maxLength": 1000},
				"queryClass":   map[string]any{"type": "string", "enum": []string{"auto", "exact", "conceptual", "orientation", "current", "provenance", "dependency", "contradiction"}, "default": "auto"},
				"allowedTiers": tiers, "limit": integerSchema(1, 50),
				"tokenBudget": integerSchema(128, 4096),
				"verbosity":   verbosity,
			}, []string{"query"}),
			"annotations": readOnly,
		},
		{
			"name": "read", "title": "Read managed knowledge",
			"description": "Read one indexed source, chunk, or generation-scoped URI after canonical path and content-hash validation.",
			"inputSchema": readInputSchema(map[string]any{
				"projectRoot": projectRoot,
				"path":        map[string]any{"type": "string"}, "chunkId": map[string]any{"type": "string"},
				"uri": map[string]any{"type": "string"}, "startLine": integerSchema(1, 10000000),
				"endLine": integerSchema(1, 10000000),
			}),
			"annotations": readOnly,
		},
		{
			"name": "context_pack", "title": "Compile immutable context pack",
			"description": "Build a reproducible, citable, hard-budgeted manager or drafter capsule. This tool does not write files.",
			"inputSchema": objectSchema(map[string]any{
				"projectRoot":  projectRoot,
				"task":         map[string]any{"type": "string", "minLength": 1, "maxLength": 2000},
				"role":         map[string]any{"type": "string", "enum": []string{"manager", "drafter"}},
				"allowedTiers": tiers, "tokenBudget": integerSchema(512, 8192),
				"requiredPaths": requiredPaths, "verbosity": verbosity,
			}, []string{"task", "role"}),
			"annotations": readOnly,
		},
		{
			"name": "context_pack_materialize", "title": "Materialize immutable context pack",
			"description": "Build and atomically publish a reproducible context pack into an approved drafter workspace path.",
			"inputSchema": objectSchema(map[string]any{
				"projectRoot":  projectRoot,
				"task":         map[string]any{"type": "string", "minLength": 1, "maxLength": 2000},
				"role":         map[string]any{"type": "string", "enum": []string{"manager", "drafter"}},
				"allowedTiers": tiers, "tokenBudget": integerSchema(512, 8192),
				"requiredPaths": requiredPaths, "verbosity": verbosity,
				"path": map[string]any{
					"type": "string", "minLength": 1, "maxLength": 500,
					"description": "Approved active/.../subagents/.../context-pack.json or recruiting run path.",
				},
				"expectedDigest": map[string]any{
					"type": "string", "pattern": "^sha256:[0-9a-f]{64}$",
					"description": "Digest retained independently from the prior read-only context_pack response.",
				},
				"expectedPackId": map[string]any{
					"type": "string", "pattern": "^context-[0-9a-f]{20}$",
					"description": "Optional pack ID retained independently from the prior read-only context_pack response.",
				},
			}, []string{"task", "role", "path", "expectedDigest"}),
			"annotations": map[string]any{
				"readOnlyHint": false, "destructiveHint": false,
				"idempotentHint": true, "openWorldHint": false,
			},
		},
		{
			"name": "recall_propose", "title": "Propose shared recall",
			"description": "Create only a tracked pending proposal under .re-discipline/memory/proposals. It cannot accept memory or promote truth.",
			"inputSchema": objectSchema(map[string]any{
				"projectRoot": projectRoot,
				"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				"content":     map[string]any{"type": "string", "minLength": 1, "maxLength": 8000},
				"sourceLinks": map[string]any{"type": "array", "maxItems": 20, "items": map[string]any{"type": "string"}},
			}, []string{"title", "content"}),
			"annotations": map[string]any{
				"readOnlyHint": false, "destructiveHint": false,
				"idempotentHint": true, "openWorldHint": false,
			},
		},
	}
}

func readInputSchema(properties map[string]any) map[string]any {
	schema := objectSchema(properties, nil)
	schema["oneOf"] = []map[string]any{
		{"required": []string{"path"}},
		{"required": []string{"chunkId"}},
		{"required": []string{"uri"}},
	}
	return schema
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
