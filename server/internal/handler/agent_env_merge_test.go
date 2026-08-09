package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func mergeEnvRequest(t *testing.T, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	req := newRequestAs(userID, http.MethodPatch, "/api/agents/env", body)
	w := httptest.NewRecorder()
	testHandler.MergeAgentsEnv(w, req)
	return w
}

func decodeMergeEnvResponse(t *testing.T, w *httptest.ResponseRecorder) mergeAgentsEnvResponse {
	t.Helper()

	var resp mergeAgentsEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode merge response: %v (body: %s)", err, w.Body.String())
	}
	return resp
}

func storedEnv(t *testing.T, agentID string) map[string]string {
	t.Helper()

	var raw string
	if err := testPool.QueryRow(context.Background(),
		`SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&raw); err != nil {
		t.Fatalf("read custom_env: %v", err)
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode custom_env: %v", err)
	}
	return out
}

func setEnv(t *testing.T, agentID string, env map[string]string) {
	t.Helper()

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("encode env: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET custom_env = $1 WHERE id = $2`, raw, agentID); err != nil {
		t.Fatalf("seed custom_env: %v", err)
	}
}

// TestMergeAgentsEnv_InjectsWithoutTouchingOtherKeys is the reason this
// endpoint exists. PUT /api/agents/{id}/env replaces the map wholesale, so
// injecting one key while preserving the rest would require reading every
// secret back first. Merge writes only the submitted keys.
func TestMergeAgentsEnv_InjectsWithoutTouchingOtherKeys(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	first := createHandlerTestAgent(t, "env-merge-first", nil)
	second := createHandlerTestAgent(t, "env-merge-second", nil)
	setEnv(t, first, map[string]string{"EXISTING": "keep-me", "SHARED": "old-value"})
	setEnv(t, second, map[string]string{})

	w := mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{first, second},
		"set":       map[string]string{"SHARED": "new-value", "FRESH": "added"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMergeEnvResponse(t, w)

	if got := storedEnv(t, first); got["EXISTING"] != "keep-me" ||
		got["SHARED"] != "new-value" || got["FRESH"] != "added" || len(got) != 3 {
		t.Errorf("first agent env = %v; untouched keys must survive the merge", got)
	}
	if got := storedEnv(t, second); got["SHARED"] != "new-value" || got["FRESH"] != "added" || len(got) != 2 {
		t.Errorf("second agent env = %v", got)
	}

	byAgent := map[string]mergeAgentsEnvResult{}
	for _, r := range resp.Results {
		byAgent[r.AgentID] = r
	}
	if got := byAgent[first]; strings.Join(got.AddedKeys, ",") != "FRESH" ||
		strings.Join(got.OverwrittenKeys, ",") != "SHARED" || got.KeyCount != 3 {
		t.Errorf("first agent result = %+v; want FRESH added, SHARED overwritten, 3 keys", got)
	}
	if got := byAgent[second]; strings.Join(got.AddedKeys, ",") != "FRESH,SHARED" ||
		len(got.OverwrittenKeys) != 0 {
		t.Errorf("second agent result = %+v; want both keys reported as added", got)
	}
}

// TestMergeAgentsEnv_ResponseNeverCarriesValues is the disclosure boundary:
// the response may name the keys the caller just submitted, and nothing else —
// no values, and no key the caller did not already know about.
func TestMergeAgentsEnv_ResponseNeverCarriesValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "env-merge-disclosure", nil)
	setEnv(t, agentID, map[string]string{"PRE_EXISTING_KEY": "pre-existing-secret"})

	w := mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{agentID},
		"set":       map[string]string{"INJECTED": "injected-secret"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	for _, forbidden := range []string{"injected-secret", "pre-existing-secret", "PRE_EXISTING_KEY"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaked %q: %s", forbidden, body)
		}
	}
}

