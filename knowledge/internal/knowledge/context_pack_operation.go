package knowledge

import (
	"context"
	"errors"
	"fmt"
)

// ContextPackMaterializeRequest is the operation contract shared by MCP and
// direct CLI callers. Project selection is transport-level routing and is not
// part of the operation payload.
type ContextPackMaterializeRequest struct {
	Action         string            `json:"action"`
	Target         ContextPackTarget `json:"target"`
	Task           string            `json:"task"`
	Role           string            `json:"role"`
	AllowedTiers   []string          `json:"allowedTiers,omitempty"`
	TokenBudget    int               `json:"tokenBudget,omitempty"`
	RequiredPaths  []string          `json:"requiredPaths,omitempty"`
	WriteGrants    []WriteGrant      `json:"writeGrants,omitempty"`
	ExpectedDigest string            `json:"expectedDigest,omitempty"`
	ExpectedPackID string            `json:"expectedPackId,omitempty"`
}

// ContextPackMaterializationResult is returned by both transports after an
// exact, independently verified materialization.
type ContextPackMaterializationResult struct {
	Action       string `json:"action"`
	Path         string `json:"path"`
	PackID       string `json:"packId"`
	Digest       string `json:"digest"`
	Materialized bool   `json:"materialized"`
}

func ValidateContextPackMaterializeRequest(request ContextPackMaterializeRequest) error {
	if request.Action != "preview" && request.Action != "materialize" {
		return fmt.Errorf(
			"context_pack_materialize action must be preview or materialize; it is %q. preview "+
				"compiles a pack and writes nothing; materialize publishes a preview whose "+
				"digest the caller retained independently",
			request.Action)
	}
	if request.Target.Kind != "active-run" && request.Target.Kind != "recruiting-run" {
		return fmt.Errorf(
			"context_pack_materialize requires target.kind \"active-run\" or \"recruiting-run\"; "+
				"it is %q. An active-run target needs campaignId, workItemId, and runId; a "+
				"recruiting-run target needs candidateSlug and recruitingRunId",
			request.Target.Kind)
	}
	if request.Action == "preview" &&
		(request.ExpectedDigest != "" || request.ExpectedPackID != "") {
		return errors.New(
			"context pack preview does not accept expectedDigest or expectedPackId; those are " +
				"the compare-and-swap a materialize supplies from a preview it already ran, " +
				"and a preview that accepted them would be verifying its own output. Remedy: " +
				"drop both fields to preview, or set action=materialize to publish")
	}
	if request.Action == "materialize" && request.ExpectedDigest == "" {
		return errors.New(
			"context pack materialization requires expectedDigest: the digest of the preview " +
				"being published, retained by the caller rather than recomputed here, so that " +
				"an index or profile change between the two calls is caught instead of " +
				"silently published. Remedy: run context_pack_materialize action=preview with " +
				"the same target, task, role, tiers, and writeGrants, then pass its digest")
	}
	if request.Action == "materialize" && request.Target.Kind == "active-run" {
		return errors.New(
			"active-run context packs are published atomically by manager_apply run.prepare, " +
				"in the same transaction as the run record, so a run can never exist without " +
				"the pack that scoped it. Remedy: preview the pack here, then submit it " +
				"unchanged as manager_apply run.prepare runPreparation.contextPack")
	}
	return nil
}

// ContextPackMaterialize previews or publishes one immutable context pack.
// Keeping this operation below both transports prevents their accepted input,
// validation, or success payload from drifting apart.
func (service *Service) ContextPackMaterialize(
	ctx context.Context,
	request ContextPackMaterializeRequest,
) (any, error) {
	if err := ValidateContextPackMaterializeRequest(request); err != nil {
		return nil, err
	}
	pack, err := service.ContextPackOptions(ctx, ContextPackRequest{
		Target: request.Target,
		Task:   request.Task, Role: request.Role, Tiers: request.AllowedTiers,
		TokenBudget: request.TokenBudget, RequiredPaths: request.RequiredPaths,
		WriteGrants: request.WriteGrants,
	})
	if err != nil {
		return nil, err
	}
	if request.Action == "preview" {
		return pack, nil
	}
	path, err := contextPackMaterializationPath(pack.Scope)
	if err != nil {
		return nil, err
	}
	if err := service.MaterializeContextPackExpected(
		path, pack, request.ExpectedDigest, request.ExpectedPackID,
	); err != nil {
		return nil, err
	}
	return ContextPackMaterializationResult{
		Action: request.Action, Path: path,
		PackID: pack.PackID, Digest: pack.Digest, Materialized: true,
	}, nil
}

func contextPackMaterializationPath(scope ContextPackScope) (string, error) {
	switch scope.Kind {
	case "active-run":
		if !managedSlugRE.MatchString(scope.CampaignSlug) || !runIDRE.MatchString(scope.RunID) {
			return "", errors.New("active context pack has invalid materialization scope")
		}
		return "active/" + scope.CampaignSlug + "/runs/" + scope.RunID + "/context-pack.json", nil
	case "recruiting-run":
		if !managedSlugRE.MatchString(scope.CandidateSlug) ||
			!workspaceIDRE.MatchString(scope.RecruitingRunID) {
			return "", errors.New("recruiting context pack has invalid materialization scope")
		}
		return ".re-discipline/agents/recruiting/" + scope.CandidateSlug +
			"/runs/" + scope.RecruitingRunID + "/context-pack.json", nil
	default:
		return "", errors.New("project context packs cannot be materialized")
	}
}
