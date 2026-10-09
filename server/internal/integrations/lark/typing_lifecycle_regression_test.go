package lark

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type blockedTypingAdd struct {
	*fakeTypingAPIClient
	started chan struct{}
	release chan struct{}
}

func (a *blockedTypingAdd) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	if p.MessageID != "trigger" {
		return a.fakeTypingAPIClient.AddMessageReaction(ctx, p)
	}
	close(a.started)
	<-a.release
	return "late-reaction", nil
}

func (a *blockedTypingAdd) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.fakeTypingAPIClient.DeleteMessageReaction(ctx, p)
}

func TestPatcherTerminalEventDuringTypingAdd(t *testing.T) {
	for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		t.Run(eventType, func(t *testing.T) {
			p, q, _ := newTestPatcher(t)
			q.taskChannelIngested = true
			q.binding.LastMessageID = pgtype.Text{String: "trigger", Valid: true}
			taskID := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
			q.task = db.AgentTaskQueue{ChatInputTaskID: taskID}
			api := &blockedTypingAdd{fakeTypingAPIClient: &fakeTypingAPIClient{addReturn: "next-reaction"}, started: make(chan struct{}), release: make(chan struct{})}
			mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{installation: q.installation}, newDiscardLogger())
			p.SetTypingIndicatorManager(mgr)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var release sync.Once
			defer release.Do(func() { close(api.release) })
			done := make(chan struct{})
			go func() {
				defer close(done)
				mgr.Add(ctx, q.installation, q.binding.ChatSessionID, "trigger", "", q.binding.ChatSessionID)
			}()
			select {
			case <-api.started:
			case <-ctx.Done():
				t.Fatal("Add did not start")
			}
			p.handleEvent(events.Event{Type: eventType, TaskID: uuidString(taskID), ChatSessionID: uuidString(q.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: "answer"}})
			if len(api.listCalled) != 1 || len(api.deleteCalled) != 0 {
				t.Fatal("terminal event must finish its empty sweep while Add is blocked")
			}
			// A later turn must survive cleanup of the older in-flight generation.
			mgr.Add(ctx, q.installation, q.binding.ChatSessionID, "next-trigger", "", q.binding.ChatSessionID)
			cancel() // Late cleanup must also survive cancellation of the add context.
			release.Do(func() { close(api.release) })
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Add did not finish")
			}
			if len(api.deleteCalled) != 1 || api.deleteCalled[0].reactionID != "late-reaction" {
				t.Fatalf("late Add left its badge: %+v", api.deleteCalled)
			}
			states := mgr.states[uuidString(q.binding.ChatSessionID)]
			if len(states) != 1 || states[0].MessageID != "next-trigger" {
				t.Fatalf("late cleanup damaged next turn: %+v", states)
			}
		})
	}
}

type deadlineTypingLister struct {
	*fakeTypingAPIClient
	replies  *fakeAPIClient
	t        *testing.T
	timedOut bool
}

func (a *deadlineTypingLister) ListMessageReactions(ctx context.Context, _ ListMessageReactionsParams) ([]MessageReaction, error) {
	a.replies.mu.Lock()
	sent := len(a.replies.textSent)
	a.replies.mu.Unlock()
	if sent != 1 {
		a.t.Errorf("slow sweep started before reply delivery: replies=%d", sent)
	}
	<-ctx.Done()
	a.timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	return nil, ctx.Err()
}

func TestPatcherSlowSweepCannotConsumeReplyBudget(t *testing.T) {
	p, q, replies := newTestPatcher(t)
	taskID := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
	q.task = db.AgentTaskQueue{ChatInputTaskID: taskID}
	q.taskChannelIngested = true
	q.binding.LastMessageID = pgtype.Text{String: "trigger", Valid: true}
	api := &deadlineTypingLister{fakeTypingAPIClient: &fakeTypingAPIClient{}, replies: replies, t: t}
	p.SetTypingIndicatorManager(NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{installation: q.installation}, newDiscardLogger()))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.processEvent(ctx, events.Event{Type: protocol.EventChatDone, TaskID: uuidString(taskID), ChatSessionID: uuidString(q.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: "answer"}}); err != nil {
		t.Fatal(err)
	}
	if !api.timedOut {
		t.Fatal("list call did not reach its independent deadline")
	}
	if ctx.Err() != nil {
		t.Fatalf("sweep exhausted reply context: %v", ctx.Err())
	}
	if len(replies.textSent) != 1 {
		t.Fatal("reply lost")
	}
}

func TestPatcherClearsTypingBeforeTerminalGates(t *testing.T) {
	for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskFailed} {
		for _, gate := range []string{"inactive installation", "missing delivery"} {
			t.Run(eventType+"/"+gate, func(t *testing.T) {
				p, q, replies := newTestPatcher(t)
				taskID := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
				q.task = db.AgentTaskQueue{ChatInputTaskID: taskID}
				q.taskChannelIngested = true
				api := &fakeTypingAPIClient{addReturn: "held-reaction"}
				mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{installation: q.installation}, newDiscardLogger())
				p.SetTypingIndicatorManager(mgr)
				mgr.Add(context.Background(), q.installation, q.binding.ChatSessionID, "trigger", "", q.binding.ChatSessionID)
				if gate == "inactive installation" {
					q.installation.Status = "revoked"
				} else {
					q.deliveryErr = pgx.ErrNoRows
				}
				p.handleEvent(events.Event{Type: eventType, TaskID: uuidString(taskID), ChatSessionID: uuidString(q.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: "answer"}})
				if len(api.deleteCalled) != 1 || api.deleteCalled[0].reactionID != "held-reaction" {
					t.Fatalf("badge survived terminal gate: %+v", api.deleteCalled)
				}
				if len(mgr.states) != 0 {
					t.Fatal("terminal state retained")
				}
				if len(replies.textSent)+len(replies.sent)+len(replies.mdCardSent) != 0 {
					t.Fatal("terminal gate unexpectedly sent a reply")
				}
			})
		}
	}
}

func TestPatcherSessionDeleteSweepsCapturedTargetWithoutLocalState(t *testing.T) {
	p, q, replies := newTestPatcher(t)
	q.deliveryErr = pgx.ErrNoRows // Both binding and delivery disappeared at commit.
	api := &fakeTypingAPIClient{listReturn: []MessageReaction{{ReactionID: "remote-reaction", OperatorType: "app", OperatorID: "cli_test_app", EmojiType: typingEmoji}}}
	p.SetTypingIndicatorManager(NewTypingIndicatorManager(api, fakeTypingCreds{secret: "shh"}, &fakeTypingQueries{}, newDiscardLogger()))
	p.handleEvent(events.Event{Type: protocol.EventTaskCancelled, TaskID: "ee777777-ee77-ee77-ee77-eeeeeeeeeeee", ChatSessionID: uuidString(q.binding.ChatSessionID), ChannelReactionTarget: &events.ChannelReactionTarget{ChannelType: channelTypeFeishu, InstallationID: uuidString(q.installation.ID), MessageID: "deleted-session-trigger"}})
	if len(api.deleteCalled) != 1 || api.deleteCalled[0].messageID != "deleted-session-trigger" {
		t.Fatalf("cross-process session delete stranded reaction: %+v", api.deleteCalled)
	}
	if len(replies.textSent)+len(replies.sent) != 0 {
		t.Fatal("cancel sent a reply")
	}
}
