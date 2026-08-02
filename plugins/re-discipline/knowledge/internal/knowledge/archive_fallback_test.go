package knowledge

import (
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

func testArchiveReceipt(t *testing.T) NormalizedBeatsRawReceipt {
	t.Helper()
	receipt := NormalizedBeatsRawReceipt{
		SchemaVersion: ArchiveFallbackReceiptVersion, ID: "archive-gate-test",
		Status: "ratified", RatifiedAt: "2026-08-02T21:00:00Z", RatifiedBy: "maintainer:test",
		EvaluatedAt: "2026-08-02T20:00:00Z", SuiteID: "normalized-finding-ablation-v1",
		SuiteDigest: "sha256:" + strings.Repeat("3", 64), Binding: testArchiveBinding(),
		CaseCount: 48, DevelopmentCases: 24, DevelopmentManager: 12,
		DevelopmentDrafter: 12, DevelopmentAbstention: 4,
		HoldoutCases: 24, HoldoutManager: 12, HoldoutDrafter: 12, HoldoutAbstention: 4,
		NormalizedRecall: 0.95, RawRecall: 0.94,
		AbstentionAccuracy: 1, FindingHandleAccuracy: 1, EvidenceHandleAccuracy: 1,
		SourceClassAccuracy: 1, ReviewStateAccuracy: 1, ValidityAccuracy: 1,
		VocabularyDisjointRate: 1, DurabilityAccuracy: 1, ReplayRate: 1,
		NormalizedMedianTokens: 350, RawMedianTokens: 900, Passed: true,
	}
	digest, err := ArchiveReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Digest = digest
	return receipt
}

func TestArchiveFallbackRequiresReceiptBoundPairedWin(t *testing.T) {
	binding := testArchiveBinding()
	defaultPolicy := ArchiveFallbackPolicy{Mode: "default-fallback"}
	if !defaultPolicy.RawIsDefault(binding) {
		t.Fatal("raw reports stopped being the default fallback without a gate")
	}
	receipt := testArchiveReceipt(t)
	optIn := ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &receipt}
	if err := ValidateArchiveFallbackPolicy(optIn, binding); err != nil {
		t.Fatal(err)
	}
	if optIn.RawIsDefault(binding) {
		t.Fatal("valid opt-in receipt did not change archive policy")
	}
	tampered := receipt
	tampered.NormalizedRecall = 0.50
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &tampered}, binding); err == nil {
		t.Fatal("inferior normalized recall was accepted")
	}
	invalidRange := receipt
	invalidRange.NormalizedRecall = 1.1
	invalidRange.Digest, _ = ArchiveReceiptDigest(invalidRange)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &invalidRange}, binding); err == nil {
		t.Fatal("out-of-range recall was accepted")
	}
	nonUTC := receipt
	nonUTC.EvaluatedAt = "2026-08-02T14:00:00-06:00"
	nonUTC.Digest, _ = ArchiveReceiptDigest(nonUTC)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &nonUTC}, binding); err == nil {
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
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &unratified}, binding); err == nil {
		t.Fatal("unratified archive receipt was accepted")
	}
	undersized := receipt
	undersized.HoldoutCases = 23
	undersized.CaseCount = 47
	undersized.HoldoutDrafter = 11
	undersized.Digest, _ = ArchiveReceiptDigest(undersized)
	if err := ValidateArchiveFallbackPolicy(ArchiveFallbackPolicy{Mode: "opt-in", Receipt: &undersized}, binding); err == nil {
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

func TestArchiveReceiptLoaderRestrictsPathAndVerifiesIntrinsicBinding(t *testing.T) {
	root := t.TempDir()
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt := testArchiveReceipt(t)
	relative := ".re-discipline/knowledge/receipts/normalized-beats-raw.json"
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(relative)), receipt)
	loaded, err := LoadNormalizedBeatsRawReceipt(boundary, relative)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Digest != receipt.Digest || loaded.SuiteDigest != receipt.SuiteDigest {
		t.Fatalf("loaded receipt lost suite binding: %#v", loaded)
	}
	if _, err := LoadNormalizedBeatsRawReceipt(boundary, "docs/receipt.json"); err == nil {
		t.Fatal("receipt outside managed knowledge control plane was accepted")
	}
	receipt.RawMedianTokens = receipt.NormalizedMedianTokens
	writeTestJSON(t, filepath.Join(root, filepath.FromSlash(relative)), receipt)
	if _, err := LoadNormalizedBeatsRawReceipt(boundary, relative); err == nil {
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
