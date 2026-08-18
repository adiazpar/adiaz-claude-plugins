package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeRealPairedGateCorpus authors 64 evaluation cases whose findings and
// paired raw run reports genuinely exist in the fixture project, following the
// packaged conformance fixture pattern: every answerable query uses reviewed
// synthetic-question vocabulary that never appears in the claim, and the
// paired raw report repeats those anchors plus deliberately verbose
// provenance so the raw arm's bounded response costs more tokens.
//
// Truth-class findings are written directly below docs/truth/findings before
// the first state transaction, which adopts pre-existing durable documents
// into the genesis inventory. Campaign and provisional findings are returned
// as documents for creation through real state transactions, because active
// canonical records may only enter the project through the engine.
func writeRealPairedGateCorpus(
	t *testing.T, root string,
) ([]FindingEvalCase, []FindingDocument, map[string]bool) {
	t.Helper()
	suffixes := []string{"aurora", "heliotrope", "isobar", "keystone"}
	cases := make([]FindingEvalCase, 0, 64)
	storeDocs := []FindingDocument{}
	ratified := map[string]bool{}
	findingNumber := 3000
	for index := 0; index < 64; index++ {
		split, local := "development", index
		if index >= 32 {
			split, local = "holdout", index-32
		}
		splitTag := "dev"
		if split == "holdout" {
			splitTag = "hold"
		}
		role := "manager"
		if local%2 == 1 {
			role = "drafter"
		}
		eval := FindingEvalCase{
			ID:    fmt.Sprintf("paired-real-%s-%02d", splitTag, local),
			Role:  role,
			Topic: fmt.Sprintf("paired-real-%s-topic-%02d", splitTag, local),
			Split: split, QueryClass: "conceptual", TokenBudget: 2048,
			ExpectedSourceClasses: map[string]string{},
			ExpectedReviewStates:  map[string]string{},
			ExpectedValidities:    map[string]string{},
		}
		pair := string(rune('a'+local/26)) + string(rune('a'+local%26))
		if local < 4 {
			absent := "zqabsent" + splitTag + pair
			eval.Query = absent + " " + absent + "nowhere?"
			eval.AllowedSourceClasses = []string{"truth"}
			eval.AllowedReviewStates = []string{"manager-ratified"}
			eval.AllowedValidities = []string{"current"}
			cases = append(cases, eval)
			continue
		}
		findingNumber++
		findingID := fmt.Sprintf("F-%04d", findingNumber)
		stem := "zqreal" + splitTag + pair
		questions := make([]string, 0, len(suffixes))
		for _, suffix := range suffixes {
			questions = append(questions, stem+" "+stem+suffix+"?")
		}
		class := []string{"truth", "campaign", "provisional"}[local%3]
		runID := fmt.Sprintf("R-20260803-%04d", findingNumber)
		reportPath := fmt.Sprintf("active/fixture-campaign/runs/%s/report.md", runID)
		claim := fmt.Sprintf(
			"The paired %s subsystem %02d selects its bounded phase after verification.",
			split, local)
		var reportBuilder strings.Builder
		reportBuilder.WriteString("# RUN PROVENANCE\n\nImmutable paired raw report for " +
			findingID + ".\n\n# QUERY ANCHORS\n\n")
		for _, question := range questions {
			reportBuilder.WriteString("- " + question + "\n")
		}
		reportBuilder.WriteString("- shared corpus anchor pairedprovenance\n")
		reportBuilder.WriteString("\n# OBSERVATION\n\n" + claim + "\n\n# IMMUTABLE DETAIL\n\n")
		for segment := 1; segment <= 120; segment++ {
			reportBuilder.WriteString(fmt.Sprintf(
				"Provenance segment %03d records bounded fixture material for paired expansion-cost measurement.\n",
				segment))
		}
		reportBody := reportBuilder.String()
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(reportPath)), reportBody)
		reviewState, validity, projection := "manager-ratified", "current", "campaign"
		findingPath := fmt.Sprintf("active/test-campaign/findings/%s.md", findingID)
		switch class {
		case "truth":
			projection = "truth"
			findingPath = fmt.Sprintf("docs/truth/findings/%s.md", findingID)
			validity = []string{"current", "superseded"}[(local/3)%2]
		case "campaign":
			validity = "challenged"
		case "provisional":
			reviewState, validity = "curator-checked", "provisional"
		}
		document := FindingDocument{
			Record: FindingRecord{
				SchemaVersion: 2, ID: findingID, CampaignID: "C-TEST",
				Revision: 1, CreatedAt: "2026-08-02T00:00:00Z", UpdatedAt: "2026-08-02T00:00:00Z",
				CreatedBy: "curator:fixture", UpdatedBy: "curator:fixture",
				CorrelationID: "corr-paired-real-" + strings.ToLower(findingID),
				Kind:          "conclusion",
				Subject:       fmt.Sprintf("paired.%s-%02d", splitTag, local),
				Claim:         claim,
				Scope:         map[string]any{"suite": "normalized-raw-real-suite"},
				AppliesWhen:   []string{"the paired archive gate fixture is measured"},
				KnownLimits:   []string{"does not establish production behavior"},
				Tags:          []string{split, "finding-evaluation"},
				Subsystems:    []string{"fixture"},
				Aliases:       []string{"paired real " + splitTag + fmt.Sprintf(" %02d", local)},
				SourceRuns:    []string{pairedGateSourceRunID(class, runID)},
				Evidence: []EvidenceReference{{
					Path: reportPath, SHA256: "sha256:" + SHA256Bytes([]byte(reportBody)),
					StartLine: 1, EndLine: 10,
				}},
				EvidenceGrade: "direct", ReviewState: reviewState,
				Validity: validity, Projection: projection,
				VerifiedAt: "2026-08-02",
				Path:       findingPath,
				Body: "# Claim\n" + claim + "\n\n## Applies when\n" +
					"The paired archive gate fixture is measured.\n\n" +
					"## Does not establish\nProduction behavior.\n\n" +
					"## Evidence\nSee the exact immutable run range.\n\n" +
					"## Reproduction\nIssue a reviewed synthetic query for " + findingID + ".\n\n" +
					"## Relations\nNo relations.\n",
			},
			SyntheticQuestions: questions,
			QuestionsReviewed:  true,
		}
		if class == "truth" {
			findingBody, err := RenderFindingDocument(document)
			if err != nil {
				t.Fatalf("render paired fixture finding %s: %v", findingID, err)
			}
			writeTestFile(t, filepath.Join(root, filepath.FromSlash(findingPath)), string(findingBody))
		} else {
			storeDocs = append(storeDocs, document)
			if class == "campaign" {
				ratified[findingID] = true
			}
		}
		// The shared pairedprovenance token appears in every raw report but in
		// no finding, mirroring real corpora where report vocabulary overlaps
		// broadly while normalized claims stay precise: the raw arm pays for
		// several verbose report cards where the normalized arm returns one.
		eval.Query = questions[0] + " pairedprovenance"
		eval.AllowedSourceClasses = []string{class}
		eval.AllowedReviewStates = []string{reviewState}
		eval.AllowedValidities = []string{validity}
		eval.ExpectedFindingIDs = []string{findingID}
		eval.ExpectedFindingHandles = []string{FindingHandle(findingID)}
		eval.ExpectedRawPaths = []string{reportPath}
		eval.ExpectedSourceClasses[findingID] = class
		eval.ExpectedReviewStates[findingID] = reviewState
		eval.ExpectedValidities[findingID] = validity
		eval.Answerable = true
		cases = append(cases, eval)
	}
	// Hard negatives point at another answerable finding in the same split,
	// giving every answerable case a concrete distractor judgment.
	byArm := map[string][]int{}
	for index, eval := range cases {
		if eval.Answerable {
			byArm[eval.Split] = append(byArm[eval.Split], index)
		}
	}
	for _, indices := range byArm {
		for position, index := range indices {
			neighbor := cases[indices[(position+1)%len(indices)]]
			cases[index].HardNegativeFindingIDs = []string{neighbor.ExpectedFindingIDs[0]}
		}
	}
	return cases, storeDocs, ratified
}

