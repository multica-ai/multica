package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestDeriveSquadMemberStatus(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	online := pgtype.Text{String: "online", Valid: true}
	offline := pgtype.Text{String: "offline", Valid: true}
	missing := pgtype.Text{}

	tsAgo := func(d time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(-d), Valid: true}
	}
	tsNone := pgtype.Timestamptz{}

	cases := []struct {
		name           string
		archived       bool
		runtimeStatus  pgtype.Text
		lastSeen       pgtype.Timestamptz
		hasWorkingTask bool
		want           string
	}{
		{"working wins over offline runtime", false, offline, tsAgo(time.Hour), true, "working"},
		{"working wins over missing runtime", false, missing, tsNone, true, "working"},
		{"online runtime, no task", false, online, tsAgo(2 * time.Second), false, "idle"},
		{"offline runtime, recent heartbeat", false, offline, tsAgo(2 * time.Minute), false, "unstable"},
		{"offline runtime, stale heartbeat", false, offline, tsAgo(2 * time.Hour), false, "offline"},
		{"offline runtime, no heartbeat", false, offline, tsNone, false, "offline"},
		{"no runtime row", false, missing, tsNone, false, "offline"},
		// Archived agents always report archived regardless of any leftover
		// runtime row or task — they should appear in the squad listing
		// but never look like they're still working or merely offline.
		{"archived agent with active task", true, online, tsAgo(time.Second), true, "archived"},
		{"archived agent with online runtime", true, online, tsAgo(time.Second), false, "archived"},
		{"archived agent already offline", true, offline, tsAgo(time.Hour), false, "archived"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveSquadMemberStatus(tc.archived, tc.runtimeStatus, tc.lastSeen, tc.hasWorkingTask, now)
			if got != tc.want {
				t.Fatalf("deriveSquadMemberStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListSquadMemberStatusRetainsWaiterIssueWithoutWorkingStatus(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "squad-status-local-directory-waiter", []byte(`{}`))
	squadID := createCommentTriggerPreviewSquad(t, "Squad Status Local Directory Waiter", agentID)
	issueID := createCommentTriggerPreviewIssue(t, "Directory waiter remains visible", "agent", agentID)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id)
		VALUES ($1, 'agent', $2)
	`, squadID, agentID); err != nil {
		t.Fatalf("insert squad member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1 AND member_id = $2`, squadID, agentID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, dispatched_at)
		VALUES ($1, $2, $3, 'waiting_local_directory', 0, now())
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert local-directory waiter: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	rows, err := testHandler.Queries.ListSquadMemberStatusRows(ctx, parseUUID(squadID))
	if err != nil {
		t.Fatalf("ListSquadMemberStatusRows with waiter: %v", err)
	}
	if len(rows) != 1 || !rows[0].TaskID.Valid || rows[0].TaskStatus.String != "waiting_local_directory" {
		t.Fatalf("waiter-only squad member rows = %+v, want the waiting task retained", rows)
	}

	assertSquadMember := func(wantWorking bool) {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListSquadMemberStatus(w, squadScopeReq(
			"",
			http.MethodGet,
			"/api/squads/"+squadID+"/members/status",
			nil,
			map[string]string{"id": squadID},
		))
		if w.Code != http.StatusOK {
			t.Fatalf("ListSquadMemberStatus: got %d: %s", w.Code, w.Body.String())
		}
		var resp SquadMemberStatusListResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode squad member status: %v", err)
		}
		if len(resp.Members) != 1 {
			t.Fatalf("squad members = %+v, want one member", resp.Members)
		}
		member := resp.Members[0]
		if member.Status == nil {
			t.Fatal("agent member status is nil")
		}
		if gotWorking := *member.Status == "working"; gotWorking != wantWorking {
			t.Fatalf("member status = %q, want working=%t", *member.Status, wantWorking)
		}
		if len(member.ActiveIssues) != 1 || member.ActiveIssues[0].IssueID != issueID {
			t.Fatalf("active issues = %+v, want waiting issue %s", member.ActiveIssues, issueID)
		}
	}
	assertSquadMember(false)

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'dispatched' WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("promote waiter to dispatched: %v", err)
	}
	rows, err = testHandler.Queries.ListSquadMemberStatusRows(ctx, parseUUID(squadID))
	if err != nil {
		t.Fatalf("ListSquadMemberStatusRows with dispatched task: %v", err)
	}
	if len(rows) != 1 || !rows[0].TaskID.Valid {
		t.Fatalf("dispatched squad member rows = %+v, want one working task row", rows)
	}
	assertSquadMember(true)
}
