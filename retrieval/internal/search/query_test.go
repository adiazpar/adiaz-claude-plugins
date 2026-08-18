package search

import (
	"strings"
	"testing"
)

func TestQueryRanksAndLabels(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "how do I bind entities to demon joints AttachJoint", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Path != "docs/engine/joint-binding.md" {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].Status != "promoted" || hits[0].Grade != "direct" {
		t.Fatalf("hit metadata: %+v", hits[0])
	}
}

func TestQuerySupersededDownranked(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "snapmap spawn limit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("superseded docs must still be retrievable")
	}
	last := hits[len(hits)-1]
	if last.Status != "superseded" {
		t.Fatalf("superseded must sort last: %+v", hits)
	}
	out := FormatHits(hits)
	if !strings.Contains(out, "[SUPERSEDED]") {
		t.Fatalf("superseded label missing:\n%s", out)
	}
}

func TestQueryEmptyAndUnusable(t *testing.T) {
	root := buildTestCorpus(t)
	hits, _, err := Query(root, "???", 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("unusable query: %v %v", hits, err)
	}
}
