package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// insertListTestAutopilot creates a bare autopilot row and registers cleanup.
// Triggers/runs cascade on delete.
func insertListTestAutopilot(t *testing.T, agentID, title string) string {
	return insertListTestAutopilotWithCreator(t, agentID, title, "member", testUserID)
}

func insertListTestAutopilotWithCreator(t *testing.T, agentID, title, creatorType, creatorID string) string {
	return insertListTestAutopilotWithActors(t, "agent", agentID, title, creatorType, creatorID)
}

func insertListTestAutopilotWithActors(t *testing.T, assigneeType, assigneeID, title, creatorType, creatorID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			status, execution_mode, created_by_type, created_by_id
		)
		VALUES ($1, $2, $3, $4, 'active', 'run_only', $5, $6)
		RETURNING id
	`, testWorkspaceID, title, assigneeType, assigneeID, creatorType, creatorID).Scan(&id); err != nil {
		t.Fatalf("failed to insert test autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, id)
	})
	return id
}

func insertListTestSquad(t *testing.T, name, leaderID, creatorID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, name, leaderID, creatorID).Scan(&id); err != nil {
		t.Fatalf("failed to insert test squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, id)
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

func TestListAutopilots_MineIncludesCurrentUserOwnedAgentCreatorsAndOwnedAssignees(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	ownedAgentID := createHandlerTestAgent(t, "autopilot-list-mine-owned-agent", []byte(`[]`))
	otherUserID := createWorkspaceMemberUser(
		t,
		"Autopilot Mine Other",
		fmt.Sprintf("autopilot-mine-other-%d@test.multica.ai", time.Now().UnixNano()),
	)
	otherAgentID := createHandlerTestAgent(t, "autopilot-list-mine-other-agent", []byte(`[]`))
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, otherUserID, otherAgentID); err != nil {
		t.Fatalf("failed to transfer other agent owner: %v", err)
	}
	now := time.Now().UnixNano()
	ownedLeaderSquadID := insertListTestSquad(
		t,
		fmt.Sprintf("autopilot-list-mine-owned-leader-%d", now),
		ownedAgentID,
		testUserID,
	)
	otherLeaderSquadID := insertListTestSquad(
		t,
		fmt.Sprintf("autopilot-list-mine-other-leader-%d", now),
		otherAgentID,
		otherUserID,
	)

	currentMember := insertListTestAutopilotWithCreator(
		t,
		otherAgentID,
		"mine-current-member",
		"member",
		testUserID,
	)
	ownedAgent := insertListTestAutopilotWithCreator(
		t,
		otherAgentID,
		"mine-owned-agent",
		"agent",
		ownedAgentID,
	)
	ownedAssignee := insertListTestAutopilotWithCreator(
		t,
		ownedAgentID,
		"mine-owned-agent-assignee",
		"member",
		otherUserID,
	)
	ownedSquadAssignee := insertListTestAutopilotWithActors(
		t,
		"squad",
		ownedLeaderSquadID,
		"mine-owned-squad-leader-assignee",
		"member",
		otherUserID,
	)
	otherMember := insertListTestAutopilotWithCreator(
		t,
		otherAgentID,
		"mine-other-member",
		"member",
		otherUserID,
	)
	otherAgent := insertListTestAutopilotWithCreator(
		t,
		otherAgentID,
		"mine-other-agent",
		"agent",
		otherAgentID,
	)
	otherSquadAssignee := insertListTestAutopilotWithActors(
		t,
		"squad",
		otherLeaderSquadID,
		"mine-other-squad-leader-assignee",
		"member",
		otherUserID,
	)

	w := httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots?mine=true", nil))
	if w.Code != 200 {
		t.Fatalf("ListAutopilots mine: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Autopilots []map[string]any `json:"autopilots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	rows := make(map[string]bool, len(body.Autopilots))
	for _, row := range body.Autopilots {
		rows[row["id"].(string)] = true
	}

	if !rows[currentMember] {
		t.Fatalf("mine=true did not include autopilot created by current member")
	}
	if !rows[ownedAgent] {
		t.Fatalf("mine=true did not include autopilot created by current user's agent")
	}
	if !rows[ownedAssignee] {
		t.Fatalf("mine=true did not include autopilot assigned to current user's agent")
	}
	if !rows[ownedSquadAssignee] {
		t.Fatalf("mine=true did not include autopilot assigned to squad led by current user's agent")
	}
	if rows[otherMember] {
		t.Fatalf("mine=true included autopilot created by another member")
	}
	if rows[otherAgent] {
		t.Fatalf("mine=true included autopilot created by another user's agent")
	}
	if rows[otherSquadAssignee] {
		t.Fatalf("mine=true included autopilot assigned to squad led by another user's agent")
	}
}
