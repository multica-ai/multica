package lark

import (
	"context"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"testing"
)

// The router's inline failed enqueue invokes OnSettled before Handle launches
// the detached OnIngested. No task exists to supply a later terminal event.
func TestTypingSettledBeforeAddRegistrationDB(t *testing.T) {
	f := newTypingDBFixture(t)
	f.Exec(t, "UPDATE chat_message SET task_id=NULL WHERE id=$1", f.messageID)
	f.Exec(t, "DELETE FROM agent_task_queue WHERE id=$1", f.taskID)
	api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}
	mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	notifier := &feishuTypingNotifier{mgr: mgr}
	next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "channel_ingested": true})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	notifier.OnSettled(cancelled, f.sessionID, engine.TypingSettlement{WorkspaceID: f.inst.WorkspaceID, InstallationID: f.inst.ID, ThroughMessageID: f.messageID, ContextRevision: 1})
	notifier.OnIngested(context.Background(), engine.ResolvedInstallation{ID: f.inst.ID, WorkspaceID: f.inst.WorkspaceID, Platform: f.inst}, channel.InboundMessage{MessageID: "taskless-input"}, f.sessionID, f.messageID)
	notifier.OnIngested(context.Background(), engine.ResolvedInstallation{ID: f.inst.ID, WorkspaceID: f.inst.WorkspaceID, Platform: f.inst}, channel.InboundMessage{MessageID: "next-input"}, f.sessionID, uuidFromString(t, next))
	if len(api.remote["next-input"]) != 1 {
		t.Fatal("settling previous input removed next debounce input")
	}
	var tasks int
	f.QueryRow(t, "SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1", f.sessionID).Scan(&tasks)
	if tasks != 0 {
		t.Fatalf("taskless settlement unexpectedly created %d tasks", tasks)
	}
	if len(api.remote["taskless-input"]) != 0 {
		t.Fatalf("settled input without a task retained reaction: %+v", api.remote)
	}
}
