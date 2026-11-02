package handler

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http"
	"testing"
)

type fakeCurrentHistory struct {
	calls int
	task  pgtype.UUID
	limit int
}

func (f *fakeCurrentHistory) Current(_ context.Context, id pgtype.UUID, limit int) (channel.HistoryPage, string, error) {
	f.calls++
	f.task = id
	f.limit = limit
	return channel.HistoryPage{ChannelType: "feishu", ThreadID: "omt_test", Messages: []channel.HistoryMessage{{ID: "om_context", Text: "native context"}}}, "Untrusted reference context; partial window.", nil
}
func TestGetChatThreadFeishuCurrentOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	instID := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": task.AgentID, "channel_type": "feishu", "config": `{"app_id":"native-history-test"}`, "status": "active", "installer_user_id": testUserID})
	dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{"chat_session_id": task.ChatSessionID, "installation_id": instID, "channel_type": "feishu", "channel_chat_id": "oc_test:omt_test", "chat_type": "group", "config": `{"chat_id":"oc_test"}`})
	fake := &fakeCurrentHistory{}
	previous := testHandler.LarkHistory
	testHandler.LarkHistory = fake
	t.Cleanup(func() { testHandler.LarkHistory = previous })
	var out ChatChannelHistoryResponse
	testutil.Call(t, http.HandlerFunc(testHandler.GetChatThread), taskActorReq("/api/chat/thread?limit=7", taskID)).Want(http.StatusOK).JSON(&out)
	if fake.calls != 1 || fake.task != task.ID || fake.limit != 7 || out.Note == "" || len(out.Messages) != 1 || out.ThreadID != "omt_test" {
		t.Fatalf("response=%+v reader=%+v", out, fake)
	}
	for _, query := range []string{"?id=omt_sibling", "?before=forged", "?id=omt_test"} {
		testutil.Call(t, http.HandlerFunc(testHandler.GetChatThread), taskActorReq("/api/chat/thread"+query, taskID)).Want(http.StatusBadRequest)
	}
	forged := newRequest(http.MethodGet, "/api/chat/thread", nil)
	forged.Header.Set("X-Task-ID", taskID)
	testutil.Call(t, http.HandlerFunc(testHandler.GetChatThread), forged).Want(http.StatusForbidden)
	if fake.calls != 1 {
		t.Fatalf("out-of-scope request reached native reader: %d", fake.calls)
	}
	withSlackHistory(t, nil)
	out = ChatChannelHistoryResponse{}
	testutil.Call(t, http.HandlerFunc(testHandler.GetChatChannelHistory), taskActorReq("/api/chat/history", taskID)).Want(http.StatusOK).JSON(&out)
	if fake.calls != 1 || out.Note != "" {
		t.Fatalf("stored transcript changed: calls=%d response=%+v", fake.calls, out)
	}
}
