package lark

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type retryTypingAPI struct {
	*crossProcessReactionAPI
	failureMu sync.Mutex
	fail      bool
}

func (a *retryTypingAPI) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	a.failureMu.Lock()
	fail := a.fail
	a.failureMu.Unlock()
	if fail {
		return errors.New("remote unavailable")
	}
	return a.crossProcessReactionAPI.DeleteMessageReaction(ctx, p)
}

// FIXME(test-integration): Failed withdrawal must retain a restart-safe anchor,
// even after all session/installation source rows disappear.
func TestTypingWithdrawalRetryAfterSourceDeletionDB(t *testing.T) {
	f := newTypingDBFixture(t)
	f.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID)
	api := &retryTypingAPI{crossProcessReactionAPI: &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: map[string][]MessageReaction{"retry-input": {{ReactionID: "human", OperatorType: "user", EmojiType: typingEmoji}}}}, fail: true}
	a := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	a.Add(context.Background(), f.inst, f.sessionID, "retry-input", "", f.messageID)
	var pending int
	f.QueryRow(t, "SELECT count(*) FROM channel_typing_reaction WHERE workspace_id=$1 AND cleanup_required AND cleaned_at IS NULL AND reaction_id<>''", f.inst.WorkspaceID).Scan(&pending)
	if pending != 1 || len(api.remote["retry-input"]) != 2 {
		t.Fatalf("failed withdrawal lost compensation: pending=%d remote=%+v", pending, api.remote)
	}
	f.Exec(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", f.sessionID)
	f.Exec(t, "DELETE FROM chat_message WHERE chat_session_id=$1", f.sessionID)
	f.Exec(t, "DELETE FROM chat_session WHERE id=$1", f.sessionID)
	f.Exec(t, "DELETE FROM channel_installation WHERE id=$1", f.inst.ID)
	b := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	// First worker failure leases the record instead of spinning or dropping it.
	b.Reconcile(context.Background(), f.sessionID)
	f.QueryRow(t, "SELECT count(*) FROM channel_typing_reaction WHERE workspace_id=$1 AND cleaned_at IS NULL AND attempts=1 AND retry_after>now()", f.inst.WorkspaceID).Scan(&pending)
	if pending != 1 {
		t.Fatal("worker failure was not durably rescheduled")
	}
	api.failureMu.Lock()
	api.fail = false
	api.failureMu.Unlock()
	f.Exec(t, "UPDATE channel_typing_reaction SET retry_after=now() WHERE workspace_id=$1", f.inst.WorkspaceID)
	b.Reconcile(context.Background(), f.sessionID)
	f.QueryRow(t, "SELECT count(*) FROM channel_typing_reaction WHERE workspace_id=$1 AND cleaned_at IS NOT NULL", f.inst.WorkspaceID).Scan(&pending)
	if pending != 1 || len(api.remote["retry-input"]) != 1 || api.remote["retry-input"][0].ReactionID != "human" {
		t.Fatalf("restart compensation failed: ack=%d remote=%+v", pending, api.remote)
	}
}

// FIXME(test-integration): Eligibility must be post-commit and input scoped.
func TestTypingCleanupPreservesRollbackAndNextInputDB(t *testing.T) {
	f := newTypingDBFixture(t)
	next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "channel_ingested": true})
	api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}
	a := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	a.Add(context.Background(), f.inst, f.sessionID, "old", "", f.messageID)
	a.Add(context.Background(), f.inst, f.sessionID, "next", "", uuidFromString(t, next))
	tx, err := f.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID); err != nil {
		t.Fatal(err)
	}
	b := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	b.Reconcile(context.Background(), f.sessionID)
	if len(api.remote["old"]) != 1 || len(api.remote["next"]) != 1 {
		t.Fatalf("uncommitted terminal state removed reactions: %+v", api.remote)
	}
	if err = tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID)
	b.Reconcile(context.Background(), f.sessionID)
	a.Reconcile(context.Background(), f.sessionID) // Also evict A's already-acknowledged local view.
	if states := a.states[uuidString(f.sessionID)]; len(states) != 1 || states[0].MessageID != "next" {
		t.Fatalf("local cleanup lost next input or retained old: %+v", states)
	}
	if len(api.remote["old"]) != 0 || len(api.remote["next"]) != 1 {
		t.Fatalf("committed cleanup damaged next input: %+v", api.remote)
	}
}

