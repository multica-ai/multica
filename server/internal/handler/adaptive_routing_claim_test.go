package handler

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestApplyAdaptiveTaskClaimRouteOverridesExecutionOnly(t *testing.T) {
	task := &db.AgentTaskQueue{
		RouteAdmissionState: "routed",
		RouteModel:          pgtype.Text{String: "claude-sonnet-test", Valid: true},
		RouteThinkingLevel:  pgtype.Text{String: "high", Valid: true},
		RouteServiceTier:    pgtype.Text{String: "priority", Valid: true},
		RouteRuntimeConfig:  []byte(`{"provider_mode":"test"}`),
		RouteCustomArgs:     []byte(`[]`),
	}
	agent := &TaskAgentData{
		ID:            "agent-source",
		Name:          "Source",
		Instructions:  "keep me",
		CustomEnv:     map[string]string{"SAFE": "unchanged"},
		CustomArgs:    []string{"--baseline"},
		McpConfig:     json.RawMessage(`{"mcpServers":{"memory":{}}}`),
		Model:         "gpt-test",
		ThinkingLevel: "medium",
		ServiceTier:   "default",
		RuntimeConfig: json.RawMessage(`{
			"baseline": true,
			"provider_failover_protected": true,
			"adaptive_routing": {"enabled": true},
			"gateway": {"token": "keep-me"}
		}`),
	}

	if err := applyAdaptiveTaskClaimRoute(task, agent); err != nil {
		t.Fatalf("apply route: %v", err)
	}
	if agent.Model != "claude-sonnet-test" ||
		agent.ThinkingLevel != "high" ||
		agent.ServiceTier != "priority" {
		t.Fatalf("execution overrides not applied: %+v", agent)
	}
	var merged map[string]any
	if err := json.Unmarshal(agent.RuntimeConfig, &merged); err != nil {
		t.Fatalf("decode merged runtime config: %v", err)
	}
	gateway, _ := merged["gateway"].(map[string]any)
	if merged["baseline"] != true ||
		merged["provider_mode"] != "test" ||
		merged["provider_failover_protected"] != true ||
		gateway["token"] != "keep-me" {
		t.Fatalf("runtime config did not preserve source policy/secrets: %#v", merged)
	}
	if !reflect.DeepEqual(agent.CustomArgs, []string{}) {
		t.Fatalf("explicit empty route args must clear baseline: %#v", agent.CustomArgs)
	}
	if agent.ID != "agent-source" || agent.Instructions != "keep me" ||
		agent.CustomEnv["SAFE"] != "unchanged" ||
		string(agent.McpConfig) != `{"mcpServers":{"memory":{}}}` {
		t.Fatalf("route changed identity/authority payload: %+v", agent)
	}
}

func TestApplyAdaptiveTaskClaimRouteRejectsProtectedRuntimeConfigOverrideAtomically(t *testing.T) {
	task := &db.AgentTaskQueue{
		RouteAdmissionState: "routed",
		RouteModel:          pgtype.Text{String: "claude-sonnet-test", Valid: true},
		RouteRuntimeConfig:  []byte(`{"provider_failover_protected":false}`),
	}
	agent := &TaskAgentData{
		Model:         "gpt-test",
		RuntimeConfig: json.RawMessage(`{"provider_failover_protected":true}`),
	}

	if err := applyAdaptiveTaskClaimRoute(task, agent); err == nil {
		t.Fatal("expected protected runtime config override error")
	}
	if agent.Model != "gpt-test" ||
		string(agent.RuntimeConfig) != `{"provider_failover_protected":true}` {
		t.Fatalf("rejected override partially mutated execution config: %+v", agent)
	}
}

func TestApplyAdaptiveTaskClaimRouteLeavesShadowUntouched(t *testing.T) {
	task := &db.AgentTaskQueue{
		RouteAdmissionState: "shadow",
		RouteModel:          pgtype.Text{String: "claude-sonnet-test", Valid: true},
	}
	agent := &TaskAgentData{Model: "gpt-test", CustomArgs: []string{"--baseline"}}
	if err := applyAdaptiveTaskClaimRoute(task, agent); err != nil {
		t.Fatalf("apply shadow: %v", err)
	}
	if agent.Model != "gpt-test" || !reflect.DeepEqual(agent.CustomArgs, []string{"--baseline"}) {
		t.Fatalf("shadow changed execution config: %+v", agent)
	}
}

func TestApplyAdaptiveTaskClaimRouteRejectsMalformedArgsAtomically(t *testing.T) {
	task := &db.AgentTaskQueue{
		RouteAdmissionState: "routed",
		RouteModel:          pgtype.Text{String: "claude-sonnet-test", Valid: true},
		RouteCustomArgs:     []byte(`{"not":"an array"}`),
	}
	agent := &TaskAgentData{Model: "gpt-test", CustomArgs: []string{"--baseline"}}
	if err := applyAdaptiveTaskClaimRoute(task, agent); err == nil {
		t.Fatal("expected malformed route args error")
	}
	if agent.Model != "gpt-test" || !reflect.DeepEqual(agent.CustomArgs, []string{"--baseline"}) {
		t.Fatalf("malformed route partially mutated config: %+v", agent)
	}
}
