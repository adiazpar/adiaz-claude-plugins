package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func buildLargeRequiredTruth(marker string, sections int) string {
	var body strings.Builder
	body.WriteString(`# Serialization contract

**Claim:** The durable frame commit writes its checksum before the payload.

**Confidence:** DIRECT

**Validity:**
- Verified: 2026-07-01
`)
	for index := 0; index < sections; index++ {
		fmt.Fprintf(&body, "\n## Recipe step %02d\n\n", index)
		for line := 0; line < 12; line++ {
			fmt.Fprintf(&body,
				"Step %02d line %02d records the ordinary re-derivation prose that "+
					"makes a maintained truth document far larger than a small pack.\n",
				index, line)
		}
		if index == sections-1 {
			fmt.Fprintf(&body,
				"\nThe %s handshake is only described in this final step.\n", marker)
		}
	}
	return body.String()
}

func documentTokens(t *testing.T, root, relative string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return EstimateTokens(string(body))
}

func TestRequiredEvidenceLargerThanBudgetIsServedAsCompactHandleCard(t *testing.T) {
	const budget = 1024
	const relative = "docs/playbooks/serialization-contract.md"
	const marker = "ZQDEEPMARK41"

	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
		buildLargeRequiredTruth(marker, 9))
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()
	whole := documentTokens(t, root, relative)
	if whole <= budget {
		t.Fatalf("fixture document unexpectedly fits: %d <= %d", whole, budget)
	}

	request := ContextPackRequest{
		Target: ContextPackTarget{Kind: "project"},
		Task:   "how does the " + marker + " handshake commit a frame",
		Role:   "manager", Tiers: []string{"playbook"}, TokenBudget: budget,
		RequiredPaths: []string{relative},
	}
	pack, err := service.ContextPackOptions(ctx, request)
	if err != nil {
		t.Fatalf("required evidence of %d tokens was refused: %v", whole, err)
	}
	required := findContextCardByPath(pack, relative)
	if required == nil {
		t.Fatalf("required path is absent: %#v", pack.Omitted)
	}
	if required.ExpansionTokens >= whole {
		t.Fatalf("required card did not narrow expansion: %d >= %d",
			required.ExpansionTokens, whole)
	}
	if !strings.HasPrefix(required.Handle, "re-discipline://"+pack.Generation.ID+"/chunks/") {
		t.Fatalf("required card lost its generation-scoped expansion handle: %#v", required)
	}
	expanded, err := service.Read(ctx, ReadOptions{URI: required.Handle})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded["passage"].(string), marker) {
		t.Fatalf("required-path selection ignored the task: %#v", expanded)
	}
	assertContextCardPinsSource(t, root, *required)
	assertPackAccountingIsHonest(t, pack, budget)

	repeat, err := service.ContextPackOptions(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Digest != pack.Digest || stableJSON(repeat) != stableJSON(pack) {
		t.Fatal("required handle card is not deterministic")
	}
}

func TestRequiredEvidenceFallsBackToClaimBearingOpeningCard(t *testing.T) {
	const relative = "docs/playbooks/serialization-contract.md"
	root := makeAdversarialProject(t)
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
		buildLargeRequiredTruth("ZQDEEPMARK41", 9))
	service := newAdversarialService(t, root, nil)
	pack, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{Kind: "project"},
		Task:   "ZQABSENTTOPIC77 QVXJHWMPKD", Role: "manager",
		Tiers: []string{"playbook"}, TokenBudget: 1024,
		RequiredPaths: []string{relative},
	})
	if err != nil {
		t.Fatal(err)
	}
	card := findContextCardByPath(pack, relative)
	if card == nil {
		t.Fatalf("required path %s is absent", relative)
	}
	if card.Metadata["startLine"] != "1" || !strings.Contains(card.Claim, "Claim") {
		t.Fatalf("unmatched required path did not select its opening claim card: %#v", card)
	}
	assertContextCardPinsSource(t, root, *card)
}

func TestRequiredEvidenceThatCannotFitStillFailsExplicitly(t *testing.T) {
	root := makeAdversarialProject(t)
	required := make([]string, 0, 6)
	for index := 0; index < 6; index++ {
		relative := fmt.Sprintf("docs/playbooks/bulk-%02d.md", index)
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)),
			buildLargeRequiredTruth(fmt.Sprintf("ZQBULKMARK%02d", index), 4))
		required = append(required, relative)
	}
	service := newAdversarialService(t, root, nil)
	_, err := service.ContextPackOptions(context.Background(), ContextPackRequest{
		Target: ContextPackTarget{Kind: "project"},
		Task:   "every bulk contract at once", Role: "manager",
		Tiers: []string{"playbook"}, TokenBudget: 512, RequiredPaths: required,
	})
	if err == nil || !strings.Contains(err.Error(), "mandatory") {
		t.Fatalf("unfittable required evidence must fail explicitly: %v", err)
	}
}

func TestRequiredEvidenceOutsideCorpusOrTiersIsRefused(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()
	for _, request := range []ContextPackRequest{
		{Target: ContextPackTarget{Kind: "project"}, Task: "missing evidence", Role: "manager",
			Tiers: []string{"truth"}, TokenBudget: 1024,
			RequiredPaths: []string{"docs/truth/does-not-exist.md"}},
		{Target: ContextPackTarget{Kind: "project"}, Task: "history evidence", Role: "manager",
			Tiers: []string{"truth"}, TokenBudget: 1024,
			RequiredPaths: []string{"docs/history/retired.md"}},
	} {
		if _, err := service.ContextPackOptions(ctx, request); err == nil ||
			!strings.Contains(err.Error(), "required path") {
			t.Fatalf("invalid required path was not refused: %v", err)
		}
	}
}

func findContextCardByPath(pack ContextPack, path string) *ContextCard {
	for index := range pack.Cards {
		if pack.Cards[index].Metadata["path"] == path {
			return &pack.Cards[index]
		}
	}
	return nil
}

func assertContextCardPinsSource(t *testing.T, root string, card ContextCard) {
	t.Helper()
	path := card.Metadata["path"]
	start, startErr := strconv.Atoi(card.Metadata["startLine"])
	end, endErr := strconv.Atoi(card.Metadata["endLine"])
	if path == "" || startErr != nil || endErr != nil || start < 1 || end < start {
		t.Fatalf("card lost its exact source range: %#v", card)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n"), "\n")
	if end > len(lines) {
		t.Fatalf("card source range exceeds source file: %#v", card)
	}
	passage := strings.Join(lines[start-1:end], "\n")
	if card.Metadata["passageHash"] != SHA256String(passage) {
		t.Fatalf("card passage digest does not pin its source range: %#v", card)
	}
}

func assertPackAccountingIsHonest(t *testing.T, pack ContextPack, budget int) {
	t.Helper()
	body, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	if EstimateTokens(string(body)) != pack.EstimatedTokens {
		t.Fatalf("pack token accounting is dishonest: serialized=%d claimed=%d",
			EstimateTokens(string(body)), pack.EstimatedTokens)
	}
	if pack.EstimatedTokens > budget {
		t.Fatalf("pack exceeded its budget: %d > %d", pack.EstimatedTokens, budget)
	}
	if _, err := VerifyContextPackValue(pack); err != nil {
		t.Fatalf("pack failed verification: %v", err)
	}
}
