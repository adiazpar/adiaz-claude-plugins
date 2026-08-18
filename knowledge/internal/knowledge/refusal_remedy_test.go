package knowledge

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// The whole cost this suite guards is the exploratory round trip: a refusal
// that says what is wrong but not what would satisfy it forces the caller to
// guess, re-send a large payload, and learn one more requirement per attempt.
// One recorded session spent seven of them on a single run.prepare.
//
// A remedy is only worth naming if it is machine-actionable, so the assertion
// is not "the message is long" or "the message contains the word remedy" - it
// is that the message names an operation the caller can actually issue, or a
// concrete field or value it can set. Prose that merely restates the rule
// fails this test, which is the point.
// An "operation" is anything the caller can name in a call: a tool, one of the
// typed manager actions, or an action=<value> on the closure, normalization, or
// context-pack surfaces.
var remedyOperationPattern = regexp.MustCompile(
	`\b(manager_apply|curation_submit|closure_apply|normalization_queue|` +
		`context_pack_materialize|state|query|read|trace|action=[a-z-]+|` +
		`(campaign|work|run|finding|decision|reconcile|intake|review)\.[a-z.-]+)\b`)

// refusalCase is one caller-reachable refusal and the specific thing its
// message must name. wantAny is deliberately per-case rather than a single
// global rule: naming "the operation" is right for a state-machine refusal and
// useless for a field-shape one, where the field name and legal values are the
// remedy.
type refusalCase struct {
	name string
	// call must return a non-nil error. It may not mutate shared state.
	call func(t *testing.T) error
	// wantAny requires at least one of these substrings. Each is a concrete
	// operation, field, or value the caller can act on.
	wantAny []string
	// noOperation exempts the handful of refusals for which no operation
	// exists, because the correct answer is "stop", not "call this instead".
	// The exemption is per-case and must be justified in a comment: a blanket
	// escape hatch would let the rule this suite enforces rot silently.
	noOperation bool
}

