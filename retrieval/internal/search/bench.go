package search

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// BenchCase is one golden regression question.
type BenchCase struct {
	Q      string `json:"q"`
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
		if err := json.Unmarshal([]byte(line), &c); err != nil || c.Q == "" || c.Expect == "" {
			report.Total++
			report.Misses = append(report.Misses, BenchCase{Q: line, Expect: "(malformed line)"})
			continue
		}
		report.Total++
		hits, _, err := Query(root, c.Q, limit)
		if err != nil {
			report.Misses = append(report.Misses, c)
			continue
		}
		found := false
		for _, h := range hits {
			if h.Path == c.Expect {
				found = true
				break
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
