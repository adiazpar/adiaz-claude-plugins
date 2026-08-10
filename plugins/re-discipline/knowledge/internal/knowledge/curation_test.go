package knowledge

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFindingSubmissionJSONPreservesCanonicalDocument(t *testing.T) {
	document := testCurationPacket(t).Candidates[0]
	submission := FindingSubmission{
		Record:             document.Record,
		SyntheticQuestions: document.SyntheticQuestions, QuestionsReviewed: document.QuestionsReviewed,
	}
	body, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FindingSubmission
	if err := decodeStrict(body, &decoded); err != nil {
		t.Fatal(err)
	}
	decodedDocument := decoded.Document()
	wantDigest, wantErr := findingDocumentDigest(document)
	gotDigest, gotErr := findingDocumentDigest(decodedDocument)
	if wantErr != nil || gotErr != nil || wantDigest != gotDigest ||
		decodedDocument.Record.Body != document.Record.Body ||
		decodedDocument.Record.Path != document.Record.Path {
		t.Fatal("finding submission JSON dropped canonical body or path content")
	}
}

func testCurationPacket(t *testing.T) CurationPacket {
	t.Helper()
	reportPath := "active/resource-registration/runs/R-20260802-0042/report.md"
	reportDigest := "sha256:" + strings.Repeat("c", 64)
	candidates := []FindingDocument{}
	ids := []string{"F-0042", "F-0043", "F-0044"}
	for index, id := range ids {
		document := testFindingDocument()
		document.Record.ID = id
		document.Record.Path = "active/resource-registration/findings/" + id + ".md"
		document.Record.Claim = fmt.Sprintf("Atomic resource registration claim %d is scoped to the recorded build.", index+1)
		document.Record.Body = strings.Replace(document.Record.Body,
			"Resource registration uses the named table.", document.Record.Claim, 1)
		document.Record.Evidence = []EvidenceReference{{
			Path: reportPath, SHA256: reportDigest, StartLine: index + 1, EndLine: index + 1,
			ObjectKey: fmt.Sprintf("path:%s#L%d-L%d", reportPath, index+1, index+1),
			SourceRun: "R-20260802-0042",
		}}
		body, err := RenderFindingDocument(document)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseFindingDocument(body, document.Record.Path)
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, parsed)
	}
	meta := RecordMeta{
		SchemaVersion: CampaignSchemaVersion, ID: "I-0042",
		CreatedAt: "2026-08-02T19:30:00Z", UpdatedAt: "2026-08-02T19:30:00Z",
		Revision: 1, CreatedBy: "curator:test", UpdatedBy: "curator:test",
		Digest: "sha256:" + strings.Repeat("b", 64), CorrelationID: "corr-curation-42",
	}
	coverage := []CoverageEntry{}
	rows := []CurationRow{}
	triage := map[string]string{}
	for index, id := range ids {
		coverage = append(coverage, CoverageEntry{
			SourceHandle: fmt.Sprintf("path:%s#L%d-L%d", reportPath, index+1, index+1),
			SourcePath:   reportPath, SourceSHA256: reportDigest,
			StartLine: index + 1, EndLine: index + 1, SourceLineCount: len(ids),
			Disposition: "candidate-finding", TargetID: id,
		})
		rows = append(rows, CurationRow{FindingID: id, Triage: "routine"})
		triage[id] = "routine"
	}
	return CurationPacket{
		Intake: IntakeRecord{
			RecordMeta: meta, CampaignID: "C-RESOURCE-REGISTRATION",
			SourceRuns: []FileHandle{{
				Path: reportPath, SHA256: reportDigest,
			}},
			CandidateFindingIDs: ids, Coverage: coverage, Triage: triage, Status: "submitted",
		},
		Candidates: candidates, Rows: rows,
	}
}

