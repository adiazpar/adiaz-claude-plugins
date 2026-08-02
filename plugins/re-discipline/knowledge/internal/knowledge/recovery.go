package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type RecoveryAction struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type RecoveryResult struct {
	SchemaVersion       int              `json:"schemaVersion"`
	ProjectRoot         string           `json:"projectRoot"`
	ManagedMarker       string           `json:"managedMarker"`
	Restored            []RecoveryAction `json:"restored"`
	ExistingPreserved   []string         `json:"existingPreserved"`
	NativeMemoryTouched bool             `json:"nativeMemoryTouched"`
}

type recoveryFile struct {
	Target   string
	Template string
	Kind     string
}

// SharedLawsMarker is the exact contract handshake between this compiled
// runtime and a project's canonical profile. Recovery and MCP validation
// both refuse projects carrying any other version; the explicit 0.7-to-0.8
// migrator is the only door from a legacy marker to this one.
const SharedLawsMarker = "<!-- re-discipline:shared-laws v0.8.0 -->"

var managedRecoveryFiles = []recoveryFile{
	{".re-discipline/config.json", "config.json", "config"},
	{".re-discipline/knowledge/README.md", "knowledge-README.md", "markdown"},
	{".re-discipline/knowledge/policy.jsonc", "policy.jsonc", "settings"},
	{".re-discipline/knowledge/retrieval-profile.json", "retrieval-profile.json", "profile"},
	{".re-discipline/memory/INDEX.md", "memory-INDEX.md", "markdown"},
	{".re-discipline/knowledge/evals/README.md", "knowledge-evals-README.md", "markdown"},
	{".claude/settings.json", "claude-settings.json", "claude-json"},
	{".codex/config.toml", "codex-config.toml", "codex-toml"},
}

var managedRecoveryDirectories = []string{
	".re-discipline/knowledge",
	".re-discipline/memory/proposals",
	".re-discipline/memory/topics",
	".re-discipline/knowledge/evals",
	".re-discipline/cache/knowledge/generations",
	".re-discipline/cache/knowledge/vectors",
	".re-discipline/cache/calibration",
	".re-discipline/agents/providers",
	".re-discipline/agents/recruiting",
	".claude",
	".codex",
}

type recoveryPlan struct {
	managed  recoveryFile
	target   string
	body     []byte
	original []byte
	mode     os.FileMode
	source   string
	existing bool
	write    bool
}

