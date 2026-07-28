package knowledge

import (
	"context"
	"testing"
)

// The review ledger is the designated destination of record for HOLD
// dispositions, and close-campaign deletes the campaign directory that holds
// it. A ledger nobody can retrieve is a ledger managers are told to consult and
// cannot find, so indexing it is part of the same contract as writing it.
func TestCampaignReviewLedgerIsIndexedAsCampaignState(t *testing.T) {
	root := makeAdversarialProject(t)
	boundary, err := NewBoundary(root)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverSources(boundary, DefaultKnowledgeSettings())
	if err != nil {
		t.Fatal(err)
	}
	tier, indexed := "", false
	for _, document := range inventory.Documents {
		if document.Path == "active/fixture-campaign/REVIEWS.md" {
			tier, indexed = document.Tier, true
		}
	}
	if !indexed {
		t.Fatal("the campaign review ledger was not indexed")
	}
	// `active`, not `campaign`: the ledger carries the manager's own
	// dispositions rather than a drafter finding a manager rederived.
	if tier != "active" {
		t.Fatalf("review ledger tier = %q, want %q", tier, "active")
	}

	// One campaign masterfile, one toggle. A project that declines to index
	// campaign state declines to index both halves of it.
	settings := DefaultKnowledgeSettings()
	settings.Sources.ActiveCampaigns = false
	disabled, err := DiscoverSources(boundary, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range disabled.Documents {
		if document.Path == "active/fixture-campaign/REVIEWS.md" {
			t.Fatal("the review ledger ignored the activeCampaigns source toggle")
		}
	}

	service := newAdversarialService(t, root, nil)
	response, err := service.Search(context.Background(), SearchOptions{
		Query: "review-ledger-hold-sigma", QueryClass: "exact",
		AllowedTiers: []string{"active"}, Limit: 5, TokenBudget: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range response.Results {
		if result.Citation.Path == "active/fixture-campaign/REVIEWS.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("an unresolved hold recorded in the ledger was not retrievable")
	}
}