func TestCurationPacketEnforcesCuratorBoundaryAndCoverage(t *testing.T) {
	packet := testCurationPacket(t)
	if err := ValidateCurationPacket("curator", packet); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurationPacket("manager", packet); err == nil {
		t.Fatal("manager self-declaration was accepted as a curator submission")
	}
	packet.Candidates[0].Record.ReviewState = "manager-ratified"
	// The refusal now names the two review states a curator may submit and the
	// transition that sets the others, so this pins the boundary it states
	// rather than the verb it used to lead with.
	if err := ValidateCurationPacket("curator", packet); err == nil ||
		!strings.Contains(err.Error(), `a curator may submit only "extracted" or "curator-checked"`) ||
		!strings.Contains(err.Error(), "manager_apply review.submit") {
		t.Fatalf("curator ratification was not rejected: %v", err)
	}
}

func TestCurationPacketSupportsSmallAndCoverageOnlyReviews(t *testing.T) {
	base := testCurationPacket(t)
	for _, count := range []int{1, 2} {
		packet := base
		packet.Candidates = append([]FindingDocument(nil), base.Candidates[:count]...)
		packet.Rows = append([]CurationRow(nil), base.Rows[:count]...)
		packet.Intake.CandidateFindingIDs = append([]string(nil), base.Intake.CandidateFindingIDs[:count]...)
		packet.Intake.Coverage = append([]CoverageEntry(nil), base.Intake.Coverage[:count]...)
		for index := range packet.Intake.Coverage {
			packet.Intake.Coverage[index].SourceLineCount = count
		}
		packet.Intake.Triage = map[string]string{}
		for _, id := range packet.Intake.CandidateFindingIDs {
			packet.Intake.Triage[id] = "routine"
		}
		if err := ValidateCurationPacket("curator", packet); err != nil {
			t.Fatalf("%d-candidate packet rejected: %v", count, err)
		}
	}

	coverageOnly := base
	coverageOnly.Candidates = nil
	coverageOnly.Rows = nil
	coverageOnly.Intake.CandidateFindingIDs = nil
	coverageOnly.Intake.Triage = map[string]string{}
	coverageOnly.Intake.Coverage = []CoverageEntry{{
		SourceHandle: "path:active/resource-registration/runs/R-20260802-0042/report.md#L1-L3",
		SourcePath:   "active/resource-registration/runs/R-20260802-0042/report.md",
		SourceSHA256: "sha256:" + strings.Repeat("c", 64),
		StartLine:    1, EndLine: 3, SourceLineCount: 3, Disposition: "non-claim",
	}}
	if err := ValidateCurationPacket("curator", coverageOnly); err != nil {
		t.Fatalf("coverage-only curator packet rejected: %v", err)
	}
	envelope := testReviewPacketEnvelope(t, coverageOnly)
	if err := ValidateReviewPacketEnvelope(envelope, coverageOnly); err != nil {
		t.Fatalf("zero-row manager packet rejected: %v", err)
	}
	review := testManagerReview(t, coverageOnly)
	if err := ValidateManagerReview("manager", coverageOnly, review); err != nil {
		t.Fatalf("coverage-only manager review rejected: %v", err)
	}
	intake, outcomes := testReviewOutcomes(coverageOnly, review)
	if len(outcomes) != 0 {
		t.Fatalf("coverage-only review invented finding outcomes: %#v", outcomes)
	}
	if err := ValidateManagerReviewOutcomes(coverageOnly, review, intake, outcomes); err != nil {
		t.Fatalf("coverage-only review outcome rejected: %v", err)
	}
}

func TestCurationIntakeAcceptsOnlyTypedSpawnedWorkProposals(t *testing.T) {
	packet := testCurationPacket(t)
	packet.Intake.SpawnedWorkItems = []string{"F-0042"}
	if err := ValidateCurationPacket("curator", packet); err == nil ||
		!strings.Contains(err.Error(), "spawned work proposals") {
		t.Fatalf("non-work proposal id was accepted: %v", err)
	}
}

