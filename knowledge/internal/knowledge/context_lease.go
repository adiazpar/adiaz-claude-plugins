package knowledge

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	contextLeaseSchemaVersion = 1
	maxContextLeases          = 128
	maxContextLeaseCards      = 4096
	maxContextLeaseSources    = 4096
)

var contextLeaseIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ContextLeaseReceipt is the bounded, deterministic proof of process-local
// deduplication returned by both the CLI and MCP query adapters. The receipt
// exposes only the source digests served by this response plus a digest and
// count for the cumulative set, so its wire size stays bounded as the lease
// grows.
type ContextLeaseReceipt struct {
	SchemaVersion          int      `json:"schemaVersion"`
	LeaseID                string   `json:"leaseId"`
	Mode                   string   `json:"mode"`
	Generation             string   `json:"generation"`
	Reset                  bool     `json:"reset"`
	QueryCount             int      `json:"queryCount"`
	ReturnedCards          int      `json:"returnedCards"`
	DeduplicatedCards      int      `json:"deduplicatedCards"`
	CurrentResponseTokens  int      `json:"currentResponseTokens"`
	CumulativeServedTokens int      `json:"cumulativeServedTokens"`
	ServedSourceDigests    []string `json:"servedSourceDigests"`
	CumulativeSourceCount  int      `json:"cumulativeSourceCount"`
	CumulativeSourceDigest string   `json:"cumulativeSourceDigest"`
	Digest                 string   `json:"digest"`
}

type contextLeaseState struct {
	servedCards      map[string]bool
	sourceDigests    map[string]bool
	cumulativeTokens int
	queryCount       int
}

func newContextLeaseState() *contextLeaseState {
	return &contextLeaseState{
		servedCards:   map[string]bool{},
		sourceDigests: map[string]bool{},
	}
}

