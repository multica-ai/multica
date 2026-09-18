package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskExecutionReuseIsolation(t *testing.T) {
	runtimeID := parseUUID("11111111-1111-1111-1111-111111111111")
	otherRuntimeID := parseUUID("22222222-2222-2222-2222-222222222222")
	owner := "33333333-3333-3333-3333-333333333333"
	otherOwner := "44444444-4444-4444-4444-444444444444"
	snapshot := func(user string) []byte {
		b, _ := json.Marshal(map[string]any{"version": 1, "execution_user_id": user, "routes": map[string]any{"55555555-5555-5555-5555-555555555555": map[string]string{"runtime_id": uuidToString(runtimeID), "runtime_owner_id": owner, "provider": "opencode", "source": "default"}}})
		return b
	}
	for _, tc := range []struct {
		name           string
		current, prior []byte
		priorRuntime   pgtype.UUID
		want           bool
	}{
		{"same execution owner", snapshot(owner), snapshot(owner), runtimeID, true},
		{"different execution owner", snapshot(owner), snapshot(otherOwner), runtimeID, false},
		{"different runtime", snapshot(owner), snapshot(owner), otherRuntimeID, false},
		{"unknown legacy owner", snapshot(owner), nil, runtimeID, false},
		{"legacy continuation", nil, nil, runtimeID, true},
		{"legacy different runtime", nil, nil, otherRuntimeID, false},
		{"malformed owner", []byte(`{"version":1}`), []byte(`{"version":1}`), runtimeID, false},
		{"invalid snapshot", []byte(`{`), []byte(`{`), runtimeID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canReuseTaskExecution(parseUUID("55555555-5555-5555-5555-555555555555"), runtimeID, tc.current, tc.priorRuntime, tc.prior); got != tc.want {
				t.Fatalf("reuse=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimPersonalRuntimeSuppressesForeignOwnerConfig(t *testing.T) {
	owner := parseUUID("33333333-3333-3333-3333-333333333333")
	foreign := parseUUID("44444444-4444-4444-4444-444444444444")
	agent := db.Agent{ID: parseUUID("55555555-5555-5555-5555-555555555555"), OwnerID: owner, CustomEnv: []byte(`{"TOKEN":"secret"}`), CustomArgs: []byte(`["--token=secret"]`), McpConfig: []byte(`{"mcpServers":{"private":{"url":"secret"}}}`), RuntimeConfig: []byte(`{"token":"secret"}`)}
	for _, tc := range []struct {
		name       string
		owner      pgtype.UUID
		routed     bool
		suppressed bool
		source     string
	}{
		{"personal foreign owner", foreign, true, true, "personal"},
		{"personal same owner", owner, true, true, "personal"},
		{"default same owner", owner, true, false, "default"},
		{"legacy configuration", foreign, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var routing []byte
			if tc.routed {
				routing, _ = json.Marshal(map[string]any{"version": 1, "execution_user_id": uuidToString(tc.owner), "routes": map[string]any{uuidToString(agent.ID): map[string]string{"runtime_id": "11111111-1111-1111-1111-111111111111", "runtime_owner_id": uuidToString(tc.owner), "provider": "opencode", "source": tc.source}}})
			}
			got := claimAgentConfiguration(agent, db.AgentRuntime{OwnerID: tc.owner, Provider: "opencode"}, routing, "opencode")
			empty := len(got.CustomEnv) == 0 && len(got.CustomArgs) == 0 && len(got.McpConfig) == 0 && len(got.RuntimeConfig) == 0
			if empty != tc.suppressed {
				t.Fatalf("config suppressed=%v, want %v", empty, tc.suppressed)
			}
		})
	}
}

func TestClaimBriefUsesFrozenExecutionUser(t *testing.T) {
	runtimeOwner := parseUUID("33333333-3333-3333-3333-333333333333")
	executor := parseUUID("44444444-4444-4444-4444-444444444444")
	runtime := db.AgentRuntime{OwnerID: runtimeOwner}
	routing := []byte(`{"version":1,"execution_user_id":"44444444-4444-4444-4444-444444444444","routes":{}}`)
	if got := claimRequestingUserID(db.AgentTaskQueue{RuntimeRouting: routing}, runtime); got != executor {
		t.Fatalf("requesting user=%s want execution user", uuidToString(got))
	}
	if got := claimRequestingUserID(db.AgentTaskQueue{}, runtime); got != runtimeOwner {
		t.Fatal("legacy owner changed")
	}
	if got := claimRequestingUserID(db.AgentTaskQueue{RuntimeRouting: []byte(`{`)}, runtime); got.Valid {
		t.Fatal("invalid routing disclosed machine owner profile")
	}
}

func TestClaimUsesFrozenPersonalModel(t *testing.T) {
	aid := parseUUID("11111111-1111-1111-1111-111111111111")
	runtime := parseUUID("22222222-2222-2222-2222-222222222222")
	owner := parseUUID("33333333-3333-3333-3333-333333333333")
	agent := db.Agent{ID: aid, OwnerID: owner, Model: pgtype.Text{String: "changed-shared", Valid: true}}
	snapshot := func(model string) []byte {
		raw, _ := json.Marshal(map[string]any{"version": 1, "execution_user_id": uuidToString(owner), "routes": map[string]any{uuidToString(aid): map[string]any{"runtime_id": uuidToString(runtime), "runtime_owner_id": uuidToString(owner), "provider": "codex", "source": "personal", "model": model}}})
		return raw
	}
	for _, model := range []string{"personal-model", ""} {
		got := claimAgentConfiguration(agent, db.AgentRuntime{OwnerID: owner}, snapshot(model), "")
		if got.Model.String != model {
			t.Fatalf("claim model=%q want %q", got.Model.String, model)
		}
	}
	if canReuseTaskExecution(aid, runtime, snapshot("new-model"), runtime, snapshot("old-model")) {
		t.Fatal("reused provider session across model changes")
	}
	if !canReuseTaskExecution(aid, runtime, snapshot("same"), runtime, snapshot("same")) {
		t.Fatal("same configuration should resume")
	}
}