func TestCuratorRunBindingRequiresTheExactCanonicalReturnedRun(t *testing.T) {
	graph := stateTestGraph()
	run := stateTestReturnedRun(2, "returned")
	run.Role, run.ActorID = "curator", "knowledge-curator"
	graph.Runs[run.ID] = run
	canonical, err := validateReturnedCuratorRunBinding(graph, run.ActorID, run)
	if err != nil || !reflect.DeepEqual(canonical, run) {
		t.Fatalf("exact returned curator run binding was rejected: %v", err)
	}

	mutated := run
	mutated.ResultSummary = "Caller-mutated result summary"
	if _, err := validateReturnedCuratorRunBinding(graph, run.ActorID, mutated); err == nil ||
		!strings.Contains(err.Error(), "exact binding") {
		t.Fatalf("mutated curator run binding was accepted: %v", err)
	}
	if _, err := validateReturnedCuratorRunBinding(graph, "another-curator", run); err == nil ||
		!strings.Contains(err.Error(), "exact binding") {
		t.Fatalf("another actor rebound the canonical curator run: %v", err)
	}
}

func TestCurationPacketRejectsCandidateCoverageOutsidePacket(t *testing.T) {
	packet := testCurationPacket(t)
	packet.Intake.Coverage[0].TargetID = "F-9999"
	if err := ValidateCurationPacket("curator", packet); err == nil ||
		!strings.Contains(err.Error(), "must target a finding in the intake") {
		t.Fatalf("candidate coverage escaped its packet: %v", err)
	}
}

func TestCurationPacketRequiresAnExhaustiveNonOverlappingSourcePartition(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CurationPacket)
		want   string
	}{
		{
			name: "gap",
			mutate: func(packet *CurationPacket) {
				packet.Intake.Coverage[1].StartLine = 3
				packet.Intake.Coverage[1].EndLine = 3
				packet.Intake.Coverage[1].SourceHandle = canonicalCoverageHandle(packet.Intake.Coverage[1])
				packet.Intake.Coverage[2].StartLine = 4
				packet.Intake.Coverage[2].EndLine = 4
				packet.Intake.Coverage[2].SourceHandle = canonicalCoverageHandle(packet.Intake.Coverage[2])
				for index := range packet.Intake.Coverage {
					packet.Intake.Coverage[index].SourceLineCount = 4
				}
			},
			want: "gap or overlap",
		},
		{
			name: "overlap",
			mutate: func(packet *CurationPacket) {
				packet.Intake.Coverage[1].StartLine = 1
				packet.Intake.Coverage[1].SourceHandle = canonicalCoverageHandle(packet.Intake.Coverage[1])
			},
			want: "gap or overlap",
		},
		{
			name: "unaccounted tail",
			mutate: func(packet *CurationPacket) {
				for index := range packet.Intake.Coverage {
					packet.Intake.Coverage[index].SourceLineCount = 4
				}
			},
			want: "ends at line 3, expected 4",
		},
		{
			name: "inconsistent line count",
			mutate: func(packet *CurationPacket) {
				packet.Intake.Coverage[1].SourceLineCount = 4
			},
			want: "disagrees on source line count",
		},
		{
			name: "noncanonical handle",
			mutate: func(packet *CurationPacket) {
				packet.Intake.Coverage[0].SourceHandle += "-forged"
			},
			want: "is not canonical",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := testCurationPacket(t)
			test.mutate(&packet)
			if err := ValidateCurationPacket("curator", packet); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid source partition was accepted: %v", err)
			}
		})
	}
}

func TestCurationPacketBindsCandidateEvidenceToExactTargetedSpans(t *testing.T) {
	packet := testCurationPacket(t)
	packet.Candidates[0].Record.Evidence[0].ObjectKey =
		"path:active/resource-registration/runs/R-20260802-0042/report.md#L2-L2"
	if err := ValidateCurationPacket("curator", packet); err == nil ||
		!strings.Contains(err.Error(), "evidence outside its exact coverage spans") {
		t.Fatalf("candidate evidence was allowed to escape its targeted span: %v", err)
	}

	packet = testCurationPacket(t)
	for index := range packet.Intake.Coverage {
		packet.Intake.Coverage[index].SourceLineCount = 4
	}
	packet.Intake.Coverage = append(packet.Intake.Coverage, CoverageEntry{
		SourceHandle: "path:active/resource-registration/runs/R-20260802-0042/report.md#L4-L4",
		SourcePath:   "active/resource-registration/runs/R-20260802-0042/report.md",
		SourceSHA256: "sha256:" + strings.Repeat("c", 64),
		StartLine:    4, EndLine: 4, SourceLineCount: 4,
		Disposition: "candidate-finding", TargetID: "F-0042",
	})
	if err := ValidateCurationPacket("curator", packet); err == nil ||
		!strings.Contains(err.Error(), "does not cite every exact coverage span") {
		t.Fatalf("candidate omitted a targeted coverage span: %v", err)
	}
}

