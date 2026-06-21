// FIR-1703 — coverage for observeCommentWakeOutcome: a member comment that
// should wake the assignee agent but cannot start a run now must leave a
// System Activity (activity_log) line stating the cause, and the genuine
// success path must stay quiet.
package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeWakeObserveIssue inserts a runtime (with the given status, or none when
// runtimeStatus == ""), an agent bound to it, and an issue assigned to that
// agent. Returns the loaded issue. All rows are cleaned up via t.Cleanup.
func makeWakeObserveIssue(t *testing.T, runtimeStatus string) db.Issue {
	t.Helper()
	ctx := context.Background()

	var runtimeID any // nil → agent.runtime_id stays NULL
	if runtimeStatus != "" {
		var rtID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
			VALUES ($1, NULL, 'Wake Observe Runtime', 'cloud', 'wake_observe_test', $2, 'Test', '{}'::jsonb, now())
			RETURNING id
		`, testWorkspaceID, runtimeStatus).Scan(&rtID); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
		runtimeID = rtID
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rtID) })
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'wake-observe-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		VALUES ($1, 'wake observe test', 'todo', 'medium', 'member', $2, 'agent', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return issue
}

// wakeFailReason returns the recorded comment_wake_failed reason for the issue,
// or "" when no such activity exists.
func wakeFailReason(t *testing.T, issueID string) string {
	t.Helper()
	rows, err := testHandler.Queries.ListActivitiesForIssue(context.Background(), db.ListActivitiesForIssueParams{
		IssueID: util.MustParseUUID(issueID),
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	for _, a := range rows {
		if a.Action != commentWakeAction {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal(a.Details, &d); err != nil {
			t.Fatalf("unmarshal details: %v", err)
		}
		if r, ok := d["reason"].(string); ok {
			return r
		}
	}
	return ""
}

func TestObserveCommentWakeOutcome(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	comment := db.Comment{ID: util.MustParseUUID(testUserID)} // any non-zero comment id; only echoed into details

	cases := []struct {
		name          string
		runtimeStatus string
		wantReason    string
	}{
		{"no runtime → no_runtime", "", commentWakeReasonNoRuntime},
		{"offline runtime → runtime_offline", "offline", commentWakeReasonRuntimeOffline},
		{"online runtime → silent success", "online", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := makeWakeObserveIssue(t, tc.runtimeStatus)
			testHandler.observeCommentWakeOutcome(ctx, issue, comment, "member", testUserID)
			got := wakeFailReason(t, util.UUIDToString(issue.ID))
			if got != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got, tc.wantReason)
			}
		})
	}
}

// A non-member author (agent-to-agent) must never produce a wake-failed line —
// the observer only governs member-driven wakes.
func TestObserveCommentWakeOutcome_NonMemberSilent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issue := makeWakeObserveIssue(t, "") // no runtime would otherwise record no_runtime
	comment := db.Comment{ID: util.MustParseUUID(testUserID)}
	testHandler.observeCommentWakeOutcome(ctx, issue, comment, "agent", testUserID)
	if got := wakeFailReason(t, util.UUIDToString(issue.ID)); got != "" {
		t.Fatalf("expected no activity for agent author, got reason %q", got)
	}
}