// pairedGateStoreRunID is the shared manager run that carries provenance for
// every store-created paired-corpus finding. Truth-class findings keep their
// per-case legacy run labels because they enter as pre-adopted durable files.
const pairedGateStoreRunID = "R-20260802-9001"

func pairedGateSourceRunID(class, runID string) string {
	if class == "truth" {
		return runID
	}
	return pairedGateStoreRunID
}

// createPairedGateStoreFindings publishes the active-campaign half of the
// paired corpus through real state transactions: a shared manager run is
// prepared first, every finding is created unratified at revision 1 citing
// that run, and the campaign-class subset is then ratified and challenged by
// a manager transaction, exactly as the engine requires.
func createPairedGateStoreFindings(
	t *testing.T,
	store *StateStore,
	opening StateTransactionReceipt,
	documents []FindingDocument,
	ratified map[string]bool,
) StateTransactionReceipt {
	t.Helper()
	head := opening.ResultingHead
	graph, err := store.LoadCampaignGraph("test-campaign")
	if err != nil {
		t.Fatal(err)
	}
	work := graph.WorkItems["W-0001"]
	priorWorkDigest := work.Digest
	work.RecordMeta = lifecycleAdvanceMeta(
		work.RecordMeta, "2026-08-02T18:30:00Z", "manager", "corr-paired-run")
	work.State = "active"
	work.ActiveRunIDs = []string{pairedGateStoreRunID}
	sharedRun := RunRecord{
		RecordMeta: RecordMeta{
			SchemaVersion: CampaignSchemaVersion, ID: pairedGateStoreRunID, Revision: 1,
			CreatedAt: "2026-08-02T18:30:00Z", UpdatedAt: "2026-08-02T18:30:00Z",
			CreatedBy: "manager", UpdatedBy: "manager", CorrelationID: "corr-paired-run",
		},
		CampaignID: "C-TEST", PrimaryWorkItemID: "W-0001",
		ActorID: "manager", Role: "manager", Status: "prepared",
	}
	runReceipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Authority: "manager", Action: "run.prepare",
		CorrelationID: "corr-paired-run", IdempotencyKey: "paired-run-once",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{
			{Path: "active/test-campaign/runs/" + pairedGateStoreRunID + "/run.json", Record: sharedRun},
			{Path: "active/test-campaign/work-items/W-0001.json", ExpectedRevision: 1,
				ExpectedDigest: priorWorkDigest, Record: work},
		},
	})
	if err != nil {
		t.Fatalf("prepare shared paired-corpus run: %v", err)
	}
	head = runReceipt.ResultingHead

	// The shared run's report carries one dedicated line per store finding so
	// intake coverage can partition it exhaustively and every candidate can
	// cite its exact span as evidence.
	reportPath := "active/test-campaign/runs/" + pairedGateStoreRunID + "/report.md"
	lines := make([]string, 0, len(documents))
	for index, document := range documents {
		lines = append(lines, fmt.Sprintf(
			"Span %04d records the bounded claim for %s.", index+1, document.Record.ID))
	}
	reportBody := strings.Join(lines, "\n") + "\n"
	reportSHA := "sha256:" + SHA256Bytes([]byte(reportBody))
	lineCount := len(documents)
	writeTestFile(t, filepath.Join(store.Boundary.Root, filepath.FromSlash(reportPath)), reportBody)

	running := sharedRun
	running.RecordMeta = lifecycleAdvanceMeta(
		running.RecordMeta, "2026-08-02T18:35:00Z", "manager", "corr-paired-run-start")
	running.Status, running.StartedAt = "running", "2026-08-02T18:35:00Z"
	startReceipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Authority: "manager", Action: "run.start",
		CorrelationID: "corr-paired-run-start", IdempotencyKey: "paired-run-start-once",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{{
			Path:             "active/test-campaign/runs/" + pairedGateStoreRunID + "/run.json",
			ExpectedRevision: 1, ExpectedDigest: runRecordDigest(t, runReceipt, pairedGateStoreRunID),
			Record: running,
		}},
	})
	if err != nil {
		t.Fatalf("start shared paired-corpus run: %v", err)
	}
	head = startReceipt.ResultingHead
	returned := running
	returned.RecordMeta = lifecycleAdvanceMeta(
		returned.RecordMeta, "2026-08-02T18:36:00Z", "manager", "corr-paired-run-return")
	returned.Status, returned.ReturnedAt = "returned", "2026-08-02T18:36:00Z"
	returned.Report = &FileHandle{Path: reportPath, SHA256: reportSHA}
	returned.ResultSummary = "Paired corpus provenance returned."
	returnReceipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Authority: "manager", Action: "paired.run.freeze",
		CorrelationID: "corr-paired-run-return", IdempotencyKey: "paired-run-return-once",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: []StateWrite{{
			Path:             "active/test-campaign/runs/" + pairedGateStoreRunID + "/run.json",
			ExpectedRevision: 2, ExpectedDigest: runRecordDigest(t, startReceipt, pairedGateStoreRunID),
			Record: returned,
		}},
	})
	if err != nil {
		t.Fatalf("return shared paired-corpus run: %v", err)
	}
	head = returnReceipt.ResultingHead

	creates := make([]StateWrite, 0, len(documents))
	intakes := []IntakeRecord{}
	for index, document := range documents {
		create := document
		create.Record.ReviewState = "curator-checked"
		create.Record.Validity = "provisional"
		create.Record.CorrelationID = "corr-paired-create"
		create.Record.Digest = ""
		create.Record.Evidence = []EvidenceReference{pairedGateSpanEvidence(reportPath, reportSHA, index)}
		creates = append(creates, StateWrite{Path: document.Record.Path, Record: create})
	}
	for batchStart := 0; batchStart < len(documents); batchStart += 10 {
		batchEnd := batchStart + 10
		if batchEnd > len(documents) {
			batchEnd = len(documents)
		}
		intakeID := fmt.Sprintf("I-%04d", len(intakes)+1)
		intake := IntakeRecord{
			RecordMeta: RecordMeta{
				SchemaVersion: CampaignSchemaVersion, ID: intakeID, Revision: 1,
				CreatedAt: "2026-08-02T18:40:00Z", UpdatedAt: "2026-08-02T18:40:00Z",
				CreatedBy: "manager", UpdatedBy: "manager",
				CorrelationID: "corr-paired-create",
			},
			CampaignID: "C-TEST",
			SourceRuns: []FileHandle{{Path: reportPath, SHA256: reportSHA}},
			Triage:     map[string]string{},
			Status:     "submitted",
		}
		for line := 1; line <= lineCount; line++ {
			entry := CoverageEntry{
				SourcePath: reportPath, SourceSHA256: reportSHA,
				StartLine: line, EndLine: line, SourceLineCount: lineCount,
				Disposition: "non-claim",
			}
			if line-1 >= batchStart && line-1 < batchEnd {
				findingID := documents[line-1].Record.ID
				entry.Disposition, entry.TargetID = "candidate-finding", findingID
				intake.CandidateFindingIDs = append(intake.CandidateFindingIDs, findingID)
				intake.Triage[findingID] = "routine"
			}
			entry.SourceHandle = canonicalCoverageHandle(entry)
			intake.Coverage = append(intake.Coverage, entry)
		}
		intakes = append(intakes, intake)
		creates = append(creates, StateWrite{
			Path: "active/test-campaign/intake/" + intakeID + ".json", Record: intake,
		})
	}
	createReceipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Authority: "manager", Action: "finding.create",
		CorrelationID: "corr-paired-create", IdempotencyKey: "paired-create-once",
		ExpectedHeadRevision: head.Revision, ExpectedHeadDigest: head.Digest,
		Writes: creates,
	})
	if err != nil {
		t.Fatalf("create paired corpus findings: %v", err)
	}
	revisions := map[string]StateRecordResult{}
	for _, record := range createReceipt.Records {
		revisions[record.RecordID] = record
	}

	updates := make([]StateWrite, 0, len(documents))
	reviewNumber := 0
	for intakeIndex, intake := range intakes {
		// Every intake candidate receives an individual decision: ratify for the
		// campaign-class subset, hold for the rest, matching the review contract.
		decisions := []ReviewDecision{}
		ratifiedInIntake := 0
		for _, findingID := range intake.CandidateFindingIDs {
			decision := ReviewDecision{
				FindingID: findingID, FindingRevision: 1, Action: "hold",
				Rationale: "Held provisional pending further paired-corpus evidence.",
			}
			if ratified[findingID] {
				decision.Action, decision.Projection = "ratify", "campaign"
				decision.Rationale = "Exact shared-run evidence verified for the paired corpus."
				ratifiedInIntake++
			}
			decisions = append(decisions, decision)
		}
		if ratifiedInIntake == 0 {
			continue
		}
		reviewNumber++
		reviewID := fmt.Sprintf("V-%04d", reviewNumber)
		packetDigest := "sha256:" + SHA256Bytes([]byte("paired-packet-"+intake.ID))
		review := ReviewRecord{
			RecordMeta: RecordMeta{
				SchemaVersion: CampaignSchemaVersion, ID: reviewID, Revision: 1,
				CreatedAt: "2026-08-02T19:30:00Z", UpdatedAt: "2026-08-02T19:30:00Z",
				CreatedBy: "manager", UpdatedBy: "manager",
				CorrelationID: "corr-paired-ratify",
			},
			CampaignID: "C-TEST", Reviewer: "manager", Authority: "manager",
			IntakeID: intake.ID, IntakeRevision: 1, PacketDigest: packetDigest,
			ReviewLoad: stateTestReviewLoad(reviewID, "C-TEST", packetDigest, len(decisions), 0),
			Decisions:  decisions,
		}
		review.ReviewLoad.PacketOrdinal = reviewNumber
		if err := SealReviewLoadReceipt(&review.ReviewLoad); err != nil {
			t.Fatal(err)
		}
		reviewed := intake
		reviewed.RecordMeta = lifecycleAdvanceMeta(
			reviewed.RecordMeta, "2026-08-02T19:30:00Z", "manager", "corr-paired-ratify")
		reviewed.Status = "reviewed"
		prior := revisions[intake.ID]
		updates = append(updates,
			StateWrite{Path: "active/test-campaign/reviews/" + reviewID + ".json", Record: review},
			StateWrite{Path: "active/test-campaign/intake/" + intake.ID + ".json",
				ExpectedRevision: prior.Revision, ExpectedDigest: prior.RecordDigest,
				Record: reviewed},
		)
		_ = intakeIndex
	}
	for _, document := range documents {
		if !ratified[document.Record.ID] {
			continue
		}
		update := document
		update.Record.ReviewState = "manager-ratified"
		update.Record.Validity = "challenged"
		update.Record.Revision = 2
		update.Record.UpdatedAt = "2026-08-02T19:30:00Z"
		update.Record.UpdatedBy = "manager:test"
		update.Record.CorrelationID = "corr-paired-ratify"
		update.Record.Digest = ""
		index := storeFindingIndex(documents, document.Record.ID)
		update.Record.Evidence = []EvidenceReference{pairedGateSpanEvidence(reportPath, reportSHA, index)}
		prior := revisions[document.Record.ID]
		updates = append(updates, StateWrite{
			Path: document.Record.Path, ExpectedRevision: prior.Revision,
			ExpectedDigest: prior.RecordDigest, Record: update,
		})
	}
	ratifyReceipt, err := store.Apply(context.Background(), StateTransactionRequest{
		CampaignSlug: "test-campaign", CampaignID: "C-TEST", Actor: "manager",
		Authority: "manager", Action: "finding.ratify",
		CorrelationID: "corr-paired-ratify", IdempotencyKey: "paired-ratify-once",
		ExpectedHeadRevision: createReceipt.ResultingHead.Revision,
		ExpectedHeadDigest:   createReceipt.ResultingHead.Digest,
		Writes:               updates,
	})
	if err != nil {
		t.Fatalf("ratify paired corpus findings: %v", err)
	}
	return ratifyReceipt
}