func TestCurationPacketRequiresAttentionForConflicts(t *testing.T) {
	packet := testCurationPacket(t)
	packet.Candidates[0].Record.Relations.Contradicts = []string{"F-0099"}
	body, err := RenderFindingDocument(packet.Candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	packet.Candidates[0], err = ParseFindingDocument(body, packet.Candidates[0].Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurationPacket("curator", packet); err == nil || !strings.Contains(err.Error(), "attention") {
		t.Fatalf("conflicted routine row was accepted: %v", err)
	}
	packet.Rows[0].Triage = "attention"
	packet.Intake.Triage[packet.Rows[0].FindingID] = "attention"
	if err := ValidateCurationPacket("curator", packet); err != nil {
		t.Fatal(err)
	}
}

func testManagerReview(t *testing.T, packet CurationPacket) ReviewRecord {
	t.Helper()
	packetDigest := testReviewPacketEnvelope(t, packet).Digest
	decisions := []ReviewDecision{}
	for _, candidate := range packet.Candidates {
		decisions = append(decisions, ReviewDecision{
			FindingID: candidate.Record.ID, FindingRevision: candidate.Record.Revision,
			Action: "ratify", Projection: "campaign", Rationale: "reviewed exact evidence",
		})
	}
	review := ReviewRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: "V-0042",
			CreatedAt: "2026-08-02T19:40:00Z", UpdatedAt: "2026-08-02T19:40:00Z",
			Revision: 1, CreatedBy: "manager:test", UpdatedBy: "manager:test",
			Digest: "sha256:" + strings.Repeat("d", 64), CorrelationID: "corr-review-42",
		},
		CampaignID: packet.Intake.CampaignID, Reviewer: "manager:test", Authority: "manager",
		IntakeID: packet.Intake.ID, IntakeRevision: packet.Intake.Revision,
		PacketDigest: packetDigest,
		ReviewLoad:   stateTestReviewLoad("V-0042", packet.Intake.CampaignID, packetDigest, len(packet.Candidates), 0),
		Decisions:    decisions,
	}
	review.ReviewLoad.StartedAt = "2026-08-02T19:35:00Z"
	review.ReviewLoad.CompletedAt = "2026-08-02T19:40:00Z"
	review.ReviewLoad.DurationSeconds = 300
	if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
		t.Fatal(err)
	}
	return review
}

