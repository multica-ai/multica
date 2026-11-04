package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatThreadSourceResponse(t *testing.T) {
	taskID := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", ChannelID: "C1", ChannelName: "engineering", Permalink: "https://example.slack.com/archives/C1/p200000000", ThreadID: "200.000000", Messages: []channel.HistoryMessage{{ID: "201.000000", Text: "retained"}}, NextCursor: "201.000000"}}
	withSlackHistory(t, fake)
	w := httptest.NewRecorder()
	testHandler.GetChatThread(w, taskActorReq("/api/chat/thread?id=200.000000&channel_id=C2&limit=1", taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("thread: %d %s", w.Code, w.Body.String())
	}
	var got ChatChannelHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChannelID != "C1" || got.ChannelName != "engineering" || got.Permalink != fake.page.Permalink || got.ThreadID != "200.000000" || got.NextCursor != "201.000000" || len(got.Messages) != 1 || fake.gotThreadID != "200.000000" {
		t.Fatalf("source mapping: %+v", got)
	}
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
	w := httptest.NewRecorder()
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Task == nil {
		t.Fatalf("claim: %v %s", err, w.Body.String())
	}
	return *out.Task
}

func TestClaimChatSourceDeliverySnapshotAndRetry(t *testing.T) {
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "source snapshot")
	seedChannelBinding(t, ctx, agentID, sessionID, "slack", "101.000000", "100.000000")
	firstID := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	seedChannelTaskDelivery(t, ctx, firstID, sessionID)
	reader := &claimSourceReader{}
	withSlackHistory(t, reader)
	// A later inbound message mutates the binding before the older task claims.
	if _, err := testPool.Exec(ctx, `UPDATE channel_chat_session_binding SET last_message_id='901.000000',last_thread_id='900.000000' WHERE chat_session_id=$1`, sessionID); err != nil {
		t.Fatal(err)
	}
	requeueTaskForClaim(t, ctx, sessionID)
	claimed := claimTaskSource(t, runtimeID)
	if claimed.ChatSource == nil || claimed.ChatSource.ThreadID != "100.000000" || len(reader.deliveries) != 1 || reader.deliveries[0].ChannelMessageID.String != "101.000000" || reader.workspace != parseUUID(testWorkspaceID) {
		t.Fatalf("claim used mutable binding: %+v %+v", claimed.ChatSource, reader.deliveries)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
	retryID := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	if err := testHandler.Queries.CopyChannelTaskDelivery(ctx, db.CopyChannelTaskDeliveryParams{ParentTaskID: parseUUID(firstID), ChildTaskID: parseUUID(retryID)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE task_id=$1`, retryID)
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='queued',started_at=NULL,dispatched_at=NULL WHERE id=$1`, retryID); err != nil {
		t.Fatal(err)
	}
	claimed = claimTaskSource(t, runtimeID)
	if claimed.ChatSource == nil || claimed.ChatSource.ThreadID != "100.000000" || len(reader.deliveries) != 2 || reader.deliveries[1].ChannelMessageID.String != "101.000000" || reader.deliveries[1].InstallationID != reader.deliveries[0].InstallationID {
		t.Fatalf("retry changed source: %+v %+v", claimed.ChatSource, reader.deliveries)
	}
}

func TestClaimChatSourceAbsentWithoutSlackDelivery(t *testing.T) {
	for _, platform := range []string{"", "feishu"} {
		t.Run("platform="+platform, func(t *testing.T) {
			ctx := context.Background()
			agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "source absent")
			seedChannelBinding(t, ctx, agentID, sessionID, "slack", "101.000000", "100.000000")
			taskID := insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
			if platform != "" {
				seedChannelTaskDelivery(t, ctx, taskID, sessionID)
				if _, err := testPool.Exec(ctx, `UPDATE channel_task_delivery SET channel_type=$2 WHERE task_id=$1`, taskID, platform); err != nil {
					t.Fatal(err)
				}
			}
			reader := &claimSourceReader{}
			withSlackHistory(t, reader)
			requeueTaskForClaim(t, ctx, sessionID)
			claimed := claimTaskSource(t, runtimeID)
			if claimed.ChatSource != nil || len(reader.deliveries) != 0 {
				t.Fatalf("source invented: %+v", claimed.ChatSource)
			}
		})
	}
}
