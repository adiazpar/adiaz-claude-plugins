package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type ExactReadRequest struct {
	Selector    string `json:"selector"`
	Value       string `json:"value"`
	CampaignID  string `json:"campaignId,omitempty"`
	StartLine   int    `json:"startLine,omitempty"`
	EndLine     int    `json:"endLine,omitempty"`
	TokenBudget int    `json:"tokenBudget,omitempty"`
	byteRange   bool
	startByte   int
	endByte     int
	pathHandle  string
}

type ExactReadResponse struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Selector        string `json:"selector"`
	Handle          string `json:"handle"`
	Path            string `json:"path,omitempty"`
	RecordID        string `json:"recordId,omitempty"`
	Revision        int64  `json:"revision,omitempty"`
	SHA256          string `json:"sha256"`
	StartLine       int    `json:"startLine,omitempty"`
	EndLine         int    `json:"endLine,omitempty"`
	ByteRange       bool   `json:"byteRange,omitempty"`
	StartByte       int    `json:"startByte,omitempty"`
	EndByte         int    `json:"endByte,omitempty"`
	Content         string `json:"content"`
	EstimatedTokens int    `json:"estimatedTokens"`
	Truncated       bool   `json:"truncated"`
	Digest          string `json:"digest"`
}

func (service *Service) ReadExact(ctx context.Context, request ExactReadRequest) (ExactReadResponse, error) {
	if service == nil {
		return ExactReadResponse{}, errors.New("service is required")
	}
	if err := ctx.Err(); err != nil {
		return ExactReadResponse{}, err
	}
	if !validOne(request.Selector, "record", "finding", "evidence", "report", "path", "chunk", "uri") ||
		strings.TrimSpace(request.Value) == "" {
		return ExactReadResponse{}, errors.New("read requires a supported selector and nonempty value")
	}
	if request.TokenBudget == 0 {
		request.TokenBudget = 1500
	}
	if request.TokenBudget < 128 || request.TokenBudget > 8192 {
		return ExactReadResponse{}, errors.New("read token budget must be between 128 and 8192")
	}
	if request.StartLine < 0 || request.EndLine < 0 ||
		(request.EndLine > 0 && request.EndLine < request.StartLine) {
		return ExactReadResponse{}, errors.New("read line range is invalid")
	}
	if request.Selector == "path" {
		parsed, err := parseExactPathHandle(request.Value)
		if err != nil {
			return ExactReadResponse{}, err
		}
		if parsed.byteRange || parsed.startLine > 0 {
			if request.StartLine != 0 || request.EndLine != 0 {
				return ExactReadResponse{}, errors.New("read path handle range cannot be combined with startLine/endLine")
			}
			request.StartLine, request.EndLine = parsed.startLine, parsed.endLine
			request.byteRange, request.startByte, request.endByte =
				parsed.byteRange, parsed.startByte, parsed.endByte
		}
		request.Value, request.pathHandle = parsed.path, parsed.canonical
	}
	if validOne(request.Selector, "path", "chunk", "uri") {
		return service.readIndexedExact(ctx, request)
	}
	store := NewStateStoreWithBoundary(service.Boundary)
	switch request.Selector {
	case "record":
		path := strings.TrimPrefix(request.Value, "record:")
		body, handle, err := store.ReadCanonicalRecord(path)
		if err != nil {
			return ExactReadResponse{}, err
		}
		return sealExactReadResponse(ExactReadResponse{
			SchemaVersion: CampaignSchemaVersion, Selector: request.Selector,
			Handle: "record:" + path, Path: path, RecordID: handle.RecordID,
			Revision: handle.Revision, SHA256: handle.ContentDigest, Content: string(body),
		}, request.TokenBudget)
	case "finding":
		id := strings.TrimPrefix(request.Value, "finding:")
		graph, finding, err := locateFinding(store, request.CampaignID, id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return service.readIndexedFindingExact(ctx, id, request.CampaignID, request.TokenBudget)
			}
			return ExactReadResponse{}, err
		}
		path := finding.Path
		if path == "" {
			path = "active/" + graph.Campaign.Slug + "/findings/" + finding.ID + ".md"
		}
		body, handle, err := store.ReadCanonicalRecord(path)
		if err != nil {
			return ExactReadResponse{}, err
		}
		return sealExactReadResponse(ExactReadResponse{
			SchemaVersion: CampaignSchemaVersion, Selector: request.Selector,
			Handle: FindingHandle(finding.ID), Path: path, RecordID: finding.ID,
			Revision: finding.Revision, SHA256: handle.ContentDigest, Content: string(body),
		}, request.TokenBudget)
	case "evidence":
		graph, finding, evidence, err := locateEvidence(store, request.CampaignID, request.Value)
		_ = graph
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return service.readIndexedEvidenceExact(ctx, request.Value, request.CampaignID, request.TokenBudget)
			}
			return ExactReadResponse{}, err
		}
		return service.readEvidenceExact(finding, evidence, request.TokenBudget)
	case "report":
		graph, run, err := locateRun(store, request.CampaignID, strings.TrimPrefix(request.Value, "run:"))
		_ = graph
		if err != nil {
			return ExactReadResponse{}, err
		}
		if run.Report == nil {
			return ExactReadResponse{}, fmt.Errorf("run %s has no report", run.ID)
		}
		return service.readFileHandleExact("report", "run:"+run.ID, *run.Report, 0, 0, request.TokenBudget)
	default:
		return ExactReadResponse{}, errors.New("unsupported exact read")
	}
}

