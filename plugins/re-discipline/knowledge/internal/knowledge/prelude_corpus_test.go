package knowledge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Measures the prelude against a real corpus when RE_DISCIPLINE_CORPUS points
// at one. Skipped otherwise so the suite stays hermetic.
func TestPreludeAgainstRealCorpus(t *testing.T) {
	root := os.Getenv("RE_DISCIPLINE_CORPUS")
	if root == "" {
		t.Skip("RE_DISCIPLINE_CORPUS not set")
	}
	sizes := []int{}
	titleOnly, withClaim, withVerified, withStatus, overCap := 0, 0, 0, 0, 0
	err := filepath.WalkDir(filepath.Join(root, "docs", "truth"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() ||
				!strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return walkErr
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			prelude := ExtractDocumentPrelude(string(body), path)
			rendered := prelude.Render()
			tokens := EstimateTokens(rendered)
			sizes = append(sizes, tokens)
			if prelude.Claim == "" {
				titleOnly++
			} else {
				withClaim++
			}
			if prelude.Verified != "" {
				withVerified++
			}
			if prelude.Status != "" {
				withStatus++
			}
			if len(rendered) > preludeMaxBytes {
				overCap++
				t.Errorf("prelude over cap for %s: %d bytes", path, len(rendered))
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	sort.Ints(sizes)
	pct := func(p int) int {
		if len(sizes) == 0 {
			return 0
		}
		return sizes[(len(sizes)-1)*p/100]
	}
	t.Logf("docs=%d tokens min=%d p50=%d p90=%d max=%d | withClaim=%d titleOnly=%d withVerified=%d withStatus=%d overCap=%d",
		len(sizes), pct(0), pct(50), pct(90), pct(100),
		withClaim, titleOnly, withVerified, withStatus, overCap)
}
