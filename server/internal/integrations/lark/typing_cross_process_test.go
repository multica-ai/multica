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
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Two independent managers share the remote API and durable DB, never local state.
type crossProcessReactionAPI struct {
	*fakeTypingAPIClient
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	remote  map[string][]MessageReaction
	lists   int
	deletes int
}

func (a *crossProcessReactionAPI) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	if p.MessageID == "late-trigger" {
		close(a.started)
		select {
		case <-a.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	reactionID := "reaction-" + p.MessageID
	a.mu.Lock()
	a.remote[p.MessageID] = append(a.remote[p.MessageID], MessageReaction{ReactionID: reactionID, OperatorType: "app", OperatorID: p.InstallationID.AppID, EmojiType: typingEmoji})
	a.mu.Unlock()
	return reactionID, nil
}

func (a *crossProcessReactionAPI) ListMessageReactions(_ context.Context, p ListMessageReactionsParams) ([]MessageReaction, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lists++
	return append([]MessageReaction(nil), a.remote[p.MessageID]...), nil
}

func (a *crossProcessReactionAPI) DeleteMessageReaction(_ context.Context, p DeleteReactionParams) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deletes++
	for i, reaction := range a.remote[p.MessageID] {
		if reaction.ReactionID == p.ReactionID {
			a.remote[p.MessageID] = append(a.remote[p.MessageID][:i], a.remote[p.MessageID][i+1:]...)
			break
		}
	}
	return nil
}

// FIXME(test-integration): Both QA orderings use a real shared DB and deterministic HTTP barriers.
func TestTypingCrossProcessLateAddAfterTerminalSweepDB(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "delivery_retained"
		if deleted {
			name = "session_deleted_captured_target"
		}
		t.Run(name, func(t *testing.T) {
			f := newTypingDBFixture(t)
			p, q, _ := newTestPatcher(t)
			q.installation = f.inst
			q.binding.ChatSessionID = f.sessionID
			q.binding.InstallationID = f.inst.ID
			q.binding.LastMessageID = pgtype.Text{String: "late-trigger", Valid: true}
			if deleted {
				q.deliveryErr = pgx.ErrNoRows
			}
			api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, started: make(chan struct{}), release: make(chan struct{}), remote: map[string][]MessageReaction{"late-trigger": {{ReactionID: "human-typing", OperatorType: "user", EmojiType: typingEmoji}}}}
			managerA := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, f.store, newDiscardLogger())
			managerB := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, f.store, newDiscardLogger())
			p.SetTypingIndicatorManager(managerB)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var release sync.Once
			defer release.Do(func() { close(api.release) })
			done := make(chan struct{})
			go func() {
				defer close(done)
				managerA.Add(ctx, q.installation, q.binding.ChatSessionID, "late-trigger", "", f.messageID)
			}()
			select {
			case <-api.started:
			case <-ctx.Done():
				t.Fatal("Add did not start")
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID)
			if deleted {
				f.Exec(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", f.sessionID)
				f.Exec(t, "DELETE FROM chat_message WHERE chat_session_id=$1", f.sessionID)
				f.Exec(t, "DELETE FROM chat_session WHERE id=$1", f.sessionID)
			}
			e := events.Event{Type: protocol.EventTaskCancelled, TaskID: uuidString(f.taskID), ChatSessionID: uuidString(q.binding.ChatSessionID)}
			if deleted {
				e.ChannelReactionTarget = &events.ChannelReactionTarget{ChannelType: channelTypeFeishu, InstallationID: uuidString(q.installation.ID), MessageID: "late-trigger"}
			}
			p.handleEvent(e)
			api.mu.Lock()
			lists, deletes := api.lists, api.deletes
			t.Logf("terminal completed before Add: lists=%d deletes=%d remote=%d", lists, deletes, len(api.remote["late-trigger"]))
			api.mu.Unlock()
			if lists != 1 || deletes != 0 {
				t.Fatalf("B must finish its app-empty sweep while A is blocked: lists=%d deletes=%d", lists, deletes)
			}
			if !deleted {
				nextTask := f.Task(t, uuidString(f.inst.AgentID), dbfx.Cols{"chat_session_id": f.sessionID, "status": "running", "runtime_id": f.runtimeID})
				nextMessage := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "task_id": nextTask, "channel_ingested": true})
				managerA.Add(ctx, f.inst, f.sessionID, "next-trigger", "", uuidFromString(t, nextMessage))
			}
			release.Do(func() { close(api.release) })
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("Add did not finish")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			states := managerA.states[uuidString(f.sessionID)]
			t.Logf("after late Add: lists=%d deletes=%d old_message_reactions=%+v process_A_states=%d process_B_states=%d", api.lists, api.deletes, api.remote["late-trigger"], len(states), len(managerB.states[uuidString(f.sessionID)]))
			if reactions := api.remote["late-trigger"]; len(reactions) != 1 || reactions[0].ReactionID != "human-typing" || api.deletes != 1 {
				t.Fatalf("late Add must delete exactly its own reaction and preserve human Typing: %+v deletes=%d", reactions, api.deletes)
			}
			if deleted {
				if len(states) != 0 {
					t.Fatalf("deleted session retains local state: %+v", states)
				}
			} else if len(states) != 1 || states[0].MessageID != "next-trigger" || len(api.remote["next-trigger"]) != 1 {
				t.Fatalf("late cleanup damaged next turn: states=%+v remote=%+v", states, api.remote)
			}
		})
	}
}