type lostTypingFinish struct{ *ChannelStore }

func (q lostTypingFinish) FinishChannelTypingReactionAdd(context.Context, db.FinishChannelTypingReactionAddParams) (db.ChannelTypingReaction, error) {
	return db.ChannelTypingReaction{}, errors.New("database unavailable after remote Add")
}

// FIXME(test-integration): Unknown HTTP outcomes are swept, never acknowledged
// as complete merely because one early list was empty.
func TestTypingUnrecordedAddRetainsRecoveryAnchorDB(t *testing.T) {
	f := newTypingDBFixture(t)
	api := &retryTypingAPI{crossProcessReactionAPI: &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}, fail: true}
	a := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, lostTypingFinish{f.store}, newDiscardLogger())
	a.Add(context.Background(), f.inst, f.sessionID, "unknown", "", f.messageID)
	f.Exec(t, "UPDATE channel_typing_reaction SET created_at=now()-interval '2 minutes',retry_after=now() WHERE workspace_id=$1", f.inst.WorkspaceID)
	api.failureMu.Lock()
	api.fail = false
	api.failureMu.Unlock()
	b := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	b.Reconcile(context.Background(), f.sessionID)
	if len(api.remote["unknown"]) != 0 {
		t.Fatal("unfinished Add was not compensated")
	}
	var retained int
	f.QueryRow(t, "SELECT count(*) FROM channel_typing_reaction WHERE workspace_id=$1 AND cleanup_required AND cleaned_at IS NULL AND NOT add_finished AND retry_after>now()", f.inst.WorkspaceID).Scan(&retained)
	if retained != 1 {
		t.Fatal("uncertain Add must retain its anchor for subsequent remote changes")
	}
}

// FIXME(test-integration): A forged settlement scope cannot mutate another
// workspace; an aborted settlement transaction must leave the input live.
func TestTypingSettlementWorkspaceAndRollbackDB(t *testing.T) {
	f := newTypingDBFixture(t)
	f.Exec(t, "UPDATE chat_message SET task_id=NULL WHERE id=$1", f.messageID)
	arg := db.SettleChannelTypingInputsParams{ChatSessionID: f.sessionID, ThroughMessageID: f.messageID, WorkspaceID: f.inst.WorkspaceID, InstallationID: f.inst.ID, ContextRevision: pgtype.Int8{Int64: 1, Valid: true}}
	tx, err := f.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = f.store.WithTx(tx).SettleChannelTypingInputs(context.Background(), arg); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	arg.WorkspaceID = f.taskID
	if err = f.store.SettleChannelTypingInputs(context.Background(), arg); err != nil {
		t.Fatal(err)
	}
	var settled bool
	f.QueryRow(t, "SELECT channel_typing_settled FROM chat_message WHERE id=$1", f.messageID).Scan(&settled)
	if settled {
		t.Fatal("rollback or foreign workspace settled the input")
	}
}

// FIXME(test-integration): Every sealed input is enumerable independently of
// delivery, while a new unowned input in the same session remains active.
func TestTypingCleanupAllBatchInputsDB(t *testing.T) {
	f := newTypingDBFixture(t)
	api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}
	a := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	for i, name := range []string{"first", "middle", "trigger"} {
		id := f.messageID
		if i > 0 {
			id = uuidFromString(t, f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": name, "task_id": f.taskID, "channel_ingested": true}))
		}
		api.remote[name] = []MessageReaction{{ReactionID: "human", OperatorType: "user", EmojiType: typingEmoji}}
		a.Add(context.Background(), f.inst, f.sessionID, name, "", id)
	}
	next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "channel_ingested": true})
	a.Add(context.Background(), f.inst, f.sessionID, "next", "", uuidFromString(t, next))
	f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.taskID)
	b := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	b.Reconcile(context.Background(), f.sessionID)
	for _, name := range []string{"first", "middle", "trigger"} {
		if r := api.remote[name]; len(r) != 1 || r[0].ReactionID != "human" {
			t.Fatalf("batch input %s retains app or loses human: %+v", name, r)
		}
	}
	if len(api.remote["next"]) != 1 {
		t.Fatal("next input reaction removed")
	}
}
