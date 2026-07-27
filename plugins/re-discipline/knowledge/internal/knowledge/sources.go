package knowledge

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	headingRE              = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	linkRE                 = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	privateKeyRE           = regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)
	credentialAssignmentRE = regexp.MustCompile(
		`(?im)^[ \t]*(?:api[_-]?key|secret(?:[_-]?key)?|password|passwd|access[_-]?token|auth[_-]?token|client[_-]?secret)[ \t]*[:=][ \t]*['"]?[A-Za-z0-9_./+=-]{16,}`,
	)
	knownTokenRE = regexp.MustCompile(
		`(?i)(?:\bsk-[A-Za-z0-9_-]{20,}\b|\bgh[pousr]_[A-Za-z0-9]{20,}\b|\bAKIA[0-9A-Z]{16}\b|\bxox[baprs]-[A-Za-z0-9-]{20,}\b)`,
	)
)

type SourceInventory struct {
	Documents       []SourceDocument
	Chunks          []Chunk
	Edges           []GraphEdge
	SourceStates    []SourceState
	DirectoryStates []SourceState
	Fingerprint     string
	Diagnostics     []string
}

type GraphEdge struct {
	Source string
	Target string
	Kind   string
}

type sourceClass struct {
	Path      string
	Tier      string
	Recursive bool
	AutoShape bool
	BaseOnly  string
	Pattern   string
	Enabled   bool
	// PromoteTier and PromoteMarker make a source's tier content-dependent: a
	// file matching PromoteMarker is indexed at PromoteTier instead of Tier.
	//
	// This is how a drafter report moves from `draft` to `campaign` when a
	// manager stamps it as reviewed. It is a deterministic regex evaluated at
	// index time, not a judgement, and it fails safe: an unstamped report
	// stays in `draft` and therefore out of every default tier set.
	PromoteTier   string
	PromoteMarker *regexp.Regexp
}

// reviewStampRE matches the disposition stamp review-subagent writes into a
// report head. Line-anchored, so a mention of "Review:" in prose cannot
// promote an unreviewed report into the retrievable tier.
var reviewStampRE = regexp.MustCompile(`(?m)^\*\*Review:\*\*[ \t]*\S`)

