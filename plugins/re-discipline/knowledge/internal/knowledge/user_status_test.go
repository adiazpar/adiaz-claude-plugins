package knowledge

import (
	"strings"
	"testing"
)

func TestBuildUserStatusTranslatesMachineryToPlainLanguage(t *testing.T) {
	healthy := map[string]any{
		"configuration":          map[string]any{"valid": true, "memoryMode": "shared-only"},
		"index":                  map[string]any{"present": true, "integrity": true, "fresh": false},
		"requestedProfile":       "plugin:balanced-v1",
		"effectiveProfile":       "hybrid-local-v1@abc",
		"fallbackReason":         nil,
		"benchmark":              map[string]any{"staleActionable": false},
		"pins":                   EvidencePinHealth{Total: 3, Intact: 3},
		"memoryProposalsPending": 9,
	}
	user := BuildUserStatus(healthy)
	if user.Knowledge != "working" {
		t.Fatalf("stale-but-intact index is user-working (it self-heals); got %q", user.Knowledge)
	}
	if user.Memory != "shared-only; 9 proposals awaiting review" {
		t.Fatalf("memory line wrong: %q", user.Memory)
	}
	if len(user.Attention) != 0 {
		t.Fatalf("healthy project must have no attention items: %v", user.Attention)
	}
	banned := []string{"generation", "lane", "rrf", "effective profile", "fingerprint",
		"evidence pin", "fresh", "stale", "fallback", "rerank", "chunker"}
	for _, s := range append([]string{user.Knowledge, user.Memory}, user.Attention...) {
		lower := strings.ToLower(s)
		for _, b := range banned {
			if strings.Contains(lower, b) {
				t.Fatalf("banned term %q leaked into user block: %q", b, s)
			}
		}
	}
}

func TestBuildUserStatusSurfacesDecisions(t *testing.T) {
	system := map[string]any{
		"configuration":    map[string]any{"valid": true, "memoryMode": "shared-only"},
		"index":            map[string]any{"present": true, "integrity": true, "fresh": true},
		"requestedProfile": "project:candidate-12750fb43b2f9efd",
		"effectiveProfile": "hybrid-local-v1@5c79510a",
		"fallbackReason":   "candidate not approved",
		"benchmark": map[string]any{
			"staleActionable":        true,
			"actionableStaleReasons": []string{"model-fingerprint"},
		},
		"pins":                   EvidencePinHealth{Total: 4, Intact: 2, Broken: 2},
		"memoryProposalsPending": 0,
	}
	user := BuildUserStatus(system)
	// Benchmark staleness and broken pins are NOT attention items - they are
	// lazily handled by the measurement skills. Only the pending profile
	// decision needs a human.
	if len(user.Attention) != 1 {
		t.Fatalf("expected exactly 1 attention item (profile decision), got %v", user.Attention)
	}
	if !strings.Contains(strings.ToLower(user.Attention[0]), "tuned search profile") {
		t.Fatalf("missing profile-decision line in %v", user.Attention)
	}
	if user.Memory != "shared-only; no proposals waiting" {
		t.Fatalf("zero-proposal memory line wrong: %q", user.Memory)
	}
}

func TestBuildUserStatusFailsClosedInPlainWords(t *testing.T) {
	system := map[string]any{
		"configuration": map[string]any{
			"valid":      false,
			"errors":     []string{"strict decode: unknown field"},
			"memoryMode": "shared-only",
		},
	}
	user := BuildUserStatus(system)
	if !strings.HasPrefix(user.Knowledge, "needs attention") {
		t.Fatalf("invalid config must need attention: %q", user.Knowledge)
	}
	if !strings.Contains(strings.ToLower(strings.Join(user.Attention, " ")),
		"internal configuration file is damaged") {
		t.Fatalf("expected plain repair line, got %v", user.Attention)
	}
}
