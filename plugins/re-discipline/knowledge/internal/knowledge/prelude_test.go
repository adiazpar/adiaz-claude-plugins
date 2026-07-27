package knowledge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const preludeFixtureTruth = `# SnapHak prefab system

**Claim:** SnapHak prefabs are on-disk ` + "`.json`" + ` rawmap files under a
per-user directory. The Prefabs tab lists, creates and loads them.

**Kind:** atomic

**Confidence:** Strong *(the create/write/load chain decompiled)*

**Validity:**
- Verified: 2026-06-19
- DOOM build: see build-state.md

**Re-verify trigger:** after any rebuild.
`

func TestPreludeExtractsEpistemicHeader(t *testing.T) {
	prelude := ExtractDocumentPrelude(preludeFixtureTruth, "docs/truth/snaphak/prefab-system.md")
	if prelude.Title != "SnapHak prefab system" {
		t.Fatalf("title: %q", prelude.Title)
	}
	if !strings.HasPrefix(prelude.Claim, "SnapHak prefabs are on-disk") {
		t.Fatalf("claim: %q", prelude.Claim)
	}
	if strings.Contains(prelude.Claim, "\n") {
		t.Fatalf("claim must be a single line: %q", prelude.Claim)
	}
	// The parenthetical qualifier is dropped; the grade is what matters.
	if prelude.Confidence != "Strong" {
		t.Fatalf("confidence: %q", prelude.Confidence)
	}
	if prelude.Verified != "2026-06-19" {
		t.Fatalf("verified: %q", prelude.Verified)
	}
	if prelude.Status != "" {
		t.Fatalf("an uncorrected document must carry no status: %q", prelude.Status)
	}
}

func TestPreludeRenderIsBoundedAndValid(t *testing.T) {
	rendered := ExtractDocumentPrelude(preludeFixtureTruth, "docs/truth/snaphak/prefab-system.md").Render()
	if len(rendered) > preludeMaxBytes {
		t.Fatalf("prelude is %d bytes, cap is %d", len(rendered), preludeMaxBytes)
	}
	if !utf8.ValidString(rendered) {
		t.Fatal("prelude is not valid UTF-8")
	}
	for _, want := range []string{"title:", "claim:", "confidence:", "verified:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered prelude is missing %s: %q", want, rendered)
		}
	}
}

// Truncation must drop verified, then confidence, then shorten the claim -
// never the title or the supersession status, which are what keep a caller
// from trusting a retired document.
func TestPreludeTruncationOrderPreservesStatus(t *testing.T) {
	// Sized so title + status + correction + a capped claim exceeds the cap
	// and truncation genuinely fires.
	body := "# " + strings.Repeat("Title ", 30) + "\n\n" +
		"**Superseded-by:** docs/truth/replacement.md\n\n" +
		"**Claim:** " + strings.Repeat("long claim text ", 40) + "\n\n" +
		"**Confidence:** Strong\n\n" +
		"**Validity:**\n- Verified: 2026-06-19\n"
	rendered := ExtractDocumentPrelude(body, "docs/truth/fixture.md").Render()

	if len(rendered) > preludeMaxBytes {
		t.Fatalf("prelude is %d bytes, cap is %d", len(rendered), preludeMaxBytes)
	}
	if !utf8.ValidString(rendered) {
		t.Fatal("prelude is not valid UTF-8 after truncation")
	}
	if !strings.Contains(rendered, "status: superseded") {
		t.Fatalf("supersession status was truncated away: %q", rendered)
	}
	if strings.Contains(rendered, "verified:") {
		t.Fatalf("verified should be dropped before the claim is cut: %q", rendered)
	}
}

func TestPreludeStatusIsLineAnchored(t *testing.T) {
	// In-prose provenance, not a document-level retirement. Marking this
	// document superseded would teach callers to distrust a current claim.
	body := "# Engine addresses\n\n" +
		"**Claim:** The reading **Supersedes** the 2026-06-20 measurement.\n\n" +
		"**Confidence:** Strong\n"
	prelude := ExtractDocumentPrelude(body, "docs/truth/fixture.md")
	if prelude.Status != "" {
		t.Fatalf("in-prose 'Supersedes' must not set status, got %q", prelude.Status)
	}
}

func TestPreludeDegradesToTitleOnly(t *testing.T) {
	// History, backlog and active documents carry no Claim block.
	prelude := ExtractDocumentPrelude("# 2026-07-18 webview merge\n\nSome prose.\n", "docs/history/x.md")
	rendered := prelude.Render()
	if rendered != "title: 2026-07-18 webview merge" {
		t.Fatalf("expected title-only prelude, got %q", rendered)
	}
}

func TestPreludeIsDeterministic(t *testing.T) {
	first := ExtractDocumentPrelude(preludeFixtureTruth, "docs/truth/snaphak/prefab-system.md").Render()
	for iteration := 0; iteration < 8; iteration++ {
		if got := ExtractDocumentPrelude(preludeFixtureTruth, "docs/truth/snaphak/prefab-system.md").Render(); got != first {
			t.Fatalf("prelude is not deterministic: %q vs %q", got, first)
		}
	}
}

func TestPreludeHandlesCRLFAndMultibyte(t *testing.T) {
	body := "# Título — engine\r\n\r\n" +
		"**Claim:** Café ré-entrant serialization " +
		strings.Repeat("with — wide é padding ", 30) + ".\r\n\r\n" +
		"**Confidence:** Strong\r\n"
	rendered := ExtractDocumentPrelude(body, "docs/truth/fixture.md").Render()
	if !utf8.ValidString(rendered) {
		t.Fatal("multibyte prelude is not valid UTF-8")
	}
	if strings.Contains(rendered, "\r") || strings.Contains(rendered, "\n") {
		t.Fatalf("prelude must be a single line: %q", rendered)
	}
}

