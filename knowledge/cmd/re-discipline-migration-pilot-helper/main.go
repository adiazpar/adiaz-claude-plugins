package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/adiaz/re-discipline-knowledge/internal/knowledge"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: re-discipline-migration-pilot-helper <interrupt|intrinsic-gate> [options]")
	}
	switch args[0] {
	case "interrupt":
		flags := flag.NewFlagSet("interrupt", flag.ContinueOnError)
		project := flags.String("project", "", "disposable migrated project")
		transaction := flags.String("transaction", "", "active transaction id")
		phase := flags.String("phase", "backed-up", "activation phase to interrupt")
		targetIndex := flags.Int("target-index", 0, "zero-based activation target index")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *project == "" || *transaction == "" || (*phase != "backed-up" && *phase != "published") || *targetIndex < 0 {
			return errors.New("interrupt requires project, transaction, a supported phase, and non-negative target-index")
		}
		engine, err := knowledge.NewMigrationEngine(*project)
		if err != nil {
			return err
		}
		injected := false
		engine.ActivationFailpoint = func(point knowledge.MigrationActivationFailpoint) error {
			if point.Phase == *phase && point.TargetIndex == *targetIndex {
				injected = true
				return fmt.Errorf("injected disposable activation interruption after %s target %d %s", point.Phase, point.TargetIndex, point.TargetPath)
			}
			return nil
		}
		_, err = engine.Resume(*transaction, "manager", "cli")
		if err == nil {
			return errors.New("activation completed without reaching the requested interruption seam")
		}
		if !injected {
			return fmt.Errorf("activation failed before requested interruption seam: %w", err)
		}
		return err
	case "intrinsic-gate":
		flags := flag.NewFlagSet("intrinsic-gate", flag.ContinueOnError)
		project := flags.String("project", "", "disposable migrated project")
		assetRoot := flags.String("asset-root", "", "snapshot plugin knowledge asset root")
		gate := flags.String("gate", "", "structural or semantic-traversal")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		artifact, err := knowledge.BuildMigrationIntrinsicGateArtifact(*project, *assetRoot, *gate)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(artifact)
	default:
		return fmt.Errorf("unknown helper action %q", args[0])
	}
}