func (service *Service) applyContextLease(
	response FindingQueryResponse,
	options FindingQueryOptions,
	generation string,
) (FindingQueryResponse, error) {
	leaseID := strings.TrimSpace(options.ContextLeaseID)
	mode := service.Configuration.Bootstrap.Context.LeaseMode
	if mode == "" {
		mode = "memory-only"
	}
	if leaseID == "" {
		if options.ResetContextLease {
			return FindingQueryResponse{}, errors.New("resetContextLease requires contextLeaseId")
		}
		return response, nil
	}
	if !contextLeaseIDRE.MatchString(leaseID) {
		return FindingQueryResponse{}, errors.New("contextLeaseId must contain 1-128 safe identifier characters")
	}
	if mode != "memory-only" {
		return FindingQueryResponse{}, fmt.Errorf("context leases are disabled by context.leaseMode=%q", mode)
	}

	service.contextLeaseMu.Lock()
	defer service.contextLeaseMu.Unlock()
	if service.contextLeases == nil {
		service.contextLeases = map[string]*contextLeaseState{}
	}
	state, present := service.contextLeases[leaseID]
	if !present {
		if len(service.contextLeases) >= maxContextLeases {
			return FindingQueryResponse{}, fmt.Errorf(
				"process-local context lease capacity (%d) reached", maxContextLeases)
		}
		state = newContextLeaseState()
	}
	if options.ResetContextLease {
		state = newContextLeaseState()
	}

	filtered := make([]ContextCard, 0, len(response.Cards))
	servedDigests := make([]string, 0, len(response.Cards))
	deduplicated := 0
	for _, card := range response.Cards {
		sourceDigest, err := contextCardSourceDigest(card)
		if err != nil {
			return FindingQueryResponse{}, err
		}
		cardKey := card.Handle + "@" + sourceDigest
		if state.servedCards[cardKey] {
			deduplicated++
			continue
		}
		filtered = append(filtered, card)
		servedDigests = append(servedDigests, sourceDigest)
	}
	servedDigests = SortedUnique(servedDigests)
	potentialSources := cloneStringSet(state.sourceDigests)
	for _, digest := range servedDigests {
		potentialSources[digest] = true
	}
	if len(potentialSources) > maxContextLeaseSources {
		return FindingQueryResponse{}, fmt.Errorf(
			"context lease %q reached its %d-source bound; reset it after compaction",
			leaseID, maxContextLeaseSources)
	}

	response.Cards = filtered
	response.Status = findingResponseStatus(filtered, response.TierDisagreements)
	response.Omitted += deduplicated
	response.ContextLease = nil
	response.EstimatedTokens = 0
	response.Digest = ""

	// Receipt metadata participates in the response token estimate and the
	// response digest. Iterate to the small integer fixed point before
	// committing the in-memory state, dropping lowest-ranked cards if the
	// mandatory receipt would otherwise exceed the caller's hard budget.
	for {
		candidateSources := cloneStringSet(state.sourceDigests)
		for _, digest := range servedDigestsForCards(response.Cards) {
			candidateSources[digest] = true
		}
		for iteration := 0; iteration < 12; iteration++ {
			receipt := ContextLeaseReceipt{
				SchemaVersion: contextLeaseSchemaVersion,
				LeaseID:       leaseID, Mode: "memory-only", Generation: generation,
				Reset: options.ResetContextLease, QueryCount: state.queryCount + 1,
				ReturnedCards: len(response.Cards), DeduplicatedCards: deduplicated,
				CurrentResponseTokens:  response.EstimatedTokens,
				CumulativeServedTokens: state.cumulativeTokens + response.EstimatedTokens,
				ServedSourceDigests:    servedDigestsForCards(response.Cards),
				CumulativeSourceCount:  len(candidateSources),
				CumulativeSourceDigest: digestStringSet(candidateSources),
			}
			receipt.Digest, _ = contextLeaseReceiptDigest(receipt)
			response.ContextLease = &receipt
			finalized, err := finalizeFindingResponse(response)
			if err != nil {
				break
			}
			if finalized.EstimatedTokens == receipt.CurrentResponseTokens &&
				receipt.CumulativeServedTokens == state.cumulativeTokens+finalized.EstimatedTokens {
				potentialCards := len(state.servedCards)
				for _, card := range finalized.Cards {
					digest, _ := contextCardSourceDigest(card)
					if !state.servedCards[card.Handle+"@"+digest] {
						potentialCards++
					}
				}
				if potentialCards > maxContextLeaseCards {
					return FindingQueryResponse{}, fmt.Errorf(
						"context lease %q reached its %d-card bound; reset it after compaction",
						leaseID, maxContextLeaseCards)
				}
				for _, card := range finalized.Cards {
					digest, _ := contextCardSourceDigest(card)
					state.servedCards[card.Handle+"@"+digest] = true
					state.sourceDigests[digest] = true
				}
				state.cumulativeTokens += finalized.EstimatedTokens
				state.queryCount++
				service.contextLeases[leaseID] = state
				return finalized, nil
			}
			response = finalized
		}
		if len(response.Cards) == 0 {
			return FindingQueryResponse{}, errors.New("finding query budget is too small for mandatory context lease receipt")
		}
		response.Cards = response.Cards[:len(response.Cards)-1]
		response.Omitted++
		response.Status = findingResponseStatus(response.Cards, response.TierDisagreements)
		response.ContextLease = nil
		response.EstimatedTokens = 0
		response.Digest = ""
	}
}

func contextCardSourceDigest(card ContextCard) (string, error) {
	for _, key := range []string{"recordDigest", "digest"} {
		if value := strings.TrimSpace(card.Metadata[key]); value != "" {
			if !strings.HasPrefix(value, "sha256:") {
				value = "sha256:" + value
			}
			if !digestRE.MatchString(value) {
				return "", fmt.Errorf("context card %s has malformed %s", card.ID, key)
			}
			return value, nil
		}
	}
	digest, err := CanonicalDigest(card)
	if err != nil {
		return "", fmt.Errorf("digest context card %s: %w", card.ID, err)
	}
	return digest, nil
}

func servedDigestsForCards(cards []ContextCard) []string {
	digests := make([]string, 0, len(cards))
	for _, card := range cards {
		digest, err := contextCardSourceDigest(card)
		if err == nil {
			digests = append(digests, digest)
		}
	}
	return SortedUnique(digests)
}

func cloneStringSet(input map[string]bool) map[string]bool {
	output := make(map[string]bool, len(input))
	for value := range input {
		output[value] = true
	}
	return output
}

func digestStringSet(values map[string]bool) string {
	ordered := make([]string, 0, len(values))
	for value := range values {
		ordered = append(ordered, value)
	}
	sort.Strings(ordered)
	digest, _ := CanonicalDigest(ordered)
	return digest
}

func contextLeaseReceiptDigest(receipt ContextLeaseReceipt) (string, error) {
	receipt.Digest = ""
	return CanonicalDigest(receipt)
}