func TestCallerReachableRefusalsNameAnOperationOrField(t *testing.T) {
	cases := []refusalCase{
		{
			name: "unsupported manager action lists the surface",
			call: func(*testing.T) error {
				_, err := (&Service{}).ManagerApply(context.Background(), ManagerApplyRequest{
					Action: "work.finish", Actor: "manager",
				})
				return err
			},
			wantAny: []string{"work.update"},
		},
		{
			name: "missing manager actor names the field and where the list lives",
			call: func(*testing.T) error {
				_, err := (&Service{}).ManagerApply(context.Background(), ManagerApplyRequest{
					Action: "work.update",
				})
				return err
			},
			wantAny: []string{"permittedManagers"},
		},
		{
			name: "wrong record kind names the actions that accept it",
			call: func(*testing.T) error {
				return validateManagerActionPayload(ManagerApplyRequest{
					Action:   "campaign.update",
					Campaign: &CampaignRecord{},
					Runs:     []RunRecord{{}},
				}, managerActionKinds["campaign.update"], Configuration{})
			},
			wantAny: []string{"run.prepare"},
		},
		{
			name: "wrong run status names the transition that owns it",
			call: func(*testing.T) error {
				return validateManagerActionPayload(ManagerApplyRequest{
					Action:    "run.start",
					Runs:      []RunRecord{{PrimaryWorkItemID: "W-0001", Status: "returned"}},
					WorkItems: []WorkItemRecord{{RecordMeta: RecordMeta{ID: "W-0001"}}},
				}, managerActionKinds["run.start"], Configuration{})
			},
			wantAny: []string{`"running"`},
		},
		{
			name: "delegated run without a pack names the operation that builds one",
			call: func(*testing.T) error {
				return validateManagerActionPayload(ManagerApplyRequest{
					Action: "run.prepare",
					Runs: []RunRecord{
						{PrimaryWorkItemID: "W-0001", Status: "prepared", Role: "investigator"},
					},
					WorkItems: []WorkItemRecord{{RecordMeta: RecordMeta{ID: "W-0001"}}},
				}, managerActionKinds["run.prepare"], Configuration{})
			},
			wantAny: []string{"context_pack_materialize"},
		},
		{
			name: "campaign.open names which of its four preconditions failed",
			call: func(*testing.T) error {
				return validateCampaignOpenPayload(ManagerApplyRequest{
					Actor:     "manager",
					Campaign:  &CampaignRecord{Status: "open", PermittedManagers: []string{"other"}},
					WorkItems: []WorkItemRecord{{}},
				})
			},
			wantAny: []string{"permittedManagers"},
		},
		{
			name: "missing expected digest names the map key and the read that returns it",
			call: func(*testing.T) error {
				return missingExpectedDigestError(
					"manager_apply", "W-0001", 3, "active/test-campaign/work-items/W-0001.json")
			},
			wantAny: []string{"expectedRecordDigests"},
		},
		{
			name: "unsupported closure action lists the surface",
			call: func(*testing.T) error {
				_, err := (&Service{}).ClosureApply(context.Background(), ClosureApplyRequest{
					Action: "abandon",
				})
				return err
			},
			wantAny: []string{"restart"},
		},
		{
			name: "closure mutation names every missing field",
			call: func(*testing.T) error {
				_, err := (&Service{}).ClosureApply(context.Background(), ClosureApplyRequest{
					Action: "start", Actor: "manager",
				})
				return err
			},
			wantAny: []string{"idempotencyKey"},
		},
		{
			name: "closure stage refusal names the single legal next stage",
			call: func(*testing.T) error {
				return errString(closureNextStageRemedy("coverage"))
			},
			wantAny: []string{`targetStage="normalize"`},
		},
		{
			name: "closure verify stage names its own action rather than a targetStage",
			call: func(*testing.T) error {
				return errString(closureNextStageRemedy("project"))
			},
			wantAny: []string{"action=verify"},
		},
		{
			name: "normalization state refusal names the one legal transition",
			call: func(*testing.T) error {
				return errString(normalizationNextTransition("claimed"))
			},
			wantAny: []string{"action=ack"},
		},
		{
			// A resolved queue item has no successor. Naming any action here
			// would send the caller to spend a round trip proving the
			// suggestion wrong, which is worse than naming none.
			name: "terminal normalization item says so instead of naming a verb",
			call: func(*testing.T) error {
				return errString(normalizationNextTransition("resolved"))
			},
			wantAny:     []string{"terminal"},
			noOperation: true,
		},
		{
			name: "unsupported normalization action names the ordered surface",
			call: func(*testing.T) error {
				_, err := (&Service{archiveTracker: &ArchiveFallbackTracker{}}).
					NormalizationQueueApply(context.Background(), NormalizationQueueRequest{
						Action: "finish",
					})
				return err
			},
			wantAny: []string{"claim -> ack -> resolve"},
		},
		{
			name: "unsupported state mode names each mode and its required arguments",
			call: func(*testing.T) error {
				_, err := compileStateView(
					context.Background(), nil, Configuration{}, nil, StateRequest{Mode: "summary"})
				return err
			},
			wantAny: []string{"sinceEventId"},
		},
		{
			name: "unsupported trace relation lists the traversable set",
			call: func(*testing.T) error {
				_, err := (&Service{}).Trace(context.Background(), TraceRequest{
					StartHandle: "finding:F-0001", Relations: []string{"caused-by"},
				})
				return err
			},
			wantAny: []string{"supersedes"},
		},
		{
			name: "read selector refusal names the handle shape of every selector",
			call: func(*testing.T) error {
				_, err := (&Service{}).ReadExact(context.Background(), ExactReadRequest{
					Selector: "document", Value: "x",
				})
				return err
			},
			wantAny: []string{"record:<canonical/path>"},
		},
		{
			name: "active-run materialize names the transition that publishes it",
			call: func(*testing.T) error {
				return ValidateContextPackMaterializeRequest(ContextPackMaterializeRequest{
					Action:         "materialize",
					Target:         ContextPackTarget{Kind: "active-run"},
					ExpectedDigest: "sha256:" + strings.Repeat("a", 64),
				})
			},
			wantAny: []string{"manager_apply run.prepare"},
		},
		{
			name: "materialize without a digest names the preview that produces one",
			call: func(*testing.T) error {
				return ValidateContextPackMaterializeRequest(ContextPackMaterializeRequest{
					Action: "materialize", Target: ContextPackTarget{Kind: "recruiting-run"},
				})
			},
			wantAny: []string{"action=preview"},
		},
		{
			name: "reviewed intake refusal names the amendment path",
			call: func(t *testing.T) error {
				packet := testCurationPacket(t)
				packet.Intake.Status = "reviewed"
				return ValidateCurationPacket("curator", packet)
			},
			wantAny: []string{"intake.coverage.retire"},
		},
		{
			name: "curator authority refusal names the transition that ratifies",
			call: func(t *testing.T) error {
				packet := testCurationPacket(t)
				packet.Candidates[0].Record.ReviewState = "manager-ratified"
				return ValidateCurationPacket("curator", packet)
			},
			wantAny: []string{"manager_apply review.submit"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.call(t)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			message := err.Error()
			if !testCase.noOperation && !remedyOperationPattern.MatchString(message) {
				t.Errorf(
					"refusal names no operation a caller could issue:\n  %s", message)
			}
			matched := false
			for _, want := range testCase.wantAny {
				if strings.Contains(message, want) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("refusal does not name any of %q:\n  %s", testCase.wantAny, message)
			}
		})
	}
}