func RecoverProject(projectRoot, pluginRoot string) (RecoveryResult, error) {
	boundary, err := NewBoundary(projectRoot)
	if err != nil {
		return RecoveryResult{}, err
	}
	markerBody, _, err := ReadProjectFile(boundary, ".re-discipline/project-profile.md")
	if err != nil {
		return RecoveryResult{}, errors.New("recovery requires an existing managed project profile")
	}
	const marker = SharedLawsMarker
	if !strings.Contains(string(markerBody), marker) {
		return RecoveryResult{}, errors.New("recovery requires the supported shared-laws v0.8.0 marker")
	}
	pluginAbsolute, err := filepath.Abs(pluginRoot)
	if err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{
		SchemaVersion: 1, ProjectRoot: boundary.Root, ManagedMarker: marker,
		Restored: []RecoveryAction{}, ExistingPreserved: []string{},
		NativeMemoryTouched: false,
	}
	plans := make([]recoveryPlan, 0, len(managedRecoveryFiles))
	for _, managed := range managedRecoveryFiles {
		target, err := safeMissingTarget(boundary, managed.Target)
		if err != nil {
			return RecoveryResult{}, fmt.Errorf("recover %s: %w", managed.Target, err)
		}
		if info, statErr := os.Lstat(target); statErr == nil {
			if !info.Mode().IsRegular() {
				return RecoveryResult{}, fmt.Errorf(
					"managed recovery target %s is not a regular file", managed.Target)
			}
			body, readErr := os.ReadFile(target)
			if readErr != nil {
				return RecoveryResult{}, readErr
			}
			if len(body) == 0 || int64(len(body)) > maxSourceBytes {
				return RecoveryResult{}, fmt.Errorf(
					"managed recovery target %s has unsafe size", managed.Target)
			}
			plans = append(plans, recoveryPlan{
				managed: managed, target: target, body: body,
				original: append([]byte(nil), body...), mode: info.Mode().Perm(),
				source: "existing", existing: true,
			})
			continue
		} else if !os.IsNotExist(statErr) {
			return RecoveryResult{}, statErr
		}
		body, source, err := recoveryBody(boundary.Root, pluginAbsolute, managed)
		if err != nil {
			return RecoveryResult{}, err
		}
		plans = append(plans, recoveryPlan{
			managed: managed, target: target, body: body, source: source, write: true,
		})
	}
	type recoveryDirectory struct {
		path     string
		relative string
		existing bool
	}
	directories := make([]recoveryDirectory, 0, len(managedRecoveryDirectories))
	for _, relative := range managedRecoveryDirectories {
		target, err := safeMissingTarget(boundary, relative)
		if err != nil {
			return RecoveryResult{}, fmt.Errorf("recover directory %s: %w", relative, err)
		}
		existing := false
		if info, statErr := os.Lstat(target); statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return RecoveryResult{}, fmt.Errorf(
					"managed recovery directory %s is not a real directory", relative)
			}
			existing = true
		} else if !os.IsNotExist(statErr) {
			return RecoveryResult{}, statErr
		}
		directories = append(directories, recoveryDirectory{
			path: target, relative: relative, existing: existing,
		})
	}

	var bootstrap BootstrapConfig
	for index := range plans {
		if plans[index].managed.Kind != "config" {
			continue
		}
		if err := decodeStrict(plans[index].body, &bootstrap); err != nil {
			return RecoveryResult{}, fmt.Errorf(
				"refuse invalid configuration recovery source for %s: %w",
				plans[index].managed.Target, err)
		}
		if err := ValidateBootstrap(bootstrap); err != nil {
			return RecoveryResult{}, fmt.Errorf(
				"refuse invalid configuration recovery source for %s: %w",
				plans[index].managed.Target, err)
		}
	}
	nativeEnabled := bootstrap.Memory.Mode != "shared-only"
	for index := range plans {
		plan := &plans[index]
		switch plan.managed.Kind {
		case "claude-json":
			reconciled, err := reconcileClaudeMemoryPolicy(plan.body, nativeEnabled)
			if err != nil {
				return RecoveryResult{}, fmt.Errorf(
					"refuse invalid configuration recovery source for %s: %w",
					plan.managed.Target, err)
			}
			if !bytes.Equal(plan.body, reconciled) {
				plan.body = reconciled
				plan.write = true
				if plan.existing {
					plan.source = "reconciled-existing"
				} else {
					plan.source += "+policy-reconciled"
				}
			}
		case "codex-toml":
			reconciled, err := reconcileCodexMemoryPolicy(plan.body, nativeEnabled)
			if err != nil {
				return RecoveryResult{}, fmt.Errorf(
					"refuse invalid configuration recovery source for %s: %w",
					plan.managed.Target, err)
			}
			if !bytes.Equal(plan.body, reconciled) {
				plan.body = reconciled
				plan.write = true
				if plan.existing {
					plan.source = "reconciled-existing"
				} else {
					plan.source += "+policy-reconciled"
				}
			}
		}
		if err := validateRecoveryBody(plan.managed.Kind, plan.body); err != nil {
			return RecoveryResult{}, fmt.Errorf(
				"refuse invalid configuration recovery source for %s: %w",
				plan.managed.Target, err)
		}
	}

	written := []int{}
	for index, plan := range plans {
		if !plan.write {
			result.ExistingPreserved = append(
				result.ExistingPreserved, plan.managed.Target)
			continue
		}
		if err := AtomicWrite(plan.target, plan.body, 0o600); err != nil {
			rollbackErr := rollbackRecoveryPlans(plans, written)
			if rollbackErr != nil {
				return RecoveryResult{}, fmt.Errorf(
					"recovery write failed: %v; rollback failed: %w", err, rollbackErr)
			}
			return RecoveryResult{}, err
		}
		written = append(written, index)
		result.Restored = append(result.Restored, RecoveryAction{
			Path: plan.managed.Target, Source: plan.source,
		})
	}
	createdDirectories := []string{}
	for _, directory := range directories {
		if directory.existing {
			continue
		}
		if err := os.MkdirAll(directory.path, 0o700); err != nil {
			for index := len(createdDirectories) - 1; index >= 0; index-- {
				_ = os.Remove(createdDirectories[index])
			}
			rollbackErr := rollbackRecoveryPlans(plans, written)
			if rollbackErr != nil {
				return RecoveryResult{}, fmt.Errorf(
					"recovery directory creation failed: %v; rollback failed: %w",
					err, rollbackErr,
				)
			}
			return RecoveryResult{}, err
		}
		createdDirectories = append(createdDirectories, directory.path)
	}
	sort.Strings(result.ExistingPreserved)
	return result, nil
}