func (service *Service) readIndexedFindingExact(
	ctx context.Context,
	findingID string,
	campaignID string,
	budget int,
) (ExactReadResponse, error) {
	if !findingIDRE.MatchString(findingID) {
		return ExactReadResponse{}, errors.New("finding handle is invalid")
	}
	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		return ExactReadResponse{}, err
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		return ExactReadResponse{}, err
	}
	defer db.Close()
	statement := `SELECT d.path,d.content_hash FROM findings f
		JOIN documents d ON d.id=f.document_id WHERE f.id=?`
	args := []any{findingID}
	if campaignID != "" {
		statement += ` AND f.campaign_id=?`
		args = append(args, campaignID)
	}
	var path, indexedHash string
	if err := db.QueryRowContext(ctx, statement, args...).Scan(&path, &indexedHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExactReadResponse{}, os.ErrNotExist
		}
		return ExactReadResponse{}, err
	}
	body, currentHash, err := ReadProjectFile(service.Boundary, path)
	if err != nil {
		return ExactReadResponse{}, err
	}
	if currentHash != indexedHash {
		return ExactReadResponse{}, errors.New("exact finding source changed after indexing")
	}
	document, err := ParseFindingDocument(body, path)
	if err != nil {
		return ExactReadResponse{}, err
	}
	return sealExactReadResponse(ExactReadResponse{
		SchemaVersion: CampaignSchemaVersion, Selector: "finding",
		Handle: FindingHandle(document.Record.ID), Path: path,
		RecordID: document.Record.ID, Revision: document.Record.Revision,
		SHA256: "sha256:" + currentHash, Content: string(body),
	}, budget)
}

func (service *Service) readIndexedEvidenceExact(
	ctx context.Context,
	handle string,
	campaignID string,
	budget int,
) (ExactReadResponse, error) {
	parts := strings.Split(handle, ":")
	if len(parts) != 3 || parts[0] != "evidence" ||
		!findingIDRE.MatchString(parts[1]) || len(parts[2]) != 20 {
		return ExactReadResponse{}, errors.New("evidence handle is invalid")
	}
	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		return ExactReadResponse{}, err
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(generation.Database))
	if err != nil {
		return ExactReadResponse{}, err
	}
	defer db.Close()
	statement := `SELECT e.finding_id,e.path,e.sha256,e.start_line,e.end_line,
		e.object_key,e.source_run FROM finding_evidence e
		JOIN findings f ON f.id=e.finding_id WHERE e.handle=?`
	args := []any{handle}
	if campaignID != "" {
		statement += ` AND f.campaign_id=?`
		args = append(args, campaignID)
	}
	var findingID string
	var evidence EvidenceReference
	if err := db.QueryRowContext(ctx, statement, args...).Scan(
		&findingID, &evidence.Path, &evidence.SHA256, &evidence.StartLine,
		&evidence.EndLine, &evidence.ObjectKey, &evidence.SourceRun,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExactReadResponse{}, os.ErrNotExist
		}
		return ExactReadResponse{}, err
	}
	if EvidenceHandle(findingID, evidence) != handle {
		return ExactReadResponse{}, errors.New("indexed evidence handle is inconsistent")
	}
	return service.readEvidenceExact(FindingRecord{ID: findingID}, evidence, budget)
}

