package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adiaz/re-discipline-knowledge/internal/knowledge"
)

func TestRetiredContextPackCLICommandIsRemoved(t *testing.T) {
	err := run(context.Background(), []string{"context-pack"})
	if err == nil || !strings.Contains(err.Error(), "usage: re-discipline-knowledge") {
		t.Fatalf("retired context-pack command remained callable: %v", err)
	}
}

func TestNormalizedVsRawCLIRequiresExplicitProjectRoot(t *testing.T) {
	err := run(context.Background(), []string{"normalized-vs-raw"})
	if err == nil || err.Error() != "normalized-vs-raw requires --project-root" {
		t.Fatalf("normalized-vs-raw accepted an implicit project: %v", err)
	}
}

func writeCLIRequest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestContextPackCLIRequestMatchesMCPInput(t *testing.T) {
	input := writeCLIRequest(t, `{
  "action": "materialize",
  "target": {
    "kind": "recruiting-run",
    "candidateSlug": "candidate-one",
    "recruitingRunId": "20260802T190000Z"
  },
  "task": "Review the frame checksum",
  "role": "drafter",
  "allowedTiers": ["truth", "campaign"],
  "tokenBudget": 1024,
  "requiredPaths": ["docs/truth/engine.md"],
  "expectedDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expectedPackId": "context-aaaaaaaaaaaaaaaaaaaa"
}`)
	var request knowledge.ContextPackMaterializeRequest
	if err := decodeCLIRequest(input, &request); err != nil {
		t.Fatal(err)
	}
	if err := knowledge.ValidateContextPackMaterializeRequest(request); err != nil {
		t.Fatal(err)
	}
	if request.Action != "materialize" || request.Task != "Review the frame checksum" ||
		request.Target.Kind != "recruiting-run" ||
		request.Target.CandidateSlug != "candidate-one" ||
		request.Target.RecruitingRunID != "20260802T190000Z" ||
		request.Role != "drafter" ||
		!reflect.DeepEqual(request.AllowedTiers, []string{"truth", "campaign"}) ||
		request.TokenBudget != 1024 ||
		!reflect.DeepEqual(request.RequiredPaths, []string{"docs/truth/engine.md"}) ||
		request.ExpectedDigest != "sha256:"+strings.Repeat("a", 64) ||
		request.ExpectedPackID != "context-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("CLI did not decode the shared MCP operation request exactly: %#v", request)
	}
}

func TestQueryCLIRequestDecodesContextLeaseControls(t *testing.T) {
	input := writeCLIRequest(t, `{
  "query": "Which registration table is current?",
  "queryClass": "contradiction",
  "allowedProvenanceTiers": ["history", "backlog"],
  "contextLeaseId": "campaign-C-0042",
  "resetContextLease": true
}`)
	var request knowledge.FindingQueryOptions
	if err := decodeCLIRequest(input, &request); err != nil {
		t.Fatal(err)
	}
	if request.Query != "Which registration table is current?" || request.QueryClass != "contradiction" ||
		len(request.AllowedProvenanceTiers) != 2 || request.AllowedProvenanceTiers[0] != "history" ||
		request.AllowedProvenanceTiers[1] != "backlog" ||
		request.ContextLeaseID != "campaign-C-0042" ||
		!request.ResetContextLease {
		t.Fatalf("CLI did not decode context lease controls exactly: %#v", request)
	}
}

