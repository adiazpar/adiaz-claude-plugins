package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/adiaz/re-discipline-knowledge/internal/knowledge"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "re-discipline-knowledge:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
		projectRoot := flags.String("project-root", "", "optional initial managed project root")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		server := &knowledge.MCPServer{AssetRoot: asset, InitialRoot: *projectRoot}
		return server.Serve(ctx, os.Stdin, os.Stdout)
	case "verify-pack":
		flags := flag.NewFlagSet("verify-pack", flag.ContinueOnError)
		input := flags.String("input", "", "context-pack.json path")
		expectedDigest := flags.String(
			"expected-digest", "",
			"independently retained sha256 context-pack digest",
		)
		expectedPackID := flags.String(
			"expected-pack-id", "",
			"optional independently retained context-pack ID",
		)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *input == "" {
			return fmt.Errorf("--input is required")
		}
		if *expectedDigest == "" {
			return fmt.Errorf("--expected-digest is required")
		}
		result, err := knowledge.VerifyContextPack(
			*input, *expectedDigest, *expectedPackID,
		)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "benchmark":
		flags := flag.NewFlagSet("benchmark", flag.ContinueOnError)
		assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
		projectRoot := flags.String(
			"project-root", "",
			"initialized project root; omit to run packaged conformance",
		)
		cacheRoot := flags.String("cache-root", "", "optional project cache root")
		mode := flags.String("mode", "quick", "quick or full")
		updateDeclared := flags.Bool(
			"update-declared", false,
			"maintainer only: rewrite packaged declared evidence from a full packaged run",
		)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		if *projectRoot != "" {
			service, err := knowledge.NewService(knowledge.ServiceOptions{
				ProjectRoot: *projectRoot, AssetRoot: asset, CacheRoot: *cacheRoot,
			})
			if err != nil {
				return err
			}
			report, err := service.RunProjectBenchmark(ctx, *mode)
			if err != nil {
				return err
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if !report.Passed {
				return fmt.Errorf("project benchmark gates failed")
			}
		} else {
			report, err := knowledge.RunPackagedBenchmark(ctx, asset, *mode)
			if err != nil {
				return err
			}
			if *updateDeclared {
				if err := knowledge.UpdateDeclaredBenchmarks(asset, report); err != nil {
					return err
				}
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if !report.Passed && !*updateDeclared {
				return fmt.Errorf("packaged benchmark gates failed")
			}
		}
		return nil
	case "normalized-vs-raw":
		flags := flag.NewFlagSet("normalized-vs-raw", flag.ContinueOnError)
		assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
		projectRoot := flags.String("project-root", "", "initialized project root")
		cacheRoot := flags.String("cache-root", "", "optional project cache root")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*projectRoot) == "" {
			return errors.New("normalized-vs-raw requires --project-root")
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		service, err := knowledge.NewService(knowledge.ServiceOptions{
			ProjectRoot: *projectRoot, AssetRoot: asset, CacheRoot: *cacheRoot,
		})
		if err != nil {
			return err
		}
		result, err := service.RunNormalizedRawGate(ctx)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "pin-evals":
		flags := flag.NewFlagSet("pin-evals", flag.ContinueOnError)
		assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
		projectRoot := flags.String("project-root", "", "initialized project root")
		cacheRoot := flags.String("cache-root", "", "optional project cache root")
		apply := flags.Bool("apply", false, "write refreshed pins back to the case files")
		force := flags.Bool(
			"force", false,
			"re-stamp pins whose claim changed; re-answer each case first",
		)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *projectRoot == "" {
			return fmt.Errorf("pin-evals requires --project-root")
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		service, err := knowledge.NewService(knowledge.ServiceOptions{
			ProjectRoot: *projectRoot, AssetRoot: asset, CacheRoot: *cacheRoot,
		})
		if err != nil {
			return err
		}
		report, err := service.PinEvalCases(*apply, *force)
		if err != nil {
			return err
		}
		if err := printJSON(report); err != nil {
			return err
		}
		if len(report.ClaimChanged) > 0 && !*force {
			return fmt.Errorf(
				"%d pinned document(s) changed what they claim; re-answer each case before re-stamping",
				len(report.ClaimChanged))
		}
		if report.Vocabulary.FailedCases > 0 {
			return fmt.Errorf(
				"%d evaluation case(s) violate their target-vocabulary disjointness policy",
				report.Vocabulary.FailedCases,
			)
		}
		return nil
	case "recover":
		flags := flag.NewFlagSet("recover", flag.ContinueOnError)
		projectRoot := flags.String("project-root", "", "managed project root or nested path")
		pluginRoot := flags.String("plugin-root", ".", "re-discipline plugin root")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		root := *projectRoot
		if root == "" {
			var err error
			root, err = knowledge.FindProjectRoot(".")
			if err != nil {
				return err
			}
		}
		result, err := knowledge.RecoverProject(root, *pluginRoot)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "ensure":
		flags := flag.NewFlagSet("ensure", flag.ContinueOnError)
		assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
		projectRoot := flags.String("project-root", "", "managed project root or nested path")
		budgetMs := flags.Int("budget-ms", 7000, "cheap-repair time budget in milliseconds")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		root := *projectRoot
		if root == "" {
			root, err = knowledge.FindProjectRoot(".")
			if err != nil {
				return err
			}
		}
		// EnsureProject owns its own recover/migrate sequencing; it must not
		// go through the generic pre-command recovery gate below.
		payload, err := knowledge.EnsureProject(ctx, root, filepath.Dir(asset), *budgetMs)
		if err != nil {
			return err
		}
		return printJSON(payload)
	case "project-version":
		flags := flag.NewFlagSet("project-version", flag.ContinueOnError)
		projectRoot := flags.String("project-root", "", "managed project root or nested path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		root := strings.TrimSpace(*projectRoot)
		if root == "" {
			var err error
			root, err = knowledge.FindProjectRoot(".")
			if err != nil {
				return fmt.Errorf("--project-root is required outside an initialized project: %w", err)
			}
		}
		version, err := knowledge.DetectProjectStateVersion(root)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"schemaVersion":       1,
			"projectStateVersion": version,
			"migrationRequired":   version != "0.8",
		})
	case "migrate-project":
		return runMigrationCommand(ctx, args[1:])
	case "state", "query", "read", "trace", "context-pack-materialize",
		"manager-apply", "curation-submit", "closure-apply", "normalization-queue":
		return runPeerCommand(ctx, args[0], args[1:])
	}
	switch args[0] {
	case "preflight", "status", "index", "replay",
		"calibrate", "promote-profile":
	default:
		return usageError()
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
	projectRoot := flags.String("project-root", "", "managed project root or nested path")
	cacheRoot := flags.String("cache-root", "", "optional cache root")
	query := flags.String("query", "", "retrieval query")
	queryClass := flags.String("query-class", "auto", "retrieval query class")
	tiers := flags.String("tiers", "profile,navigation,truth,memory", "comma-separated epistemic tiers")
	limit := flags.Int("limit", 12, "maximum passages")
	tokenBudget := flags.Int("token-budget", 1024, "hard estimated-token budget")
	verbosity := flags.String(
		"verbosity", "compact",
		"compact omits re-derivable citation and provenance metadata; verbose keeps it",
	)
	candidate := flags.String("candidate", "", "calibration candidate-profile.json path")
	report := flags.String("report", "", "calibration report.json path")
	approve := flags.Bool("approve", false, "confirm explicit user approval for profile promotion")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	root := *projectRoot
	if root == "" {
		var err error
		root, err = knowledge.FindProjectRoot(".")
		if err != nil {
			return fmt.Errorf("--project-root is required outside an initialized project: %w", err)
		}
	}
	asset, err := filepath.Abs(*assetRoot)
	if err != nil {
		return err
	}
	if args[0] != "status" {
		if _, err := knowledge.RecoverProject(root, filepath.Dir(asset)); err != nil {
			return err
		}
	}
	service, err := knowledge.NewService(knowledge.ServiceOptions{
		ProjectRoot: root, AssetRoot: asset, CacheRoot: *cacheRoot,
	})
	if err != nil {
		return err
	}
	switch args[0] {
	case "preflight", "status":
		value, err := service.Status(ctx)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "index":
		value, err := service.ReconcileIndex(ctx)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "replay":
		if strings.TrimSpace(*query) == "" {
			return fmt.Errorf("--query is required")
		}
		value, err := service.DeterministicReplay(ctx, knowledge.SearchOptions{
			Query: *query, QueryClass: *queryClass, AllowedTiers: splitCSV(*tiers),
			Limit: *limit, TokenBudget: *tokenBudget, Verbosity: *verbosity,
		})
		if err != nil {
			return err
		}
		return printJSON(value)
	case "calibrate":
		report, err := service.Calibrate(ctx)
		if err != nil {
			return err
		}
		return printJSON(report)
	case "promote-profile":
		if *candidate == "" || *report == "" {
			return fmt.Errorf("--candidate and --report are required")
		}
		result, err := service.PromoteProfile(ctx, *candidate, *report, *approve)
		if err != nil {
			return err
		}
		return printJSON(result)
	default:
		return usageError()
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func usageError() error {
	return fmt.Errorf("usage: re-discipline-knowledge <serve|state|query|read|trace|context-pack-materialize|manager-apply|curation-submit|closure-apply|normalization-queue|recover|ensure|project-version|preflight|status|index|replay|benchmark|normalized-vs-raw|calibrate|pin-evals|promote-profile|verify-pack|migrate-project> [options]")
}

func runPeerCommand(ctx context.Context, command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
	projectRoot := flags.String("project-root", "", "managed project root or nested path")
	cacheRoot := flags.String("cache-root", "", "optional cache root")
	input := flags.String("input", "", "strict JSON request path, or - for stdin")
	session := flags.Bool(
		"session", false,
		"query only: process a stream of strict JSON requests in one lease-preserving process",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	if *session && command != "query" {
		return fmt.Errorf("--session is supported only by query")
	}
	var contextPackRequest *knowledge.ContextPackMaterializeRequest
	if command == "context-pack-materialize" {
		var request knowledge.ContextPackMaterializeRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		if err := knowledge.ValidateContextPackMaterializeRequest(request); err != nil {
			return err
		}
		contextPackRequest = &request
	}
	root := strings.TrimSpace(*projectRoot)
	if root == "" {
		var err error
		root, err = knowledge.FindProjectRoot(".")
		if err != nil {
			return fmt.Errorf("--project-root is required outside an initialized project: %w", err)
		}
	}
	asset, err := filepath.Abs(*assetRoot)
	if err != nil {
		return err
	}
	service, err := knowledge.NewService(knowledge.ServiceOptions{
		ProjectRoot: root, AssetRoot: asset, CacheRoot: *cacheRoot,
	})
	if err != nil {
		return err
	}
	switch command {
	case "state":
		var request knowledge.StateRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.State(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "query":
		if *session {
			return runQuerySessionInput(ctx, *input, os.Stdout, service.Query)
		}
		var request knowledge.FindingQueryOptions
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.Query(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "read":
		var request knowledge.ExactReadRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.ReadExact(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "trace":
		var request knowledge.TraceRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.Trace(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "context-pack-materialize":
		value, err := service.ContextPackMaterialize(ctx, *contextPackRequest)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "manager-apply":
		var request knowledge.ManagerApplyRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.ManagerApply(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "curation-submit":
		var request knowledge.CurationSubmitRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.CurationSubmit(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "closure-apply":
		var request knowledge.ClosureApplyRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.ClosureApply(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	case "normalization-queue":
		var request knowledge.NormalizationQueueRequest
		if err := decodeCLIRequest(*input, &request); err != nil {
			return err
		}
		value, err := service.NormalizationQueueApply(ctx, request)
		if err != nil {
			return err
		}
		return printJSON(value)
	default:
		return usageError()
	}
}

func runQuerySessionInput(
	ctx context.Context,
	input string,
	output io.Writer,
	query func(context.Context, knowledge.FindingQueryOptions) (knowledge.FindingQueryResponse, error),
) error {
	var reader io.Reader
	var closeInput func() error
	if input == "-" {
		reader = os.Stdin
		closeInput = func() error { return nil }
	} else {
		file, err := os.Open(input)
		if err != nil {
			return err
		}
		reader = file
		closeInput = file.Close
	}
	defer closeInput()
	return runQuerySession(ctx, reader, output, query)
}

func runQuerySession(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	query func(context.Context, knowledge.FindingQueryOptions) (knowledge.FindingQueryResponse, error),
) error {
	const maxSessionBytes = 16 << 20
	limited := &io.LimitedReader{R: input, N: maxSessionBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	encoder := json.NewEncoder(output)
	requests := 0
	for {
		var request knowledge.FindingQueryOptions
		if err := decoder.Decode(&request); err != nil {
			if err == io.EOF {
				if limited.N == 0 {
					return fmt.Errorf("query session exceeds %d bytes", maxSessionBytes)
				}
				break
			}
			return fmt.Errorf("decode query session request %d: %w", requests+1, err)
		}
		response, err := query(ctx, request)
		if err != nil {
			return fmt.Errorf("query session request %d: %w", requests+1, err)
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
		if flusher, ok := output.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				return err
			}
		}
		requests++
	}
	if requests == 0 {
		return errors.New("query session requires at least one request")
	}
	return nil
}

func decodeCLIRequest(input string, target any) error {
	const maxRequestBytes = 4 << 20
	var reader io.Reader
	if input == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(input)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	limited := io.LimitReader(reader, maxRequestBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > maxRequestBytes {
		return fmt.Errorf("request exceeds %d bytes", maxRequestBytes)
	}
	body, err = normalizeCLIJSONEncoding(body)
	if err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("decode request: trailing JSON value")
	}
	return nil
}

func splitCSV(value string) []string {
	out := []string{}
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func runMigrationCommand(ctx context.Context, args []string) error {
	_ = ctx // Migration operations are bounded filesystem transactions today.
	flags := flag.NewFlagSet("migrate-project", flag.ContinueOnError)
	assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
	project := flags.String("project", "", "managed 0.7 project root")
	projectRoot := flags.String("project-root", "", "managed 0.7 project root")
	previewMode := flags.Bool("preview", false, "build a read-only migration preview")
	applyDigest := flags.String("apply", "", "exact approved preview plan digest")
	resumeID := flags.String("resume", "", "migration transaction ID to resume")
	statusMode := flags.Bool("status", false, "show migration state")
	verifyMode := flags.Bool("verify", false, "build a read-only candidate certification receipt")
	ratifyDigest := flags.String("ratify", "", "exact candidate certification digest to ratify")
	coveragePath := flags.String("coverage", "", "coverage receipt JSON to submit")
	gate := flags.String("gate", "", "certification gate to record")
	gatePassed := flags.Bool("gate-passed", false, "record the named gate as passed")
	artifact := flags.String("artifact", "", "project-relative gate artifact; retrieval and host gates require strict gate-specific evidence")
	artifactDigest := flags.String("artifact-digest", "", "exact sha256 digest rederived from the gate artifact bytes")
	output := flags.String("output", "", "external preview output directory")
	live := flags.String("live-campaigns", "", "comma-separated manager-designated live campaign slugs")
	actor := flags.String("actor", "manager", "manager identity recorded in receipts")
	reviewer := flags.String("reviewer", "manager", "reviewer identity for gate receipts")
	shadowQuery := flags.String("shadow-query", "", "query digest-pinned legacy report provenance")
	shadowCampaign := flags.String("shadow-campaign", "", "optional campaign filter for shadow query")
	shadowLimit := flags.Int("shadow-limit", 5, "maximum shadow query results")
	profileConflict := flags.Bool("profile-conflict", false, "export the digest-bound unsupported legacy retrieval-profile conflict packet")
	profileDecision := flags.String("profile-decision", "", "submit an explicit non-activating manager retrieval-profile conversion decision from strict JSON")
	replaceProfileDecision := flags.String("replace-profile-decision", "", "exact prior decision digest required to replace a submitted profile decision")
	truthConflicts := flags.Bool("truth-conflicts", false, "export the digest-bound legacy truth atomicization conflict packet")
	truthReview := flags.String("truth-review", "", "submit a manager-reviewed truth atomicization/split mapping from strict JSON")
	replaceTruthReview := flags.String("replace-truth-review", "", "exact prior review digest required to replace a submitted truth review")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root := strings.TrimSpace(*project)
	if root == "" {
		root = strings.TrimSpace(*projectRoot)
	}
	if root == "" {
		var err error
		root, err = knowledge.FindProjectRoot(".")
		if err != nil {
			return fmt.Errorf("--project is required outside an initialized project: %w", err)
		}
	}
	actions := 0
	for _, selected := range []bool{
		*previewMode, *applyDigest != "", *resumeID != "", *statusMode,
		*verifyMode, *ratifyDigest != "", *coveragePath != "", *gate != "",
		*shadowQuery != "", *profileConflict, *profileDecision != "", *truthConflicts, *truthReview != "",
	} {
		if selected {
			actions++
		}
	}
	if actions != 1 {
		return fmt.Errorf("migrate-project requires exactly one of --preview, --apply, --resume, --status, --verify, --ratify, --coverage, --gate, --shadow-query, --profile-conflict, --profile-decision, --truth-conflicts, or --truth-review")
	}
	liveCampaigns := splitCSV(*live)
	if *profileConflict {
		value, err := knowledge.ExportMigrationProfileConflict(root)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *profileDecision != "" {
		var submission knowledge.MigrationProfileDecisionSubmission
		if err := decodeCLIRequest(*profileDecision, &submission); err != nil {
			return err
		}
		value, err := knowledge.SubmitMigrationProfileDecision(root, submission, *replaceProfileDecision)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *truthConflicts {
		value, err := knowledge.ExportMigrationTruthConflicts(root)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *truthReview != "" {
		var submission knowledge.MigrationTruthReviewSubmission
		if err := decodeCLIRequest(*truthReview, &submission); err != nil {
			return err
		}
		value, err := knowledge.SubmitMigrationTruthReview(root, submission, *replaceTruthReview)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *previewMode {
		value, err := knowledge.PreviewMigration(root, liveCampaigns)
		if err != nil {
			return err
		}
		if *output != "" {
			if err := knowledge.WriteMigrationPreview(root, *output, value); err != nil {
				return err
			}
		}
		return printJSON(value)
	}
	asset, err := filepath.Abs(*assetRoot)
	if err != nil {
		return err
	}
	engine, err := knowledge.NewMigrationEngineWithAssetRoot(root, asset)
	if err != nil {
		return err
	}
	if *applyDigest != "" {
		value, err := knowledge.PreviewMigration(root, liveCampaigns)
		if err != nil {
			return err
		}
		state, err := engine.Start(value.Plan, *applyDigest, *actor, "cli")
		if err != nil {
			return err
		}
		return printJSON(state)
	}
	if *resumeID != "" {
		state, err := engine.Resume(*resumeID, *actor, "cli")
		if err != nil {
			return err
		}
		return printJSON(state)
	}
	if *statusMode {
		state, err := engine.Status()
		if os.IsNotExist(err) {
			return printJSON(map[string]any{
				"schemaVersion": 1, "state": "legacy",
				"safeNextAction": "run migrate-project --preview; no mutation occurred",
			})
		}
		if err != nil {
			return err
		}
		return printJSON(state)
	}
	if *verifyMode {
		value, err := engine.Verify()
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *coveragePath != "" {
		var receipt knowledge.MigrationCoverageReceipt
		if err := decodeCLIRequest(*coveragePath, &receipt); err != nil {
			return err
		}
		value, err := engine.SubmitCoverage(receipt)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *gate != "" {
		value, err := engine.RecordGate(knowledge.MigrationGateReceipt{
			Gate: *gate, Passed: *gatePassed, Artifact: *artifact,
			ArtifactDigest: *artifactDigest, Reviewer: *reviewer,
		})
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	if *shadowQuery != "" {
		value, err := engine.QueryShadow(*shadowQuery, *shadowCampaign, *shadowLimit)
		if err != nil {
			return err
		}
		return printJSON(value)
	}
	state, err := engine.Status()
	if err != nil {
		return err
	}
	state, err = engine.Ratify(state.TransactionID, *ratifyDigest, *actor, "cli")
	if err != nil {
		return err
	}
	return printJSON(state)
}
