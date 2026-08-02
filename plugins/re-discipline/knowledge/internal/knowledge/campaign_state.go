package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
)

// CampaignState is the bounded health census exposed by diagnostic status.
// It is derived only from canonical v2 records; legacy campaign prose is
// intentionally invisible outside the explicit migrator.
type CampaignState struct {
	CampaignID         string   `json:"campaignId,omitempty"`
	Slug               string   `json:"slug,omitempty"`
	Status             string   `json:"status,omitempty"`
	Revision           int64    `json:"revision,omitempty"`
	LastEventID        string   `json:"lastEventId,omitempty"`
	StateViewPresent   bool     `json:"stateViewPresent"`
	BlockerCount       int      `json:"blockerCount"`
	PendingRunCount    int      `json:"pendingRunCount"`
	PendingReviewCount int      `json:"pendingReviewCount"`
	Attention          []string `json:"attention,omitempty"`
}

func (service *Service) campaignStatus() []CampaignState {
	store := NewStateStoreWithBoundary(service.Boundary)
	campaigns, err := store.ListCampaigns()
	if err != nil {
		return []CampaignState{{Attention: []string{"canonical campaign census failed: " + err.Error()}}}
	}
	states := make([]CampaignState, 0, len(campaigns))
	for _, campaign := range campaigns {
		state := CampaignState{
			CampaignID: campaign.ID, Slug: campaign.Slug, Status: campaign.Status,
			Revision: campaign.Revision, LastEventID: campaign.LastEventID,
			Attention: []string{},
		}
		viewPath := filepath.Join(service.Boundary.Root, "active", campaign.Slug, "STATE.md")
		if info, statErr := os.Lstat(viewPath); statErr == nil && info.Mode().IsRegular() {
			state.StateViewPresent = true
		} else if statErr == nil {
			state.Attention = append(state.Attention, "generated STATE.md is not a regular file")
		} else if os.IsNotExist(statErr) {
			state.Attention = append(state.Attention, "generated STATE.md is missing")
		} else {
			state.Attention = append(state.Attention, "generated STATE.md cannot be inspected: "+statErr.Error())
		}
		graph, graphErr := store.LoadCampaignGraph(campaign.ID)
		if graphErr != nil {
			state.Attention = append(state.Attention, fmt.Sprintf("canonical graph is invalid: %v", graphErr))
			states = append(states, state)
			continue
		}
		for _, item := range graph.WorkItems {
			if item.State == "blocked" || (item.State == "deferred" && item.Deferment != nil && item.Deferment.BlocksClosure) {
				state.BlockerCount++
			}
		}
		for _, run := range graph.Runs {
			if run.Status == "returned" {
				state.PendingRunCount++
			}
		}
		for _, intake := range graph.Intakes {
			if intake.Status == "submitted" || intake.Status == "draft" {
				state.PendingReviewCount++
			}
		}
		states = append(states, state)
	}
	return states
}