// TestMergeAgentsEnv_NoRevealAudit checks that injection stays off the reveal
// trail. A bulk edit that had to read existing values would write one
// `agent_env_revealed` row per agent; this path writes only the update rows.
func TestMergeAgentsEnv_NoRevealAudit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "env-merge-audit", nil)

	w := mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{agentID},
		"set":       map[string]string{"AUDITED": "s3cret-audit-value"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var revealed int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1 AND action = $2 AND details->>'agent_id' = $3
	`, testWorkspaceID, agentEnvActivityRevealed, agentID).Scan(&revealed); err != nil {
		t.Fatalf("count reveal rows: %v", err)
	}
	if revealed != 0 {
		t.Errorf("merge must not produce reveal audit rows, got %d", revealed)
	}

	var details string
	if err := testPool.QueryRow(ctx, `
		SELECT details::text FROM activity_log
		WHERE workspace_id = $1 AND action = $2 AND details->>'agent_id' = $3
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentEnvActivityUpdated, agentID).Scan(&details); err != nil {
		t.Fatalf("load update audit row: %v", err)
	}
	// jsonb re-renders with its own spacing, so match on content not layout.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(details), &parsed); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	if parsed["source"] != "bulk_merge" {
		t.Errorf("audit row should mark the bulk path, got %v", parsed["source"])
	}
	if !strings.Contains(details, "AUDITED") {
		t.Errorf("audit row should name the touched key, got %s", details)
	}
	if strings.Contains(details, "s3cret-audit-value") {
		t.Errorf("audit row must never carry values, got %s", details)
	}
}

// TestMergeAgentsEnv_AgentActorForbidden keeps the MUL-2600 rule intact on the
// new surface: an agent token cannot reach env management, even when its host
// member would be allowed.
func TestMergeAgentsEnv_AgentActorForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	hostAgentID := createHandlerTestAgent(t, "env-merge-host-agent", nil)
	hostTaskID := createHandlerTestTaskForAgent(t, hostAgentID)
	targetID := createHandlerTestAgent(t, "env-merge-actor-target", nil)

	req := newRequestAs(testUserID, http.MethodPatch, "/api/agents/env", map[string]any{
		"agent_ids": []string{targetID},
		"set":       map[string]string{"SNEAKY": "value"},
	})
	req.Header.Set("X-Agent-ID", hostAgentID)
	req.Header.Set("X-Task-ID", hostTaskID)
	w := httptest.NewRecorder()
	testHandler.MergeAgentsEnv(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an agent actor, got %d: %s", w.Code, w.Body.String())
	}
	if got := storedEnv(t, targetID); len(got) != 0 {
		t.Errorf("agent actor must not write env, got %v", got)
	}
}