func (service *Service) readIndexedExact(ctx context.Context, request ExactReadRequest) (ExactReadResponse, error) {
	options := ReadOptions{
		StartLine: request.StartLine, EndLine: request.EndLine,
		ByteRange: request.byteRange, StartByte: request.startByte, EndByte: request.endByte,
	}
	switch request.Selector {
	case "path":
		options.Path = request.Value
	case "chunk":
		options.ChunkID = request.Value
	case "uri":
		options.URI = request.Value
	}
	value, err := service.Read(ctx, options)
	if err != nil {
		return ExactReadResponse{}, err
	}
	passage, passageOK := value["passage"].(string)
	citation, citationOK := value["citation"].(Citation)
	if !passageOK || !citationOK || citation.Path == "" {
		return ExactReadResponse{}, errors.New(
			"indexed exact read returned an incomplete passage or citation")
	}
	sourceDigest := citation.SourceHash
	if !strings.HasPrefix(sourceDigest, "sha256:") {
		sourceDigest = "sha256:" + sourceDigest
	}
	if !digestRE.MatchString(sourceDigest) {
		return ExactReadResponse{}, errors.New(
			"indexed exact read returned an invalid source digest")
	}
	handle := request.Selector + ":" + request.Value
	if request.Selector == "path" {
		handle = request.pathHandle
		if handle == "" {
			handle = "path:" + request.Value
		}
	}
	return sealExactReadResponse(ExactReadResponse{
		SchemaVersion: CampaignSchemaVersion, Selector: request.Selector,
		Handle: handle, Path: citation.Path, SHA256: sourceDigest,
		StartLine: citation.StartLine, EndLine: citation.EndLine,
		ByteRange: citation.ByteRange, StartByte: citation.StartByte, EndByte: citation.EndByte,
		Content: passage,
	}, request.TokenBudget)
}

type parsedExactPathHandle struct {
	path               string
	canonical          string
	startLine, endLine int
	byteRange          bool
	startByte, endByte int
}

// parseExactPathHandle is the single parser for public path handles. A plain
// canonical path remains accepted for compatibility; emitted `path:` handles
// and their #L/#B fragments can be passed back verbatim.
func parseExactPathHandle(value string) (parsedExactPathHandle, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return parsedExactPathHandle{}, errors.New("path handle is empty or contains surrounding whitespace")
	}
	value = strings.TrimPrefix(value, "path:")
	if strings.Contains(value, ":") {
		return parsedExactPathHandle{}, errors.New("path handle uses an unsupported scheme")
	}
	pathValue, fragment := value, ""
	if marker := strings.LastIndex(value, "#"); marker >= 0 {
		candidate := value[marker+1:]
		if len(candidate) >= 4 && (candidate[0] == 'L' || candidate[0] == 'B') &&
			strings.Contains(candidate, "-"+candidate[:1]) {
			pathValue, fragment = value[:marker], candidate
		}
	}
	if err := validateRelativeRecordPath(pathValue); err != nil {
		return parsedExactPathHandle{}, err
	}
	parsed := parsedExactPathHandle{path: pathValue, canonical: "path:" + pathValue}
	if fragment == "" {
		return parsed, nil
	}
	if len(fragment) < 4 || (fragment[0] != 'L' && fragment[0] != 'B') {
		return parsedExactPathHandle{}, errors.New("path handle fragment must be #L<start>-L<end> or #B<start>-B<end>")
	}
	prefix := fragment[:1]
	parts := strings.Split(fragment, "-"+prefix)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], prefix) {
		return parsedExactPathHandle{}, errors.New("path handle range fragment is invalid")
	}
	start, startErr := strconv.Atoi(strings.TrimPrefix(parts[0], prefix))
	end, endErr := strconv.Atoi(parts[1])
	if startErr != nil || endErr != nil {
		return parsedExactPathHandle{}, errors.New("path handle range is not numeric")
	}
	if prefix == "L" {
		if start < 1 || end < start {
			return parsedExactPathHandle{}, errors.New("path handle line range is invalid")
		}
		parsed.startLine, parsed.endLine = start, end
	} else {
		if start < 0 || end <= start {
			return parsedExactPathHandle{}, errors.New("path handle byte range is invalid")
		}
		parsed.byteRange, parsed.startByte, parsed.endByte = true, start, end
	}
	parsed.canonical = "path:" + pathValue + "#" + fragment
	return parsed, nil
}

