package agent

import (
	"context"
	"encoding/json"
	"testing"
)

const acpOfferedOptions = `"options":[
	{"optionId":"once","kind":"allow_once"},
	{"optionId":"always","kind":"allow_always"},
	{"optionId":"no","kind":"reject_once"},
	{"optionId":"never","kind":"reject_always"}]`

func acpParams(t *testing.T, title string) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"sessionId":"ses_1",` + acpOfferedOptions + `,
		"toolCall":{"toolCallId":"tc_1","title":"` + title + `","kind":"execute","rawInput":{"command":"ls"}}}`)
}

func selectedOption(t *testing.T, result map[string]any) (outcome string, optionID string) {
	t.Helper()
	got, ok := result["outcome"].(map[string]any)
	if !ok {
		t.Fatalf("result has no outcome object: %+v", result)
	}
	outcome, _ = got["outcome"].(string)
	optionID, _ = got["optionId"].(string)
	return outcome, optionID
}

// An allowed tool proceeds — but only under a once-scoped approval. Answering
// with the always-scoped option would tell the agent to stop asking, which ends
// enforcement for the rest of the session.
func TestACPPermissionAllowUsesOnceScopedApproval(t *testing.T) {
	var sawTool string
	policy := func(_ context.Context, tool string, _ map[string]any) (bool, string) {
		sawTool = tool
		return true, "Allowed by default"
	}

	outcome, optionID := selectedOption(t, cerebroACPPermissionResult(context.Background(), policy, nil, acpParams(t, "terminal: ls")))

	if outcome != "selected" || optionID != "once" {
		t.Fatalf("outcome=%q optionId=%q, want selected/once", outcome, optionID)
	}
	if sawTool != "terminal" {
		t.Fatalf("policy saw tool %q, want the canonical name %q", sawTool, "terminal")
	}
}

func TestACPPermissionDeniedToolIsRejected(t *testing.T) {
	policy := func(_ context.Context, _ string, _ map[string]any) (bool, string) {
		return false, "Denied by workspace policy"
	}

	outcome, optionID := selectedOption(t, cerebroACPPermissionResult(context.Background(), policy, nil, acpParams(t, "terminal: rm -rf /")))

	if outcome != "selected" || optionID != "no" {
		t.Fatalf("outcome=%q optionId=%q, want selected/no", outcome, optionID)
	}
}

// No policy callback means the run is not under enforcement. That must never
// resolve to "go ahead".
func TestACPPermissionWithoutPolicyDenies(t *testing.T) {
	outcome, optionID := selectedOption(t, cerebroACPPermissionResult(context.Background(), nil, nil, acpParams(t, "terminal: ls")))

	if outcome != "selected" || optionID != "no" {
		t.Fatalf("outcome=%q optionId=%q, want selected/no", outcome, optionID)
	}
}

func TestACPPermissionUnparseableParamsCancels(t *testing.T) {
	policy := func(_ context.Context, _ string, _ map[string]any) (bool, string) {
		return true, "Allowed by default"
	}

	outcome, _ := selectedOption(t, cerebroACPPermissionResult(context.Background(), policy, nil, json.RawMessage(`not json`)))

	if outcome != "cancelled" {
		t.Fatalf("outcome=%q, want cancelled", outcome)
	}
}

// An agent that offers only an always-scoped approval cannot be answered
// without ending enforcement, so the call does not proceed.
func TestACPPermissionWithoutOnceScopedApprovalCancels(t *testing.T) {
	policy := func(_ context.Context, _ string, _ map[string]any) (bool, string) {
		return true, "Allowed by default"
	}
	params := json.RawMessage(`{"options":[{"optionId":"always","kind":"allow_always"}],
		"toolCall":{"toolCallId":"tc_1","title":"terminal: ls","kind":"execute"}}`)

	outcome, _ := selectedOption(t, cerebroACPPermissionResult(context.Background(), policy, nil, params))

	if outcome != "cancelled" {
		t.Fatalf("outcome=%q, want cancelled", outcome)
	}
}

// A denial with no rejection option on offer still must not run the tool.
func TestACPPermissionDenyWithoutRejectOptionCancels(t *testing.T) {
	policy := func(_ context.Context, _ string, _ map[string]any) (bool, string) {
		return false, "Denied by workspace policy"
	}
	params := json.RawMessage(`{"options":[{"optionId":"once","kind":"allow_once"}],
		"toolCall":{"toolCallId":"tc_1","title":"terminal: ls","kind":"execute"}}`)

	outcome, _ := selectedOption(t, cerebroACPPermissionResult(context.Background(), policy, nil, params))

	if outcome != "cancelled" {
		t.Fatalf("outcome=%q, want cancelled", outcome)
	}
}

// The tool identity handed to the policy must be the canonical name, not the
// human ACP title — a policy row is written against the name.
func TestACPToolNameFallsBackToExplicitName(t *testing.T) {
	var req acpPermissionRequest
	if err := json.Unmarshal([]byte(`{"toolCall":{"name":"multica__get_me","title":"","kind":""}}`), &req); err != nil {
		t.Fatal(err)
	}
	if got := acpToolName(req); got != "multica__get_me" {
		t.Fatalf("acpToolName = %q, want multica__get_me", got)
	}
}

// The two permission shapes measured in a live hermes run on 2026-08-07. Both
// carry a title that is prose, not a tool name — resolving from the title alone
// asks the policy about "Approve edit" and "delete in root path", which match no
// inventory row, so every edit and every dangerous command would be denied.
func TestACPToolNameFromMeasuredHermesShapes(t *testing.T) {
	for name, params := range map[string]string{
		"write_file": `{"toolCall":{"title":"Approve edit: /tmp/x.txt","kind":"edit",
			"rawInput":{"tool":"write_file","arguments":{"path":"/tmp/x.txt"}}}}`,
		"patch": `{"toolCall":{"title":"Approve edit: /tmp/x.txt","kind":"edit",
			"rawInput":{"tool":"patch","arguments":{"path":"/tmp/x.txt"}}}}`,
		"terminal": `{"toolCall":{"title":"delete in root path: rm -rf .","kind":"execute",
			"rawInput":{"command":"rm -rf .","description":"delete in root path"}}}`,
	} {
		var req acpPermissionRequest
		if err := json.Unmarshal([]byte(params), &req); err != nil {
			t.Fatal(err)
		}
		if got := acpToolName(req); got != name {
			t.Errorf("acpToolName = %q, want %q", got, name)
		}
	}
}
