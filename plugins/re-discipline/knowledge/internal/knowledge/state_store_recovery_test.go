package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reopenStateTestStore(t *testing.T, root string) *StateStore {
	t.Helper()
	store, err := NewStateStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fixed, _ := time.Parse(time.RFC3339, stateTestTime)
	store.Now = func() time.Time { return fixed }
	return store
}

func TestStateStoreRecoversEveryPublicationSeam(t *testing.T) {
	points := []string{
		FailAfterJournal,
		FailAfterRecordPublish,
		FailAfterEventPublish,
		FailAfterReceiptPublish,
		FailBeforeHeadPublish,
		FailAfterHeadPublish,
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			store, root := newStateTestStore(t)
			_, opening := openStateTestCampaign(t, store)
			request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-crash", "idem-crash")
			store.Failpoint = func(hit StateFailpoint) error {
				if hit.Name == point {
					return errors.New("simulated process loss")
				}
				return nil
			}
			if _, err := store.Apply(context.Background(), request); err == nil {
				t.Fatal("failpoint did not interrupt the transaction")
			}

			recovered := reopenStateTestStore(t, root)
			if err := recovered.Recover(context.Background()); err != nil {
				t.Fatalf("recover: %v", err)
			}
			head, err := recovered.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			graph, err := recovered.LoadCampaignGraph("test-campaign")
			if err != nil {
				t.Fatalf("recovered graph: %v", err)
			}
			committed := point == FailAfterHeadPublish
			_, hasWork := graph.WorkItems["W-0002"]
			if committed {
				if head.Revision != opening.ResultingHead.Revision+1 || !hasWork {
					t.Fatalf("published head did not roll forward: head=%+v hasWork=%v", head, hasWork)
				}
				receipt, err := recovered.Apply(context.Background(), request)
				if err != nil || receipt.ResultingHead.Digest != head.Digest {
					t.Fatalf("post-recovery retry did not replay committed receipt: %v", err)
				}
			} else if head.Digest != opening.ResultingHead.Digest || hasWork {
				t.Fatalf("uncommitted transaction did not roll back: head=%+v hasWork=%v", head, hasWork)
			}
			staging := filepath.Join(root, ".re-discipline", "state", "staging")
			entries, err := os.ReadDir(staging)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("recovery left transaction staging behind: %v", entries)
			}
			events, err := os.ReadFile(filepath.Join(root, "active", "test-campaign", "events", "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			wantLines := 1
			if committed {
				wantLines = 2
			}
			if lines := len(strings.Split(strings.TrimSpace(string(events)), "\n")); lines != wantLines {
				t.Fatalf("event journal has %d lines, want %d", lines, wantLines)
			}
		})
	}
}

func TestStateStoreRecoveryRefusesOutOfBandTargetChange(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-dirty", "idem-dirty")
	store.Failpoint = func(hit StateFailpoint) error {
		if hit.Name == FailAfterJournal {
			return errors.New("simulated process loss")
		}
		return nil
	}
	if _, err := store.Apply(context.Background(), request); err == nil {
		t.Fatal("failpoint did not interrupt the transaction")
	}
	target := filepath.Join(root, "active", "test-campaign", "work-items", "W-0002.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("out-of-band change")
	if err := os.WriteFile(target, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	recovered := reopenStateTestStore(t, root)
	if err := recovered.Recover(context.Background()); !errors.Is(err, ErrStateDirty) {
		t.Fatalf("recovery overwrote an unexpected target instead of failing dirty: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != string(foreign) {
		t.Fatalf("dirty target was changed by recovery: %q err=%v", body, err)
	}
}

func TestStateStoreRejectsHardLinkedCanonicalRecord(t *testing.T) {
	store, root := newStateTestStore(t)
	openStateTestCampaign(t, store)
	campaign := filepath.Join(root, "active", "test-campaign", "campaign.json")
	alias := filepath.Join(root, "campaign-alias.json")
	if err := os.Link(campaign, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, _, err := store.ReadCanonicalRecord("active/test-campaign/campaign.json"); err == nil {
		t.Fatal("hard-linked canonical record was accepted")
	}
}

func TestStateStoreRecoveryCleansCommittedStaging(t *testing.T) {
	store, root := newStateTestStore(t)
	_, receipt := openStateTestCampaign(t, store)
	staging := filepath.Join(root, ".re-discipline", "state", "staging", receipt.TransactionID)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "orphan"), []byte("left after journal commit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(context.Background()); err != nil {
		t.Fatalf("recover committed transaction: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("committed staging was not cleaned: %v", err)
	}
}

func TestStateViewRecoversPreparedJournalBeforeReading(t *testing.T) {
	store, root := newStateTestStore(t)
	_, opening := openStateTestCampaign(t, store)
	request := stateTestCreateWorkRequest(opening.ResultingHead, "W-0002", "corr-read-crash", "idem-read-crash")
	store.Failpoint = func(hit StateFailpoint) error {
		if hit.Name == FailAfterRecordPublish {
			return errors.New("simulated process loss")
		}
		return nil
	}
	if _, err := store.Apply(context.Background(), request); err == nil {
		t.Fatal("failpoint did not interrupt the transaction")
	}
	service := &Service{Boundary: store.Boundary}
	if _, err := service.State(context.Background(), StateRequest{Mode: "orient"}); err != nil {
		t.Fatalf("state read did not recover its prepared journal: %v", err)
	}
	recovered := reopenStateTestStore(t, root)
	graph, err := recovered.LoadCampaignGraph("C-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, present := graph.WorkItems["W-0002"]; present {
		t.Fatal("state read exposed an uncommitted record after recovery")
	}
}
