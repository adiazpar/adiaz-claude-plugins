package knowledge

import (
	"sort"
	"testing"
)

// An engine transition that no caller can name is not a capability. The
// closure workflow queues normalization work against a closing campaign, and
// creating the curator run that clears it is refused for every action except
// closure.remediation.run.create. That action was wired end to end - manager
// authorization, permitted record kinds, submission obligation, and the
// closing-campaign exemption in the state journal - but was never listed in
// manager_apply's action enum. Closure could therefore demand work that the
// exposed surface forbade, and the only escape was to reopen the campaign and
// hand-repair canonical state. Bind the surface to the engine so the two
// cannot drift apart again.
func TestManagerApplyExposesEveryEngineAction(t *testing.T) {
	var managerTool map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] == "manager_apply" {
			managerTool = tool
			break
		}
	}
	if managerTool == nil {
		t.Fatal("manager_apply tool is missing")
	}
	input := asObject(t, managerTool["inputSchema"])
	properties := asObject(t, input["properties"])
	action := asObject(t, properties["action"])
	exposed, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("manager_apply action enum is not a string list: %#v", action["enum"])
	}

	exposedSet := make(map[string]bool, len(exposed))
	for _, value := range exposed {
		if exposedSet[value] {
			t.Errorf("manager_apply action %q is listed twice", value)
		}
		exposedSet[value] = true
	}

	missing := []string{}
	for engineAction := range managerActionKinds {
		if !exposedSet[engineAction] {
			missing = append(missing, engineAction)
		}
	}
	sort.Strings(missing)
	for _, engineAction := range missing {
		t.Errorf("engine honors %q but manager_apply does not expose it", engineAction)
	}

	unknown := []string{}
	for _, value := range exposed {
		if _, ok := managerActionKinds[value]; !ok {
			unknown = append(unknown, value)
		}
	}
	sort.Strings(unknown)
	for _, value := range unknown {
		t.Errorf("manager_apply exposes %q but the engine has no such transition", value)
	}
}

// The same binding for closure. closure.restart is the second door into
// `closing` and the only supported escape from a reopened job; an engine
// transition no caller can name is not a capability, and a caller that cannot
// see expectedClosurePlanRevision in the schema cannot construct a restart the
// engine will accept.
func TestClosureApplyExposesEveryEngineAction(t *testing.T) {
	var closureTool map[string]any
	for _, tool := range toolDefinitions() {
		if tool["name"] == "closure_apply" {
			closureTool = tool
			break
		}
	}
	if closureTool == nil {
		t.Fatal("closure_apply tool is missing")
	}
	input := asObject(t, closureTool["inputSchema"])
	properties := asObject(t, input["properties"])
	action := asObject(t, properties["action"])
	exposed, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("closure_apply action enum is not a string list: %#v", action["enum"])
	}
	if len(exposed) != len(ClosureActions) {
		t.Fatalf("closure_apply exposes %v but the engine honors %v", exposed, ClosureActions)
	}
	for index, value := range ClosureActions {
		if exposed[index] != value {
			t.Fatalf("closure_apply exposes %v but the engine honors %v", exposed, ClosureActions)
		}
	}
	for _, name := range []string{"expectedClosurePlanRevision", "expectedCoverageRevision"} {
		if _, present := properties[name]; !present {
			t.Errorf("closure_apply schema omits %s, which restart refuses without", name)
		}
	}
}

// Every exposed transition must also tell the caller what records it expects,
// or the caller learns the contract one terse refusal at a time.
func TestEveryManagerActionCarriesASubmissionObligation(t *testing.T) {
	actions := make([]string, 0, len(managerActionKinds))
	for engineAction := range managerActionKinds {
		actions = append(actions, engineAction)
	}
	sort.Strings(actions)
	for _, engineAction := range actions {
		if len(managerActionKinds[engineAction]) == 0 {
			// A transition that submits no typed record has nothing to state.
			continue
		}
		if _, ok := managerActionObligations[engineAction]; !ok {
			t.Errorf("manager action %q submits records but states no obligation", engineAction)
		}
	}
}
