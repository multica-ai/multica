package lark

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type qaR2CheckQueries struct {
	*ChannelStore
	oldID pgtype.UUID
	fail  bool
	t     *testing.T
}

func (q *qaR2CheckQueries) IsChannelMessageTypingActive(ctx context.Context, p db.IsChannelMessageTypingActiveParams) (bool, error) {
	if p.MessageID == q.oldID {
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) > typingCleanupTimeout {
			q.t.Errorf("lifecycle check lacks independent bounded context")
		}
		if q.fail {
			return true, errors.New("QA database check unavailable")
		}
	}
	return q.ChannelStore.IsChannelMessageTypingActive(ctx, p)
}

type qaR2DeleteBarrier struct {
	*crossProcessReactionAPI
	started, release chan struct{}
	t                *testing.T
}

func (a *qaR2DeleteBarrier) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	if p.MessageID == "checked-trigger" {
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) > typingCleanupTimeout {
			a.t.Errorf("reaction withdrawal lacks independent bounded context")
		}
		close(a.started)
		select {
		case <-a.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return a.crossProcessReactionAPI.DeleteMessageReaction(ctx, p)
}

func TestQAR2WithdrawalPreservesNextDebounceInput(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "committed_terminal"
		if failure {
			name = "database_check_error"
		}
		t.Run(name, func(t *testing.T) {
			f := newTypingDBFixture(t)
			if !failure {
				f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.taskID)
			}
			next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next debounce input", "channel_ingested": true})
			api := &qaR2DeleteBarrier{crossProcessReactionAPI: &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: map[string][]MessageReaction{"checked-trigger": {{ReactionID: "human-typing", OperatorType: "user", EmojiType: typingEmoji}}}}, started: make(chan struct{}), release: make(chan struct{}), t: t}
			queries := &qaR2CheckQueries{ChannelStore: f.store, oldID: f.messageID, fail: failure, t: t}
			mgr := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "fixture"}, queries, newDiscardLogger())
			var once sync.Once
			defer once.Do(func() { close(api.release) })
			done := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // HTTP fake models a successful Add delivered after caller cancellation.
			go func() { defer close(done); mgr.Add(ctx, f.inst, f.sessionID, "checked-trigger", "", f.messageID) }()
			select {
			case <-api.started:
			case <-time.After(5 * time.Second):
				t.Fatal("old reaction withdrawal did not start")
			}
			mgr.Add(context.Background(), f.inst, f.sessionID, "next-trigger", "", uuidFromString(t, next))
			once.Do(func() { close(api.release) })
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("old withdrawal did not finish")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if old := api.remote["checked-trigger"]; len(old) != 1 || old[0].ReactionID != "human-typing" {
				t.Fatalf("old reaction or human reaction incorrect: %+v", old)
			}
			states := mgr.states[uuidString(f.sessionID)]
			if len(api.remote["next-trigger"]) != 1 || len(states) != 1 || states[0].MessageID != "next-trigger" {
				t.Fatalf("next debounce input lost: remote=%+v states=%+v", api.remote, states)
			}
			if api.deletes != 1 {
				t.Fatalf("withdrawals=%d want 1", api.deletes)
			}
			t.Log("old app reaction removed; human Typing and next taskless input reaction/state retained; query and withdrawal independent of cancelled Add context")
		})
	}
}
