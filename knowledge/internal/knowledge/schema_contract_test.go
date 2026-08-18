package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type packagedSchemaShape struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func TestCanonicalJSONTemplatesDecodeAndValidate(t *testing.T) {
	templateRoot := filepath.Clean(filepath.Join(adversarialAssetRoot(t), "..", "templates", "campaign"))
	tests := []struct {
		name     string
		value    any
		validate func() error
	}{
		{"campaign.json", &CampaignRecord{}, nil},
		{"work-item.json", &WorkItemRecord{}, nil},
		{"deferred-work-item.json", &WorkItemRecord{}, nil},
		{"run.json", &RunRecord{}, nil},
		{"intake.json", &IntakeRecord{}, nil},
		{"review.json", &ReviewRecord{}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(templateRoot, test.name))
			if err != nil {
				t.Fatal(err)
			}
			if err := decodeStrict(body, test.value); err != nil {
				t.Fatalf("template does not decode into its canonical runtime type: %v", err)
			}
			var validationErr error
			switch value := test.value.(type) {
			case *CampaignRecord:
				validationErr = ValidateCampaign(*value)
			case *WorkItemRecord:
				validationErr = ValidateWorkItem(*value)
			case *RunRecord:
				validationErr = ValidateRun(*value)
			case *IntakeRecord:
				validationErr = ValidateIntake(*value)
			case *ReviewRecord:
				validationErr = ValidateReview(*value)
			}
			if validationErr != nil {
				t.Fatalf("template violates its runtime validator: %v", validationErr)
			}
		})
	}
}

func TestPackagedStateSchemasMatchRuntimeJSONShapes(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"campaign.schema.json", CampaignRecord{}},
		{"work-item.schema.json", WorkItemRecord{}},
		{"run.schema.json", RunRecord{}},
		{"intake.schema.json", IntakeRecord{}},
		{"review.schema.json", ReviewRecord{}},
		{"review-decision.schema.json", ReviewDecision{}},
		{"review-load-receipt.schema.json", ReviewLoadReceipt{}},
		{"review-packet.schema.json", ReviewPacketEnvelope{}},
		{"event.schema.json", StateEvent{}},
		{"closure-job.schema.json", ClosureJob{}},
		{"closure-coverage.schema.json", ClosureCoverage{}},
		{"archive-manifest.schema.json", ArchiveManifest{}},
		{"context-card.schema.json", ContextCard{}},
		{"finding-evidence.schema.json", EvidenceReference{}},
		{"context-pack.schema.json", ContextPack{}},
		{"transaction.schema.json", StateTransactionDescriptor{}},
		{"campaign-merge-plan.schema.json", CampaignMergePlan{}},
		{"campaign-merge-id-map.schema.json", CampaignMergeIDMap{}},
		{"campaign-chronology.schema.json", CampaignChronology{}},
		{"state-inventory.schema.json", StateInventory{}},
		{"closure-plan.schema.json", ClosurePlan{}},
		{"closure-receipt.schema.json", ClosureReceipt{}},
		{"finding-eval-suite.schema.json", FindingEvalSuite{}},
		{"benchmark-result.schema.json", BenchmarkReport{}},
		{"normalized-beats-raw-receipt.schema.json", NormalizedBeatsRawReceipt{}},
		{"normalized-vs-raw-candidate.schema.json", NormalizedRawGateReport{}},
		{"normalization-queue.schema.json", archiveFallbackTrackerState{}},
		{"migration-plan.schema.json", MigrationPlan{}},
		{"migration-operation.schema.json", MigrationOperation{}},
		{"migration-receipt.schema.json", MigrationCertification{}},
		{"migration-gate-artifact.schema.json", MigrationGateArtifact{}},
		{"migration-retrieval-gate-evidence.schema.json", MigrationRetrievalGateEvidence{}},
		{"migration-blinded-agent-evaluation.schema.json", MigrationBlindedAgentEvaluation{}},
		{"migration-host-conformance.schema.json", MigrationHostConformanceEvidence{}},
		{"migration-profile-conflict-packet.schema.json", MigrationProfileConflictPacket{}},
		{"migration-profile-decision-submission.schema.json", MigrationProfileDecisionSubmission{}},
		{"migration-profile-conversion-decision.schema.json", MigrationProfileConversionDecision{}},
		{"migration-truth-conflict-packet.schema.json", MigrationTruthConflictPacket{}},
		{"migration-truth-review-submission.schema.json", MigrationTruthReviewSubmission{}},
		{"migration-truth-atomicization-review.schema.json", MigrationTruthAtomicizationReview{}},
		{"truth-compatibility-receipt.schema.json", MigrationTruthCompatibilityReceipt{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(adversarialAssetRoot(t) + "/schemas/" + test.name)
			if err != nil {
				t.Fatal(err)
			}
			var schema packagedSchemaShape
			if err := json.Unmarshal(body, &schema); err != nil {
				t.Fatal(err)
			}
			properties, required := runtimeJSONShape(reflect.TypeOf(test.value))
			actualProperties := make([]string, 0, len(schema.Properties))
			for name := range schema.Properties {
				actualProperties = append(actualProperties, name)
			}
			sort.Strings(actualProperties)
			sort.Strings(schema.Required)
			if strings.Join(actualProperties, "\x00") != strings.Join(properties, "\x00") {
				t.Fatalf("schema/runtime properties differ\nschema:  %v\nruntime: %v", actualProperties, properties)
			}
			if strings.Join(schema.Required, "\x00") != strings.Join(required, "\x00") {
				t.Fatalf("schema/runtime required fields differ\nschema:  %v\nruntime: %v", schema.Required, required)
			}
		})
	}
}

func TestEmbeddedSubrecordSchemasHaveCanonicalRecordConsumers(t *testing.T) {
	tests := []struct {
		parent string
		field  string
		child  string
	}{
		{"review.schema.json", "decisions", "review-decision.schema.json"},
		{"finding-frontmatter.schema.json", "evidence", "finding-evidence.schema.json"},
	}
	for _, test := range tests {
		t.Run(test.child, func(t *testing.T) {
			body, err := os.ReadFile(adversarialAssetRoot(t) + "/schemas/" + test.parent)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Items map[string]any `json:"items"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(body, &schema); err != nil {
				t.Fatal(err)
			}
			if reference, _ := schema.Properties[test.field].Items["$ref"].(string); reference != test.child {
				t.Fatalf("%s.%s does not consume %s: %q", test.parent, test.field, test.child, reference)
			}
		})
	}
}

func runtimeJSONShape(valueType reflect.Type) ([]string, []string) {
	properties := map[string]bool{}
	required := map[string]bool{}
	var visit func(reflect.Type)
	visit = func(current reflect.Type) {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		for index := 0; index < current.NumField(); index++ {
			field := current.Field(index)
			if field.PkgPath != "" {
				continue
			}
			parts := strings.Split(field.Tag.Get("json"), ",")
			name := parts[0]
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				visit(field.Type)
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = true
			optional := false
			for _, option := range parts[1:] {
				optional = optional || option == "omitempty"
			}
			if !optional {
				required[name] = true
			}
		}
	}
	visit(valueType)
	propertyNames := make([]string, 0, len(properties))
	for name := range properties {
		propertyNames = append(propertyNames, name)
	}
	requiredNames := make([]string, 0, len(required))
	for name := range required {
		requiredNames = append(requiredNames, name)
	}
	sort.Strings(propertyNames)
	sort.Strings(requiredNames)
	return propertyNames, requiredNames
}
