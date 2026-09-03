package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type channelIssueRouteFixture struct {
	issueID       string
	initialTaskID pgtype.UUID
	sessionID     string
	installID     string
}

func createChannelIssueRouteFixture(t *testing.T, agentID, title string) channelIssueRouteFixture {
	t.Helper()
	appID := "web-update-" + util.UUIDToString(dbid.NewV7())
	installID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "feishu",
		"config":            testutil.Raw(fmt.Sprintf("jsonb_build_object('app_id', '%s'::text)", appID)),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	sessionID := dbfx.Insert(t, "chat_session", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"agent_id":     agentID,
		"creator_id":   testUserID,
		"title":        "",
	})
	dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": sessionID,
		"installation_id": installID,
		"channel_type":    "feishu",
		"channel_chat_id": "oc_test_" + sessionID[:8],
		"chat_type":       "group",
		"config":          testutil.Raw("'{}'::jsonb"),
		"route_revision":  1,
	})
	dbfx.InsertNoID(t, "channel_chat_context_generation", testutil.Cols{
		"chat_session_id": sessionID,
		"revision":        1,
	}, "chat_session_id = $1 AND revision = $2", sessionID, 1)

	result, err := testHandler.IssueService.Create(context.Background(), service.IssueCreateParams{
		WorkspaceID:  util.MustParseUUID(testWorkspaceID),
		Title:        title,
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(testUserID),
		OriginType:   pgtype.Text{String: "lark_chat", Valid: true},
		OriginID:     util.MustParseUUID(sessionID),
	}, service.IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create channel issue: %v", err)
	}
	if !result.AssignedTaskID.Valid {
		t.Fatalf("channel issue should have an initial assigned task")
	}
	if _, err := testHandler.Queries.GetChannelTaskDelivery(context.Background(), result.AssignedTaskID); err != nil {
		t.Fatalf("initial task delivery missing: %v", err)
	}

	fixture := channelIssueRouteFixture{
		issueID:       util.UUIDToString(result.Issue.ID),
		initialTaskID: result.AssignedTaskID,
		sessionID:     sessionID,
		installID:     installID,
	}
	dbfx.Cleanup(t, `DELETE FROM channel_task_delivery WHERE task_id IN (SELECT id FROM agent_task_queue WHERE issue_id = $1)`, fixture.issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fixture.issueID)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, fixture.issueID)
	return fixture
}

func completeInitialChannelIssueTask(t *testing.T, fixture channelIssueRouteFixture) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, fixture.initialTaskID)
	var deliveryCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM channel_task_delivery WHERE task_id = $1`, fixture.initialTaskID).Scan(&deliveryCount)
	if deliveryCount != 1 {
		t.Fatalf("initial channel delivery count = %d, want 1", deliveryCount)
	}
}

func assertNoNewChannelDelivery(t *testing.T, fixture channelIssueRouteFixture, agentID string) {
	t.Helper()
	var taskID string
	dbfx.QueryRow(t, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running')
		ORDER BY created_at DESC LIMIT 1`, fixture.issueID, agentID).Scan(&taskID)
	var deliveryCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM channel_task_delivery WHERE task_id = $1`, taskID).Scan(&deliveryCount)
	if deliveryCount != 0 {
		t.Fatalf("follow-up task %s has %d channel deliveries, want 0", taskID, deliveryCount)
	}
	var initialDeliveryCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM channel_task_delivery WHERE task_id = $1`, fixture.initialTaskID).Scan(&initialDeliveryCount)
	if initialDeliveryCount != 1 {
		t.Fatalf("initial channel delivery after web update = %d, want 1", initialDeliveryCount)
	}
}

func TestChannelIssueDelivery_WebUpdatePathsDoNotSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	initialAgentID := seededReadyAgentID(t)

	t.Run("update", func(t *testing.T) {
		newAgentID := createHandlerTestAgent(t, "ChannelWebUpdateAgent", []byte("[]"))
		fixture := createChannelIssueRouteFixture(t, initialAgentID, "channel web update")
		completeInitialChannelIssueTask(t, fixture)

		req := withURLParam(newRequest("PUT", "/api/issues/"+fixture.issueID, map[string]any{
			"assignee_type": "agent",
			"assignee_id":   newAgentID,
		}), "id", fixture.issueID)
		testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
		assertNoNewChannelDelivery(t, fixture, newAgentID)
	})

	t.Run("batch_update", func(t *testing.T) {
		newAgentID := createHandlerTestAgent(t, "ChannelBatchUpdateAgent", []byte("[]"))
		fixture := createChannelIssueRouteFixture(t, initialAgentID, "channel batch update")
		completeInitialChannelIssueTask(t, fixture)

		req := newRequest("POST", "/api/issues/batch-update", map[string]any{
			"issue_ids": []string{fixture.issueID},
			"updates": map[string]any{
				"assignee_type": "agent",
				"assignee_id":   newAgentID,
			},
		})
		testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
		assertNoNewChannelDelivery(t, fixture, newAgentID)
	})

	t.Run("comment", func(t *testing.T) {
		var hasRecoverySettledAt bool
		dbfx.QueryRow(t, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public'
				  AND table_name = 'delegated_failure_recovery'
				  AND column_name = 'recovery_settled_at')`).Scan(&hasRecoverySettledAt)
		if !hasRecoverySettledAt {
			t.Skip("comment trigger reconciliation migration is not applied")
		}
		fixture := createChannelIssueRouteFixture(t, initialAgentID, "channel comment trigger")
		completeInitialChannelIssueTask(t, fixture)

		req := withURLParam(newRequest("POST", "/api/issues/"+fixture.issueID+"/comments", map[string]any{
			"content": "[@Agent](mention://agent/" + initialAgentID + ") follow-up",
		}), "id", fixture.issueID)
		testutil.Call(t, testHandler.CreateComment, req).WantOneOf(http.StatusCreated, http.StatusOK)
		assertNoNewChannelDelivery(t, fixture, initialAgentID)
	})
}
