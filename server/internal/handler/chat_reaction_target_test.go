package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatSessionRemovalCarriesReactionTargetsAfterCommit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, action := range []string{"delete", "archive"} {
		t.Run(action, func(t *testing.T) {
			f := newArchiveCancelFixture(t, "reaction targets "+action)
			installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
				"workspace_id": testWorkspaceID, "agent_id": f.agentID,
				"channel_type": "feishu", "installer_user_id": testUserID,
			})
			bindingID := dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{
				"chat_session_id": f.sessionID, "installation_id": installationID,
				"channel_type": "feishu", "channel_chat_id": "reaction-room", "chat_type": "group",
			})
			expected := map[string]string{f.queuedTaskID: "queued-trigger", f.runningTaskID: "running-trigger"}
			for taskID, messageID := range expected {
				dbfx.InsertNoID(t, "channel_task_delivery", testutil.Cols{
					"task_id": taskID, "binding_id": bindingID, "installation_id": installationID,
					"channel_type": "feishu", "channel_chat_id": "reaction-room", "chat_type": "group",
					"channel_message_id": messageID, "route_revision": 1,
				}, "task_id = $1", taskID)
			}
			seen := make(chan events.Event, 2)
			testHandler.Bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
				if e.ChatSessionID != f.sessionID {
					return
				}
				// Observe through the pool, not the transaction: the event must be post-commit.
				_, err := testHandler.Queries.GetChannelTaskDelivery(context.Background(), parseUUID(e.TaskID))
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Errorf("cancel published before delivery deletion committed: %v", err)
				}
				seen <- e
			})
			if action == "archive" {
				f.archive(t)
			} else {
				req := httptest.NewRequest(http.MethodDelete, "/api/chat/sessions/"+f.sessionID, nil)
				req.Header.Set("X-User-ID", testUserID)
				req = withChatTestWorkspaceCtx(t, withURLParam(req, "sessionId", f.sessionID))
				w := httptest.NewRecorder()
				testHandler.DeleteChatSession(w, req)
				if w.Code != http.StatusNoContent {
					t.Fatalf("delete: %d %s", w.Code, w.Body.String())
				}
			}
			if len(seen) != len(expected) {
				t.Fatalf("got %d cancellation events, want %d", len(seen), len(expected))
			}
			for range expected {
				e := <-seen
				target := e.ChannelReactionTarget
				if target == nil || target.ChannelType != "feishu" || target.InstallationID != installationID || target.MessageID != expected[e.TaskID] {
					t.Fatalf("task %s lost its frozen reaction target: %+v", e.TaskID, target)
				}
				payload, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(payload), target.MessageID) || strings.Contains(string(payload), installationID) {
					t.Fatalf("internal cleanup metadata leaked into serialized event: %s", payload)
				}
			}
		})
	}
}
