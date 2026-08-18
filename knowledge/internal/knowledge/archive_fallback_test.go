package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testArchiveBinding() ArchiveFallbackBinding {
	return ArchiveFallbackBinding{
		CorpusFingerprint:  "sha256:" + strings.Repeat("1", 64),
		ProfileIdentity:    "plugin:balanced-v1@test",
		RuntimeFingerprint: "sha256:" + strings.Repeat("2", 64),
		FindingFormat:      FindingFormatVersion,
		IdentifierAnalyzer: IdentifierAnalyzerVersion,
	}
}

func testArchiveEvidence(
	t *testing.T,
	generation *Generation,
	selected *SelectedProfile,
) (NormalizedBeatsRawReceipt, NormalizedRawGateReport, FindingEvalSuite) {
	t.Helper()
	suite, fixtureGeneration, fixtureSelected, evaluation := normalizedRawGateFixture(t, 100, 200)
	if generation != nil {
		fixtureGeneration = *generation
		suite.CorpusSnapshot = generation.CorpusFingerprint
		var err error
		suite.Digest, err = FindingEvalSuiteDigest(suite)
		if err != nil {
			t.Fatal(err)
		}
		evaluation.SuiteDigest = suite.Digest
		evaluation.CorpusSnapshot = suite.CorpusSnapshot
		evaluation, err = sealFindingAblationReport(evaluation)
		if err != nil {
			t.Fatal(err)
		}
	}
	if selected != nil {
		fixtureSelected = *selected
	}
	report, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, fixtureGeneration, fixtureSelected, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	reportBody, err := canonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := archiveOptInReceiptRelative(report.Digest)
	settings := DefaultKnowledgeSettings()
	settings.Schema = "plugin://re-discipline/schemas/knowledge-settings.schema.json"
	settings.Archive.FallbackMode = "opt-in"
	settings.Archive.ReportFallbackUntilMeasured = false
	settings.Archive.NormalizedBeatsRawReceipt = receiptPath
	settingsBody, err := canonicalKnowledgeSettingsBody(settings)
	if err != nil {
		t.Fatal(err)
	}
	request := ManagerApplyRequest{
		Actor: "maintainer:test", CorrelationID: "archive-test",
		IdempotencyKey: "archive-test-idempotency",
	}
	decision := ArchiveFallbackOptInDecision{
		CandidateRunID: report.RunID, CandidateReportDigest: report.Digest,
		CandidateContentDigest: "sha256:" + SHA256Bytes(reportBody),
		RatifiedAt:             "2026-08-02T21:00:00Z",
		ExpectedSettingsDigest: stateTestDigest("e"),
	}
	receipt, err := buildNormalizedBeatsRawReceipt(
		request, decision, report, suite, normalizedRawMeasurementRelative(report.RunID),
		receiptPath, settings, "sha256:"+SHA256Bytes(settingsBody))
	if err != nil {
		t.Fatal(err)
	}
	return receipt, report, suite
}

func TestArchiveFallbackRequiresReceiptBoundPairedWin(t *testing.T) {
	receipt, report, _ := testArchiveEvidence(t, nil, nil)
	binding := receipt.Binding
	defaultPolicy := ArchiveFallbackPolicy{Mode: "default-fallback"}
	if !defaultPolicy.RawIsDefault(binding) {
		t.Fatal("raw reports stopped being the default fallback without a gate")
	}
	optIn := ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &receipt, Report: &report}
	if err := ValidateArchiveFallbackPolicy(optIn, binding); err != nil {
		t.Fatal(err)
	}
	if optIn.RawIsDefault(binding) {
		t.Fatal("valid opt-in receipt did not change archive policy")
	}
	tampered := receipt
	tampered.NormalizedRecall = 0.50
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &tampered, Report: &report}, binding); err == nil {
		t.Fatal("inferior normalized recall was accepted")
	}
	invalidRange := receipt
	invalidRange.NormalizedRecall = 1.1
	invalidRange.Digest, _ = ArchiveReceiptDigest(invalidRange)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &invalidRange, Report: &report}, binding); err == nil {
		t.Fatal("out-of-range recall was accepted")
	}
	nonUTC := receipt
	nonUTC.EvaluatedAt = "2026-08-02T14:00:00-06:00"
	nonUTC.Digest, _ = ArchiveReceiptDigest(nonUTC)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &nonUTC, Report: &report}, binding); err == nil {
		t.Fatal("non-UTC receipt timestamp was accepted")
	}
	staleBinding := binding
	staleBinding.IdentifierAnalyzer = "identifier-analysis-v2"
	if err := ValidateArchiveFallbackPolicy(optIn, staleBinding); err == nil {
		t.Fatal("stale representation receipt was accepted")
	}
	unratified := receipt
	unratified.Status = "candidate"
	unratified.Digest, _ = ArchiveReceiptDigest(unratified)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &unratified, Report: &report}, binding); err == nil {
		t.Fatal("unratified archive receipt was accepted")
	}
	undersized := receipt
	undersized.HoldoutCases = 23
	undersized.CaseCount = 47
	undersized.HoldoutDrafter = 11
	undersized.Digest, _ = ArchiveReceiptDigest(undersized)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &undersized, Report: &report}, binding); err == nil {
		t.Fatal("undersized holdout evaluation was accepted")
	}
}

func TestArchiveNormalizationQueueSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/normalization-queue.json"
	tracker, err := OpenArchiveFallbackTracker(2, path)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("b", 64)
	if _, err := tracker.Record(digest, "request-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Record(digest, "request-2"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenArchiveFallbackTracker(2, path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot()["sha256:"+digest] != 2 {
		t.Fatalf("serve ledger did not survive restart: %#v", reopened.Snapshot())
	}
	suggestions := reopened.Suggestions()
	if len(suggestions) != 1 || suggestions[0].ReportDigest != "sha256:"+digest ||
		suggestions[0].ServeCount != 2 || suggestions[0].Status != "queued" {
		t.Fatalf("normalization queue did not survive restart: %#v", suggestions)
	}
	status := reopened.QueueStatus(1)
	if status.Queued != 1 || len(status.Items) != 1 || status.Omitted != 0 {
		t.Fatalf("durable normalization work was not exposed through bounded status: %#v", status)
	}
	repeated, err := reopened.Record(digest, "request-2")
	if err != nil {
		t.Fatal(err)
	}
	if !repeated.RepeatedRequestIgnored || repeated.ServeCount != 2 {
		t.Fatalf("durable request idempotence was lost: %#v", repeated)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)/2] ^= 1
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenArchiveFallbackTracker(2, path); err == nil {
		t.Fatal("tampered normalization queue was accepted")
	}
}