func TestQueryCLISessionKeepsOneLeaseCapableProcess(t *testing.T) {
	input := writeCLIRequest(t, `
{"query":"first","contextLeaseId":"campaign-C-0042"}
{"query":"second","contextLeaseId":"campaign-C-0042"}
`)
	var output bytes.Buffer
	queryCount := 0
	err := runQuerySessionInput(
		context.Background(), input, &output,
		func(_ context.Context, request knowledge.FindingQueryOptions) (knowledge.FindingQueryResponse, error) {
			queryCount++
			return knowledge.FindingQueryResponse{
				Query: request.Query, QueryClass: "auto", Status: "sufficient",
				Cards: []knowledge.ContextCard{}, TierDisagreements: []knowledge.TierDisagreement{},
				ContextLease: &knowledge.ContextLeaseReceipt{
					SchemaVersion: 1, LeaseID: request.ContextLeaseID,
					Mode: "memory-only", QueryCount: queryCount,
				},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	for expected := 1; expected <= 2; expected++ {
		var response knowledge.FindingQueryResponse
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.ContextLease == nil ||
			response.ContextLease.LeaseID != "campaign-C-0042" ||
			response.ContextLease.QueryCount != expected {
			t.Fatalf("session response %d lost lease continuity: %#v", expected, response)
		}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("query session emitted an unexpected trailing value: %v", err)
	}
}

func TestQueryCLISessionStreamsBeforeInputClosesAndPreservesLeaseDedup(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	delivered := false
	go func() {
		err := runQuerySession(
			context.Background(), inputReader, outputWriter,
			func(_ context.Context, request knowledge.FindingQueryOptions) (knowledge.FindingQueryResponse, error) {
				response := knowledge.FindingQueryResponse{
					Query: request.Query, QueryClass: "auto", Status: "sufficient",
					Cards: []knowledge.ContextCard{}, TierDisagreements: []knowledge.TierDisagreement{},
					ContextLease: &knowledge.ContextLeaseReceipt{
						SchemaVersion: 1, LeaseID: request.ContextLeaseID, Mode: "memory-only",
					},
				}
				if !delivered {
					delivered = true
					response.Cards = []knowledge.ContextCard{{SchemaVersion: 1, ID: "F-0001", CardType: "finding"}}
					response.ContextLease.QueryCount = 1
					response.ContextLease.ReturnedCards = 1
					return response, nil
				}
				response.Status = "abstained"
				response.ContextLease.QueryCount = 2
				response.ContextLease.DeduplicatedCards = 1
				return response, nil
			},
		)
		_ = outputWriter.CloseWithError(err)
		done <- err
	}()

	decoder := json.NewDecoder(outputReader)
	readResponse := func() <-chan struct {
		response knowledge.FindingQueryResponse
		err      error
	} {
		result := make(chan struct {
			response knowledge.FindingQueryResponse
			err      error
		}, 1)
		go func() {
			var response knowledge.FindingQueryResponse
			err := decoder.Decode(&response)
			result <- struct {
				response knowledge.FindingQueryResponse
				err      error
			}{response: response, err: err}
		}()
		return result
	}

	if _, err := fmt.Fprintln(inputWriter, `{"query":"first","contextLeaseId":"campaign-C-0042"}`); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-readResponse():
		if result.err != nil {
			t.Fatal(result.err)
		}
		if len(result.response.Cards) != 1 || result.response.ContextLease == nil ||
			result.response.ContextLease.QueryCount != 1 {
			t.Fatalf("first streamed response was incomplete: %#v", result.response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first session response waited for stdin to close")
	}

	if _, err := fmt.Fprintln(inputWriter, `{"query":"second","contextLeaseId":"campaign-C-0042"}`); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-readResponse():
		if result.err != nil {
			t.Fatal(result.err)
		}
		if len(result.response.Cards) != 0 || result.response.ContextLease == nil ||
			result.response.ContextLease.QueryCount != 2 ||
			result.response.ContextLease.DeduplicatedCards != 1 {
			t.Fatalf("second streamed response lost lease deduplication: %#v", result.response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second session response was not streamed")
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query session did not finish after stdin closed")
	}
}

func TestContextPackCLIRejectsRetiredAdapterFields(t *testing.T) {
	for _, field := range []string{"tiers", "output", "path", "verbosity"} {
		t.Run(field, func(t *testing.T) {
			input := writeCLIRequest(t, `{
  "action": "preview",
  "task": "Review the frame checksum",
  "role": "drafter",
  "`+field+`": "retired"
}`)
			var request knowledge.ContextPackMaterializeRequest
			err := decodeCLIRequest(input, &request)
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("retired field %q was not rejected strictly: %v", field, err)
			}
		})
	}
}

func TestContextPackCLIValidatesOperationBeforeProjectResolution(t *testing.T) {
	input := writeCLIRequest(t, `{"task":"Review the frame checksum","role":"drafter"}`)
	err := run(context.Background(), []string{
		"context-pack-materialize", "--input", input,
		"--project-root", filepath.Join(t.TempDir(), "not-a-project"),
	})
	if err == nil || err.Error() != "context_pack_materialize action must be preview or materialize" {
		t.Fatalf("CLI and MCP validation order diverged: %v", err)
	}
}

func TestContextPackCLIActionValidationMatchesMCP(t *testing.T) {
	validDigest := "sha256:" + strings.Repeat("a", 64)
	activeTarget := knowledge.ContextPackTarget{
		Kind: "active-run", CampaignID: "C-TEST", WorkItemID: "W-0001",
		RunID: "R-20260802-0001",
	}
	recruitingTarget := knowledge.ContextPackTarget{
		Kind: "recruiting-run", CandidateSlug: "candidate-one",
		RecruitingRunID: "20260802T190000Z",
	}
	tests := []struct {
		name    string
		request knowledge.ContextPackMaterializeRequest
		want    string
	}{
		{name: "missing action", request: knowledge.ContextPackMaterializeRequest{Target: recruitingTarget}, want: "action must be preview or materialize"},
		{name: "unknown action", request: knowledge.ContextPackMaterializeRequest{Action: "write", Target: recruitingTarget}, want: "action must be preview or materialize"},
		{name: "missing target", request: knowledge.ContextPackMaterializeRequest{Action: "preview"}, want: "requires an active-run or recruiting-run target"},
		{name: "unsupported target", request: knowledge.ContextPackMaterializeRequest{Action: "preview", Target: knowledge.ContextPackTarget{Kind: "project"}}, want: "requires an active-run or recruiting-run target"},
		{name: "preview with identity", request: knowledge.ContextPackMaterializeRequest{Action: "preview", Target: recruitingTarget, ExpectedDigest: validDigest}, want: "preview does not accept materialization fields"},
		{name: "materialize without digest", request: knowledge.ContextPackMaterializeRequest{Action: "materialize", Target: recruitingTarget}, want: "requires expectedDigest"},
		{name: "active direct materialize", request: knowledge.ContextPackMaterializeRequest{Action: "materialize", Target: activeTarget, ExpectedDigest: validDigest}, want: "manager_apply run.prepare"},
		{name: "valid active preview", request: knowledge.ContextPackMaterializeRequest{Action: "preview", Target: activeTarget}},
		{name: "valid recruiting preview", request: knowledge.ContextPackMaterializeRequest{Action: "preview", Target: recruitingTarget}},
		{name: "valid recruiting materialize", request: knowledge.ContextPackMaterializeRequest{Action: "materialize", Target: recruitingTarget, ExpectedDigest: validDigest}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := knowledge.ValidateContextPackMaterializeRequest(testCase.request)
			if testCase.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want substring %q", err, testCase.want)
			}
		})
	}
}

func TestContextPackCLIMaterializationResultMatchesMCP(t *testing.T) {
	request := knowledge.ContextPackMaterializeRequest{
		Action: "materialize",
		Target: knowledge.ContextPackTarget{
			Kind: "recruiting-run", CandidateSlug: "candidate-one",
			RecruitingRunID: "20260802T190000Z",
		},
	}
	pack := knowledge.ContextPack{
		PackID: "context-aaaaaaaaaaaaaaaaaaaa",
		Digest: "sha256:" + strings.Repeat("a", 64),
	}
	got := knowledge.ContextPackMaterializationResult{
		Action: request.Action,
		Path: ".re-discipline/agents/recruiting/candidate-one/runs/" +
			"20260802T190000Z/context-pack.json",
		PackID: pack.PackID, Digest: pack.Digest, Materialized: true,
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"action":"materialize","path":".re-discipline/agents/recruiting/candidate-one/runs/20260802T190000Z/context-pack.json","packId":"context-aaaaaaaaaaaaaaaaaaaa","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","materialized":true}`
	if string(body) != want {
		t.Fatalf("shared CLI/MCP result JSON = %s, want %s", body, want)
	}
}