// TestMergeAgentsEnv_SkipsAgentsTheCallerMayNotManage mirrors the migration
// endpoint's bulk contract on the env surface.
func TestMergeAgentsEnv_SkipsAgentsTheCallerMayNotManage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	callerID := createPermissionTestMember(t, "env-merge-caller@multica.test")
	otherID := createPermissionTestMember(t, "env-merge-other@multica.test")
	own := createHandlerTestAgent(t, "env-merge-own", nil)
	foreign := createHandlerTestAgent(t, "env-merge-foreign", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, callerID, own); err != nil {
		t.Fatalf("assign own agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, otherID, foreign); err != nil {
		t.Fatalf("assign foreign agent: %v", err)
	}
	missing := uuid.NewString()

	w := mergeEnvRequest(t, callerID, map[string]any{
		"agent_ids": []string{own, foreign, missing},
		"set":       map[string]string{"KEY": "value"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMergeEnvResponse(t, w)

	if len(resp.Results) != 1 || resp.Results[0].AgentID != own {
		t.Fatalf("expected only the caller's agent written, got %+v", resp.Results)
	}
	reasons := map[string]string{}
	for _, s := range resp.Skipped {
		reasons[s.AgentID] = s.Reason
	}
	if reasons[foreign] != migrateSkipForbidden || reasons[missing] != migrateSkipNotFound {
		t.Errorf("unexpected skip reasons: %+v", resp.Skipped)
	}
	if got := storedEnv(t, foreign); len(got) != 0 {
		t.Errorf("a skipped agent must not be written, got %v", got)
	}
}

// TestMergeAgentsEnv_AuditFailureRollsBackWholeBatch pins the fail-closed
// promise the single-agent path makes, across a batch: if the audit row cannot
// be written for ANY agent, no agent's env is left mutated. Simulated with a
// trigger that rejects the audit insert for one agent — the same failure mode a
// real activity_log outage produces mid-batch.
func TestMergeAgentsEnv_AuditFailureRollsBackWholeBatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	first := createHandlerTestAgent(t, "env-merge-rollback-first", nil)
	poison := createHandlerTestAgent(t, "env-merge-rollback-poison", nil)
	setEnv(t, first, map[string]string{"ORIGINAL": "unchanged"})

	if _, err := testPool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION multica_test_reject_env_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.details->>'agent_name' = 'env-merge-rollback-poison' THEN
				RAISE EXCEPTION 'simulated audit outage';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER multica_test_reject_env_audit
			BEFORE INSERT ON activity_log
			FOR EACH ROW EXECUTE FUNCTION multica_test_reject_env_audit();
	`); err != nil {
		t.Fatalf("install audit-failure trigger: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS multica_test_reject_env_audit ON activity_log;
			DROP FUNCTION IF EXISTS multica_test_reject_env_audit();
		`)
	})

	// `first` is written before the poisoned agent is reached, so it is the
	// row that proves the rollback covered work already done in this batch.
	w := mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{first, poison},
		"set":       map[string]string{"INJECTED": "value"},
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the audit write fails, got %d: %s", w.Code, w.Body.String())
	}

	if got := storedEnv(t, first); got["INJECTED"] != "" || got["ORIGINAL"] != "unchanged" {
		t.Errorf("audit failure must roll back the whole batch, first agent env = %v", got)
	}
	if got := storedEnv(t, poison); len(got) != 0 {
		t.Errorf("poisoned agent env = %v; want nothing written", got)
	}
}