func runRecordDigest(t *testing.T, receipt StateTransactionReceipt, recordID string) string {
	t.Helper()
	for _, record := range receipt.Records {
		if record.RecordID == recordID {
			return record.RecordDigest
		}
	}
	t.Fatalf("receipt does not carry record %s", recordID)
	return ""
}

// pairedGateSpanEvidence cites one dedicated shared-report line, using the
// canonical coverage handle as the object key so intake coverage rows and
// candidate evidence bind the same exact span identity.
func pairedGateSpanEvidence(reportPath, reportSHA string, index int) EvidenceReference {
	handle := canonicalCoverageHandle(CoverageEntry{
		SourcePath: reportPath, StartLine: index + 1, EndLine: index + 1,
	})
	return EvidenceReference{
		Path: reportPath, SHA256: reportSHA,
		StartLine: index + 1, EndLine: index + 1,
		ObjectKey: handle, SourceRun: pairedGateStoreRunID,
	}
}

func storeFindingIndex(documents []FindingDocument, findingID string) int {
	for index, document := range documents {
		if document.Record.ID == findingID {
			return index
		}
	}
	return -1
}

func prepareArchiveOptInManagerFixture(
	t *testing.T,
) (string, *Service, ManagerApplyRequest, []byte) {
	t.Helper()
	root := makeAdversarialProject(t)
	suiteCases, storeDocs, ratified := writeRealPairedGateCorpus(t, root)
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	_, opening := openStateTestCampaign(t, store)
	opening = createPairedGateStoreFindings(t, store, opening, storeDocs, ratified)
	service := newAdversarialService(t, root, nil)
	generation, _, selected, _, err := service.ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	suite := FindingEvalSuite{
		SchemaVersion: FindingEvalSuiteVersion, ID: "normalized-raw-real-suite",
		Status: "ratified", RatifiedAt: "2026-08-02T20:00:00Z",
		RatifiedBy: "maintainer:test", CorpusSnapshot: generation.CorpusFingerprint,
		Cases: suiteCases,
	}
	suite.Digest, err = FindingEvalSuiteDigest(suite)
	if err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(
		root, ".re-discipline", "knowledge", "evals", "findings", suite.ID+".json")
	writeTestJSON(t, suitePath, suite)
	evaluation, err := EvaluateFindingSuite(context.Background(), Retriever{
		Boundary: service.Boundary, Generation: generation, Profile: selected,
	}, suite)
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildNormalizedRawGateReport(
		"2026-08-02T20:02:00Z", suite, generation, selected, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision.Outcome != "passed" {
		if body, dumpErr := canonicalJSON(report); dumpErr == nil {
			dump := filepath.Join(os.TempDir(), "paired-gate-report.json")
			_ = os.WriteFile(dump, body, 0o644)
			t.Logf("paired gate report dumped to %s", dump)
		}
		t.Fatalf("real paired fixture evaluation did not pass the archive gate: %+v", report.Decision)
	}
	reportBody, err := canonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath, err := containedOutputPath(
		service.Index.CacheRoot, normalizedRawCacheRelative(report.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(candidatePath, reportBody, 0o600); err != nil {
		t.Fatal(err)
	}
	policyBody, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(knowledgePolicyPath)))
	if err != nil {
		t.Fatal(err)
	}
	request := ManagerApplyRequest{
		Action: "knowledge.archive-fallback.opt-in", Actor: "manager",
		CampaignSlug: "test-campaign", CampaignID: "C-TEST",
		CorrelationID: "archive-opt-in", IdempotencyKey: "archive-opt-in-once",
		Rationale:            "Ratify the exact passing normalized-vs-raw report.",
		ExpectedHeadRevision: opening.ResultingHead.Revision,
		ExpectedHeadDigest:   opening.ResultingHead.Digest,
		ArchiveFallbackDecision: &ArchiveFallbackOptInDecision{
			CandidateRunID: report.RunID, CandidateReportDigest: report.Digest,
			CandidateContentDigest: "sha256:" + SHA256Bytes(reportBody),
			RatifiedAt:             "2026-08-02T21:00:00Z",
			ExpectedSettingsDigest: "sha256:" + SHA256Bytes(policyBody),
		},
	}
	return root, service, request, reportBody
}

func TestArchiveOptInManagerActionPublishesOneAtomicDecisionAndReplays(t *testing.T) {
	root, service, request, _ := prepareArchiveOptInManagerFixture(t)
	receipt, err := service.ManagerApply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Records) != 1 || receipt.Records[0].RecordID != "C-TEST" ||
		len(receipt.Artifacts) != 3 || receipt.Event.Action != request.Action {
		t.Fatalf("archive decision did not publish one state transaction with its campaign event head: %#v", receipt)
	}
	configuration := LoadConfiguration(root)
	if !configuration.Valid || configuration.Settings.Archive.FallbackMode != "opt-in" ||
		configuration.Settings.Archive.ReportFallbackUntilMeasured ||
		configuration.Settings.Archive.NormalizedBeatsRawReceipt == "" {
		t.Fatalf("archive opt-in settings were not atomically selected: %#v", configuration)
	}
	decisionReceipt, report, err := LoadNormalizedBeatsRawReceipt(
		service.Boundary, configuration.Settings.Archive.NormalizedBeatsRawReceipt)
	if err != nil {
		t.Fatal(err)
	}
	settingsDigest := ""
	for _, artifact := range receipt.Artifacts {
		if artifact.Path == knowledgePolicyPath {
			settingsDigest = artifact.ContentDigest
		}
	}
	if decisionReceipt.DecisionCorrelationID != request.CorrelationID ||
		decisionReceipt.ReportDigest != report.Digest ||
		decisionReceipt.ResultingSettingsDigest != settingsDigest {
		t.Fatalf("archive receipt lost its report, decision, or settings binding: %#v", decisionReceipt)
	}
	cacheCandidate, err := containedOutputPath(
		service.Index.CacheRoot,
		normalizedRawCacheRelative(request.ArchiveFallbackDecision.CandidateRunID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cacheCandidate); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.ManagerApply(context.Background(), request)
	if err != nil || !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("archive decision did not replay after derived candidate pruning: %v", err)
	}
	changed := request
	changedDecision := *request.ArchiveFallbackDecision
	changedDecision.CandidateContentDigest = stateTestDigest("f")
	changed.ArchiveFallbackDecision = &changedDecision
	if _, err := service.ManagerApply(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed archive decision reused an idempotency key: %v", err)
	}
}

