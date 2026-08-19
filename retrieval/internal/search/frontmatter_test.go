package search

import "testing"

const goodDoc = `---
status: promoted
kind: fact
grade: direct
tags: [entities, animation]
idents: [idAnimatedEntity::AttachJoint, ai_bindFlags]
evidence: [archive/demo/reports/R-007.md, archive/demo/reports/R-008.md]
---
# Entity binding goes through idAnimatedEntity::AttachJoint

Body text here.
`

func TestParseDocFrontmatter(t *testing.T) {
	d := ParseDoc("docs/engine/joint-binding.md", goodDoc)
	if d.Status != "promoted" || d.Kind != "fact" || d.Grade != "direct" {
		t.Fatalf("fields: %+v", d)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "entities" {
		t.Fatalf("tags: %v", d.Tags)
	}
	if len(d.Idents) != 2 || d.Idents[0] != "idAnimatedEntity::AttachJoint" || d.Idents[1] != "ai_bindFlags" {
		t.Fatalf("idents: %v", d.Idents)
	}
	if len(d.Evidence) != 2 || d.Evidence[0] != "archive/demo/reports/R-007.md" || d.Evidence[1] != "archive/demo/reports/R-008.md" {
		t.Fatalf("evidence: %v", d.Evidence)
	}
	if d.Title != "Entity binding goes through idAnimatedEntity::AttachJoint" {
		t.Fatalf("title: %q", d.Title)
	}
	if d.Warning != "" {
		t.Fatalf("unexpected warning: %q", d.Warning)
	}
}

func TestParseDocMalformedIsLenient(t *testing.T) {
	d := ParseDoc("docs/broken.md", "---\nstatus: promoted\nno closing fence\n# A heading\nbody")
	if d.Warning == "" {
		t.Fatal("want warning for unclosed frontmatter")
	}
	if d.Body == "" || d.Status != "" {
		t.Fatalf("malformed doc must fall back to plain text: %+v", d)
	}
}

func TestParseDocCRLFAndNoFrontmatter(t *testing.T) {
	d := ParseDoc("docs/x.md", "# Title line\r\nbody\r\n")
	if d.Title != "Title line" || d.Warning != "" {
		t.Fatalf("%+v", d)
	}
	d2 := ParseDoc("docs/spawn-limit.md", "just prose, no heading")
	if d2.Title != "spawn-limit" {
		t.Fatalf("filename-stem title fallback, got %q", d2.Title)
	}
}
