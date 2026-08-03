package knowledge

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func leaseTestResponse(t *testing.T) FindingQueryResponse {
	t.Helper()
	response := FindingQueryResponse{
		Query:  "Which registration table is current?",
		Status: "answered",
		Cards: []ContextCard{{
			SchemaVersion: CampaignSchemaVersion,
			ID:            "F-0042", CardType: "finding",
			Claim:       "Table Alpha drives resource registration.",
			SourceClass: "campaign", Handle: "finding:F-0042",
			ExpansionTokens: 24,
			Metadata: map[string]string{
				"recordDigest": "sha256:" + strings.Repeat("a", 64),
			},
		}},
		TokenBudget: 4096,
		Trace: FindingQueryTrace{
			AnalyzerVersion:  IdentifierAnalyzerVersion,
			FindingFormat:    FindingFormatVersion,
			FilteredByReason: map[string]int{},
		},
	}
	finalized, err := finalizeFindingResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	return finalized
}

func leaseTestService(mode string) *Service {
	bootstrap := DefaultBootstrapConfig()
	bootstrap.Context.LeaseMode = mode
	return &Service{
		Configuration: Configuration{Bootstrap: bootstrap, Valid: true},
		contextLeases: map[string]*contextLeaseState{},
	}
}

func TestContextLeaseStateBoundsFailClosed(t *testing.T) {
	t.Run("lease count", func(t *testing.T) {
		service := leaseTestService("memory-only")
		for index := 0; index < maxContextLeases; index++ {
			service.contextLeases[fmt.Sprintf("lease-%03d", index)] = newContextLeaseState()
		}
		_, err := service.applyContextLease(
			leaseTestResponse(t),
			FindingQueryOptions{ContextLeaseID: "one-lease-too-many"},
			"generation-1",
		)
		if err == nil || !strings.Contains(err.Error(), "lease capacity") {
			t.Fatalf("unbounded lease allocation was accepted: %v", err)
		}
	})

	t.Run("card count", func(t *testing.T) {
		service := leaseTestService("memory-only")
		state := newContextLeaseState()
		for index := 0; index < maxContextLeaseCards; index++ {
			state.servedCards[fmt.Sprintf("finding:F-%04d@sha256:%064x", index, index)] = true
		}
		service.contextLeases["full-card-ledger"] = state
		_, err := service.applyContextLease(
			leaseTestResponse(t),
			FindingQueryOptions{ContextLeaseID: "full-card-ledger"},
			"generation-1",
		)
		if err == nil || !strings.Contains(err.Error(), "card bound") {
			t.Fatalf("unbounded card ledger growth was accepted: %v", err)
		}
		if state.queryCount != 0 || len(state.servedCards) != maxContextLeaseCards {
			t.Fatalf("failed request partially committed lease state: %#v", state)
		}
	})

	t.Run("source count", func(t *testing.T) {
		service := leaseTestService("memory-only")
		state := newContextLeaseState()
		for index := 0; index < maxContextLeaseSources; index++ {
			state.sourceDigests["sha256:"+fmt.Sprintf("%064x", index)] = true
		}
		service.contextLeases["full-source-ledger"] = state
		_, err := service.applyContextLease(
			leaseTestResponse(t),
			FindingQueryOptions{ContextLeaseID: "full-source-ledger"},
			"generation-1",
		)
		if err == nil || !strings.Contains(err.Error(), "source bound") {
			t.Fatalf("unbounded source ledger growth was accepted: %v", err)
		}
		if state.queryCount != 0 || len(state.sourceDigests) != maxContextLeaseSources {
			t.Fatalf("failed request partially committed lease state: %#v", state)
		}
	})
}

