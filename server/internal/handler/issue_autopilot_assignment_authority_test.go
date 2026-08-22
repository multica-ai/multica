package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createAutopilotChildIssue(t *testing.T, assigneeType, assigneeID, parentIssueID, status, actorAgentID, taskID string) (*httptest.ResponseRecorder, IssueResponse) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "autopilot private-assignee child " + t.Name(),
		"status":          status,
		"priority":        "low",
		"assignee_type":   assigneeType,
		"assignee_id":     assigneeID,
		"parent_issue_id": parentIssueID,
		"allow_duplicate": true,
	})
	if actorAgentID != "" {
		r.Header.Set("X-Agent-ID", actorAgentID)
	}
	if taskID != "" {
		r.Header.Set("X-Task-ID", taskID)
	}
	testHandler.CreateIssue(w, r)

	var created IssueResponse
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode created child issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
		})
	}
	return w, created
}

func TestCreateIssue_AutopilotLeaderAssignsPrivateWorker(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("verified lineage parks backlog child without enqueue", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		parentIssueID := uuidToString(fx.Issue.ID)

		w, created := createAutopilotChildIssue(t, "agent", workerID, parentIssueID, "backlog", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		if created.ParentIssueID == nil || *created.ParentIssueID != parentIssueID {
			t.Fatalf("created child parent_issue_id = %v, want %q", created.ParentIssueID, parentIssueID)
		}
		if created.AssigneeType == nil || *created.AssigneeType != "agent" || created.AssigneeID == nil || *created.AssigneeID != workerID {
			t.Fatalf("created child assignee = (%v, %v), want (agent, %s)", created.AssigneeType, created.AssigneeID, workerID)
		}

		var queued int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
		`, created.ID, workerID).Scan(&queued); err != nil {
			t.Fatalf("count worker tasks: %v", err)
		}
		if queued != 0 {
			t.Fatalf("backlog child must not enqueue the private worker, got %d tasks", queued)
		}
	})

	t.Run("verified lineage creates active child and enqueues once", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")

		w, created := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "todo", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var taskCount int
		var originatorCount int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*), count(originator_user_id) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
		`, created.ID, workerID).Scan(&taskCount, &originatorCount); err != nil {
			t.Fatalf("count worker tasks: %v", err)
		}
		if taskCount != 1 {
			t.Fatalf("active child must enqueue the private worker exactly once, got %d tasks", taskCount)
		}
		if originatorCount != 0 {
			t.Fatal("autopilot creator authority is authorization-only; worker task must remain unattributed")
		}
	})

	t.Run("verified lineage creates squad child and enqueues its private leader once", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		squadID := dbfx.Squad(t, "Autopilot Private Leader Squad", workerID)

		w, created := createAutopilotChildIssue(t, "squad", squadID, uuidToString(fx.Issue.ID), "todo", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		if created.AssigneeType == nil || *created.AssigneeType != "squad" || created.AssigneeID == nil || *created.AssigneeID != squadID {
			t.Fatalf("created child assignee = (%v, %v), want (squad, %s)", created.AssigneeType, created.AssigneeID, squadID)
		}

		var taskCount int
		var originatorCount int
		var squadTaskCount int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*), count(originator_user_id), count(*) FILTER (WHERE squad_id = $3)
			FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
		`, created.ID, workerID, squadID).Scan(&taskCount, &originatorCount, &squadTaskCount); err != nil {
			t.Fatalf("count private squad leader tasks: %v", err)
		}
		if taskCount != 1 || squadTaskCount != 1 {
			t.Fatalf("active squad child must enqueue its private leader exactly once with squad lineage, got %d tasks (%d with squad_id)", taskCount, squadTaskCount)
		}
		if originatorCount != 0 {
			t.Fatal("autopilot creator authority is authorization-only; squad leader task must remain unattributed")
		}
	})

	t.Run("creator without invoke rights is denied", func(t *testing.T) {
		workerID, _, plainMemberID := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, plainMemberID, "autopilot")

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("real originator takes precedence over autopilot creator", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		if _, err := testPool.Exec(context.Background(), `
			UPDATE agent_task_queue
			SET originator_user_id = $1, accountable_user_id = $1, originator_source = 'direct_human'
			WHERE id = $2
		`, plainMemberID, fx.LeaderTaskID); err != nil {
			t.Fatalf("attribute leader task: %v", err)
		}

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing task lineage is denied", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("task actor mismatch is denied", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		workerTaskID := seedTaskOnIssue(t, workerID, uuidToString(fx.Issue.ID), fx.RuntimeID)

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, workerTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("task bound to another issue is denied", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		otherIssueID := seedBareIssue(t, fx.LeaderAgentID)
		otherTaskID := seedTaskOnIssue(t, fx.LeaderAgentID, otherIssueID, fx.RuntimeID)

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, otherTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-autopilot parent is denied", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "")

		w, _ := createAutopilotChildIssue(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, fx.LeaderTaskID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})
}
