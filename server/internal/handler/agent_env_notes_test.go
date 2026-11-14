package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAgentEnvNotesPersistOnReopen(t *testing.T) {
	id := dbfx.Agent(t, "env-notes", "")
	body := map[string]any{
		"custom_env":       map[string]string{"API_BASE_URL": "https://staging.example.test"},
		"custom_env_notes": map[string]string{"API_BASE_URL": "Staging API"},
	}
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+id+"/env", body), "id", id)
	testutil.Call(t, testHandler.UpdateAgentEnv, req).Want(http.StatusOK)
	req = withURLParam(newRequest(http.MethodGet, "/api/agents/"+id+"/env", nil), "id", id)
	var got struct {
		CustomEnv map[string]string `json:"custom_env"`
		Notes     map[string]string `json:"custom_env_notes"`
	}
	testutil.Call(t, testHandler.GetAgentEnv, req).Want(http.StatusOK).JSON(&got)
	if got.Notes["API_BASE_URL"] != "Staging API" || got.CustomEnv["API_BASE_URL"] != "https://staging.example.test" || len(got.CustomEnv) != 1 {
		t.Fatalf("env and note did not round-trip separately: %+v", got)
	}
}

func TestAgentEnvSpecialKeysRoundTrip(t *testing.T) {
	id := dbfx.Agent(t, "env-special-keys", "")
	put := func(body any) {
		t.Helper()
		req := withURLParam(newRequest(http.MethodPut, "/env", body), "id", id)
		testutil.Call(t, testHandler.UpdateAgentEnv, req).Want(http.StatusOK)
	}
	get := func() AgentEnvResponse {
		t.Helper()
		var got AgentEnvResponse
		req := withURLParam(newRequest(http.MethodGet, "/env", nil), "id", id)
		testutil.Call(t, testHandler.GetAgentEnv, req).Want(http.StatusOK).JSON(&got)
		return got
	}
	env := map[string]string{"__proto__": "special-value", "constructor": "constructor-value", "prototype": "prototype-value", "SAFE": "safe-value"}
	notes := map[string]string{"__proto__": "special-note", "constructor": "constructor-note", "prototype": "prototype-note"}
	put(map[string]any{"custom_env": env, "custom_env_notes": notes})
	// A legacy client omits or sends null metadata while editing another value.
	for _, explicitNull := range []bool{false, true} {
		env["SAFE"] = "edited-safe"
		body := map[string]any{"custom_env": env}
		if explicitNull {
			body["custom_env_notes"] = nil
		}
		put(body)
		got := get()
		for key, value := range env {
			if got.CustomEnv[key] != value {
				t.Fatalf("value for %q did not round-trip", key)
			}
		}
		for key, note := range notes {
			if got.CustomEnvNotes[key] != note {
				t.Fatalf("note for %q did not round-trip", key)
			}
		}
	}
	delete(notes, "__proto__")
	put(map[string]any{"custom_env": env, "custom_env_notes": notes})
	if got := get(); got.CustomEnv["__proto__"] != "special-value" || len(got.CustomEnvNotes) != 2 {
		t.Fatal("clearing the special key's note changed its value")
	}
	delete(env, "__proto__")
	put(map[string]any{"custom_env": env, "custom_env_notes": notes})
	if _, exists := get().CustomEnv["__proto__"]; exists {
		t.Fatal("explicit key deletion was not persisted")
	}
}

