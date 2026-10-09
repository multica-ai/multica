package lark

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type qaCoalescedCheckBarrier struct {
	TypingIndicatorQueries
	checked chan bool
	release chan struct{}
}

func (q *qaCoalescedCheckBarrier) IsChannelMessageTypingActive(ctx context.Context, arg db.IsChannelMessageTypingActiveParams) (bool, error) {
	active, err := q.TypingIndicatorQueries.IsChannelMessageTypingActive(ctx, arg)
	if err != nil {
		return active, err
	}
	q.checked <- active
	select {
	case <-q.release:
		return active, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// A real committed DB read is paused after it observes active=true. Process B
// then terminates the task and sweeps its single delivery trigger, which is not
// the other input from the same sealed debounce batch.
// FIXME(test-integration): Preserves QA's exact post-SELECT barrier and its
// delivery-trigger control, including source deletion before terminal delivery.
func TestTypingTrueCheckThenCrossProcessTerminalDB(t *testing.T) {
	for _, removal := range []string{"retained", "deleted", "archived"} {
		t.Run(removal, func(t *testing.T) {
			t.Run("actual_delivery_trigger", func(t *testing.T) { qaRunTrueCheckThenTerminal(t, true, removal) })
			t.Run("coalesced_non_trigger", func(t *testing.T) { qaRunTrueCheckThenTerminal(t, false, removal) })
		})
	}
}

func qaRunTrueCheckThenTerminal(t *testing.T, actualTrigger bool, removal string) {
	f := newTypingDBFixture(t)
	f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "delivery trigger in same batch", "task_id": f.taskID, "channel_ingested": true})
	p, patcherQueries, _ := newTestPatcher(t)
	patcherQueries.installation = f.inst
	patcherQueries.binding.ChatSessionID = f.sessionID
	patcherQueries.binding.InstallationID = f.inst.ID
	patcherQueries.binding.LastMessageID = pgtype.Text{String: "delivery-trigger", Valid: true}
	api := &crossProcessReactionAPI{
		fakeTypingAPIClient: &fakeTypingAPIClient{},
		remote: map[string][]MessageReaction{
			"coalesced-input":  {{ReactionID: "human-typing", OperatorType: "user", EmojiType: typingEmoji}},
			"delivery-trigger": {{ReactionID: "trigger-app", OperatorType: "app", OperatorID: f.inst.AppID, EmojiType: typingEmoji}},
		},
	}
	platformMessage := "coalesced-input"
	wantTriggerRemaining := 0
	if actualTrigger {
		platformMessage = "delivery-trigger"
		wantTriggerRemaining = 1
		api.remote[platformMessage] = []MessageReaction{{ReactionID: "human-typing", OperatorType: "user", EmojiType: typingEmoji}}
	}
	barrier := &qaCoalescedCheckBarrier{TypingIndicatorQueries: f.store, checked: make(chan bool, 1), release: make(chan struct{})}
	managerA := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, barrier, newDiscardLogger())
	managerB := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, f.store, newDiscardLogger())
	p.SetTypingIndicatorManager(managerB)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var release sync.Once
	defer release.Do(func() { close(barrier.release) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		managerA.Add(ctx, f.inst, f.sessionID, platformMessage, "", f.messageID)
	}()
	select {
	case active := <-barrier.checked:
		if !active {
			t.Fatal("precondition: real DB must report old input active before termination")
		}
	case <-ctx.Done():
		t.Fatal("real DB lifecycle check did not complete")
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID)
	event := events.Event{Type: protocol.EventTaskCancelled, TaskID: uuidString(f.taskID), ChatSessionID: uuidString(f.sessionID)}
	if removal == "deleted" {
		f.Exec(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", f.sessionID)
		f.Exec(t, "DELETE FROM chat_message WHERE chat_session_id=$1", f.sessionID)
		f.Exec(t, "DELETE FROM chat_session WHERE id=$1", f.sessionID)
		patcherQueries.deliveryErr = pgx.ErrNoRows
		event.ChannelReactionTarget = &events.ChannelReactionTarget{ChannelType: channelTypeFeishu, InstallationID: uuidString(f.inst.ID), MessageID: "delivery-trigger"}
	} else if removal == "archived" {
		f.Exec(t, "UPDATE chat_session SET status='archived' WHERE id=$1", f.sessionID)
	}
	p.handleEvent(event)
	api.mu.Lock()
	triggerLeft, deletes, lists := len(api.remote["delivery-trigger"]), api.deletes, api.lists
	api.mu.Unlock()
	wantCleanups := 2
	if actualTrigger {
		wantCleanups = 1
	}
	if triggerLeft != wantTriggerRemaining || deletes != wantCleanups || lists != wantCleanups {
		t.Fatalf("precondition: B must remove actual delivery trigger: trigger=%d deletes=%d lists=%d", triggerLeft, deletes, lists)
	}
	release.Do(func() { close(barrier.release) })
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Add did not complete")
	}
	active, err := f.store.IsChannelMessageTypingActive(ctx, db.IsChannelMessageTypingActiveParams{MessageID: f.messageID, ChatSessionID: f.sessionID, WorkspaceID: f.inst.WorkspaceID, InstallationID: f.inst.ID, ChannelType: channelTypeFeishu})
	if err != nil || active {
		t.Fatalf("precondition: committed terminal input must now be inactive: active=%v err=%v", active, err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	remaining := api.remote[platformMessage]
	t.Logf("after active SELECT -> B terminal sweep -> A return: lists=%d deletes=%d trigger=%d coalesced=%+v A_states=%d B_states=%d db_active=%v", api.lists, api.deletes, len(api.remote["delivery-trigger"]), remaining, len(managerA.states[uuidString(f.sessionID)]), len(managerB.states[uuidString(f.sessionID)]), active)
	if len(remaining) != 1 || remaining[0].ReactionID != "human-typing" {
		t.Fatalf("terminal cleanup must remove app reaction and preserve human Typing: want [human-typing], got %+v", remaining)
	}
}
