package knowledge

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CampaignMasterfileStaleAfter is how far a campaign's own artifacts may run
// ahead of its masterfile before the masterfile is called stale.
//
// CAMPAIGN.md is the cold-resume surface: the one file a session with no prior
// context reads to learn where the campaign is. Everything else under the
// campaign - reports, ledgers, evidence notes - is written by the work itself
// and keeps moving whether or not anyone rewrites the masterfile. So the
// masterfile is the only campaign file that rots by standing still, and nothing
// in the directory looks wrong while it does.
//
// Two days is deliberately loose. A checkpoint is expected at session end, so a
// single session's worth of drift is normal and must not raise an alarm;
// crossing two days means at least one session ended without one.
const CampaignMasterfileStaleAfter = 48 * time.Hour

// CampaignState is the per-campaign staleness observation status reports.
type CampaignState struct {
	Slug              string `json:"slug"`
	MasterfilePresent bool   `json:"masterfilePresent"`
	// Stale is the threshold verdict; StaleDays is the observed lag in whole
	// days regardless of the verdict, so a caller can see a campaign that is
	// approaching the threshold rather than only one that crossed it.
	Stale     bool `json:"stale"`
	StaleDays int  `json:"staleDays"`
	// NewestArtifact is the project-relative path that ran ahead. Naming it is
	// what makes the report actionable: it says which work the masterfile has
	// not caught up with.
	NewestArtifact string `json:"newestArtifact,omitempty"`
}

// campaignStatus surveys active campaigns for masterfile rot. It stats files
// and never reads them, so it stays inside the cheap-health-check budget that
// every session start runs.
func (service *Service) campaignStatus() []CampaignState {
	states := []CampaignState{}
	activeRoot := filepath.Join(service.Boundary.Root, "active")
	entries, err := os.ReadDir(activeRoot)
	if err != nil {
		return states
	}
	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			slugs = append(slugs, entry.Name())
		}
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		states = append(states, campaignMasterfileState(activeRoot, slug))
	}
	return states
}

func campaignMasterfileState(activeRoot, slug string) CampaignState {
	state := CampaignState{Slug: slug}
	campaignRoot := filepath.Join(activeRoot, slug)
	masterfile := filepath.Join(campaignRoot, "CAMPAIGN.md")
	info, err := os.Lstat(masterfile)
	if err != nil || !info.Mode().IsRegular() {
		// A campaign with no masterfile has nothing to be stale against;
		// open-campaign owns that failure, not this census.
		return state
	}
	state.MasterfilePresent = true
	masterfileTime := info.ModTime()

	newest := masterfileTime
	newestPath := ""
	_ = filepath.WalkDir(campaignRoot, func(
		path string, entry fs.DirEntry, walkErr error,
	) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "cache":
				if path != campaignRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || path == masterfile {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if entryInfo.ModTime().After(newest) {
			newest = entryInfo.ModTime()
			newestPath = path
		}
		return nil
	})
	lag := newest.Sub(masterfileTime)
	if lag <= 0 {
		return state
	}
	state.StaleDays = int(lag.Hours() / 24)
	state.Stale = lag > CampaignMasterfileStaleAfter
	if state.Stale && newestPath != "" {
		if relative, relErr := filepath.Rel(
			filepath.Dir(activeRoot), newestPath,
		); relErr == nil {
			state.NewestArtifact = filepath.ToSlash(relative)
		}
	}
	return state
}