func DiscoverSources(boundary Boundary, settings KnowledgeSettings) (SourceInventory, error) {
	classes := []sourceClass{
		{Path: ".re-discipline/project-profile.md", Tier: "profile", Enabled: true},
		{Path: "docs/INDEX.md", Tier: "navigation", Enabled: true},
		{Path: "docs/truth", Tier: "truth", Recursive: true, Enabled: settings.Sources.Truth},
		{Path: "docs/history", Tier: "history", Recursive: true, Enabled: settings.Sources.History},
		{Path: "docs/backlog", Tier: "backlog", Recursive: true, Enabled: settings.Sources.Backlog},
		{Path: "active", Tier: "active", Recursive: true, BaseOnly: "CAMPAIGN.md", Enabled: settings.Sources.ActiveCampaigns},
		// Drafter reports carry a mandated section schema - VERDICT, CLAIMS,
		// RESIDUAL UNCERTAINTIES, EVIDENCE INDEX - as markdown headings, so
		// the chunker already partitions them along the epistemic boundary.
		// Indexing them is what makes an in-flight campaign's own findings
		// reachable instead of reachable only through its masterfile.
		{
			Path: "active", Tier: "draft", Recursive: true, BaseOnly: "report.md",
			PromoteTier: "campaign", PromoteMarker: reviewStampRE,
			Enabled: settings.Sources.DrafterReports,
		},
		{Path: ".re-discipline/memory/INDEX.md", Tier: "navigation", Enabled: settings.Sources.SharedMemory},
		{Path: ".re-discipline/memory/topics", Tier: "memory", Recursive: true, Enabled: settings.Sources.SharedMemory},
	}
	for _, additional := range settings.Sources.Additional {
		classes = append(classes, sourceClass{
			Path: additional.Path, Tier: additional.Tier, Pattern: additional.Pattern,
			AutoShape: true, Enabled: true,
		})
	}
	type candidate struct {
		Path string
		Tier string
	}
	candidates := map[string]candidate{}
	sourceStates := map[string]SourceState{}
	directoryStates := map[string]SourceState{}
	diagnostics := []string{}
	for _, class := range classes {
		if !class.Enabled {
			continue
		}
		absolute := filepath.Join(boundary.Root, filepath.FromSlash(class.Path))
		info, err := os.Stat(absolute)
		if err != nil {
			if os.IsNotExist(err) {
				state := SourceState{Path: class.Path, Exists: false, IsDir: class.Recursive}
				if class.Recursive {
					directoryStates[class.Path] = state
				} else {
					sourceStates[class.Path] = state
				}
				continue
			}
			diagnostics = append(diagnostics, class.Path+": "+err.Error())
			continue
		}
		recursive := class.Recursive || class.AutoShape && info.IsDir()
		if !recursive {
			sourceStates[class.Path] = stateFromInfo(class.Path, info)
			patternMatch := true
			if class.Pattern != "" {
				patternMatch, _ = filepath.Match(class.Pattern, filepath.Base(class.Path))
			}
			if info.Mode().IsRegular() && patternMatch {
				if existing, duplicate := candidates[class.Path]; duplicate &&
					existing.Tier != class.Tier {
					return SourceInventory{}, fmt.Errorf(
						"source %s is assigned conflicting tiers %s and %s",
						class.Path, existing.Tier, class.Tier)
				}
				candidates[class.Path] = candidate{class.Path, class.Tier}
			}
			continue
		}
		resolvedClass, resolveErr := canonicalExistingPath(absolute)
		if resolveErr != nil || !withinRoot(boundary.Root, resolvedClass) {
			diagnostics = append(diagnostics,
				class.Path+": source-class root escapes the project boundary")
			continue
		}
		directoryStates[class.Path] = stateFromInfo(class.Path, info)
		if !info.IsDir() {
			diagnostics = append(diagnostics, class.Path+": expected directory")
			continue
		}
		err = filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				diagnostics = append(diagnostics, path+": "+walkErr.Error())
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "cache" || entry.Name() == "proposals" {
					if path != absolute {
						return filepath.SkipDir
					}
				}
				resolvedDirectory, resolveErr := canonicalExistingPath(path)
				if resolveErr != nil || !withinRoot(boundary.Root, resolvedDirectory) {
					diagnostics = append(diagnostics,
						filepath.ToSlash(path)+": directory escapes the project boundary")
					if path != absolute {
						return filepath.SkipDir
					}
					return errors.New("source-class root escapes the project boundary")
				}
				relative, relativeErr := filepath.Rel(boundary.Root, path)
				if relativeErr == nil {
					if entryInfo, infoErr := entry.Info(); infoErr == nil {
						normalized := filepath.ToSlash(relative)
						directoryStates[normalized] = stateFromInfo(normalized, entryInfo)
					}
				}
				return nil
			}
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return nil
			}
			if class.BaseOnly != "" && !strings.EqualFold(entry.Name(), class.BaseOnly) {
				return nil
			}
			if class.Pattern != "" {
				matches, matchErr := filepath.Match(class.Pattern, entry.Name())
				if matchErr != nil || !matches {
					return nil
				}
			}
			relative, err := boundary.Relative(path)
			if err != nil {
				diagnostics = append(diagnostics, path+": "+err.Error())
				return nil
			}
			if IsForbiddenSource(relative) {
				return nil
			}
			if entryInfo, infoErr := entry.Info(); infoErr == nil {
				sourceStates[relative] = stateFromInfo(relative, entryInfo)
			}
			tier := class.Tier
			if class.PromoteMarker != nil {
				// Read only to decide the tier. A file that fails to read
				// keeps the unpromoted tier, which is the safe direction.
				if body, _, readErr := ReadProjectFile(boundary, relative); readErr == nil &&
					class.PromoteMarker.Match(body) {
					tier = class.PromoteTier
				}
			}
			if existing, duplicate := candidates[relative]; duplicate &&
				existing.Tier != tier {
				return fmt.Errorf(
					"source %s is assigned conflicting tiers %s and %s",
					relative, existing.Tier, tier)
			}
			candidates[relative] = candidate{relative, tier}
			return nil
		})
		if err != nil {
			return SourceInventory{}, err
		}
	}

	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	documents := make([]SourceDocument, 0, len(paths))
	chunks := []Chunk{}
	for _, path := range paths {
		body, hash, err := ReadProjectFile(boundary, path)
		if err != nil {
			diagnostics = append(diagnostics, path+": "+err.Error())
			continue
		}
		if !utf8.Valid(body) {
			diagnostics = append(diagnostics, path+": source is not valid UTF-8")
			continue
		}
		if reason := SensitiveContentReason(string(body)); reason != "" {
			diagnostics = append(diagnostics, path+": "+reason+"; source excluded")
			continue
		}
		title := titleFromMarkdown(string(body), path)
		doc := SourceDocument{
			ID: StableID("doc", path, hash), Path: path, Tier: candidates[path].Tier,
			Title: title, Content: string(body), ContentHash: hash, Size: int64(len(body)),
			MtimeNS: sourceStates[path].MtimeNS,
		}
		documents = append(documents, doc)
		chunks = append(chunks, ChunkMarkdown(doc)...)
	}
	edges := BuildGraphEdges(documents, chunks)
	type fingerprintDocument struct {
		ID          string `json:"id"`
		Path        string `json:"path"`
		Tier        string `json:"tier"`
		Title       string `json:"title"`
		ContentHash string `json:"contentHash"`
		Size        int64  `json:"size"`
	}
	fingerprintDocuments := make([]fingerprintDocument, 0, len(documents))
	for _, document := range documents {
		fingerprintDocuments = append(fingerprintDocuments, fingerprintDocument{
			ID: document.ID, Path: document.Path, Tier: document.Tier,
			Title: document.Title, ContentHash: document.ContentHash, Size: document.Size,
		})
	}
	fingerprintInput := struct {
		Parser    string                `json:"parser"`
		Chunker   string                `json:"chunker"`
		Documents []fingerprintDocument `json:"documents"`
	}{ParserVersion, ChunkerVersion, fingerprintDocuments}
	fingerprint, err := CanonicalDigest(fingerprintInput)
	if err != nil {
		return SourceInventory{}, err
	}
	return SourceInventory{
		Documents: documents, Chunks: chunks, Edges: edges,
		SourceStates:    sortedSourceStates(sourceStates),
		DirectoryStates: sortedSourceStates(directoryStates),
		Fingerprint:     fingerprint, Diagnostics: diagnostics,
	}, nil
}

