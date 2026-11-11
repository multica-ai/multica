package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatThreadSourceResponse(t *testing.T) {
	agentID, sessionID, runtimeID := newChatSourceSession(t)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "completed"})
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", ChannelID: "C1", ChannelName: "engineering", Permalink: "https://example.slack.com/archives/C1/p200000000", ThreadID: "200.000000", Messages: []channel.HistoryMessage{{ID: "201.000000", Text: "retained"}}, NextCursor: "201.000000"}}
	withSlackHistory(t, fake)
	var got ChatChannelHistoryResponse
	testutil.Call(t, testHandler.GetChatThread, taskActorReq("/api/chat/thread?id=200.000000&channel_id=C2&limit=1", taskID)).Want(http.StatusOK).JSON(&got)
	if got.ChannelID != "C1" || got.ChannelName != "engineering" || got.Permalink != fake.page.Permalink || got.ThreadID != "200.000000" || got.NextCursor != "201.000000" || len(got.Messages) != 1 || fake.gotThreadID != "200.000000" {
		t.Fatalf("source mapping: %+v", got)
	}
}

func newChatSourceSession(t *testing.T) (agentID, sessionID, runtimeID string) {
	t.Helper()
	runtimeID = dbfx.Runtime(t, "Chat source runtime")
	agentID = dbfx.Agent(t, "Chat source agent", runtimeID)
	sessionID = dbfx.ChatSession(t, agentID, testutil.Cols{"explicitly_created_at": testutil.Raw("now()")})
	return agentID, sessionID, runtimeID
}

func seedChatSourceBinding(t *testing.T, agentID, sessionID string) (bindingID, installationID string) {
	t.Helper()
	installationID = dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agentID,
		"channel_type": "slack", "installer_user_id": testUserID,
	})
	bindingID = dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": sessionID, "installation_id": installationID,
		"channel_type": "slack", "channel_chat_id": "C1", "chat_type": "group",
		"last_message_id": "101.000000", "last_thread_id": "100.000000",
	})
	dbfx.InsertNoID(t, "channel_chat_context_generation", testutil.Cols{
		"chat_session_id": sessionID, "revision": 1,
	}, "chat_session_id=$1 AND revision=$2", sessionID, 1)
	return bindingID, installationID
}

func seedChatSourceDelivery(t *testing.T, taskID, bindingID, installationID, platform string) {
	t.Helper()
	dbfx.InsertNoID(t, "channel_task_delivery", testutil.Cols{
		"task_id": taskID, "binding_id": bindingID, "installation_id": installationID,
		"channel_type": platform, "channel_chat_id": "C1", "chat_type": "group",
		"channel_message_id": "101.000000", "channel_thread_id": "100.000000", "route_revision": 1,
	}, "task_id=$1", taskID)
}

type claimSourceReader struct {
	fakeChatHistoryReader
	deliveries []db.ChannelTaskDelivery
	workspace  pgtype.UUID
}

func (r *claimSourceReader) TaskSource(_ context.Context, d db.ChannelTaskDelivery, ws pgtype.UUID) *protocol.ChatSource {
	r.deliveries = append(r.deliveries, d)
	r.workspace = ws
	return &protocol.ChatSource{ChannelID: "C1", ThreadID: d.ChannelThreadID.String}
}

func claimTaskSource(t *testing.T, runtimeID string) AgentTaskResponse {
	t.Helper()
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	var out struct {
		Task *AgentTaskResponse `json:"task"`
	}
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&out)
	if out.Task == nil {
		t.Fatalf("expected a claimed task, got nil: %s", w.Body.String())
	}
	return *out.Task
}

func TestClaimChatSourceDeliverySnapshotAndRetry(t *testing.T) {
	ctx := context.Background()
	agentID, sessionID, runtimeID := newChatSourceSession(t)
	bindingID, installationID := seedChatSourceBinding(t, agentID, sessionID)
	firstID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID})
	seedChatSourceDelivery(t, firstID, bindingID, installationID, "slack")
	reader := &claimSourceReader{}
	withSlackHistory(t, reader)
	// A later inbound message mutates the binding before the older task claims.
	dbfx.Exec(t, `UPDATE channel_chat_session_binding SET last_message_id='901.000000',last_thread_id='900.000000' WHERE chat_session_id=$1`, sessionID)
	claimed := claimTaskSource(t, runtimeID)
	if claimed.ID != firstID || claimed.ChatSource == nil || claimed.ChatSource.ThreadID != "100.000000" || len(reader.deliveries) != 1 || reader.deliveries[0].ChannelMessageID.String != "101.000000" || reader.workspace != parseUUID(testWorkspaceID) {
		t.Fatalf("claim used mutable binding: %+v %+v", claimed.ChatSource, reader.deliveries)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, firstID)
	retryID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID})
	dbfx.Cleanup(t, `DELETE FROM channel_task_delivery WHERE task_id=$1`, retryID)
	if err := testHandler.Queries.CopyChannelTaskDelivery(ctx, db.CopyChannelTaskDeliveryParams{ParentTaskID: parseUUID(firstID), ChildTaskID: parseUUID(retryID)}); err != nil {
		t.Fatal(err)
	}
	claimed = claimTaskSource(t, runtimeID)
	if claimed.ID != retryID || claimed.ChatSource == nil || claimed.ChatSource.ThreadID != "100.000000" || len(reader.deliveries) != 2 || reader.deliveries[1].ChannelMessageID.String != "101.000000" || reader.deliveries[1].InstallationID != reader.deliveries[0].InstallationID {
		t.Fatalf("retry changed source: %+v %+v", claimed.ChatSource, reader.deliveries)
	}
}

func TestClaimChatSourceAbsentWithoutSlackDelivery(t *testing.T) {
	for _, platform := range []string{"", "feishu"} {
		t.Run("platform="+platform, func(t *testing.T) {
			agentID, sessionID, runtimeID := newChatSourceSession(t)
			bindingID, installationID := seedChatSourceBinding(t, agentID, sessionID)
			taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID})
			if platform != "" {
				seedChatSourceDelivery(t, taskID, bindingID, installationID, platform)
			}
			reader := &claimSourceReader{}
			withSlackHistory(t, reader)
			claimed := claimTaskSource(t, runtimeID)
			if claimed.ID != taskID || claimed.ChatSource != nil || len(reader.deliveries) != 0 {
				t.Fatalf("source invented: %+v", claimed.ChatSource)
			}
		})
	}
}