// errString adapts the remedy-rendering helpers, which return the sentence a
// refusal embeds rather than an error, so the table above can hold them beside
// real refusals and hold them to the same standard.
func errString(value string) error {
	return remedyOnly(value)
}

type remedyOnly string

func (value remedyOnly) Error() string { return string(value) }

// A refusal that names an operation the caller cannot reach from the state the
// refusal fires in is worse than one that names none: it sends the caller to
// spend a round trip proving the suggestion wrong. This is the specific mistake
// caught during review, when a refusal that fires while an intake is still
// `submitted` was going to advertise intake.coverage.retire, which requires
// `reviewed`.
func TestRefusalsDoNotAdvertiseUnreachableRemedies(t *testing.T) {
	// run.complete's guard fires while the run is still `returned` and its only
	// covering intakes are dirty. curation_submit accepts a further intake in
	// that state, so naming it is reachable; intake.coverage.retire requires a
	// covering intake that is already `reviewed`, so it may appear only when one
	// is.
	graph := CampaignGraph{
		Campaign: &CampaignRecord{
			RecordMeta: RecordMeta{ID: "C-TEST"}, Slug: "test-campaign", Status: "open",
		},
		Intakes: map[string]IntakeRecord{
			"I-ONE": {
				RecordMeta: RecordMeta{ID: "I-ONE"},
				Status:     "submitted",
				SourceRuns: []FileHandle{{Path: "report.md", SHA256: "sha256:" + strings.Repeat("a", 64)}},
				Coverage: []CoverageEntry{{
					SourcePath: "report.md", SourceSHA256: "sha256:" + strings.Repeat("a", 64),
					StartLine: 1, EndLine: 2, Disposition: "unresolved",
				}},
			},
		},
	}
	run := RunRecord{
		RecordMeta: RecordMeta{ID: "R-20260802-0001"},
		Status:     "completed",
		Report:     &FileHandle{Path: "report.md", SHA256: "sha256:" + strings.Repeat("a", 64)},
	}
	err := validateRunReportIsCurated(graph, run)
	if err == nil {
		t.Fatal("a dirty covering intake did not block completion")
	}
	if strings.Contains(err.Error(), "intake.coverage.retire") {
		t.Errorf(
			"refusal advertises intake.coverage.retire while the only covering intake is still "+
				"submitted; that transition requires a reviewed intake, so the caller would "+
				"spend a round trip discovering it cannot be used:\n  %s", err)
	}
	if !strings.Contains(err.Error(), "curation_submit") {
		t.Errorf("refusal names no reachable remedy:\n  %s", err)
	}

	// The same guard against a reviewed dirty intake must name the retirement,
	// because there the cheap repair really is available.
	reviewed := graph.Intakes["I-ONE"]
	reviewed.Status = "reviewed"
	graph.Intakes["I-ONE"] = reviewed
	err = validateRunReportIsCurated(graph, run)
	if err == nil {
		t.Fatal("a reviewed dirty covering intake did not block completion")
	}
	if !strings.Contains(err.Error(), "intake.coverage.retire") {
		t.Errorf(
			"refusal withholds the retirement that is reachable from a reviewed intake:\n  %s",
			err)
	}
}