func rollbackRecoveryPlans(plans []recoveryPlan, written []int) error {
	rollbackErrors := []string{}
	for position := len(written) - 1; position >= 0; position-- {
		plan := plans[written[position]]
		if plan.existing {
			mode := plan.mode
			if mode == 0 {
				mode = 0o600
			}
			if err := AtomicWrite(plan.target, plan.original, mode); err != nil {
				rollbackErrors = append(
					rollbackErrors,
					fmt.Sprintf("restore %s: %v", plan.managed.Target, err),
				)
			}
			continue
		}
		if err := os.Remove(plan.target); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(
				rollbackErrors,
				fmt.Sprintf("remove %s: %v", plan.managed.Target, err),
			)
		}
	}
	if len(rollbackErrors) > 0 {
		return errors.New(strings.Join(rollbackErrors, "; "))
	}
	return nil
}

func safeMissingTarget(boundary Boundary, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(relative, "\\", "/")))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(clean) {
		return "", errors.New("managed recovery path is unsafe")
	}
	target := filepath.Join(boundary.Root, clean)
	parent, err := evalNearestExisting(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if !withinRoot(boundary.Root, parent) {
		return "", errors.New("managed recovery parent escapes project boundary")
	}
	return target, nil
}

func recoveryBody(root, pluginRoot string, managed recoveryFile) ([]byte, string, error) {
	if body, err := gitHeadFile(root, managed.Target); err == nil {
		return body, "tracked-head", nil
	}
	body, err := readContainedAsset(
		pluginRoot,
		filepath.ToSlash(filepath.Join(
			"templates", "project", filepath.FromSlash(managed.Template),
		)),
	)
	if err != nil {
		return nil, "", fmt.Errorf("read packaged recovery template %s: %w", managed.Template, err)
	}
	if len(body) == 0 || int64(len(body)) > maxSourceBytes {
		return nil, "", fmt.Errorf("packaged recovery template %s has unsafe size", managed.Template)
	}
	return body, "packaged-template", nil
}

func gitHeadFile(root, relative string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "show", "HEAD:"+filepath.ToSlash(relative))
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	body, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || int64(len(body)) > maxSourceBytes {
		return nil, errors.New("tracked recovery source has unsafe size")
	}
	return body, nil
}

