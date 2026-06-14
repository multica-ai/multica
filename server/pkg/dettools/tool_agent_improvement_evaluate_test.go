package dettools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strings"
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

func TestAgentImprovementEvaluateThresholdBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		uniqueTasks int
		want        string
	}{
		{
			name:        "at thresholds needs review",
			count:       3,
			uniqueTasks: 2,
			want:        agentImprovementDecisionReadyForReview,
		},
		{
			name:        "below signature count defers",
			count:       2,
			uniqueTasks: 2,
			want:        agentImprovementDecisionDefer,
		},
		{
			name:        "below unique tasks defers",
			count:       3,
			uniqueTasks: 1,
			want:        agentImprovementDecisionDefer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{
				"candidate_dettools": []map[string]any{{
					"suggested_name":            "detect_threshold",
					"source_signature_key":      "threshold::case",
					"expected_determinism_gain": 0.40,
					"decision_hint":             "ready_for_review",
				}},
				"repeat_signatures": []map[string]any{{
					"key":           "threshold::case",
					"count":         tt.count,
					"unique_tasks":  tt.uniqueTasks,
					"unique_agents": 1,
				}},
			}), testEnv(t.TempDir()))

			if res.Status != StatusOK {
				t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
			}
			decisions := mustAgentImprovementDecisions(t, res)
			if got := decisions[0].Decision; got != tt.want {
				t.Fatalf("decision = %q, want %q", got, tt.want)
			}
		})
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

func TestAgentImprovementEvaluateCountsRecommendationsBeforeOutputCap(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{
		"candidate_dettools": [
			{"suggested_name": "detect_c", "source_signature_key": "c", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_candidate"},
			{"suggested_name": "detect_a", "source_signature_key": "a", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_candidate"},
			{"suggested_name": "detect_b", "source_signature_key": "b", "expected_determinism_gain": 0.1, "decision_hint": "ready_for_candidate"}
		],
		"repeat_signatures": [
			{"key": "a", "failure_reason": "a", "count": 3, "unique_tasks": 2, "unique_agents": 1},
			{"key": "b", "failure_reason": "b", "count": 3, "unique_tasks": 2, "unique_agents": 1},
			{"key": "c", "failure_reason": "c", "count": 3, "unique_tasks": 2, "unique_agents": 1}
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
	if got := res.MachineData["recommended_candidate_count"]; got != 3 {
		t.Fatalf("recommended_candidate_count = %v, want 3", got)
	}
	if decisions[0].SourceSignatureKey != "a" || decisions[1].SourceSignatureKey != "b" {
		t.Fatalf("decisions sorted/capped = %v, want a then b", decisions)
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
	res := agentImprovementEvaluateHandler(t.Context(), json.RawMessage(strings.Repeat(" ", agentImprovementMaxPayloadBytes+1)), testEnv(t.TempDir()))
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("payload overflow got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}

	candidates := make([]map[string]any, agentImprovementMaxCandidates+1)
	for i := range candidates {
		candidates[i] = map[string]any{"suggested_name": "x", "source_signature_key": "x"}
	}
	res = agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{"candidate_dettools": candidates}), testEnv(t.TempDir()))
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

func TestAgentImprovementEvaluateRejectsOversizedStrings(t *testing.T) {
	oversized := strings.Repeat("x", agentImprovementMaxStringBytes+1)

	res := agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{
		"candidate_dettools": []map[string]any{{
			"suggested_name":       oversized,
			"source_signature_key": "sig",
		}},
	}), testEnv(t.TempDir()))
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("candidate string overflow got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}

	res = agentImprovementEvaluateHandler(t.Context(), mustJSONRawMessage(t, map[string]any{
		"repeat_signatures": []map[string]any{{
			"key":             "sig",
			"example_raw_ref": oversized,
		}},
	}), testEnv(t.TempDir()))
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("signature string overflow got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestAgentImprovementEvaluateRejectsInvalidMaxDecisions(t *testing.T) {
	res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), `{"max_decisions": 26}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("max_decisions got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestAgentImprovementEvaluateRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"candidate_dettools": [`},
		{name: "wrong candidate type", body: `{"candidate_dettools": "not-an-array"}`},
		{name: "negative max decisions", body: `{"max_decisions": -1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callHandler(t, agentImprovementEvaluateHandler, t.TempDir(), tt.body)
			if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
				t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
			}
		})
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

func TestAgentImprovementEvaluateDescriptorAndMCPRoundTrip(t *testing.T) {
	reg := NewRegistry([]string{agentImprovementEvaluateName})
	descriptors := reg.Descriptors()
	if got, want := len(descriptors), 1; got != want {
		t.Fatalf("descriptors len = %d, want %d", got, want)
	}
	if descriptors[0]["name"] != agentImprovementEvaluateName {
		t.Fatalf("descriptor name = %v, want %s", descriptors[0]["name"], agentImprovementEvaluateName)
	}
	var schema map[string]any
	if err := json.Unmarshal(descriptors[0]["inputSchema"].(json.RawMessage), &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("schema additionalProperties = %v, want false", schema["additionalProperties"])
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agent_improvement_evaluate","arguments":{"candidate_dettools":[{"suggested_name":"detect_lock","source_signature_key":"lock::timeout","expected_determinism_gain":0.8,"decision_hint":"ready_for_candidate"}],"repeat_signatures":[{"key":"lock::timeout","failure_reason":"lock","count":3,"unique_tasks":2,"unique_agents":1}]}}}`,
	}, "\n") + "\n"

	var out strings.Builder
	err := Serve(context.Background(), strings.NewReader(input), &out, reg,
		ServerInfo{Name: "multica-tools", Version: "test"}, testEnv(t.TempDir()),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	responses := decodeResponses(t, out.String())
	if got, want := len(responses), 2; got != want {
		t.Fatalf("responses len = %d, want %d", got, want)
	}

	tools := responses[0]["result"].(map[string]any)["tools"].([]any)
	if got, want := len(tools), 1; got != want {
		t.Fatalf("tools/list len = %d, want %d", got, want)
	}
	callResult := responses[1]["result"].(map[string]any)
	if callResult["isError"] != false {
		t.Fatalf("isError = %v, want false", callResult["isError"])
	}
	structured := callResult["structuredContent"].(map[string]any)
	if structured["status"] != StatusOK {
		t.Fatalf("structured status = %v, want ok", structured["status"])
	}
	machineData := structured["machine_data"].(map[string]any)
	decisions := machineData["decisions"].([]any)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("decisions len = %d, want %d", got, want)
	}
	decision := decisions[0].(map[string]any)
	if decision["decision"] != agentImprovementDecisionReadyForCandidate {
		t.Fatalf("decision = %v, want %s", decision["decision"], agentImprovementDecisionReadyForCandidate)
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