func testReviewPacketEnvelope(t *testing.T, packet CurationPacket) ReviewPacketEnvelope {
	t.Helper()
	packetCreatedAt := "2026-08-02T19:35:00Z"
	if base, err := time.Parse(time.RFC3339Nano, packet.Intake.UpdatedAt); err == nil {
		packetCreatedAt = base.Add(10 * time.Second).UTC().Format(time.RFC3339Nano)
	}
	envelope := ReviewPacketEnvelope{
		SchemaVersion: CampaignSchemaVersion, ID: "review-packet-I-0042-r1",
		CampaignID: packet.Intake.CampaignID, IntakeID: packet.Intake.ID,
		IntakeRevision: packet.Intake.Revision, CoverageComplete: true,
		CreatedAt: packetCreatedAt,
	}
	for _, candidate := range packet.Candidates {
		evidenceHandles := make([]string, 0, len(candidate.Record.Evidence))
		for _, evidence := range candidate.Record.Evidence {
			evidenceHandles = append(evidenceHandles, EvidenceHandle(candidate.Record.ID, evidence))
		}
		envelope.Rows = append(envelope.Rows, ReviewPacketRow{
			FindingID: candidate.Record.ID, FindingRevision: candidate.Record.Revision,
			Claim: candidate.Record.Claim, EvidenceGrade: candidate.Record.EvidenceGrade,
			Triage:     packet.Intake.Triage[candidate.Record.ID],
			Conflicted: findingIsConflicted(candidate.Record), Recommendation: "Review candidate",
			EvidenceHandles: evidenceHandles,
		})
	}
	var err error
	envelope.Digest, err = ReviewPacketDigest(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func testReviewOutcomes(packet CurationPacket, review ReviewRecord) (IntakeRecord, []FindingDocument) {
	intake := packet.Intake
	intake.Revision++
	intake.UpdatedAt, intake.UpdatedBy, intake.Status = "2026-08-02T19:40:00Z", "manager:test", "reviewed"
	outcomes := make([]FindingDocument, 0, len(packet.Candidates))
	for index, candidate := range packet.Candidates {
		outcome := candidate
		outcome.Record.Revision++
		outcome.Record.UpdatedAt, outcome.Record.UpdatedBy = intake.UpdatedAt, intake.UpdatedBy
		decision := review.Decisions[index]
		switch decision.Action {
		case "ratify":
			outcome.Record.ReviewState = "manager-ratified"
			if decision.Projection != "" {
				outcome.Record.Projection = decision.Projection
			}
		case "reject":
			outcome.Record.ReviewState, outcome.Record.Validity, outcome.Record.Projection = "manager-rejected", "invalid", "rejected"
		}
		outcomes = append(outcomes, outcome)
	}
	return intake, outcomes
}

func TestManagerReviewBindsEveryFindingRevisionAndIsImmutable(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(t, packet)
	if err := ValidateManagerReview("manager", packet, review); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManagerReview("curator", packet, review); err == nil {
		t.Fatal("curator was allowed to ratify")
	}
	missing := review
	missing.Decisions = append([]ReviewDecision(nil), review.Decisions[:2]...)
	if err := ValidateManagerReview("manager", packet, missing); err == nil {
		t.Fatal("partial review receipt was accepted")
	}
	invalidPacket := packet
	invalidPacket.Candidates = append([]FindingDocument(nil), packet.Candidates...)
	invalidPacket.Candidates[0].Record.Relations.Contradicts = []string{"F-0099"}
	body, err := RenderFindingDocument(invalidPacket.Candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	invalidPacket.Candidates[0], err = ParseFindingDocument(body, invalidPacket.Candidates[0].Record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManagerReview("manager", invalidPacket, review); err == nil || !strings.Contains(err.Error(), "attention") {
		t.Fatalf("manager review bypassed invalid curator triage: %v", err)
	}
	digest, err := ReviewReceiptDigest(review)
	if err != nil {
		t.Fatal(err)
	}
	review.Digest = digest
	identical := review
	identical.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	if err := VerifyImmutableReviewReceipt(review, identical); err != nil {
		t.Fatal(err)
	}
	invalidDigest := identical
	invalidDigest.Digest = "sha256:" + strings.Repeat("e", 64)
	if err := VerifyImmutableReviewReceipt(invalidDigest, invalidDigest); err == nil || !strings.Contains(err.Error(), "invalid content digest") {
		t.Fatalf("self-consistent but unsealed receipt was accepted: %v", err)
	}
	identical.Decisions[0].Rationale = "rewritten after publication"
	identical.Digest, err = ReviewReceiptDigest(identical)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyImmutableReviewReceipt(review, identical); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("mutated receipt was accepted: %v", err)
	}
}

func TestReviewPacketEnvelopeBindsDisplayedRowsAndPersistedTriage(t *testing.T) {
	packet := testCurationPacket(t)
	envelope := testReviewPacketEnvelope(t, packet)
	if err := ValidateReviewPacketEnvelope(envelope, packet); err != nil {
		t.Fatal(err)
	}
	tampered := envelope
	tampered.Rows = append([]ReviewPacketRow(nil), envelope.Rows...)
	tampered.Rows[0].Claim = "A different claim was displayed to the manager."
	tampered.Digest, _ = ReviewPacketDigest(tampered)
	if err := ValidateReviewPacketEnvelope(tampered, packet); err == nil || !strings.Contains(err.Error(), "persisted candidate") {
		t.Fatalf("packet display substitution was accepted: %v", err)
	}
	tampered = envelope
	tampered.Rows = append([]ReviewPacketRow(nil), envelope.Rows...)
	tampered.Rows[0].Triage = "attention"
	tampered.Digest, _ = ReviewPacketDigest(tampered)
	if err := ValidateReviewPacketEnvelope(tampered, packet); err == nil || !strings.Contains(err.Error(), "triage") {
		t.Fatalf("packet triage substitution was accepted: %v", err)
	}
	tampered = envelope
	tampered.Rows = append([]ReviewPacketRow(nil), envelope.Rows...)
	tampered.Rows[0].EvidenceHandles = append([]string(nil), envelope.Rows[0].EvidenceHandles...)
	tampered.Rows[0].EvidenceHandles[0] = "evidence:F-0042:00000000000000000000"
	tampered.Digest, _ = ReviewPacketDigest(tampered)
	if err := ValidateReviewPacketEnvelope(tampered, packet); err == nil ||
		!strings.Contains(err.Error(), "exact evidence handles") {
		t.Fatalf("packet evidence substitution was accepted: %v", err)
	}
}

func TestManagerReviewDecisionsRequireExactFindingOutcomes(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(t, packet)
	envelope := testReviewPacketEnvelope(t, packet)
	review.PacketDigest = envelope.Digest
	intake, outcomes := testReviewOutcomes(packet, review)
	if err := ValidateManagerReviewOutcomes(packet, review, intake, outcomes); err != nil {
		t.Fatal(err)
	}

	reject := review
	reject.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	reject.Decisions[0].Action, reject.Decisions[0].Projection = "reject", "rejected"
	if err := ValidateManagerReviewOutcomes(packet, reject, intake, outcomes); err == nil || !strings.Contains(err.Error(), "do not match reject") {
		t.Fatalf("reject receipt published a ratified finding: %v", err)
	}
	_, rejectedOutcomes := testReviewOutcomes(packet, reject)
	if err := ValidateManagerReviewOutcomes(packet, reject, intake, rejectedOutcomes); err != nil {
		t.Fatalf("valid reject outcome was rejected: %v", err)
	}

	omitted := append([]FindingDocument(nil), outcomes[1:]...)
	if err := ValidateManagerReviewOutcomes(packet, review, intake, omitted); err == nil || !strings.Contains(err.Error(), "no resulting finding revision") {
		t.Fatalf("decision without a finding outcome was accepted: %v", err)
	}

	wrongRevision := append([]FindingDocument(nil), outcomes...)
	wrongRevision[0].Record.Revision++
	if err := ValidateManagerReviewOutcomes(packet, review, intake, wrongRevision); err == nil || !strings.Contains(err.Error(), "advance reviewed revision") {
		t.Fatalf("decision was rebound to the wrong finding revision: %v", err)
	}

	attention := review
	attention.Decisions = append([]ReviewDecision(nil), review.Decisions...)
	attention.Decisions[0].Action = "challenge"
	if err := ValidateManagerReviewOutcomes(packet, attention, intake, outcomes); err == nil || !strings.Contains(err.Error(), "individual attention") {
		t.Fatalf("attention decision used a routine intake row: %v", err)
	}
}

func TestAppliedManagerReviewBindsDecisionsToCanonicalFindingRevision(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(t, packet)
	intake, outcomes := testReviewOutcomes(packet, review)
	candidates := map[string]FindingDocument{}
	for _, candidate := range packet.Candidates {
		candidates[candidate.Record.ID] = candidate
	}
	canonical := candidates[packet.Candidates[0].Record.ID]
	canonical.Record.Revision++
	candidates[canonical.Record.ID] = canonical
	if err := validateManagerReviewDocumentOutcomes(
		packet.Intake, candidates, review, intake, outcomes,
	); err == nil || !strings.Contains(err.Error(), "current finding revision") {
		t.Fatalf("review packet was allowed to substitute a stale canonical finding: %v", err)
	}
}

func TestManagerReviewCannotSubstitutePacketBoundFindingContent(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(t, packet)
	intake, outcomes := testReviewOutcomes(packet, review)
	tests := []struct {
		name   string
		mutate func(*FindingDocument)
	}{
		{name: "claim", mutate: func(outcome *FindingDocument) { outcome.Record.Claim = "A substituted claim." }},
		{name: "evidence", mutate: func(outcome *FindingDocument) {
			outcome.Record.Evidence[0].StartLine++
		}},
		{name: "source run", mutate: func(outcome *FindingDocument) {
			outcome.Record.SourceRuns = []string{"R-20260802-9999"}
		}},
		{name: "relations", mutate: func(outcome *FindingDocument) {
			outcome.Record.Relations.Supports = []string{"F-9999"}
		}},
		{name: "body", mutate: func(outcome *FindingDocument) {
			outcome.Record.Body += "\nSubstituted review content."
		}},
		{name: "synthetic questions", mutate: func(outcome *FindingDocument) {
			outcome.SyntheticQuestions[0] = "What substituted question was introduced?"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tampered := append([]FindingDocument(nil), outcomes...)
			tampered[0].Record.Evidence = append([]EvidenceReference(nil), outcomes[0].Record.Evidence...)
			tampered[0].Record.SourceRuns = append([]string(nil), outcomes[0].Record.SourceRuns...)
			tampered[0].Record.Relations = outcomes[0].Record.Relations
			tampered[0].Record.Relations.Supports = append([]string(nil), outcomes[0].Record.Relations.Supports...)
			tampered[0].SyntheticQuestions = append([]string(nil), outcomes[0].SyntheticQuestions...)
			test.mutate(&tampered[0])
			if err := ValidateManagerReviewOutcomes(packet, review, intake, tampered); err == nil ||
				!strings.Contains(err.Error(), "may not substitute") {
				t.Fatalf("review content substitution was accepted: %v", err)
			}
		})
	}
}

func TestManagerReviewCreatesOnlyProposedNewAndReviewLinkedWork(t *testing.T) {
	packet := testCurationPacket(t)
	review := testManagerReview(t, packet)
	packet.Intake.SpawnedWorkItems = []string{"W-0042"}
	work := stateTestWorkItem("W-0042")
	work.DecisionIDs = []string{review.ID}
	previous := CampaignGraph{WorkItems: map[string]WorkItemRecord{}}
	if err := validateReviewWorkItemCreations(previous, packet.Intake, review, []WorkItemRecord{work}); err != nil {
		t.Fatalf("proposed review-linked work was rejected: %v", err)
	}

	unlinked := work
	unlinked.DecisionIDs = nil
	if err := validateReviewWorkItemCreations(previous, packet.Intake, review, []WorkItemRecord{unlinked}); err == nil ||
		!strings.Contains(err.Error(), "link back") {
		t.Fatalf("unlinked proposed work was accepted: %v", err)
	}

	unproposed := work
	unproposed.ID = "W-0043"
	if err := validateReviewWorkItemCreations(previous, packet.Intake, review, []WorkItemRecord{unproposed}); err == nil ||
		!strings.Contains(err.Error(), "was not proposed") {
		t.Fatalf("unproposed review work was accepted: %v", err)
	}

	previous.WorkItems[work.ID] = work
	if err := validateReviewWorkItemCreations(previous, packet.Intake, review, []WorkItemRecord{work}); err == nil ||
		!strings.Contains(err.Error(), "only once") {
		t.Fatalf("existing proposed work was recreated: %v", err)
	}
}