func validateRecoveryBody(kind string, body []byte) error {
	if bytes.IndexByte(body, 0) >= 0 {
		return errors.New("recovery source contains a NUL byte")
	}
	switch kind {
	case "config":
		var config BootstrapConfig
		if err := decodeStrict(body, &config); err != nil {
			return err
		}
		return ValidateBootstrap(config)
	case "settings":
		stripped, err := StripJSONComments(body)
		if err != nil {
			return err
		}
		var raw struct {
			Schema string `json:"$schema"`
			KnowledgeSettings
		}
		if err := decodeStrict(stripped, &raw); err != nil {
			return err
		}
		return ValidateSettings(raw.KnowledgeSettings)
	case "profile":
		var profile RetrievalProfile
		if err := decodeStrict(body, &profile); err != nil {
			return err
		}
		return ValidateProfile(profile)
	case "claude-json":
		var value any
		if err := decodeStrict(body, &value); err != nil {
			return err
		}
		if _, ok := value.(map[string]any); !ok {
			return errors.New("Claude project settings must be a JSON object")
		}
	case "codex-toml":
		_, _, err := parseTOMLKeys(body)
		return err
	case "markdown":
		if reason := SensitiveContentReason(string(body)); reason != "" {
			return errors.New(reason)
		}
	case "text":
		if len(bytes.TrimSpace(body)) == 0 {
			return errors.New("recovery text is empty")
		}
	default:
		return errors.New("unknown recovery validator")
	}
	return nil
}

func reconcileClaudeMemoryPolicy(body []byte, enabled bool) ([]byte, error) {
	var settings map[string]any
	if err := decodeStrict(body, &settings); err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, errors.New("Claude project settings must be a JSON object")
	}
	if _, exists := settings["autoMemoryEnabled"]; exists &&
		!bytes.Contains(body, []byte(`"autoMemoryEnabled"`)) {
		return nil, errors.New(
			"Claude autoMemoryEnabled must use the literal managed key spelling")
	}
	if nestedJSONKey(settings, "autoMemoryEnabled", true) {
		return nil, errors.New(
			"Claude autoMemoryEnabled is ambiguous outside the top-level policy key")
	}
	if current, ok := settings["autoMemoryEnabled"]; ok {
		if _, ok := current.(bool); !ok {
			return nil, errors.New("Claude autoMemoryEnabled must be a boolean")
		}
	}
	if current, ok := settings["autoMemoryEnabled"].(bool); ok && current == enabled {
		return body, nil
	}
	settings["autoMemoryEnabled"] = enabled
	reconciled, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(reconciled, '\n'), nil
}

func nestedJSONKey(value any, target string, top bool) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target && !top {
				return true
			}
			if nestedJSONKey(child, target, false) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if nestedJSONKey(child, target, false) {
				return true
			}
		}
	}
	return false
}

var tomlBareSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlKeySegments splits a TOML dotted key into its decoded segments.
//
// A key segment may be bare, basic-quoted, or literal-quoted. Accepting only
// bare segments made an ordinary Codex configuration - `[projects."C:\\Users\\x"]`
// is what the tool itself writes for a per-project section - report as
// malformed at every session start, and a host whose configuration is judged
// malformed never receives its memory policy. Quoting is the only way to write
// a Windows path as a key at all, so rejecting it rejected the common case.
//
// Escape handling inside a basic-quoted segment matches tomlBracketDelta and
// stripTOMLComment: a backslash escapes the character after it. Segments are
// returned decoded, so `"a"` and bare `a` are the same key, as TOML requires.
func tomlKeySegments(key string) ([]string, bool) {
	segments := []string{}
	rest := key
	for {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return nil, false
		}
		var segment string
		var ok bool
		switch rest[0] {
		case '"':
			segment, rest, ok = decodeTOMLBasicSegment(rest)
		case '\'':
			segment, rest, ok = decodeTOMLLiteralSegment(rest)
		default:
			end := strings.IndexAny(rest, " \t.")
			if end < 0 {
				end = len(rest)
			}
			segment, rest, ok = rest[:end], rest[end:], true
			if !tomlBareSegmentRE.MatchString(segment) {
				return nil, false
			}
		}
		// An empty segment is legal TOML and pathological in practice. The hook
		// scanners report a malformed key by returning an empty path, so an
		// empty segment is the one shape they cannot express; refusing it in
		// all four keeps them from disagreeing.
		if !ok || segment == "" {
			return nil, false
		}
		segments = append(segments, segment)
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return segments, true
		}
		if rest[0] != '.' {
			return nil, false
		}
		rest = rest[1:]
	}
}