func formatExactPathHandle(
	pathValue string,
	byteRange bool,
	startLine, endLine, startByte, endByte int,
) string {
	if byteRange {
		return fmt.Sprintf("path:%s#B%d-B%d", pathValue, startByte, endByte)
	}
	if startLine > 0 && endLine >= startLine {
		return fmt.Sprintf("path:%s#L%d-L%d", pathValue, startLine, endLine)
	}
	return "path:" + pathValue
}

func (service *Service) readEvidenceExact(
	finding FindingRecord,
	evidence EvidenceReference,
	budget int,
) (ExactReadResponse, error) {
	return service.readFileHandleExact(
		"evidence", EvidenceHandle(finding.ID, evidence),
		FileHandle{Path: evidence.Path, SHA256: evidence.SHA256},
		evidence.StartLine, evidence.EndLine, budget,
	)
}

func (service *Service) readFileHandleExact(
	selector, handle string,
	file FileHandle,
	start, end, budget int,
) (ExactReadResponse, error) {
	if IsForbiddenSource(file.Path) {
		return ExactReadResponse{}, errors.New("exact handle points into an excluded or sensitive source class")
	}
	absolute, err := service.Boundary.Resolve(file.Path, true)
	if err != nil {
		return ExactReadResponse{}, err
	}
	body, err := os.ReadFile(absolute)
	if err != nil {
		return ExactReadResponse{}, err
	}
	got := SHA256Bytes(body)
	want := strings.TrimPrefix(file.SHA256, "sha256:")
	if got != want {
		return ExactReadResponse{}, errors.New("exact handle source digest changed")
	}
	content := string(body)
	if start > 0 || end > 0 {
		if start == 0 {
			start = 1
		}
		if end == 0 {
			end = start
		}
		content, err = lineRangeBody(body, start, end)
		if err != nil {
			return ExactReadResponse{}, err
		}
	}
	return sealExactReadResponse(ExactReadResponse{
		SchemaVersion: CampaignSchemaVersion, Selector: selector, Handle: handle,
		Path: file.Path, SHA256: "sha256:" + got, StartLine: start, EndLine: end,
		Content: content,
	}, budget)
}

func sealExactReadResponse(response ExactReadResponse, budget int) (ExactReadResponse, error) {
	if response.SchemaVersion != CampaignSchemaVersion || response.Selector == "" ||
		response.Handle == "" || !digestRE.MatchString(response.SHA256) {
		return ExactReadResponse{}, errors.New("exact read response is incomplete")
	}
	for {
		response.Digest = ""
		response.EstimatedTokens = 0
		body, err := json.Marshal(response)
		if err != nil {
			return ExactReadResponse{}, err
		}
		cost := EstimateTokens(string(body))
		response.EstimatedTokens = cost
		if cost <= budget {
			break
		}
		if len(response.Content) < 64 {
			return ExactReadResponse{}, errors.New("read token budget is too small for mandatory metadata")
		}
		cut := len(response.Content) * 3 / 4
		for cut > 0 && !utf8Boundary(response.Content, cut) {
			cut--
		}
		response.Content = response.Content[:cut]
		response.Truncated = true
	}
	for iteration := 0; iteration < 4; iteration++ {
		response.Digest = ""
		digest, err := CanonicalDigest(response)
		if err != nil {
			return ExactReadResponse{}, err
		}
		response.Digest = digest
		body, _ := json.Marshal(response)
		cost := EstimateTokens(string(body))
		if cost == response.EstimatedTokens {
			return response, nil
		}
		response.EstimatedTokens = cost
	}
	if response.EstimatedTokens > budget {
		return ExactReadResponse{}, errors.New("read response exceeded token budget after sealing")
	}
	return response, nil
}

