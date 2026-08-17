package knowledge

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestStaleFindingDependentsCompareVerifiedAtWithoutGuessing(t *testing.T) {
	findings := map[string]FindingRecord{
		"F-0001": {ID: "F-0001", VerifiedAt: "2026-07-01", Relations: FindingRelations{DependsOn: []string{"F-0002", "F-0003"}}},
		"F-0002": {ID: "F-0002", VerifiedAt: "2026-08-01"},
		"F-0003": {ID: "F-0003"},
		"F-0004": {ID: "F-0004", VerifiedAt: "2026-08-02", Relations: FindingRelations{DependsOn: []string{"F-0002"}}},
		"F-0005": {ID: "F-0005", VerifiedAt: "2026-09-01", Relations: FindingRelations{DependsOn: []string{"F-0001"}}},
	}
	stale := StaleFindingDependents(findings)
	if len(stale) != 2 || len(stale["F-0001"]) != 1 || stale["F-0001"][0] != "F-0002" ||
		len(stale["F-0005"]) != 1 || stale["F-0005"][0] != "F-0001" {
		t.Fatalf("verifiedAt staleness classification was not conservative: %#v", stale)
	}
	paths := FindingStalenessPaths(findings)["F-0005"]
	if len(paths) != 1 || strings.Join(paths[0].Path, ">") != "F-0005>F-0001>F-0002" ||
		paths[0].RootDependency != "F-0002" {
		t.Fatalf("transitive staleness lost its auditable dependency path: %#v", paths)
	}
}

func TestClosureStalenessBlocksOnlyEpistemicallyLiveDependents(t *testing.T) {
	findings := map[string]FindingRecord{
		"F-0001": {
			ID: "F-0001", ReviewState: "manager-rejected", Validity: "invalid",
			VerifiedAt: "2026-07-01", Relations: FindingRelations{DependsOn: []string{"F-0003"}},
		},
		"F-0002": {
			ID: "F-0002", ReviewState: "manager-ratified", Validity: "current",
			VerifiedAt: "2026-07-01", Relations: FindingRelations{DependsOn: []string{"F-0003"}},
		},
		"F-0003": {
			ID: "F-0003", ReviewState: "manager-ratified", Validity: "current",
			VerifiedAt: "2026-08-01",
		},
	}
	stale := StaleFindingDependents(findings)
	if len(stale["F-0001"]) != 1 || len(stale["F-0002"]) != 1 {
		t.Fatalf("fixture did not produce both stale dependents: %#v", stale)
	}
	blocked := closureStaleFindingDependents(findings)
	if len(blocked) != 1 || len(blocked["F-0002"]) != 1 || blocked["F-0002"][0] != "F-0003" {
		t.Fatalf("terminal rejected finding still blocked closure staleness: %#v", blocked)
	}
}

func TestFindingRetrievalSurfacesTypedStaleDependentAlert(t *testing.T) {
	db, err := sql.Open("sqlite", t.TempDir()+"/stale.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE findings (key TEXT PRIMARY KEY, verified_at TEXT NOT NULL)`,
		`CREATE TABLE finding_relations (source_key TEXT NOT NULL, target_key TEXT NOT NULL, kind TEXT NOT NULL)`,
		`INSERT INTO findings(key,verified_at) VALUES('C-TEST/F-0001','2026-09-01'),('C-TEST/F-0002','2026-07-01'),('C-TEST/F-0003','2026-08-01')`,
		`INSERT INTO finding_relations(source_key,target_key,kind) VALUES('C-TEST/F-0001','C-TEST/F-0002','depends-on'),('C-TEST/F-0002','C-TEST/F-0003','depends-on')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	dependent := &findingCandidate{record: FindingRecord{ID: "F-0001", CampaignID: "C-TEST"}, sourceClass: "truth"}
	dependency := &findingCandidate{record: FindingRecord{ID: "F-0002", CampaignID: "C-TEST"}, sourceClass: "truth"}
	root := &findingCandidate{record: FindingRecord{ID: "F-0003", CampaignID: "C-TEST"}, sourceClass: "truth"}
	eligible := map[string]*findingCandidate{
		dependent.storageKey(): dependent, dependency.storageKey(): dependency, root.storageKey(): root,
	}
	visible, err := expandFindingRelations(context.Background(), db,
		[]*findingCandidate{dependent, dependency, root}, eligible, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 3 || !containsString(dependent.relationAlerts, "stale-dependent:F-0002") ||
		!containsString(dependency.relationAlerts, "stale-dependent:F-0003") {
		t.Fatalf("retrieval omitted stale dependent signal: visible=%d alerts=%v", len(visible), dependent.relationAlerts)
	}
	// Cards carry the bounded direct-hop signal only. The full transitive
	// path inventory scales with the corpus dependency graph and priced
	// finding cards out of every realistic budget; it stays available from
	// FindingStalenessPaths and the stale-dependents state view.
	for _, alert := range dependent.relationAlerts {
		if strings.HasPrefix(alert, "stale-path:") {
			t.Fatalf("card alerts must stay bounded to direct hops: %v", dependent.relationAlerts)
		}
	}
	card := findingContextCard(dependent)
	if err := ValidateContextCard(card); err != nil || !containsString(card.RelationAlerts, "stale-dependent:F-0002") {
		t.Fatalf("typed stale alert was not card-safe: card=%+v err=%v", card, err)
	}
}

func TestClosureStateAggregatesStaleDependentsBoundedly(t *testing.T) {
	graph := NewCampaignGraph()
	graph.Campaign = &CampaignRecord{RecordMeta: RecordMeta{ID: "C-TEST"}}
	graph.Findings["F-0001"] = FindingRecord{
		ID: "F-0001", VerifiedAt: "2026-07-01", Relations: FindingRelations{DependsOn: []string{"F-0002"}},
	}
	graph.Findings["F-0002"] = FindingRecord{ID: "F-0002", VerifiedAt: "2026-08-01"}
	graph.Findings["F-0003"] = FindingRecord{
		ID: "F-0003", VerifiedAt: "2026-09-01", Relations: FindingRelations{DependsOn: []string{"F-0001"}},
	}
	view := StateView{Status: "ok", Cards: []ContextCard{}}
	appendFindingStalenessState(graph, &view)
	if view.Status != "attention" || len(view.Cards) != 1 ||
		view.Cards[0].Metadata["findingIds"] != "F-0001,F-0003" ||
		!strings.Contains(view.Cards[0].Metadata["paths"], "F-0003>F-0001>F-0002") ||
		!strings.Contains(view.Cards[0].Claim, "re-verified") {
		t.Fatalf("closure state omitted bounded stale-dependent visibility: %+v", view)
	}
}
