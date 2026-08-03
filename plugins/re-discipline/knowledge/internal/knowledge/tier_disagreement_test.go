package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func tierDisagreementDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/tier-disagreement.db")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE documents (id TEXT PRIMARY KEY,path TEXT NOT NULL,tier TEXT NOT NULL)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY,document_id TEXT NOT NULL)`,
		`CREATE TABLE edges (source_id TEXT NOT NULL,target_id TEXT NOT NULL,kind TEXT NOT NULL)`,
		`INSERT INTO documents(id,path,tier) VALUES
		 ('truth-doc','docs/truth/current.md','truth'),
		 ('memory-doc','.re-discipline/memory/topics/recall.md','memory')`,
		`INSERT INTO chunks(id,document_id) VALUES('truth-chunk','truth-doc'),('memory-chunk','memory-doc')`,
		`INSERT INTO edges(source_id,target_id,kind) VALUES('memory-chunk','truth-chunk','contradicts')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	return db
}

func TestTierDisagreementIsTypedAndKeepsTruthAuthority(t *testing.T) {
	db := tierDisagreementDB(t)
	defer db.Close()
	status, err := loadTierDisagreements(context.Background(), db, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if status.Count != 1 || len(status.Signals) != 1 {
		t.Fatalf("explicit cross-tier contradiction was not surfaced: %+v", status)
	}
	signal := status.Signals[0]
	if signal.Signal != "truth-vs-memory" || signal.AuthorityTier != "truth" ||
		signal.AdvisoryTier != "memory" || !signal.RequiresManagerReview ||
		status.AuthorityRule != "truth-remains-authoritative-until-closure" {
		t.Fatalf("tier signal granted memory authority or lost its type: %+v", status)
	}
	response := SearchResponse{
		Results:           []SearchResult{{Citation: Citation{Path: signal.MemoryPath}}},
		TierDisagreements: status.Signals,
	}
	body, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(body), `"tierDisagreements"`) ||
		!strings.Contains(string(body), `"authorityTier":"truth"`) {
		t.Fatalf("retrieval response omitted typed tier disagreement: %s err=%v", body, err)
	}
}

func TestPublicFindingQueryReturnsBoundedTierDisagreement(t *testing.T) {
	fixture := buildFindingIndexFixture(t)
	db, err := sql.Open("sqlite", fixture.generation.Database)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO documents(id,path,tier,title,content_hash,size,source_kind) VALUES
		 ('truth-disagreement','docs/truth/current.md','truth','truth','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',12,'document'),
		 ('memory-disagreement','.re-discipline/memory/topics/recall.md','memory','memory','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',12,'memory')`,
		`INSERT INTO chunks(id,document_id,path,tier,heading,start_line,end_line,content,content_hash) VALUES
		 ('truth-disagreement-chunk','truth-disagreement','docs/truth/current.md','truth','Truth',1,1,'current truth','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),
		 ('memory-disagreement-chunk','memory-disagreement','.re-discipline/memory/topics/recall.md','memory','Memory',1,1,'contrary recall','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')`,
		`INSERT INTO edges(source_id,target_id,kind) VALUES('memory-disagreement-chunk','truth-disagreement-chunk','contradicts')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := fixture.retriever.QueryFindingCards(context.Background(), FindingQueryOptions{
		Query: "Where does memory contradict current truth?", QueryClass: "contradiction",
		Limit: 1, TokenBudget: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.QueryClass != "contradiction" || response.Status != "conflicted" ||
		len(response.TierDisagreements) != 1 || response.TierDisagreementsOmitted != 0 ||
		response.TierDisagreements[0].AuthorityTier != "truth" ||
		response.TierAuthorityRule != "truth-remains-authoritative-until-closure" ||
		response.EstimatedTokens > response.TokenBudget {
		t.Fatalf("public finding query lost bounded typed disagreement authority: %+v", response)
	}
	definitions, err := json.Marshal(toolDefinitions())
	if err != nil || !strings.Contains(string(definitions), `"queryClass"`) ||
		!strings.Contains(string(definitions), `"contradiction"`) {
		t.Fatalf("MCP query schema omitted contradiction query class: %s err=%v", definitions, err)
	}
}

func TestTierDisagreementRequiresExplicitCrossTierEdge(t *testing.T) {
	documents := []SourceDocument{
		{ID: "truth-doc", Path: "docs/truth/current.md", Tier: "truth"},
		{ID: "memory-doc", Path: ".re-discipline/memory/topics/recall.md", Tier: "memory"},
	}
	chunks := []Chunk{
		{ID: "truth-chunk", DocumentID: "truth-doc", Path: documents[0].Path, Tier: "truth", Content: "Current truth."},
		{ID: "memory-chunk", DocumentID: "memory-doc", Path: documents[1].Path, Tier: "memory",
			Content: "contradicts: ../../../docs/truth/current.md"},
	}
	edges := BuildGraphEdges(documents, chunks)
	found := false
	for _, edge := range edges {
		found = found || edge.Source == "memory-chunk" && edge.Target == "truth-chunk" && edge.Kind == "contradicts"
	}
	if !found {
		t.Fatalf("explicit memory-to-truth contradiction did not become a typed edge: %+v", edges)
	}
}

func TestUserStatusReportsTierDisagreementWithoutChangingAuthority(t *testing.T) {
	status := BuildUserStatus(map[string]any{
		"configuration":          map[string]any{"valid": true, "memoryMode": "shared-only"},
		"index":                  map[string]any{"present": true, "integrity": true},
		"memoryProposalsPending": 0,
		"tierDisagreements":      TierDisagreementStatus{Count: 2},
	})
	if len(status.Attention) != 1 || !strings.Contains(status.Attention[0], "Truth remains authoritative") {
		t.Fatalf("plain status omitted the non-authoritative memory boundary: %+v", status)
	}
}