func TestArchiveOptInRejectsTamperAndStaleSettingsWithoutPartialWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string, *Service, *ManagerApplyRequest, []byte)
	}{
		{
			name: "candidate tamper",
			mutate: func(_ string, service *Service, request *ManagerApplyRequest, body []byte) {
				path, err := containedOutputPath(
					service.Index.CacheRoot,
					normalizedRawCacheRelative(request.ArchiveFallbackDecision.CandidateRunID))
				if err != nil {
					t.Fatal(err)
				}
				body = append(append([]byte(nil), body...), ' ')
				if err := AtomicWrite(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "stale policy digest",
			mutate: func(_ string, _ *Service, request *ManagerApplyRequest, _ []byte) {
				request.ArchiveFallbackDecision.ExpectedSettingsDigest = stateTestDigest("9")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, service, request, body := prepareArchiveOptInManagerFixture(t)
			test.mutate(root, service, &request, body)
			before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(knowledgePolicyPath)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ManagerApply(context.Background(), request); err == nil {
				t.Fatal("tampered or stale archive decision was accepted")
			}
			after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(knowledgePolicyPath)))
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected archive decision partially changed policy: %v", err)
			}
			receiptPath := filepath.Join(
				root, filepath.FromSlash(archiveOptInReceiptRelative(
					request.ArchiveFallbackDecision.CandidateReportDigest)))
			if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
				t.Fatalf("rejected archive decision published a receipt: %v", err)
			}
		})
	}
}

