package knowledge

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
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

const maxChunkBytes = 1400

type SourceInventory struct {
	Documents       []SourceDocument
	Findings        []FindingDocument
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
	Path          string
	Tier          string
	Recursive     bool
	AutoShape     bool
	BaseOnly      string
	Pattern       string
	Enabled       bool
	SourceKind    string
	ExcludePrefix string
}

func DiscoverSources(boundary Boundary, settings KnowledgeSettings) (SourceInventory, error) {
	cutoverCache := map[string]bool{}
	classes := []sourceClass{
		{Path: ".re-discipline/project-profile.md", Tier: "profile", Enabled: true},
		{Path: "docs/INDEX.md", Tier: "navigation", Enabled: true},
		// Goals index the campaigns serving one outcome and where their
		// durable results went. They make no claims, so they carry no
		// epistemic weight and sit in navigation alongside the indexes. They
		// live in docs/ because a goal outlives its campaigns.
		{Path: "docs/goals", Tier: "navigation", Recursive: true, Enabled: true},
		{Path: "docs/truth", Tier: "truth", Recursive: true, Enabled: settings.Sources.Truth},
		// Promoted atomic findings retain their canonical record and exact
		// evidence handles under docs/truth/findings. The broad truth class above
		// still indexes their Markdown projection; this more-specific class marks
		// the same path as a typed finding for card retrieval.
		{
			Path: "docs/truth/findings", Tier: "truth", Recursive: true, Pattern: "F-*.md",
			SourceKind: "finding", Enabled: settings.Sources.Truth,
		},
		{
			Path: "docs/history", Tier: "history", Recursive: true,
			Enabled: settings.Sources.HistoryFindings, ExcludePrefix: "docs/history/campaigns/",
		},
		{Path: "docs/backlog", Tier: "backlog", Recursive: true, Enabled: settings.Sources.Backlog},
		// Closure-owned playbooks are durable procedural knowledge. They are
		// indexed separately from truth because a procedure is not an empirical
		// claim, but it must still be reachable after its source campaign retires.
		{Path: "docs/playbooks", Tier: "playbook", Recursive: true, Enabled: true},
		// Atomic finding files are indexed as a typed projection as well as
		// ordinary Markdown provenance. Their review/validity fields determine
		// the final retrieval class after strict parsing.
		{
			Path: "active", Tier: "provisional", Recursive: true, Pattern: "F-*.md",
			SourceKind: "finding", Enabled: settings.Sources.ActiveFindings,
		},
		// Closed campaign archives preserve normalized findings and raw run
		// provenance under distinct classes. Archive README prose is navigation,
		// not a substitute for the typed records, and is intentionally excluded.
		{
			Path: "docs/history/campaigns", Tier: "history", Recursive: true, Pattern: "F-*.md",
			SourceKind: "finding", Enabled: settings.Sources.HistoryFindings,
		},
		// Immutable run reports remain a lower-ranked provenance fallback until
		// the ratified normalized-beats-raw gate permits archive opt-in.
		{
			Path: "active", Tier: "archive", Recursive: true, BaseOnly: "report.md",
			SourceKind: "raw-report", Enabled: settings.Sources.ReportFallback,
		},
		{
			Path: "docs/history/campaigns", Tier: "archive", Recursive: true, BaseOnly: "report.md",
			SourceKind: "raw-report", Enabled: settings.Sources.ReportFallback,
		},
		{Path: ".re-discipline/memory/INDEX.md", Tier: "navigation", Enabled: settings.Sources.SharedMemory},
		{Path: ".re-discipline/memory/topics", Tier: "memory", Recursive: true, Enabled: settings.Sources.SharedMemory},
	}
	for _, additional := range settings.Sources.Additional {
		// Measurement receipts are excluded again at candidate admission by
		// IsForbiddenSource. That second boundary matters when an otherwise
		// valid broad parent class contains a measurements/ descendant.
		classes = append(classes, sourceClass{
			Path: additional.Path, Tier: additional.Tier, Pattern: additional.Pattern,
			AutoShape: true, Enabled: true,
		})
	}
	type candidate struct {
		Path       string
		Tier       string
		SourceKind string
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
				candidates[class.Path] = candidate{class.Path, class.Tier, class.SourceKind}
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
			if class.SourceKind == "finding" &&
				!validFindingSourcePath(boundary, path, cutoverCache) {
				return nil
			}
			if class.SourceKind == "raw-report" &&
				!validRawReportSourcePath(boundary, path, cutoverCache) {
				return nil
			}
			relative, err := boundary.Relative(path)
			if err != nil {
				diagnostics = append(diagnostics, path+": "+err.Error())
				return nil
			}
			if IsForbiddenSource(relative) {
				return nil
			}
			if class.ExcludePrefix != "" && strings.HasPrefix(relative, class.ExcludePrefix) {
				return nil
			}
			if entryInfo, infoErr := entry.Info(); infoErr == nil {
				sourceStates[relative] = stateFromInfo(relative, entryInfo)
			}
			tier := class.Tier
			if existing, duplicate := candidates[relative]; duplicate &&
				existing.Tier != tier {
				return fmt.Errorf(
					"source %s is assigned conflicting tiers %s and %s",
					relative, existing.Tier, tier)
			}
			candidates[relative] = candidate{relative, tier, class.SourceKind}
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
	findings := []FindingDocument{}
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
		candidate := candidates[path]
		var finding FindingDocument
		if candidate.SourceKind == "finding" {
			finding, err = ParseFindingDocument(body, path)
			if err != nil {
				diagnostics = append(diagnostics, path+": invalid finding: "+err.Error())
				continue
			}
			title = finding.Record.Subject
			candidate.Tier = FindingSourceClassAtPath(finding.Record, path)
			findings = append(findings, finding)
		}
		doc := SourceDocument{
			ID: StableID("doc", path, hash), Path: path, Tier: candidate.Tier,
			Title: title, Content: string(body), ContentHash: hash, Size: int64(len(body)),
			MtimeNS: sourceStates[path].MtimeNS, SourceKind: candidate.SourceKind,
		}
		if candidate.SourceKind == "finding" {
			doc.FindingID = finding.Record.ID
			doc.CampaignID = finding.Record.CampaignID
			doc.FindingClaim = finding.Record.Claim
			doc.EvidenceGrade = finding.Record.EvidenceGrade
			doc.ReviewState = finding.Record.ReviewState
			doc.Validity = finding.Record.Validity
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
		SourceKind  string `json:"sourceKind,omitempty"`
		FindingID   string `json:"findingId,omitempty"`
	}
	fingerprintDocuments := make([]fingerprintDocument, 0, len(documents))
	for _, document := range documents {
		fingerprintDocuments = append(fingerprintDocuments, fingerprintDocument{
			ID: document.ID, Path: document.Path, Tier: document.Tier,
			Title: document.Title, ContentHash: document.ContentHash, Size: document.Size,
			SourceKind: document.SourceKind, FindingID: document.FindingID,
		})
	}
	fingerprintInput := struct {
		Parser    string                `json:"parser"`
		Chunker   string                `json:"chunker"`
		Analyzer  string                `json:"analyzer"`
		Documents []fingerprintDocument `json:"documents"`
	}{ParserVersion, ChunkerVersion, IdentifierAnalyzerVersion, fingerprintDocuments}
	fingerprint, err := CanonicalDigest(fingerprintInput)
	if err != nil {
		return SourceInventory{}, err
	}
	return SourceInventory{
		Documents: documents, Findings: findings, Chunks: chunks, Edges: edges,
		SourceStates:    sortedSourceStates(sourceStates),
		DirectoryStates: sortedSourceStates(directoryStates),
		Fingerprint:     fingerprint, Diagnostics: diagnostics,
	}, nil
}

// Typed campaign sources are deliberately constrained to the 0.8 layout.
// A recursive basename match alone would silently admit legacy subagents/
// reports or nested lookalikes, weakening both the hard cutover and the
// provenance handle contract.
func validFindingSourcePath(boundary Boundary, absolute string, cutoverCache map[string]bool) bool {
	relative, err := boundary.Relative(absolute)
	if err != nil {
		return false
	}
	parts := strings.Split(relative, "/")
	if len(parts) == 4 && parts[0] == "docs" && parts[1] == "truth" &&
		parts[2] == "findings" {
		return findingIDRE.MatchString(strings.TrimSuffix(parts[3], filepath.Ext(parts[3])))
	}
	if len(parts) == 4 && parts[0] == "active" &&
		managedSlugRE.MatchString(parts[1]) && parts[2] == "findings" {
		return findingIDRE.MatchString(strings.TrimSuffix(parts[3], filepath.Ext(parts[3]))) &&
			activeCampaignSourcesVisible(boundary, parts[1], cutoverCache)
	}
	if len(parts) == 6 && parts[0] == "docs" && parts[1] == "history" &&
		parts[2] == "campaigns" && managedSlugRE.MatchString(parts[3]) &&
		parts[4] == "findings" {
		destination := strings.Join(parts[:4], "/")
		archiveRelative := strings.Join(parts[4:], "/")
		return findingIDRE.MatchString(strings.TrimSuffix(parts[5], filepath.Ext(parts[5]))) &&
			archiveCutoverPublished(boundary, destination, "", archiveRelative, cutoverCache)
	}
	return false
}

func validRawReportSourcePath(boundary Boundary, absolute string, cutoverCache map[string]bool) bool {
	relative, err := boundary.Relative(absolute)
	if err != nil {
		return false
	}
	parts := strings.Split(relative, "/")
	if len(parts) == 5 && parts[0] == "active" &&
		managedSlugRE.MatchString(parts[1]) && parts[2] == "runs" &&
		runIDRE.MatchString(parts[3]) && parts[4] == "report.md" {
		return activeCampaignSourcesVisible(boundary, parts[1], cutoverCache)
	}
	if len(parts) == 7 && parts[0] == "docs" && parts[1] == "history" &&
		parts[2] == "campaigns" && managedSlugRE.MatchString(parts[3]) &&
		parts[4] == "runs" && runIDRE.MatchString(parts[5]) &&
		parts[6] == "report.md" {
		return archiveCutoverPublished(
			boundary, strings.Join(parts[:4], "/"), "", strings.Join(parts[4:], "/"), cutoverCache)
	}
	return false
}

// Closure publishes the archive before it marks the active campaign closed.
// The final receipt and README are the cutover marker: before both exist,
// discovery keeps using active sources and ignores the staged archive copy.
// The marker is accepted only after every archived manifest file verifies and
// receipt truth digests bind the immutable projection inventory. After that
// hard gate, discovery uses the archive and retires the
// active copy. This preserves exactly one retrieval identity across every
// transaction seam and lets journal rollback restore the prior choice without
// moving source files.
func activeCampaignSourcesVisible(boundary Boundary, slug string, cutoverCache map[string]bool) bool {
	campaignPath := filepath.Join(boundary.Root, "active", slug, "campaign.json")
	body, err := readSingleLinkRegularFile(campaignPath)
	if err != nil {
		return true
	}
	var campaign CampaignRecord
	if decodeStrictJSON(body, &campaign) != nil || ValidateCampaign(campaign) != nil ||
		campaign.Slug != slug || campaign.Status != "closed" {
		return true
	}
	return !archiveCutoverPublished(boundary, campaign.ArchiveDestination, campaign.ID, "", cutoverCache)
}

func archiveCutoverPublished(
	boundary Boundary,
	destination, campaignID, requiredRelative string,
	cutoverCache map[string]bool,
) (published bool) {
	cacheKey := destination + "\x00" + campaignID + "\x00" + requiredRelative
	if cached, present := cutoverCache[cacheKey]; present {
		return cached
	}
	defer func() { cutoverCache[cacheKey] = published }()
	if validateArchiveDestination(destination) != nil {
		return false
	}
	manifestPath, err := boundary.Resolve(path.Join(destination, "manifest.json"), true)
	if err != nil {
		return false
	}
	manifestBody, err := readSingleLinkRegularFile(manifestPath)
	if err != nil {
		return false
	}
	var manifest ArchiveManifest
	if decodeStrictJSON(manifestBody, &manifest) != nil || ValidateArchiveManifest(manifest) != nil ||
		(campaignID != "" && manifest.CampaignID != campaignID) {
		return false
	}
	if requiredRelative != "" {
		if _, included := manifest.Files[requiredRelative]; !included {
			return false
		}
	}
	for relative, digest := range manifest.Files {
		absolute, resolveErr := boundary.Resolve(path.Join(destination, relative), true)
		if resolveErr != nil {
			return false
		}
		body, readErr := readSingleLinkRegularFile(absolute)
		if readErr != nil || "sha256:"+SHA256Bytes(body) != digest {
			return false
		}
	}
	receiptPath, err := boundary.Resolve(path.Join(destination, "closure", "receipt.json"), true)
	if err != nil {
		return false
	}
	receiptBody, err := readSingleLinkRegularFile(receiptPath)
	if err != nil {
		return false
	}
	var receipt ClosureReceipt
	if decodeStrictJSON(receiptBody, &receipt) != nil || ValidateClosureReceipt(receipt) != nil ||
		receipt.CampaignID != manifest.CampaignID || receipt.ArchiveDestination != destination ||
		receipt.ArchiveDigest != manifest.Digest || receipt.CoverageDigest != manifest.Coverage.Digest {
		return false
	}
	projectionDigests := map[string]bool{}
	for _, digest := range manifest.Projections {
		projectionDigests[digest] = true
	}
	for _, digest := range receipt.TruthDigests {
		if !projectionDigests[digest] {
			return false
		}
	}
	readmePath, err := boundary.Resolve(path.Join(destination, "README.md"), true)
	if err != nil {
		return false
	}
	_, err = readSingleLinkRegularFile(readmePath)
	if err != nil {
		return false
	}
	allowed := map[string]bool{
		"manifest.json": true, "closure/receipt.json": true, "README.md": true,
		"finalization/campaign.json": true, "finalization/closure-job.json": true,
		"finalization/request.json": true, "finalization/events/events.jsonl": true,
	}
	for relative := range manifest.Files {
		allowed[relative] = true
	}
	return verifyArchiveDirectoryInventory(boundary, destination, allowed, false) == nil
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
	var header DocumentPrelude
	switch {
	case document.SourceKind == "finding":
		header = ExtractFindingPrelude(document)
	case document.SourceKind == "raw-report":
		header = ExtractReportPrelude(body, document.Path)
	default:
		header = ExtractDocumentPrelude(body, document.Path)
	}
	prelude := header.Render()

	chunks := []Chunk{}
	lineOffsets := make([]int, len(lines))
	offset := 0
	for index, line := range lines {
		lineOffsets[index] = offset
		offset += len([]byte(line))
		if index+1 < len(lines) {
			offset++
		}
	}
	for _, sec := range sections {
		for _, span := range splitSectionBytes(lines, lineOffsets, sec.start, sec.end, maxChunkBytes) {
			hash := SHA256String(span.content)
			chunks = append(chunks, Chunk{
				ID: StableID("chunk", document.Path, sec.heading,
					fmt.Sprintf("%d", span.startLine+1), fmt.Sprintf("%d", span.endLine+1),
					fmt.Sprintf("%d", span.startByte), fmt.Sprintf("%d", span.endByte), hash),
				DocumentID: document.ID, Path: document.Path, Tier: document.Tier,
				Heading: sec.heading, StartLine: span.startLine + 1, EndLine: span.endLine + 1,
				ByteRange: span.byteRange, StartByte: span.startByte, EndByte: span.endByte,
				Content: span.content, ContentHash: hash,
			})
		}
	}
	// Chunk 0 already contains the header verbatim, so attaching the prelude
	// there would duplicate it and spend budget saying nothing new.
	//
	// That reasoning holds only for a header the document actually contains.
	// A raw report's provenance status is synthesized, so its first chunk must
	// carry the same label as every later chunk.
	firstChunkNeedsPrelude := strings.HasPrefix(header.Status, "UNNORMALIZED PROVENANCE")
	for index := range chunks {
		if prelude == "" || (index == 0 && !firstChunkNeedsPrelude) {
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

type chunkSourceSpan struct {
	startLine int
	endLine   int
	byteRange bool
	startByte int
	endByte   int
	content   string
}

// splitSectionBytes enforces maxBytes even for a single very long source line.
// Ordinary chunks retain the historical line-range citation. Split-line
// chunks carry an absolute half-open byte range over normalized source bytes.
func splitSectionBytes(lines []string, lineOffsets []int, start, end, maxBytes int) []chunkSourceSpan {
	if start > end || maxBytes < 1 {
		return nil
	}
	result := []chunkSourceSpan{}
	currentStart := -1
	currentEnd := -1
	current := []string{}
	currentBytes := 0
	flush := func() {
		if currentStart < 0 {
			return
		}
		result = append(result, chunkSourceSpan{
			startLine: currentStart, endLine: currentEnd,
			content: strings.Join(current, "\n"),
		})
		currentStart, currentEnd = -1, -1
		current = nil
		currentBytes = 0
	}
	for lineIndex := start; lineIndex <= end; lineIndex++ {
		line := lines[lineIndex]
		lineBytes := []byte(line)
		if len(lineBytes) > maxBytes {
			flush()
			for _, segment := range splitUTF8ByteRanges(lineBytes, maxBytes) {
				absoluteStart := lineOffsets[lineIndex] + segment[0]
				absoluteEnd := lineOffsets[lineIndex] + segment[1]
				result = append(result, chunkSourceSpan{
					startLine: lineIndex, endLine: lineIndex, byteRange: true,
					startByte: absoluteStart, endByte: absoluteEnd,
					content: string(lineBytes[segment[0]:segment[1]]),
				})
			}
			continue
		}
		additional := len(lineBytes)
		if len(current) > 0 {
			additional++
		}
		if len(current) > 0 && currentBytes+additional > maxBytes {
			flush()
			additional = len(lineBytes)
		}
		if currentStart < 0 {
			currentStart = lineIndex
		}
		currentEnd = lineIndex
		current = append(current, line)
		currentBytes += additional
	}
	flush()
	return result
}

func splitUTF8ByteRanges(value []byte, maxBytes int) [][2]int {
	result := [][2]int{}
	for start := 0; start < len(value); {
		end := start + maxBytes
		if end > len(value) {
			end = len(value)
		}
		for end > start && !utf8.Valid(value[start:end]) {
			end--
		}
		if end == start {
			_, size := utf8.DecodeRune(value[start:])
			end = start + size
		}
		result = append(result, [2]int{start, end})
		start = end
	}
	return result
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
			case strings.HasPrefix(lower, "contradicts:"):
				kind = "contradicts"
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
			resolved := resolveManagedReference(chunk.Path, target)
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

// resolveManagedReference accepts both ordinary document-relative references
// and canonical project-relative managed paths. Supersession metadata has
// historically used both forms; joining `docs/truth/new.md` to the referring
// document's directory would silently manufacture
// `docs/truth/docs/truth/new.md` and sever the edge.
func resolveManagedReference(sourcePath, target string) string {
	target = filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	for _, root := range []string{"docs/", "active/", ".re-discipline/"} {
		if strings.HasPrefix(target, root) {
			return target
		}
	}
	return filepath.ToSlash(filepath.Clean(
		filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(target)),
	))
}

func extractTerms(chunk Chunk) []string {
	// The prelude carries the document's claim sentence, which is its best
	// natural-language summary. Indexing it makes every chunk findable by what
	// its document asserts, not only by the prose that happens to fall inside
	// that chunk's line range.
	return IdentifierTerms(chunk.Path + "\n" + chunk.Heading + "\n" +
		chunk.Context + "\n" + chunk.Content)
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