func utf8Boundary(value string, index int) bool {
	return index == len(value) || index == 0 || (value[index]&0xc0) != 0x80
}

func locateFinding(store *StateStore, campaignID, findingID string) (CampaignGraph, FindingRecord, error) {
	if !findingIDRE.MatchString(findingID) {
		return CampaignGraph{}, FindingRecord{}, errors.New("finding handle is invalid")
	}
	graphs, err := candidateGraphs(store, campaignID)
	if err != nil {
		return CampaignGraph{}, FindingRecord{}, err
	}
	var found *FindingRecord
	var foundGraph CampaignGraph
	for _, graph := range graphs {
		if finding, ok := graph.Findings[findingID]; ok {
			copy := finding
			if found != nil {
				return CampaignGraph{}, FindingRecord{}, errors.New("finding id is not unique across campaigns")
			}
			found, foundGraph = &copy, graph
		}
	}
	if found == nil {
		return CampaignGraph{}, FindingRecord{}, os.ErrNotExist
	}
	return foundGraph, *found, nil
}

func locateRun(store *StateStore, campaignID, runID string) (CampaignGraph, RunRecord, error) {
	if !runIDRE.MatchString(runID) {
		return CampaignGraph{}, RunRecord{}, errors.New("run handle is invalid")
	}
	graphs, err := candidateGraphs(store, campaignID)
	if err != nil {
		return CampaignGraph{}, RunRecord{}, err
	}
	var found *RunRecord
	var foundGraph CampaignGraph
	for _, graph := range graphs {
		if run, ok := graph.Runs[runID]; ok {
			copy := run
			if found != nil {
				return CampaignGraph{}, RunRecord{}, errors.New("run id is not unique across campaigns")
			}
			found, foundGraph = &copy, graph
		}
	}
	if found == nil {
		return CampaignGraph{}, RunRecord{}, os.ErrNotExist
	}
	return foundGraph, *found, nil
}

func locateEvidence(
	store *StateStore,
	campaignID, handle string,
) (CampaignGraph, FindingRecord, EvidenceReference, error) {
	parts := strings.Split(handle, ":")
	if len(parts) != 3 || parts[0] != "evidence" || !findingIDRE.MatchString(parts[1]) {
		return CampaignGraph{}, FindingRecord{}, EvidenceReference{}, errors.New("evidence handle is invalid")
	}
	graph, finding, err := locateFinding(store, campaignID, parts[1])
	if err != nil {
		return CampaignGraph{}, FindingRecord{}, EvidenceReference{}, err
	}
	for _, evidence := range finding.Evidence {
		if EvidenceHandle(finding.ID, evidence) == handle {
			return graph, finding, evidence, nil
		}
	}
	return CampaignGraph{}, FindingRecord{}, EvidenceReference{}, os.ErrNotExist
}

func candidateGraphs(store *StateStore, campaignID string) ([]CampaignGraph, error) {
	if campaignID != "" {
		graph, err := store.LoadCampaignGraph(campaignID)
		if err != nil {
			return nil, err
		}
		return []CampaignGraph{graph}, nil
	}
	campaigns, err := store.ListCampaigns()
	if err != nil {
		return nil, err
	}
	sort.Slice(campaigns, func(i, j int) bool { return campaigns[i].ID < campaigns[j].ID })
	graphs := make([]CampaignGraph, 0, len(campaigns))
	for _, campaign := range campaigns {
		graph, err := store.LoadCampaignGraph(campaign.ID)
		if err != nil {
			return nil, err
		}
		graphs = append(graphs, graph)
	}
	return graphs, nil
}
