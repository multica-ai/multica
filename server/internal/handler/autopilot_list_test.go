package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
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

// TestListAutopilots_SubscribersMatchDetail is the regression guard for
// MUL-6680: GET /api/autopilots used to hard-code an empty subscriber slice to
// avoid an N+1, which serialized as "subscribers": [] on every row. The key was
// present and looked authoritative, so callers could not tell "no subscribers"
// from "not fetched" — and the detail endpoint reported something different for
// the very same autopilot. The two projections must now agree.
func TestListAutopilots_SubscribersMatchDetail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "autopilot-list-subs-agent", []byte(`[]`))
	withSubs := insertListTestAutopilot(t, agentID, "list-subs-populated")
	withoutSubs := insertListTestAutopilot(t, agentID, "list-subs-empty")

	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot_subscriber (autopilot_id, user_type, user_id)
		VALUES ($1, 'member', $2)
	`, withSubs, testUserID); err != nil {
		t.Fatalf("insert subscriber fixture: %v", err)
	}

	// list
	w := httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots", nil))
	if w.Code != 200 {
		t.Fatalf("ListAutopilots: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listBody struct {
		Autopilots []map[string]any `json:"autopilots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("failed to decode list body: %v", err)
	}
	listRows := make(map[string]map[string]any)
	for _, row := range listBody.Autopilots {
		listRows[row["id"].(string)] = row
	}

	// detail, for the same autopilot at the same moment
	w = httptest.NewRecorder()
	testHandler.GetAutopilot(w, withURLParam(
		newRequest("GET", "/api/autopilots/"+withSubs+"?workspace_id="+testWorkspaceID, nil), "id", withSubs))
	if w.Code != 200 {
		t.Fatalf("GetAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detailBody struct {
		Autopilot map[string]any `json:"autopilot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detailBody); err != nil {
		t.Fatalf("failed to decode detail body: %v", err)
	}

	fromList, _ := listRows[withSubs]["subscribers"].([]any)
	fromDetail, _ := detailBody.Autopilot["subscribers"].([]any)
	if len(fromDetail) != 1 {
		t.Fatalf("detail subscribers: expected the seeded member, got %v", detailBody.Autopilot["subscribers"])
	}
	if len(fromList) != len(fromDetail) {
		t.Errorf("list/detail disagree on subscribers: list=%v detail=%v", fromList, fromDetail)
	}
	if len(fromList) == 1 {
		got, _ := fromList[0].(map[string]any)
		if got["user_id"] != testUserID {
			t.Errorf("list subscriber user_id: expected %s, got %v", testUserID, got["user_id"])
		}
	}

	// An autopilot with genuinely no subscribers must still serialize the key
	// as [] rather than omitting it — the field's documented contract, which
	// only becomes trustworthy now that it is populated.
	empty, ok := listRows[withoutSubs]["subscribers"]
	if !ok {
		t.Errorf("subscribers key must be present even when empty")
	} else if entries, _ := empty.([]any); len(entries) != 0 {
		t.Errorf("expected empty subscribers for %s, got %v", withoutSubs, empty)
	}
}

// hideAutopilotSubscriberTable makes reads of autopilot_subscriber fail for the
// duration of one test, so the subscriber query's error branch can be exercised
// while the autopilot query itself still succeeds. A rename isolates the
// failure to exactly that one query; t.Cleanup restores it. Safe to run
// alongside the rest of the suite because non-parallel tests in a package run
// sequentially, and no parallel test in this package touches this table.
func hideAutopilotSubscriberTable(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `ALTER TABLE autopilot_subscriber RENAME TO autopilot_subscriber_hidden`); err != nil {
		t.Fatalf("hide autopilot_subscriber: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `ALTER TABLE autopilot_subscriber_hidden RENAME TO autopilot_subscriber`); err != nil {
			t.Fatalf("restore autopilot_subscriber: %v", err)
		}
	})
}

// TestAutopilotSubscriberReadFailureFailsClosed guards the error path of the
// MUL-6680 fix. "subscribers" has no omitempty and is documented as
// authoritative, so degrading a failed read to an empty array would recreate
// the very defect this change removes: the caller cannot tell "none
// configured" from "read failed", and a client that round-trips the response
// into a full-replace PATCH would silently wipe a real subscriber list. Both
// projections must surface the error instead.
func TestAutopilotSubscriberReadFailureFailsClosed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "autopilot-subs-failclosed-agent", []byte(`[]`))
	apID := insertListTestAutopilot(t, agentID, "list-subs-fail-closed")
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO autopilot_subscriber (autopilot_id, user_type, user_id)
		VALUES ($1, 'member', $2)
	`, apID, testUserID); err != nil {
		t.Fatalf("insert subscriber fixture: %v", err)
	}

	hideAutopilotSubscriberTable(t)

	t.Run("list", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots", nil))
		if w.Code != 500 {
			t.Fatalf("expected 500 when the subscriber read fails, got %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"subscribers":[]`) {
			t.Errorf("response must not carry an authoritative empty subscriber list: %s", w.Body.String())
		}
	})

	t.Run("detail", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetAutopilot(w, withURLParam(
			newRequest("GET", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, nil), "id", apID))
		if w.Code != 500 {
			t.Fatalf("expected 500 when the subscriber read fails, got %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"subscribers":[]`) {
			t.Errorf("response must not carry an authoritative empty subscriber list: %s", w.Body.String())
		}
	})
}