func SensitiveContentReason(value string) string {
	switch {
	case privateKeyRE.MatchString(value):
		return "private-key content signature detected"
	case credentialAssignmentRE.MatchString(value):
		return "credential-assignment content signature detected"
	case knownTokenRE.MatchString(value):
		return "access-token content signature detected"
	default:
		return ""
	}
}

func stateFromInfo(path string, info os.FileInfo) SourceState {
	return SourceState{
		Path: filepath.ToSlash(path), Exists: true, IsDir: info.IsDir(),
		Size: info.Size(), MtimeNS: info.ModTime().UnixNano(),
	}
}

func sortedSourceStates(states map[string]SourceState) []SourceState {
	paths := make([]string, 0, len(states))
	for path := range states {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]SourceState, 0, len(paths))
	for _, path := range paths {
		out = append(out, states[path])
	}
	return out
}

func titleFromMarkdown(body, path string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		if match := headingRE.FindStringSubmatch(scanner.Text()); match != nil {
			return strings.TrimSpace(match[2])
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

type section struct {
	start   int
	end     int
	heading string
}

func ChunkMarkdown(document SourceDocument) []Chunk {
	body := strings.ReplaceAll(document.Content, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	sections := []section{}
	stack := make([]string, 6)
	currentStart := 0
	currentHeading := document.Title
	for index, line := range lines {
		match := headingRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if index > currentStart {
			sections = append(sections, section{currentStart, index - 1, currentHeading})
		}
		level := len(match[1])
		stack[level-1] = strings.TrimSpace(match[2])
		for i := level; i < len(stack); i++ {
			stack[i] = ""
		}
		ancestry := []string{}
		for _, heading := range stack {
			if heading != "" {
				ancestry = append(ancestry, heading)
			}
		}
		currentStart = index
		currentHeading = strings.Join(ancestry, " > ")
	}
	sections = append(sections, section{currentStart, len(lines) - 1, currentHeading})

	// The document's claim, confidence, verification date and supersession
	// status appear only in its opening section. Every later chunk would
	// otherwise reach a caller stripped of them, so a passage from a corrected
	// document could be served with no sign that a correction exists.
	// A drafter report's header lives under different names than a truth
	// document's - VERDICT rather than Claim, OVERALL CONFIDENCE rather than
	// Confidence - and carries a review disposition instead of a verification
	// date. Same renderer, same cap, different extractor.
	var prelude string
	switch document.Tier {
	case "draft", "campaign":
		prelude = ExtractReportPrelude(
			body, document.Path, document.Tier == "campaign").Render()
	default:
		prelude = ExtractDocumentPrelude(body, document.Path).Render()
	}

	chunks := []Chunk{}
	for _, sec := range sections {
		for _, span := range splitSection(lines, sec.start, sec.end, 2200) {
			content := strings.Join(lines[span[0]:span[1]+1], "\n")
			hash := SHA256String(content)
			chunks = append(chunks, Chunk{
				ID: StableID("chunk", document.Path, sec.heading,
					fmt.Sprintf("%d", span[0]+1), fmt.Sprintf("%d", span[1]+1), hash),
				DocumentID: document.ID, Path: document.Path, Tier: document.Tier,
				Heading: sec.heading, StartLine: span[0] + 1, EndLine: span[1] + 1,
				Content: content, ContentHash: hash,
			})
		}
	}
	// Chunk 0 already contains the header verbatim, so attaching the prelude
	// there would duplicate it and spend budget saying nothing new.
	for index := range chunks {
		if index == 0 || prelude == "" {
			continue
		}
		chunks[index].Context = prelude
		chunks[index].ContextHash = SHA256String(prelude)
	}
	for index := range chunks {
		if index > 0 {
			chunks[index].PreviousID = chunks[index-1].ID
		}
		if index+1 < len(chunks) {
			chunks[index].NextID = chunks[index+1].ID
		}
	}
	firstByHeading := map[string]string{}
	for index := range chunks {
		if first, ok := firstByHeading[chunks[index].Heading]; ok {
			chunks[index].ParentID = first
		} else {
			firstByHeading[chunks[index].Heading] = chunks[index].ID
		}
	}
	return chunks
}

func splitSection(lines []string, start, end, maxBytes int) [][2]int {
	if start > end {
		return nil
	}
	spans := [][2]int{}
	chunkStart := start
	size := 0
	inFence := false
	for i := start; i <= end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
		lineBytes := len([]byte(line)) + 1
		if size > 0 && size+lineBytes > maxBytes && !inFence {
			breakAt := i - 1
			if breakAt >= chunkStart {
				spans = append(spans, [2]int{chunkStart, breakAt})
			}
			chunkStart = i
			size = 0
		}
		size += lineBytes
		if isFence {
			inFence = !inFence
		}
	}
	if chunkStart <= end {
		spans = append(spans, [2]int{chunkStart, end})
	}
	return spans
}

func BuildGraphEdges(documents []SourceDocument, chunks []Chunk) []GraphEdge {
	firstByPath := map[string]string{}
	chunksByDoc := map[string][]Chunk{}
	for _, chunk := range chunks {
		chunksByDoc[chunk.Path] = append(chunksByDoc[chunk.Path], chunk)
		if firstByPath[chunk.Path] == "" {
			firstByPath[chunk.Path] = chunk.ID
		}
	}
	edges := []GraphEdge{}
	seen := map[string]bool{}
	add := func(source, target, kind string) {
		if source == "" || target == "" || source == target {
			return
		}
		key := source + "\x00" + target + "\x00" + kind
		if !seen[key] {
			seen[key] = true
			edges = append(edges, GraphEdge{source, target, kind})
		}
	}
	for _, chunk := range chunks {
		add(chunk.ID, chunk.PreviousID, "adjacent")
		add(chunk.ID, chunk.NextID, "adjacent")
		add(chunk.ID, chunk.ParentID, "section")
		for _, match := range linkRE.FindAllStringSubmatch(chunk.Content, -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(chunk.Path), filepath.FromSlash(target))))
			add(chunk.ID, firstByPath[resolved], "link")
		}
		// Supersession is a relationship between documents, not a property of
		// one. A claim that moved to a replacement leaves the old document
		// still retrievable, so the edge is what lets a caller be told the
		// document it is reading has been retired.
		for _, line := range strings.Split(chunk.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			lower := strings.ToLower(trimmed)
			kind := ""
			switch {
			case strings.HasPrefix(lower, "depends-on:"):
				kind = "depends-on"
			case strings.HasPrefix(lower, "**superseded-by:**"),
				strings.HasPrefix(lower, "superseded-by:"):
				kind = "superseded-by"
			case strings.HasPrefix(lower, "**supersedes:**"),
				strings.HasPrefix(lower, "supersedes:"):
				kind = "supersedes"
			default:
				continue
			}
			target := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
			target = strings.Trim(target, "*` ")
			target = strings.SplitN(target, "#", 2)[0]
			target = strings.SplitN(target, "@", 2)[0]
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(chunk.Path), filepath.FromSlash(target))))
			add(chunk.ID, firstByPath[resolved], kind)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Kind < edges[j].Kind
	})
	return edges
}

func extractTerms(chunk Chunk) []string {
	// The prelude carries the document's claim sentence, which is its best
	// natural-language summary. Indexing it makes every chunk findable by what
	// its document asserts, not only by the prose that happens to fall inside
	// that chunk's line range.
	fields := strings.FieldsFunc(chunk.Path+"\n"+chunk.Heading+"\n"+
		chunk.Context+"\n"+chunk.Content, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	terms := []string{}
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if len(field) >= 3 && len(field) <= 200 {
			terms = append(terms, field)
		}
	}
	return SortedUnique(terms)
}

func termTrigrams(value string) []string {
	value = strings.ToLower(value)
	if len(value) < 3 {
		return nil
	}
	values := []string{}
	for index := 0; index+3 <= len(value); index++ {
		values = append(values, value[index:index+3])
	}
	return SortedUnique(values)
}

func normalizeNewlines(data []byte) []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
}
