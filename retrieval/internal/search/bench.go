package search

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// BenchCase is one golden regression question. Exactly one of Q
// (free-text retrieval) or Symbol (exact-name symbol lookup) is set.
// For Q cases Expect is the doc path that must appear in the top hits;
// for Symbol cases it is the symbol name that must appear among the
// returned symbols (case-insensitive), so both the exact and the
// substring-fallback lookup paths are testable.
type BenchCase struct {
	Q      string `json:"q,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	Expect string `json:"expect"`
}

// BenchReport summarizes a bench run.
type BenchReport struct {
	Total  int
	Passed int
	Misses []BenchCase
}

// RunBench answers every golden question and checks the expected doc
// appears in the top `limit` hits. Malformed lines count as misses;
// they never abort the run.
func RunBench(root, goldenPath string, limit int) (BenchReport, error) {
	f, err := os.Open(goldenPath)
	if err != nil {
		return BenchReport{}, err
	}
	defer f.Close()

	var report BenchReport
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c BenchCase
		if err := json.Unmarshal([]byte(line), &c); err != nil || c.Expect == "" ||
			(c.Q == "") == (c.Symbol == "") {
			report.Total++
			report.Misses = append(report.Misses, BenchCase{Q: line, Expect: "(malformed line)"})
			continue
		}
		report.Total++
		found := false
		if c.Symbol != "" {
			res, _, err := LookupSymbol(root, c.Symbol, limit)
			if err != nil {
				report.Misses = append(report.Misses, c)
				continue
			}
			for _, s := range res.Symbols {
				if strings.EqualFold(s.Name, c.Expect) {
					found = true
					break
				}
			}
		} else {
			hits, _, err := Query(root, c.Q, limit)
			if err != nil {
				report.Misses = append(report.Misses, c)
				continue
			}
			for _, h := range hits {
				if h.Path == c.Expect {
					found = true
					break
				}
			}
		}
		if found {
			report.Passed++
		} else {
			report.Misses = append(report.Misses, c)
		}
	}
	return report, scanner.Err()
}