func TestContextLeaseDeduplicatesTracksAndResets(t *testing.T) {
	service := leaseTestService("memory-only")
	options := FindingQueryOptions{ContextLeaseID: "work-W-0042"}
	first, err := service.applyContextLease(leaseTestResponse(t), options, "generation-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Cards) != 1 || first.ContextLease == nil ||
		first.ContextLease.ReturnedCards != 1 || first.ContextLease.DeduplicatedCards != 0 ||
		first.ContextLease.QueryCount != 1 || first.ContextLease.CumulativeServedTokens != first.EstimatedTokens ||
		len(first.ContextLease.ServedSourceDigests) != 1 ||
		!strings.HasPrefix(first.ContextLease.Digest, "sha256:") {
		t.Fatalf("first lease receipt is incomplete: %#v", first.ContextLease)
	}
	second, err := service.applyContextLease(leaseTestResponse(t), options, "generation-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Cards) != 0 || second.Status != "abstained" || second.ContextLease == nil ||
		second.ContextLease.DeduplicatedCards != 1 || second.ContextLease.QueryCount != 2 ||
		second.ContextLease.CumulativeSourceCount != 1 ||
		second.ContextLease.CumulativeServedTokens != first.EstimatedTokens+second.EstimatedTokens {
		t.Fatalf("repeated card was not accounted for deterministically: %#v", second)
	}
	reset := options
	reset.ResetContextLease = true
	third, err := service.applyContextLease(leaseTestResponse(t), reset, "generation-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Cards) != 1 || third.ContextLease == nil || !third.ContextLease.Reset ||
		third.ContextLease.QueryCount != 1 || third.ContextLease.Generation != "generation-2" ||
		third.ContextLease.CumulativeServedTokens != third.EstimatedTokens {
		t.Fatalf("reset did not start a fresh process-local lease: %#v", third.ContextLease)
	}
}

func TestContextLeaseReceiptIsBoundedAndDeterministicAcrossAdapters(t *testing.T) {
	options := FindingQueryOptions{ContextLeaseID: "adapter-parity"}
	left, err := leaseTestService("memory-only").applyContextLease(
		leaseTestResponse(t), options, "generation-1")
	if err != nil {
		t.Fatal(err)
	}
	right, err := leaseTestService("memory-only").applyContextLease(
		leaseTestResponse(t), options, "generation-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) || left.Digest != right.Digest {
		t.Fatalf("fresh CLI/MCP service instances produced different lease receipts:\nleft=%#v\nright=%#v", left, right)
	}
	if len(left.ContextLease.ServedSourceDigests) > 5 {
		t.Fatalf("lease receipt exposed an unbounded current source set: %#v", left.ContextLease)
	}
	for _, invalid := range []FindingQueryOptions{
		{ResetContextLease: true},
		{ContextLeaseID: "unsafe lease id"},
	} {
		if _, err := leaseTestService("memory-only").applyContextLease(
			leaseTestResponse(t), invalid, "generation-1"); err == nil {
			t.Fatalf("invalid lease request was accepted: %#v", invalid)
		}
	}
	if _, err := leaseTestService("none").applyContextLease(
		leaseTestResponse(t), options, "generation-1"); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled lease mode was not enforced: %v", err)
	}
}

func TestContextLeaseConcurrentSameLeaseServesCardOnce(t *testing.T) {
	const workers = 24
	service := leaseTestService("memory-only")
	base := leaseTestResponse(t)
	options := FindingQueryOptions{ContextLeaseID: "concurrent-same-lease"}
	type result struct {
		response FindingQueryResponse
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response, err := service.applyContextLease(base, options, "generation-1")
			results <- result{response: response, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	served := 0
	queryCounts := map[int]bool{}
	for output := range results {
		if output.err != nil {
			t.Fatalf("concurrent lease query failed: %v", output.err)
		}
		receipt := output.response.ContextLease
		if receipt == nil || receipt.CumulativeSourceCount != 1 ||
			receipt.CumulativeServedTokens < receipt.CurrentResponseTokens {
			t.Fatalf("concurrent lease receipt is incomplete: %#v", receipt)
		}
		if queryCounts[receipt.QueryCount] {
			t.Fatalf("concurrent lease reused query count %d", receipt.QueryCount)
		}
		queryCounts[receipt.QueryCount] = true
		switch len(output.response.Cards) {
		case 1:
			served++
			if receipt.DeduplicatedCards != 0 || receipt.ReturnedCards != 1 {
				t.Fatalf("serving receipt has wrong accounting: %#v", receipt)
			}
		case 0:
			if receipt.DeduplicatedCards != 1 || receipt.ReturnedCards != 0 {
				t.Fatalf("deduplicated receipt has wrong accounting: %#v", receipt)
			}
		default:
			t.Fatalf("concurrent lease returned an impossible card count: %d", len(output.response.Cards))
		}
	}
	if served != 1 || len(queryCounts) != workers {
		t.Fatalf("same lease served %d cards across %d distinct queries, want 1 and %d",
			served, len(queryCounts), workers)
	}
	for queryCount := 1; queryCount <= workers; queryCount++ {
		if !queryCounts[queryCount] {
			t.Fatalf("concurrent lease omitted serialized query count %d", queryCount)
		}
	}

	service.contextLeaseMu.Lock()
	state := service.contextLeases[options.ContextLeaseID]
	if state == nil || state.queryCount != workers || len(state.servedCards) != 1 ||
		len(state.sourceDigests) != 1 || state.cumulativeTokens < 1 {
		service.contextLeaseMu.Unlock()
		t.Fatalf("concurrent lease committed inconsistent state: %#v", state)
	}
	service.contextLeaseMu.Unlock()
}
