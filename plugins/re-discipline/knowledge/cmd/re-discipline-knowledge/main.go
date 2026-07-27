package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
		disableDense := flags.Bool("disable-dense", false, "select predefined no-embedding fallback")
		disableRerank := flags.Bool("disable-rerank", false, "select predefined no-reranker fallback")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		asset, err := filepath.Abs(*assetRoot)
		if err != nil {
			return err
		}
		server := &knowledge.MCPServer{
			AssetRoot: asset, InitialRoot: *projectRoot,
			DisableDense: *disableDense, DisableRerank: *disableRerank,
		}
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
			if err := printJSON(report); err != nil {
				return err
			}
			if !report.Passed {
				return fmt.Errorf("packaged benchmark gates failed")
			}
		}
		return nil
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
	}
	switch args[0] {
	case "preflight", "status", "index", "replay", "context-pack",
		"calibrate", "promote-profile":
	default:
		return usageError()
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	assetRoot := flags.String("asset-root", "knowledge", "plugin knowledge asset root")
	projectRoot := flags.String("project-root", "", "managed project root or nested path")
	cacheRoot := flags.String("cache-root", "", "optional cache root")
	disableDense := flags.Bool("disable-dense", false, "select predefined no-embedding fallback")
	disableRerank := flags.Bool("disable-rerank", false, "select predefined no-reranker fallback")
	query := flags.String("query", "", "retrieval query")
	queryClass := flags.String("query-class", "auto", "retrieval query class")
	tiers := flags.String("tiers", "profile,navigation,truth,memory", "comma-separated epistemic tiers")
	limit := flags.Int("limit", 12, "maximum passages")
	tokenBudget := flags.Int("token-budget", 1024, "hard estimated-token budget")
	role := flags.String("role", "manager", "manager or drafter")
	task := flags.String("task", "", "context-pack task")
	output := flags.String("output", "", "managed context-pack materialization path")
	expectedDigest := flags.String(
		"expected-digest", "",
		"independently retained sha256 context-pack digest",
	)
	expectedPackID := flags.String(
		"expected-pack-id", "",
		"optional independently retained context-pack ID",
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
		DisableDense: *disableDense, DisableRerank: *disableRerank,
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
			Limit: *limit, TokenBudget: *tokenBudget,
		})
		if err != nil {
			return err
		}
		return printJSON(value)
	case "context-pack":
		if strings.TrimSpace(*task) == "" {
			return fmt.Errorf("--task is required")
		}
		pack, err := service.ContextPack(ctx, *task, *role, splitCSV(*tiers), *tokenBudget)
		if err != nil {
			return err
		}
		if *output != "" {
			if *expectedDigest == "" {
				return fmt.Errorf(
					"--expected-digest is required when --output is used",
				)
			}
			if err := service.MaterializeContextPackExpected(
				*output, pack, *expectedDigest, *expectedPackID,
			); err != nil {
				return err
			}
			return printJSON(map[string]any{
				"path": *output, "packId": pack.PackID, "digest": pack.Digest,
				"materialized": true,
			})
		}
		return printJSON(pack)
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
	return fmt.Errorf("usage: re-discipline-knowledge <serve|recover|preflight|status|index|replay|context-pack|benchmark|calibrate|pin-evals|promote-profile|verify-pack> [options]")
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