func TestArchiveOptInCrashBeforeHeadPublishRollsBackAllThreeArtifacts(t *testing.T) {
	root, service, request, _ := prepareArchiveOptInManagerFixture(t)
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := store.LoadCampaignGraph(request.CampaignID)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := service.prepareArchiveFallbackOptInArtifacts(
		context.Background(), store, request)
	if err != nil {
		t.Fatal(err)
	}
	writes, reviewHandle, err := buildManagerWrites(service.Boundary, request, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Campaign == nil {
		t.Fatal("archive decision fixture has no campaign")
	}
	policyBefore, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(knowledgePolicyPath)))
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected archive-policy publication crash")
	store.Failpoint = func(hit StateFailpoint) error {
		if hit.Name == FailAfterRecordPublish && hit.Path == knowledgePolicyPath {
			return injected
		}
		return nil
	}
	_, err = store.Apply(
		context.Background(),
		managerStateTransactionRequest(request, writes, artifacts, reviewHandle))
	if !errors.Is(err, injected) {
		t.Fatalf("archive publication failpoint returned %v", err)
	}
	reopened, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	policyAfter, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(knowledgePolicyPath)))
	if err != nil || !reflect.DeepEqual(policyAfter, policyBefore) {
		t.Fatalf("archive crash recovery did not restore prior policy: %v", err)
	}
	for _, artifact := range artifacts {
		if artifact.Path == knowledgePolicyPath {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(artifact.Path))); !os.IsNotExist(err) {
			t.Fatalf("archive crash recovery retained partial artifact %s: %v", artifact.Path, err)
		}
	}
	committed, err := service.ManagerApply(context.Background(), request)
	if err != nil || committed.ResultingHead.Revision != request.ExpectedHeadRevision+1 {
		t.Fatalf("archive decision did not retry cleanly after recovery: receipt=%#v err=%v", committed, err)
	}
}

