package knowledge

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeWriteGrantsCanonicalizesStrictProjectPaths(t *testing.T) {
	grants, err := NormalizeWriteGrants([]WriteGrant{
		{Mode: "exact", Path: "src/main.go"},
		{Mode: "directory", Path: "generated/output"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []WriteGrant{
		{Mode: "directory", Path: "generated/output"},
		{Mode: "exact", Path: "src/main.go"},
	}
	if !reflect.DeepEqual(grants, want) {
		t.Fatalf("normalized grants differ: %#v", grants)
	}
	if err := ValidateCanonicalWriteGrants(want); err != nil {
		t.Fatalf("canonical grants were rejected: %v", err)
	}
	if !EqualWriteGrants(nil, []WriteGrant{}) {
		t.Fatal("absent and explicitly empty grants should have the same authority")
	}
}

func TestNormalizeWriteGrantsRejectsEscapeManagedAndAmbiguousScopes(t *testing.T) {
	tests := []struct {
		name   string
		grants []WriteGrant
		want   string
	}{
		{"escape", []WriteGrant{{Mode: "exact", Path: "../outside"}}, "canonical project-relative"},
		{"absolute", []WriteGrant{{Mode: "exact", Path: "C:/outside"}}, "safely representable"},
		{"glob", []WriteGrant{{Mode: "directory", Path: "src/**"}}, "safely representable"},
		{"managed", []WriteGrant{{Mode: "exact", Path: "active/campaign/campaign.json"}}, "engine-managed"},
		{"state", []WriteGrant{{Mode: "directory", Path: ".re-discipline/state"}}, "engine-managed"},
		{"overlap", []WriteGrant{
			{Mode: "directory", Path: "src"}, {Mode: "exact", Path: "src/main.go"},
		}, "overlap ambiguously"},
		{"duplicate", []WriteGrant{
			{Mode: "exact", Path: "src/main.go"}, {Mode: "exact", Path: "src/main.go"},
		}, "overlap ambiguously"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeWriteGrants(test.grants)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("grants returned %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestRunWriteGrantsAreImmutableAndMigrationRunsMayHaveNone(t *testing.T) {
	prepared := RunRecord{
		RecordMeta:        stateTestMeta("R-20260802-0077", 1),
		CampaignID:        "C-TEST",
		PrimaryWorkItemID: "W-0001",
		ActorID:           "drafter-1",
		Role:              "investigator",
		Status:            "prepared",
		Brief:             stateTestFileHandle("brief.md"),
		ContextPack:       stateTestFileHandle("context-pack.json"),
		WriteGrants:       []WriteGrant{{Mode: "exact", Path: "src/main.go"}},
	}
	running := prepared
	running.Revision++
	running.Status = "running"
	running.StartedAt = stateTestTime
	if err := ValidateRunTransition(&prepared, running); err != nil {
		t.Fatalf("unchanged grants blocked legal transition: %v", err)
	}
	running.WriteGrants = nil
	if err := ValidateRunTransition(&prepared, running); err == nil || !strings.Contains(err.Error(), "write grants are immutable") {
		t.Fatalf("changed grants returned %v", err)
	}

	migrationImport := prepared
	migrationImport.ID = "R-20260802-0078"
	migrationImport.WriteGrants = nil
	if err := ValidateRun(migrationImport); err != nil {
		t.Fatalf("grant-free imported run was rejected: %v", err)
	}
}
