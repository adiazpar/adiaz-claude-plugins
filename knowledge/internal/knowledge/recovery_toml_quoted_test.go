package knowledge

import (
	"strings"
	"testing"
)

// A quoted table header is ordinary Codex configuration, not a malformed file.
//
// The scanner accepted only bare dotted keys, so [projects."C:\\Users\\x"] -
// which is what Codex itself writes for a per-project section, and the only way
// to spell a Windows path as a key at all - was reported malformed at every
// session start. A configuration judged malformed is preserved untouched, so
// the managed memory policy never reached the host that needed it most.
const quotedHeaderCodexConfig = `model = "gpt-test"

[projects."C:\\Users\\x"]
trust_level = "trusted"

[projects.'C:\Users\y']
trust_level = "trusted"

[mcp_servers."alpha beta".tools]
command = "npx"
`

func TestQuotedTOMLTableHeadersValidateAndReceiveThePolicy(t *testing.T) {
	keys, _, err := parseTOMLKeys([]byte(quotedHeaderCodexConfig))
	if err != nil {
		t.Fatalf("a quoted table header must validate: %v", err)
	}
	if _, ok := keys[`projects.C:\Users\x.trust_level`]; !ok {
		t.Fatalf("quoted segment was not decoded into the key path: %#v", keys)
	}
	if _, ok := keys[`mcp_servers.alpha beta.tools.command`]; !ok {
		t.Fatalf("a quoted segment containing a space was lost: %#v", keys)
	}

	reconciled, err := reconcileCodexMemoryPolicy([]byte(quotedHeaderCodexConfig), false)
	if err != nil {
		t.Fatalf("memory policy reconciliation refused a quoted header: %v", err)
	}
	body := string(reconciled)
	for _, want := range []string{
		"[features]", "memories = false",
		"generate_memories = false", "use_memories = false",
		`[projects."C:\\Users\\x"]`, `[projects.'C:\Users\y']`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reconciled configuration is missing %q:\n%s", want, body)
		}
	}
	if _, _, err := parseTOMLKeys(reconciled); err != nil {
		t.Fatalf("reconciled configuration no longer parses: %v", err)
	}

	// Settling: a second pass must be a no-op.
	settled, err := reconcileCodexMemoryPolicy(reconciled, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(settled) != body {
		t.Fatal("memory policy reconciliation does not settle on quoted headers")
	}
}

// Quoting is not a licence to accept anything: an unterminated or otherwise
// unbalanced header must still be refused, so a genuinely broken file is
// preserved rather than rewritten.
func TestMalformedQuotedTOMLHeadersAreStillRefused(t *testing.T) {
	cases := map[string]string{
		"unterminated basic quote":  "[projects.\"C:\\\\Users\\\\x]\n",
		"unterminated literal":      "[projects.'C:\\Users\\y]\n",
		"trailing text after quote": "[projects.\"a\"b]\n",
		"missing closing bracket":   "[projects.\"a\"\n",
		"empty quoted segment":      "[projects.\"\"]\n",
		"array of tables":           "[[projects]]\n",
		"quoted managed table key":  "[\"features.memories\"]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseTOMLKeys([]byte(body)); err == nil {
				t.Fatalf("malformed header was accepted: %q", body)
			}
			if _, err := reconcileCodexMemoryPolicy([]byte(body), false); err == nil {
				t.Fatalf("malformed header was rewritten: %q", body)
			}
		})
	}
}

// Decoding must not weaken duplicate and conflict detection: a quoted key names
// the same thing as its bare spelling, exactly as TOML says.
func TestQuotedAndBareTOMLKeysAreTheSameKey(t *testing.T) {
	if _, _, err := parseTOMLKeys([]byte("[projects]\n\"alpha\" = 1\nalpha = 2\n")); err == nil {
		t.Fatal("a quoted key must duplicate its bare spelling")
	}
	if _, _, err := parseTOMLKeys([]byte("[\"projects\"]\nalpha = 1\n[projects]\nbeta = 2\n")); err == nil {
		t.Fatal("a quoted table must duplicate its bare spelling")
	}
	segments, ok := tomlKeySegments(`a . "b" . 'c'`)
	if !ok || strings.Join(segments, "|") != "a|b|c" {
		t.Fatalf("dotted key segmentation = %#v (ok=%v)", segments, ok)
	}
}
