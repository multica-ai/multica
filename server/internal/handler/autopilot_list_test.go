package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// insertListTestAutopilot creates a bare autopilot row and registers cleanup.
// Triggers/runs cascade on delete.
func insertListTestAutopilot(t *testing.T, agentID, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			status, execution_mode, created_by_type, created_by_id
		)
		VALUES ($1, $2, 'agent', $3, 'active', 'run_only', 'member', $4)
		RETURNING id
	`, testWorkspaceID, title, agentID, testUserID).Scan(&id); err != nil {
		t.Fatalf("failed to insert test autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, id)
	})
	return id
}

// TestListAutopilots_DerivedFields guards the three list-only derived
// columns added for the list UI (trigger badges, next run, last-run
// outcome): trigger_kinds/next_run_at must consider ENABLED triggers only,
// last_run_status must be the most recent run's status, and all three must
// be omitted entirely when there is nothing to derive (the optional-field
// contract older clients rely on).
func TestListAutopilots_DerivedFields(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "autopilot-list-derived-agent", []byte(`[]`))
	withData := insertListTestAutopilot(t, agentID, "list-derived-with-data")
	bare := insertListTestAutopilot(t, agentID, "list-derived-bare")

	// Enabled schedule (carries next_run_at), enabled webhook, and a
	// DISABLED api trigger that must not leak into trigger_kinds.
	for _, q := range []string{
		`INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, timezone, next_run_at)
		 VALUES ($1, 'schedule', true, '0 9 * * *', 'UTC', now() + interval '1 hour')`,
		`INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, webhook_token)
		 VALUES ($1, 'webhook', true, 'list-derived-tok')`,
		`INSERT INTO autopilot_trigger (autopilot_id, kind, enabled)
		 VALUES ($1, 'api', false)`,
	} {
		if _, err := testPool.Exec(ctx, q, withData); err != nil {
			t.Fatalf("failed to insert trigger: %v", err)
		}
	}

	// Older completed run, newer failed run — last_run_status must be the
	// newest by triggered_at, not insertion order.
	for _, q := range []string{
		`INSERT INTO autopilot_run (autopilot_id, source, status, triggered_at)
		 VALUES ($1, 'schedule', 'failed', now() - interval '1 hour')`,
		`INSERT INTO autopilot_run (autopilot_id, source, status, triggered_at)
		 VALUES ($1, 'schedule', 'completed', now() - interval '2 hour')`,
	} {
		if _, err := testPool.Exec(ctx, q, withData); err != nil {
			t.Fatalf("failed to insert run: %v", err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots", nil))
	if w.Code != 200 {
		t.Fatalf("ListAutopilots: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Autopilots []map[string]any `json:"autopilots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	rows := make(map[string]map[string]any)
	for _, row := range body.Autopilots {
		rows[row["id"].(string)] = row
	}

	rich, ok := rows[withData]
	if !ok {
		t.Fatalf("autopilot %s missing from list", withData)
	}
	kinds, _ := rich["trigger_kinds"].([]any)
	if len(kinds) != 2 || kinds[0] != "schedule" || kinds[1] != "webhook" {
		t.Errorf("trigger_kinds: expected [schedule webhook] (enabled only, sorted), got %v", rich["trigger_kinds"])
	}
	if s, _ := rich["next_run_at"].(string); s == "" {
		t.Errorf("next_run_at: expected the enabled schedule trigger's time, got %v", rich["next_run_at"])
	}
	if rich["last_run_status"] != "failed" {
		t.Errorf("last_run_status: expected most recent run (failed), got %v", rich["last_run_status"])
	}

	plain, ok := rows[bare]
	if !ok {
		t.Fatalf("autopilot %s missing from list", bare)
	}
	for _, key := range []string{"trigger_kinds", "next_run_at", "last_run_status"} {
		if _, present := plain[key]; present {
			t.Errorf("%s: expected field omitted for autopilot with no triggers/runs, got %v", key, plain[key])
		}
	}
}

func TestListAutopilots_DefaultExcludesArchived(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "autopilot-list-archived-agent", []byte(`[]`))
	archived := insertListTestAutopilot(t, agentID, "list-archived-hidden")
	if _, err := testPool.Exec(ctx, `UPDATE autopilot SET status = 'archived' WHERE id = $1`, archived); err != nil {
		t.Fatalf("archive autopilot fixture: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots", nil))
	if w.Code != 200 {
		t.Fatalf("ListAutopilots default: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Autopilots []map[string]any `json:"autopilots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode default list: %v", err)
	}
	for _, row := range body.Autopilots {
		if row["id"] == archived {
			t.Fatalf("archived autopilot %s appeared in default list", archived)
		}
	}

	w = httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots?status=archived", nil))
	if w.Code != 200 {
		t.Fatalf("ListAutopilots archived: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body.Autopilots = nil
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode archived list: %v", err)
	}
	found := false
	for _, row := range body.Autopilots {
		if row["id"] == archived {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("archived autopilot %s missing from status=archived list", archived)
	}
}
