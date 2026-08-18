package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentSupersessionPacksReplacementBeforeObsoletePassage(t *testing.T) {
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "replacement.md"), `# Current replacement

**Claim:** The maintained contract is replacement-protocol-v2.

**Confidence:** DIRECT
`)
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "obsolete.md"), `# Obsolete contract

**Superseded-by:** docs/playbooks/replacement.md

`+strings.Repeat("header padding without the query term\n", 60)+`
## Retired detail

OBSOLETEONLYTOKEN described the retired protocol.
`)

	service := newAdversarialService(t, root, nil)
	search, err := service.Search(context.Background(), SearchOptions{
		Query: "OBSOLETEONLYTOKEN", QueryClass: "exact",
		AllowedTiers: []string{"playbook"}, Limit: 12, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	replacementIndex, obsoleteIndex := -1, -1
	for index, result := range search.Results {
		switch result.Citation.Path {
		case "docs/playbooks/replacement.md":
			if replacementIndex < 0 {
				replacementIndex = index
			}
		case "docs/playbooks/obsolete.md":
			if obsoleteIndex < 0 {
				obsoleteIndex = index
			}
			if !strings.Contains(result.DocumentContext, "status: superseded") {
				t.Fatalf("obsolete passage lost its supersession status: %#v", result)
			}
		}
	}
	if replacementIndex < 0 || obsoleteIndex < 0 {
		t.Fatalf("replacement and obsolete evidence were not paired: %#v", search.Results)
	}
	if replacementIndex >= obsoleteIndex {
		t.Fatalf("replacement must precede obsolete evidence: replacement=%d obsolete=%d",
			replacementIndex, obsoleteIndex)
	}
}

func TestDocumentSupersessionDropsObsoletePassageWhenReplacementDoesNotFit(t *testing.T) {
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "large-replacement.md"),
		"# Current replacement\n\n**Claim:** "+strings.Repeat("authoritative replacement detail ", 90)+"\n")
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "small-obsolete.md"), `# Small obsolete contract

**Superseded-by:** large-replacement.md

SMALLOBSOLETETOKEN is retired.
`)

	service := newAdversarialService(t, root, nil)
	search, err := service.Search(context.Background(), SearchOptions{
		Query: "SMALLOBSOLETETOKEN", QueryClass: "exact",
		AllowedTiers: []string{"playbook"}, Limit: 12, TokenBudget: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range search.Results {
		if result.Citation.Path == "docs/playbooks/small-obsolete.md" {
			t.Fatalf("obsolete passage escaped without its replacement: %#v", search)
		}
	}
	if search.OmittedByReason["superseded"] == 0 {
		t.Fatalf("supersession omission was not reported: %#v", search.OmittedByReason)
	}
}

func TestDocumentSupersessionDropsObsoletePassageWhenReplacementIsMissing(t *testing.T) {
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, "docs", "playbooks", "orphaned-obsolete.md"), `# Orphaned obsolete contract

**Superseded-by:** docs/playbooks/missing-replacement.md

ORPHANEDOBSOLETETOKEN is retired.
`)
	service := newAdversarialService(t, root, nil)
	search, err := service.Search(context.Background(), SearchOptions{
		Query: "ORPHANEDOBSOLETETOKEN", QueryClass: "exact",
		AllowedTiers: []string{"playbook"}, Limit: 12, TokenBudget: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range search.Results {
		if result.Citation.Path == "docs/playbooks/orphaned-obsolete.md" {
			t.Fatalf("obsolete passage escaped with an unresolved correction: %#v", search)
		}
	}
	if search.OmittedByReason["superseded"] == 0 {
		t.Fatalf("unresolved supersession omission was not reported: %#v", search.OmittedByReason)
	}
}