// TestMergeAgentsEnv_RejectsUnusableInput covers the request guards: something
// to write, no blank keys, and no masked placeholder (which means "keep the
// existing value" on the single-agent path and has no meaning in an injection).
func TestMergeAgentsEnv_RejectsUnusableInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "env-merge-input-guards", nil)
	cases := []struct {
		name string
		set  map[string]string
	}{
		{"empty set", map[string]string{}},
		{"blank key", map[string]string{"   ": "value"}},
		{"masked placeholder", map[string]string{"KEY": envSentinel}},
		// Keys are trimmed before they are stored, so these two name one
		// variable. Accepting both would let Go's randomised map iteration
		// pick the winner: the same request would write a different secret on
		// different runs. The UI cannot produce this; the API and CLI can.
		{"keys colliding after trim", map[string]string{"KEY": "one", " KEY ": "two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := mergeEnvRequest(t, testUserID, map[string]any{
				"agent_ids": []string{agentID},
				"set":       tc.set,
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if got := storedEnv(t, agentID); len(got) != 0 {
		t.Errorf("rejected requests must write nothing, got %v", got)
	}
}

// TestMergeAgentsEnv_TrimsKeysBeforeWriting pins the other half of the trim
// rule: a single padded key is accepted and stored under its trimmed name, so
// `" KEY "` and a later `"KEY"` address the same variable instead of silently
// creating two.
func TestMergeAgentsEnv_TrimsKeysBeforeWriting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "env-merge-trim", nil)

	w := mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{agentID},
		"set":       map[string]string{"  PADDED  ": "value"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	stored := storedEnv(t, agentID)
	if stored["PADDED"] != "value" || len(stored) != 1 {
		t.Fatalf("expected the key stored trimmed, got %v", stored)
	}
	resp := decodeMergeEnvResponse(t, w)
	if len(resp.Results) != 1 || strings.Join(resp.Results[0].AddedKeys, ",") != "PADDED" {
		t.Errorf("response should report the trimmed key, got %+v", resp.Results)
	}

	// Re-submitting the same variable unpadded is an overwrite of that one
	// key, not a second key.
	w = mergeEnvRequest(t, testUserID, map[string]any{
		"agent_ids": []string{agentID},
		"set":       map[string]string{"PADDED": "second"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on the second write, got %d: %s", w.Code, w.Body.String())
	}
	if stored := storedEnv(t, agentID); stored["PADDED"] != "second" || len(stored) != 1 {
		t.Errorf("expected one key overwritten, got %v", stored)
	}
}

// TestMergeEnvKeys is the pure merge rule: submitted keys win, everything else
// survives, and only real changes are reported.
func TestMergeEnvKeys(t *testing.T) {
	existing := map[string]string{"KEEP": "a", "SAME": "b", "CHANGE": "c"}
	merged, result := mergeEnvKeys(existing, map[string]string{
		"SAME":   "b",
		"CHANGE": "c2",
		"NEW":    "d",
	})

	want := map[string]string{"KEEP": "a", "SAME": "b", "CHANGE": "c2", "NEW": "d"}
	for k, v := range want {
		if merged[k] != v {
			t.Errorf("merged[%s] = %q, want %q", k, merged[k], v)
		}
	}
	if len(merged) != len(want) {
		t.Errorf("merged has %d keys, want %d", len(merged), len(want))
	}
	if strings.Join(result.AddedKeys, ",") != "NEW" {
		t.Errorf("added = %v, want [NEW]", result.AddedKeys)
	}
	if strings.Join(result.OverwrittenKeys, ",") != "CHANGE" {
		t.Errorf("overwritten = %v, want [CHANGE]; an identical resubmit is not a change", result.OverwrittenKeys)
	}
	if result.KeyCount != 4 {
		t.Errorf("key_count = %d, want 4", result.KeyCount)
	}
	if existing["CHANGE"] != "c" {
		t.Error("mergeEnvKeys must not mutate the caller's existing map")
	}
}

// TestMergeAgentsEnv_HiddenAgentIsNotFound pins the same non-disclosure rule
// the migration endpoint follows (MUL-5758 security review): an agent inside
// the workspace but invisible to this caller is reported exactly like an id
// that never existed. Reporting it as `forbidden` with its name would let a
// plain member enumerate private agents through a bulk write.
func TestMergeAgentsEnv_HiddenAgentIsNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	callerID := createPermissionTestMember(t, "env-hidden-caller@multica.test")
	otherID := createPermissionTestMember(t, "env-hidden-other@multica.test")
	hidden := createHiddenAgentOnRuntime(t, "env-hidden-agent", handlerTestRuntimeID(t), otherID)

	w := mergeEnvRequest(t, callerID, map[string]any{
		"agent_ids": []string{hidden},
		"set":       map[string]string{"INJECTED": "value"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMergeEnvResponse(t, w)

	if len(resp.Results) != 0 {
		t.Fatalf("a hidden agent must not be written, got %+v", resp.Results)
	}
	if len(resp.Skipped) != 1 {
		t.Fatalf("expected 1 skip, got %+v", resp.Skipped)
	}
	if resp.Skipped[0].Reason != migrateSkipNotFound {
		t.Errorf("reason = %q, want %q", resp.Skipped[0].Reason, migrateSkipNotFound)
	}
	if resp.Skipped[0].Name != "" {
		t.Errorf("a hidden agent's name must not be disclosed, got %q", resp.Skipped[0].Name)
	}
	if strings.Contains(w.Body.String(), "env-hidden-agent") {
		t.Errorf("response must not name a hidden agent; body: %s", w.Body.String())
	}
}
