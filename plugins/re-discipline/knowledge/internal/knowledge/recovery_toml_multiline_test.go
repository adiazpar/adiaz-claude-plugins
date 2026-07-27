package knowledge

import "strings"

import "testing"

// Codex project configuration commonly declares developer_instructions with a
// TOML multi-line basic string. A line-oriented reader misreads its interior
// lines as assignments and fails closed, which disables the whole knowledge
// system for a valid project.
const multilineCodexConfig = `developer_instructions = """
Never use emojis.
Never add generated-by attribution to commit messages.
"""
approval_policy = "on-request"
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = false
`

func TestParseTOMLKeysAcceptsMultilineBasicString(t *testing.T) {
	keys, values, err := parseTOMLKeys([]byte(multilineCodexConfig))
	if err != nil {
		t.Fatalf("valid multi-line TOML rejected: %v", err)
	}
	if _, ok := keys["developer_instructions"]; !ok {
		t.Fatalf("developer_instructions key not recorded: %v", keys)
	}
	if got := values["approval_policy"]; got != `"on-request"` {
		t.Fatalf("approval_policy = %q, want %q", got, `"on-request"`)
	}
	for _, absent := range []string{"never use emojis.", "sandbox_workspace_write"} {
		if _, ok := keys[absent]; ok {
			t.Fatalf("string content leaked into keys: %q", absent)
		}
	}
}

func TestParseTOMLKeysAcceptsMultilineLiteralString(t *testing.T) {
	body := "notes = '''\nliteral = not an assignment\n'''\nmemories = false\n"
	keys, _, err := parseTOMLKeys([]byte(body))
	if err != nil {
		t.Fatalf("valid multi-line literal TOML rejected: %v", err)
	}
	if _, ok := keys["literal"]; ok {
		t.Fatal("literal string content parsed as an assignment")
	}
}

func TestParseTOMLKeysRejectsUnterminatedMultiline(t *testing.T) {
	if _, _, err := parseTOMLKeys([]byte("a = \"\"\"\nopen forever\n")); err == nil {
		t.Fatal("unterminated multi-line string accepted")
	}
}

func TestParseTOMLKeysSingleLineTripleQuoteStaysInline(t *testing.T) {
	keys, _, err := parseTOMLKeys([]byte("a = \"\"\"inline\"\"\"\nb = 1\n"))
	if err != nil {
		t.Fatalf("inline triple-quoted string rejected: %v", err)
	}
	if _, ok := keys["b"]; !ok {
		t.Fatal("parser lost track after a closed inline triple-quoted string")
	}
}

// writable_roots is a stock Codex sandbox setting whose value is a TOML array.
// An assignment ending in "]" must not be mistaken for table syntax.
func TestParseTOMLKeysAcceptsArrayValues(t *testing.T) {
	body := "[sandbox_workspace_write]\n" +
		"writable_roots = [\"C:\\\\Users\\\\dev\\\\project\"]\n" +
		"network_access = false\n"
	keys, values, err := parseTOMLKeys([]byte(body))
	if err != nil {
		t.Fatalf("array assignment rejected: %v", err)
	}
	if _, ok := keys["sandbox_workspace_write.writable_roots"]; !ok {
		t.Fatalf("array key not recorded: %v", keys)
	}
	if got := values["sandbox_workspace_write.network_access"]; got != "false" {
		t.Fatalf("network_access = %q, want %q", got, "false")
	}
}

func TestParseTOMLKeysRejectsArrayOfTablesAndStrayBracket(t *testing.T) {
	if _, _, err := parseTOMLKeys([]byte("[[products]]\nname = \"a\"\n")); err == nil {
		t.Fatal("array of tables accepted")
	}
	if _, _, err := parseTOMLKeys([]byte("stray]\n")); err == nil {
		t.Fatal("stray bracket line accepted")
	}
}

// MCP server declarations routinely spread args across several lines. Those
// element lines are array content, not assignments.
func TestParseTOMLKeysAcceptsMultilineArray(t *testing.T) {
	body := "[mcp_servers.ghidra]\n" +
		"command = \"python.exe\"\n" +
		"args = [\n" +
		"  \"bridge.py\",\n" +
		"  \"--server\",\n" +
		"  \"http://127.0.0.1:8080/\"\n" +
		"]\n" +
		"startup_timeout_sec = 30\n"
	keys, values, err := parseTOMLKeys([]byte(body))
	if err != nil {
		t.Fatalf("multi-line array rejected: %v", err)
	}
	if got := values["mcp_servers.ghidra.startup_timeout_sec"]; got != "30" {
		t.Fatalf("parser lost position after the array: got %q", got)
	}
	if _, ok := keys["mcp_servers.ghidra.args"]; !ok {
		t.Fatalf("args key not recorded: %v", keys)
	}
}

func TestParseTOMLKeysRejectsUnterminatedArray(t *testing.T) {
	if _, _, err := parseTOMLKeys([]byte("args = [\n  \"a\",\n")); err == nil {
		t.Fatal("unterminated array accepted")
	}
}

// An array element that looks like managed policy must not be rewritten.
func TestReconcileCodexMemoryPolicyIgnoresArrayContent(t *testing.T) {
	body := "notes = [\n  \"memories = true\",\n]\n"
	got, err := reconcileCodexMemoryPolicy([]byte(body), false)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if !strings.Contains(string(got), "\"memories = true\",") {
		t.Fatalf("writer edited inside an array:\n%s", got)
	}
}

func TestReconcileCodexMemoryPolicyPreservesMultilineString(t *testing.T) {
	got, err := reconcileCodexMemoryPolicy([]byte(multilineCodexConfig), false)
	if err != nil {
		t.Fatalf("reconcile rejected valid multi-line TOML: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "Never use emojis.") {
		t.Fatal("multi-line string content was destroyed")
	}
	for _, want := range []string{
		"[features]", "memories = false",
		"[memories]", "generate_memories = false", "use_memories = false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("managed policy %q missing from result:\n%s", want, out)
		}
	}
	if _, _, err := parseTOMLKeys(got); err != nil {
		t.Fatalf("reconciled output is not parseable: %v", err)
	}
}

// A multi-line string whose content resembles managed policy must never be
// rewritten: the writer would corrupt user prose and silently claim a policy.
func TestReconcileCodexMemoryPolicyIgnoresPolicyTextInsideString(t *testing.T) {
	body := "guide = \"\"\"\n[features]\nmemories = true\n\"\"\"\n"
	got, err := reconcileCodexMemoryPolicy([]byte(body), false)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "\n[features]\nmemories = true\n\"\"\"") {
		t.Fatalf("writer edited inside a multi-line string:\n%s", out)
	}
	if strings.Count(out, "[features]") != 2 {
		t.Fatalf("expected the quoted text plus one real managed table:\n%s", out)
	}
}
