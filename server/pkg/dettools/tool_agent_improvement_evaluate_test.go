package dettools

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestAgentImprovementEvaluateDecidesCandidates(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{
		"candidate_dettools": [
			{"suggested_name": "detect_workspace_lock", "source_signature_key": "workspace lock::timeout", "expected_determinism_gain": 0.80, "decision_hint": "ready_for_candidate"},
			{"suggested_name": "detect_git_auth", "source_signature_key": "git auth::prompt", "expected_determinism_gain": 0.35, "decision_hint": "ready_for_review"},
			{"suggested_name": "detect_sparse_signal", "source_signature_key": "sparse::single-task", "expected_determinism_gain": 0.10, "decision_hint": "ready_for_review"},
			{"suggested_name": "detect_missing_signal", "source_signature_key": "missing::signal", "expected_determinism_gain": 0.05, "decision_hint": "ready_for_candidate"}
		],
		"repeat_signatures": [
			{"key": "workspace lock::timeout", "failure_reason": "workspace lock", "error_signature": "timeout", "count": 6, "unique_tasks": 3, "unique_agents": 2},
			{"key": "git auth::prompt", "failure_reason": "git auth", "error_signature": "prompt", "count": 3, "unique_tasks": 2, "unique_agents": 1},
			{"key": "sparse::single-task", "failure_reason": "sparse", "error_signature": "single-task", "count": 3, "unique_tasks": 1, "unique_agents": 1}
		],
		"max_decisions": 10
	}`)

	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	decisions := mustAgentImprovementDecisions(t, res)
	if got, want := len(decisions), 4; got != want {
		t.Fatalf("decisions len = %d, want %d", got, want)
	}
	gotDecisions := []string{decisions[0].Decision, decisions[1].Decision, decisions[2].Decision, decisions[3].Decision}
	wantDecisions := []string{
		agentImprovementDecisionReadyForCandidate,
		agentImprovementDecisionReadyForReview,
		agentImprovementDecisionDefer,
		agentImprovementDecisionDefer,
	}
	if !slices.Equal(gotDecisions, wantDecisions) {
		t.Fatalf("decisions = %v, want %v", gotDecisions, wantDecisions)
	}
	if got := res.MachineData["recommended_candidate_count"]; got != 1 {
		t.Fatalf("recommended_candidate_count = %v, want 1", got)
	}
	if got := res.MachineData["signal_count"]; got != 3 {
		t.Fatalf("signal_count = %v, want 3", got)
	}
}

func TestAgentImprovementEvaluateUsesHighVolumeCandidateFallback(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{
		"candidate_dettools": [
			{"suggested_name": "detect_retry_loop", "source_signature_key": "retry::loop", "expected_determinism_gain": 0.45, "decision_hint": "ready_for_review"}
		],
		"repeat_signatures": [
			{"key": "retry::loop", "failure_reason": "retry", "error_signature": "loop", "count": 6, "unique_tasks": 3, "unique_agents": 1}
		]
	}`)

	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	decisions := mustAgentImprovementDecisions(t, res)
	if got := decisions[0].Decision; got != agentImprovementDecisionReadyForCandidate {
		t.Fatalf("decision = %q, want %q", got, agentImprovementDecisionReadyForCandidate)
	}
}

func TestAgentImprovementEvaluateDefersSignalsWithoutCandidates(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{
		"repeat_signatures": [
			{"key": "", "failure_reason": "empty", "count": 4, "unique_tasks": 2, "unique_agents": 1},
			{"key": "shell::quoting", "failure_reason": "shell", "error_signature": "quoting", "count": 4, "unique_tasks": 2, "unique_agents": 1}
		]
	}`)

	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	decisions := mustAgentImprovementDecisions(t, res)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("decisions len = %d, want %d", got, want)
	}
	for _, decision := range decisions {
		if decision.Decision != agentImprovementDecisionDefer {
			t.Fatalf("decision = %q, want defer", decision.Decision)
		}
	}
	if got := res.MachineData["recommended_candidate_count"]; got != 0 {
		t.Fatalf("recommended_candidate_count = %v, want 0", got)
	}
}

func TestAgentImprovementEvaluateCapsOutput(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{
		"candidate_dettools": [
			{"suggested_name": "detect_a", "source_signature_key": "a", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_review"},
			{"suggested_name": "detect_b", "source_signature_key": "b", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_review"},
			{"suggested_name": "detect_c", "source_signature_key": "c", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_review"}
		],
		"repeat_signatures": [
			{"key": "a", "failure_reason": "a", "count": 3, "unique_tasks": 2, "unique_agents": 1},
			{"key": "b", "failure_reason": "b", "count": 4, "unique_tasks": 2, "unique_agents": 1},
			{"key": "c", "failure_reason": "c", "count": 5, "unique_tasks": 2, "unique_agents": 1}
		],
		"max_decisions": 2
	}`)

	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	decisions := mustAgentImprovementDecisions(t, res)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("decisions len = %d, want %d", got, want)
	}
	if decisions[0].SourceSignatureKey != "c" || decisions[1].SourceSignatureKey != "b" {
		t.Fatalf("decisions sorted/capped = %v, want c then b", decisions)
	}
}

func TestAgentImprovementEvaluateRejectsUnknownFields(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{"candidate_dettools": [], "unexpected": true}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}

	res = callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{"candidate_dettools": [{"suggested_name": "x", "source_signature_key": "x", "extra": true}]}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("nested got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestAgentImprovementEvaluateRejectsOversizedInput(t *testing.T) {
	candidates := make([]map[string]any, agentImprovementMaxCandidates+1)
	for i := range candidates {
		candidates[i] = map[string]any{"suggested_name": "x", "source_signature_key": "x"}
	}
	res := agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{"candidate_dettools": candidates}), testEnv(t.TempDir()))
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("candidate overflow got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}

	signatures := make([]map[string]any, agentImprovementMaxSignatures+1)
	for i := range signatures {
		signatures[i] = map[string]any{"key": "x"}
	}
	res = agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{"repeat_signatures": signatures}), testEnv(t.TempDir()))
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("signature overflow got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestAgentImprovementEvaluateRejectsInvalidMaxDecisions(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{"max_decisions": 26}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("max_decisions got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestAgentImprovementEvaluateIsRegistered(t *testing.T) {
	reg := NewRegistry([]string{agentImprovementEvaluateName})
	tool, ok := reg.Lookup(agentImprovementEvaluateName)
	if !ok {
		t.Fatal("agent_improvement_evaluate not registered")
	}
	if tool.Name != agentImprovementEvaluateName {
		t.Fatalf("tool name = %q, want %q", tool.Name, agentImprovementEvaluateName)
	}
}

func mustAgentImprovementDecisions(t *testing.T, res Result) []agentImprovementDecision {
	t.Helper()
	decisions, ok := res.MachineData["decisions"].([]agentImprovementDecision)
	if !ok {
		t.Fatalf("decisions type = %T, want []agentImprovementDecision", res.MachineData["decisions"])
	}
	return decisions
}

func mustJSONRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test input: %v", err)
	}
	return json.RawMessage(data)
}
