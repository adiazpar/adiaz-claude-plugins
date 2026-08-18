package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adiazpar/re-discipline/retrieval/internal/search"
)

func runBench(root string, limit int, jsonOut bool) error {
	golden := filepath.Join(root, ".re-discipline", "golden.jsonl")
	report, err := search.RunBench(root, golden, limit)
	if err != nil {
		return err
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(report)
	} else {
		fmt.Printf("bench: %d/%d passed\n", report.Passed, report.Total)
		for _, m := range report.Misses {
			fmt.Printf("MISS: %q expected %s\n", m.Q, m.Expect)
		}
	}
	if len(report.Misses) > 0 {
		os.Exit(1)
	}
	return nil
}