// decodeTOMLBasicSegment decodes a leading "..." segment and returns it with
// the unread remainder.
//
// A backslash escapes the character after it and is otherwise not interpreted.
// That is exactly what tomlBracketDelta and stripTOMLComment already do when
// they walk a string, and holding all four scanners to one rule matters more
// here than resolving \n or \uXXXX: a decoded segment is only ever used to
// detect duplicate keys and to recognize the managed namespaces, never to
// reproduce the key's text.
func decodeTOMLBasicSegment(value string) (string, string, bool) {
	var decoded strings.Builder
	escaped := false
	for index, char := range value {
		if index == 0 {
			continue
		}
		switch {
		case escaped:
			decoded.WriteRune(char)
			escaped = false
		case char == '\\':
			escaped = true
		case char == '"':
			return decoded.String(), value[index+1:], true
		default:
			decoded.WriteRune(char)
		}
	}
	return "", "", false
}

// decodeTOMLLiteralSegment reads a leading '...' segment verbatim.
func decodeTOMLLiteralSegment(value string) (string, string, bool) {
	end := strings.IndexByte(value[1:], '\'')
	if end < 0 {
		return "", "", false
	}
	return value[1 : 1+end], value[end+2:], true
}

// canonicalTOMLKey joins decoded segments into the dotted form the key maps
// are indexed by.
//
// A quoted segment containing a dot therefore reads as two segments here.
// That is deliberate: the hook scanners join the same way, and every outcome
// of the conflation is a refusal - `["features.memories"]` is rejected as a
// managed key declared as a table, and `[a.b]` beside `["a.b"]` is rejected as
// a duplicate. A refusal preserves the file untouched. Escaping the dot would
// make one scanner accept what the others refuse, and a host whose
// configuration two scanners disagree about is worse than a pathological key
// being refused.
func canonicalTOMLKey(segments []string) string {
	return strings.Join(segments, ".")
}

// openTOMLMultiline reports the delimiter of a multi-line basic or literal
// string opened by value and not closed on the same line.
func openTOMLMultiline(value string) string {
	for _, delim := range []string{`"""`, `'''`} {
		if strings.HasPrefix(value, delim) &&
			!strings.Contains(value[len(delim):], delim) {
			return delim
		}
	}
	return ""
}

// tomlBracketDelta counts unquoted array and inline-table nesting in text.
func tomlBracketDelta(text string) int {
	delta := 0
	inBasic, inLiteral, escaped := false, false, false
	for _, char := range text {
		switch {
		case escaped:
			escaped = false
		case inBasic && char == '\\':
			escaped = true
		case !inLiteral && char == '"':
			inBasic = !inBasic
		case !inBasic && char == '\'':
			inLiteral = !inLiteral
		case !inBasic && !inLiteral && (char == '[' || char == '{'):
			delta++
		case !inBasic && !inLiteral && (char == ']' || char == '}'):
			delta--
		}
	}
	return delta
}

// tomlCodeLines marks the lines that carry TOML structure. Interior lines of a
// multi-line string, array, or inline table are value content, never tables,
// assignments, or comments, so every structural scan must skip them.
func tomlCodeLines(lines []string) ([]bool, error) {
	code := make([]bool, len(lines))
	delim := ""
	depth := 0
	for index, raw := range lines {
		if delim != "" {
			at := strings.Index(raw, delim)
			if at < 0 {
				continue
			}
			rest := raw[at+len(delim):]
			delim = ""
			depth += tomlBracketDelta(stripTOMLComment(rest))
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced TOML brackets at line %d", index+1)
			}
			continue
		}
		if depth > 0 {
			depth += tomlBracketDelta(stripTOMLComment(raw))
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced TOML brackets at line %d", index+1)
			}
			continue
		}
		code[index] = true
		line := strings.TrimSpace(stripTOMLComment(raw))
		equal := strings.IndexByte(line, '=')
		if equal <= 0 {
			continue
		}
		value := strings.TrimSpace(line[equal+1:])
		if opened := openTOMLMultiline(value); opened != "" {
			delim = opened
			continue
		}
		depth += tomlBracketDelta(value)
		if depth < 0 {
			return nil, fmt.Errorf("unbalanced TOML brackets at line %d", index+1)
		}
	}
	if delim != "" {
		return nil, fmt.Errorf("unterminated TOML multi-line string")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unterminated TOML array or inline table")
	}
	return code, nil
}