func TestNormalizationQueueIsCanonicalCacheIndependentAndActionable(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, ".re-discipline", "knowledge", "normalization-queue.json")
	cacheRoot := filepath.Join(root, ".cache", "re-discipline")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "derived-index"), []byte("deletable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tracker, err := OpenArchiveFallbackTracker(2, canonical)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("c", 64)
	source := NormalizationSource{
		CampaignID: "C-TEST", CampaignSlug: "test-campaign", RunID: "R-20260802-0042",
		ReportPath:   "active/test-campaign/runs/R-20260802-0042/report.md",
		ReportHandle: "run:R-20260802-0042",
		SourceHandle: "path:active/test-campaign/runs/R-20260802-0042/report.md",
	}
	if _, err := tracker.RecordSource(digest, "request-1", source); err != nil {
		t.Fatal(err)
	}
	served, err := tracker.RecordSource(digest, "request-2", source)
	if err != nil {
		t.Fatal(err)
	}
	if !served.NormalizationSuggested || served.SuggestionID == "" {
		t.Fatalf("threshold did not create durable normalization work: %#v", served)
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("canonical queue was not persisted outside the cache: %v", err)
	}
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenArchiveFallbackTracker(2, canonical)
	if err != nil {
		t.Fatal(err)
	}
	queued, present := reopened.Get(served.SuggestionID)
	if !present || queued.CampaignID != source.CampaignID || queued.RunID != source.RunID ||
		queued.ReportPath != source.ReportPath || queued.ReportHandle != source.ReportHandle ||
		queued.SourceHandle != source.SourceHandle {
		t.Fatalf("normalization source handles did not survive cache deletion: %#v", queued)
	}
	if _, err := reopened.Transition(
		"claim", queued.ID, "manager", "2099-01-01T00:00:00Z",
		"sha256:"+strings.Repeat("0", 64), nil,
	); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("stale queue mutation was accepted: %v", err)
	}
	if _, err := reopened.Transition(
		"ack", queued.ID, "manager", "2099-01-01T00:00:00Z", queued.Digest, nil,
	); err == nil {
		t.Fatal("normalization lifecycle skipped the claim transition")
	}
	claimed, err := reopened.Transition(
		"claim", queued.ID, "manager", "2099-01-01T00:00:00Z", queued.Digest, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := reopened.Transition(
		"ack", claimed.ID, "manager", "2099-01-01T00:01:00Z", claimed.Digest, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolution := NormalizationResolution{
		SchemaVersion: CampaignSchemaVersion, Disposition: "normalized",
		SourceReport: FileHandle{Path: acknowledged.ReportPath, SHA256: acknowledged.ReportDigest},
		CuratorRunID: "R-20260802-0043", CuratorRunDigest: stateTestDigest("1"),
		CuratorReport: FileHandle{
			Path:   "active/test-campaign/runs/R-20260802-0043/report.md",
			SHA256: stateTestDigest("2"),
		},
		IntakeID: "I-0001", IntakeRevision: 2, IntakeDigest: stateTestDigest("3"),
		CoverageDigest: stateTestDigest("4"), ReviewID: "V-0001", ReviewRevision: 1,
		ReviewDigest: stateTestDigest("5"), ResolvedFindingIDs: []string{"F-0001"},
	}
	if err := sealNormalizationResolution(&resolution); err != nil {
		t.Fatal(err)
	}
	resolved, err := reopened.Transition(
		"resolve", acknowledged.ID, "manager", "2099-01-01T00:02:00Z",
		acknowledged.Digest, &resolution,
	)
	if err != nil || resolved.Status != "resolved" {
		t.Fatalf("normalization lifecycle did not resolve: item=%#v err=%v", resolved, err)
	}
	final, err := OpenArchiveFallbackTracker(2, canonical)
	if err != nil {
		t.Fatal(err)
	}
	status := final.QueueStatus(20)
	if status.Resolved != 1 || status.Queued != 0 || status.Claimed != 0 ||
		status.Acknowledged != 0 || len(status.Items) != 0 {
		t.Fatalf("resolved durable work was not retained outside the active queue: %#v", status)
	}
}

func TestArchiveReceiptLoaderRestrictsPathAndVerifiesIntrinsicBinding(t *testing.T) {
	root := t.TempDir()
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, report, _ := testArchiveEvidence(t, nil, nil)
	relative := receipt.ResultingSettings.Archive.NormalizedBeatsRawReceipt
	reportBody, err := canonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if got := "sha256:" + SHA256Bytes(reportBody); got != receipt.ReportContentDigest {
		t.Fatalf("test report content digest drift: %s != %s", got, receipt.ReportContentDigest)
	}
	if err := AtomicWrite(filepath.Join(root, filepath.FromSlash(receipt.ReportPath)), reportBody, 0o600); err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(relative)), receipt)
	loaded, loadedReport, err := LoadNormalizedBeatsRawReceipt(boundary, relative)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Digest != receipt.Digest || loaded.SuiteDigest != receipt.SuiteDigest ||
		loadedReport.Digest != report.Digest {
		t.Fatalf("loaded receipt lost suite binding: %#v", loaded)
	}
	if _, _, err := LoadNormalizedBeatsRawReceipt(boundary, "docs/receipt.json"); err == nil {
		t.Fatal("receipt outside managed knowledge control plane was accepted")
	}
	receipt.RawMedianTokens = receipt.NormalizedMedianTokens
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(relative)), receipt)
	if _, _, err := LoadNormalizedBeatsRawReceipt(boundary, relative); err == nil {
		t.Fatal("tampered receipt was accepted")
	}
}

func TestArchiveServeAccountingIsDigestKeyedIdempotentAndSuggestsNormalization(t *testing.T) {
	tracker, err := NewArchiveFallbackTracker(2)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	first, err := tracker.Record(digest, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ServeCount != 1 || first.NormalizationSuggested {
		t.Fatalf("unexpected first serve: %#v", first)
	}
	repeated, err := tracker.Record("sha256:"+digest, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ServeCount != 1 || !repeated.RepeatedRequestIgnored {
		t.Fatalf("repeated request changed count: %#v", repeated)
	}
	second, err := tracker.Record(digest, "request-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.ServeCount != 2 || !second.NormalizationSuggested {
		t.Fatalf("threshold did not suggest normalization: %#v", second)
	}
	if tracker.Snapshot()["sha256:"+digest] != 2 {
		t.Fatalf("snapshot did not preserve digest-keyed count: %#v", tracker.Snapshot())
	}
}