func TestAgentEnvNotesCompatibleUpdates(t *testing.T) {
	id := dbfx.Agent(t, "env-notes-compat", "")
	put := func(body any) {
		t.Helper()
		req := withURLParam(newRequest(http.MethodPut, "/env", body), "id", id)
		testutil.Call(t, testHandler.UpdateAgentEnv, req).Want(http.StatusOK)
	}
	get := func() AgentEnvResponse {
		t.Helper()
		var got AgentEnvResponse
		req := withURLParam(newRequest(http.MethodGet, "/env", nil), "id", id)
		testutil.Call(t, testHandler.GetAgentEnv, req).Want(http.StatusOK).JSON(&got)
		return got
	}
	put(map[string]any{
		"custom_env":       map[string]string{"KEEP": "synthetic-secret", "DELETE": "other"},
		"custom_env_notes": map[string]string{"KEEP": "private-note", "DELETE": "old-note", "ORPHAN": "ignored"},
	})
	if len(get().CustomEnvNotes) != 2 {
		t.Fatal("orphan note persisted")
	}
	// Older clients omit notes and may send masked values. Retained-key metadata survives.
	put(map[string]any{"custom_env": map[string]string{"KEEP": "****", "NEW": "****"}})
	got := get()
	if got.CustomEnv["KEEP"] != "synthetic-secret" || got.CustomEnvNotes["KEEP"] != "private-note" || len(got.CustomEnv) != 1 || len(got.CustomEnvNotes) != 1 {
		t.Fatalf("legacy/sentinel/delete behavior: %+v", got)
	}
	put(map[string]any{"custom_env": got.CustomEnv, "custom_env_notes": map[string]string{"KEEP": "edited-note"}})
	if get().CustomEnvNotes["KEEP"] != "edited-note" {
		t.Fatal("note edit lost")
	}
	bad := withURLParam(newRequest(http.MethodPut, "/env", map[string]any{
		"custom_env":       map[string]string{"KEEP": "must-not-save"},
		"custom_env_notes": map[string]int{"KEEP": 12},
	}), "id", id)
	testutil.Call(t, testHandler.UpdateAgentEnv, bad).Want(http.StatusBadRequest)
	if current := get(); current.CustomEnv["KEEP"] != "synthetic-secret" || current.CustomEnvNotes["KEEP"] != "edited-note" {
		t.Fatal("malformed note changed persisted configuration")
	}
	// Whitespace-only notes clear a note without changing the underlying value.
	put(map[string]any{"custom_env": got.CustomEnv, "custom_env_notes": map[string]string{"KEEP": "  "}})
	if len(get().CustomEnvNotes) != 0 {
		t.Fatal("blank note persisted")
	}
	put(map[string]any{"custom_env": got.CustomEnv, "custom_env_notes": map[string]string{"KEEP": "again"}})
	put(map[string]any{"custom_env": got.CustomEnv, "custom_env_notes": map[string]string{}})
	if len(get().CustomEnvNotes) != 0 {
		t.Fatal("explicit empty notes did not clear")
	}
	var details string
	dbfx.QueryRow(t, `SELECT string_agg(details::text, ',') FROM activity_log WHERE details->>'agent_id'=$1`, id).Scan(&details)
	for _, secret := range []string{"synthetic-secret", "private-note", "edited-note", "old-note", "again"} {
		if strings.Contains(details, secret) {
			t.Fatalf("audit leaked synthetic secret or note %q", secret)
		}
	}
	if !strings.Contains(details, "note_changed_keys") {
		t.Fatal("missing note-change audit")
	}
}

func TestAgentEnvNotesAbsentFromPublicResponsesAndClaim(t *testing.T) {
	taskID, runtimeID, _ := startClaimFixture(t, "dispatched")
	ctx := context.Background()
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	id := uuidToString(task.AgentID)
	dbfx.Exec(t, `UPDATE agent SET custom_env='{"KEY":"synthetic-value"}', custom_env_notes='{"KEY":"private-note"}' WHERE id=$1`, id)
	for name, fn := range map[string]http.HandlerFunc{"get": testHandler.GetAgent, "list": testHandler.ListAgents} {
		t.Run(name, func(t *testing.T) {
			req := withURLParam(newRequest(http.MethodGet, "/agents/"+id, nil), "id", id)
			resp := testutil.Call(t, fn, req).Want(http.StatusOK)
			if !strings.Contains(resp.Body.String(), id) {
				t.Fatal("fixture agent missing from response")
			}
			for _, forbidden := range []string{"custom_env_notes", "private-note", "synthetic-value"} {
				if strings.Contains(resp.Body.String(), forbidden) {
					t.Fatalf("public response contains %q", forbidden)
				}
			}
		})
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(broadcastAgentResponse(testHandler.agentToResponse(agent)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(event), "private-note") || strings.Contains(string(event), "custom_env_notes") {
		t.Fatal("broadcast contains notes")
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(newOwnerDaemonRequest("POST", "/claim", nil), &task, runtime, runtimeID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("build claim: %+v", failure)
	}
	wire, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "private-note") || strings.Contains(string(wire), "custom_env_notes") {
		t.Fatal("daemon claim contains notes")
	}
	if len(resp.Agent.CustomEnv) != 1 || resp.Agent.CustomEnv["KEY"] != "synthetic-value" {
		t.Fatalf("process env changed: %v", resp.Agent.CustomEnv)
	}
	req := withURLParam(newRequest(http.MethodPut, "/agents/"+id, map[string]any{"custom_env_notes": map[string]string{"KEY": "bad"}}), "id", id)
	testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusBadRequest)
}