func parseTOMLKeys(body []byte) (map[string]int, map[string]string, error) {
	keys := map[string]int{}
	values := map[string]string{}
	tables := map[string]int{}
	section := ""
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	code, codeErr := tomlCodeLines(lines)
	if codeErr != nil {
		return nil, nil, codeErr
	}
	for index, raw := range lines {
		if !code[index] {
			continue
		}
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		// Reject arrays of tables. A non-table line that ends in "]" is an
		// ordinary array assignment such as writable_roots = ["..."]; the
		// assignment branch below still rejects one that has no key and value.
		if strings.HasPrefix(line, "[[") {
			return nil, nil, fmt.Errorf("unsupported TOML table syntax at line %d", index+1)
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, nil, fmt.Errorf("malformed TOML table at line %d", index+1)
			}
			// The header's own brackets are the delimiters; anything else that
			// looks like one is either inside a quoted segment, where it is
			// content, or outside it, where the segmenter rejects the header.
			name := strings.TrimSpace(line[1 : len(line)-1])
			segments, ok := tomlKeySegments(name)
			if !ok {
				return nil, nil, fmt.Errorf("unsupported TOML table at line %d", index+1)
			}
			section = canonicalTOMLKey(segments)
			if prior, duplicate := tables[section]; duplicate {
				return nil, nil, fmt.Errorf(
					"duplicate TOML table %q at lines %d and %d",
					section, prior, index+1)
			}
			if prior, scalar := keys[section]; scalar {
				return nil, nil, fmt.Errorf(
					"TOML table %q conflicts with scalar at line %d",
					section, prior)
			}
			for key, prior := range keys {
				if strings.HasPrefix(section, key+".") {
					return nil, nil, fmt.Errorf(
						"TOML table %q conflicts with scalar %q at line %d",
						section, key, prior)
				}
			}
			switch section {
			case "features.memories", "memories.generate_memories", "memories.use_memories":
				return nil, nil, fmt.Errorf(
					"managed TOML key %q cannot be declared as a table", section)
			}
			tables[section] = index + 1
			continue
		}
		equal := strings.IndexByte(line, '=')
		if equal <= 0 || strings.TrimSpace(line[equal+1:]) == "" {
			return nil, nil, fmt.Errorf("malformed TOML assignment at line %d", index+1)
		}
		key := strings.TrimSpace(line[:equal])
		segments, ok := tomlKeySegments(key)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported TOML key at line %d", index+1)
		}
		full := canonicalTOMLKey(segments)
		if section != "" {
			full = section + "." + full
		}
		if prior, table := tables[full]; table {
			return nil, nil, fmt.Errorf(
				"TOML scalar %q conflicts with table at line %d", full, prior)
		}
		for key, prior := range keys {
			if strings.HasPrefix(full, key+".") ||
				strings.HasPrefix(key, full+".") {
				return nil, nil, fmt.Errorf(
					"TOML scalar %q conflicts with scalar %q at line %d",
					full, key, prior)
			}
		}
		if prior, duplicate := keys[full]; duplicate {
			return nil, nil, fmt.Errorf(
				"duplicate TOML key %q at lines %d and %d", full, prior, index+1)
		}
		keys[full] = index + 1
		values[full] = strings.TrimSpace(line[equal+1:])
	}
	return keys, values, nil
}