func TestArchiveOptInReportValidatorRejectsCaseLevelAndAggregateForgery(t *testing.T) {
	_, report, suite := testArchiveEvidence(t, nil, nil)
	if err := validateNormalizedRawGateReportForSuite(report, suite); err != nil {
		t.Fatal(err)
	}
	caseTamper := report
	caseTamper.PairedEvaluation.Cases = append(
		[]FindingCaseOutcome(nil), report.PairedEvaluation.Cases...)
	caseTamper.PairedEvaluation.Cases[4].RawTokens++
	caseTamper.PairedEvaluation, _ = sealFindingAblationReport(caseTamper.PairedEvaluation)
	caseTamper.Digest = ""
	caseTamper.Digest, _ = CanonicalDigest(caseTamper)
	if err := validateNormalizedRawGateReportForSuite(caseTamper, suite); err == nil {
		t.Fatal("self-resealed case-level forgery was accepted")
	}
	aggregateTamper := report
	aggregateTamper.Checks.LowerTokenCostOverall = false
	aggregateTamper.Digest = ""
	aggregateTamper.Digest, _ = CanonicalDigest(aggregateTamper)
	if err := validateNormalizedRawGateReportForSuite(aggregateTamper, suite); err == nil {
		t.Fatal("self-resealed aggregate gate forgery was accepted")
	}
}