func stripTOMLComment(line string) string {
	inBasic, inLiteral, escaped := false, false, false
	for index, char := range line {
		switch {
		case escaped:
			escaped = false
		case inBasic && char == '\\':
			escaped = true
		case !inLiteral && char == '"':
			inBasic = !inBasic
		case !inBasic && char == '\'':
			inLiteral = !inLiteral
		case !inBasic && !inLiteral && char == '#':
			return line[:index]
		}
	}
	return line
}

// normalizeTOMLKey renders a dotted key in the canonical form the key maps are
// indexed by. A key that does not parse is returned with only its whitespace
// normalized: callers that care about validity use tomlKeySegments directly,
// and the ones that call this have already been through parseTOMLKeys.
func normalizeTOMLKey(key string) string {
	if segments, ok := tomlKeySegments(key); ok {
		return canonicalTOMLKey(segments)
	}
	parts := strings.Split(key, ".")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, ".")
}

func reconcileCodexMemoryPolicy(body []byte, enabled bool) ([]byte, error) {
	_, values, err := parseTOMLKeys(body)
	if err != nil {
		return nil, err
	}
	for _, namespace := range []string{"features", "memories"} {
		if _, scalar := values[namespace]; scalar {
			return nil, fmt.Errorf(
				"managed TOML namespace %q conflicts with a scalar or inline table",
				namespace)
		}
	}
	desired := "false"
	if enabled {
		desired = "true"
	}
	managed := map[string]string{
		"features.memories":          desired,
		"memories.generate_memories": desired,
		"memories.use_memories":      desired,
	}
	for key, expected := range managed {
		if value, ok := values[key]; ok && value != "true" && value != "false" {
			return nil, fmt.Errorf("%s must be a boolean", key)
		} else if ok && value == expected {
			delete(managed, key)
		}
	}
	if len(managed) == 0 {
		return body, nil
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	code, codeErr := tomlCodeLines(lines)
	if codeErr != nil {
		return nil, codeErr
	}
	currentSection := ""
	for index, raw := range lines {
		if !code[index] {
			continue
		}
		line := strings.TrimSpace(stripTOMLComment(raw))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = normalizeTOMLKey(
				strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			continue
		}
		equal := strings.IndexByte(line, '=')
		if equal <= 0 {
			continue
		}
		full := normalizeTOMLKey(strings.TrimSpace(line[:equal]))
		if currentSection != "" {
			full = currentSection + "." + full
		}
		expected, ok := managed[full]
		if !ok {
			continue
		}
		prefix := raw[:strings.Index(raw, "=")+1]
		comment := ""
		if commentAt := strings.Index(raw, "#"); commentAt >= 0 {
			comment = " " + strings.TrimSpace(raw[commentAt:])
		}
		lines[index] = prefix + " " + expected + comment
		delete(managed, full)
	}
	for _, section := range []string{"features", "memories"} {
		additions := []string{}
		for _, key := range []string{"memories", "generate_memories", "use_memories"} {
			full := section + "." + key
			if value, ok := managed[full]; ok {
				additions = append(additions, key+" = "+value)
				delete(managed, full)
			}
		}
		if len(additions) == 0 {
			continue
		}
		headerIndex := -1
		insertIndex := len(lines)
		sectionCode, sectionErr := tomlCodeLines(lines)
		if sectionErr != nil {
			return nil, sectionErr
		}
		for index, raw := range lines {
			if !sectionCode[index] {
				continue
			}
			line := strings.TrimSpace(stripTOMLComment(raw))
			if line == "["+section+"]" {
				headerIndex = index
				insertIndex = len(lines)
				continue
			}
			if headerIndex >= 0 && index > headerIndex &&
				strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				insertIndex = index
				break
			}
		}
		if headerIndex < 0 {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, "["+section+"]")
			lines = append(lines, additions...)
			continue
		}
		insertion := append(append([]string{}, additions...), "")
		lines = append(lines[:insertIndex],
			append(insertion, lines[insertIndex:]...)...)
	}
	reconciled := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if _, _, err := parseTOMLKeys([]byte(reconciled)); err != nil {
		return nil, err
	}
	return []byte(reconciled), nil
}
