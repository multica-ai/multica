package lark

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIXME(test-integration): Recreate the durable state left by a hard crash
// after successful Add but before the in-memory debounce timer creates a task.
func TestTypingTasklessCrashRecoveryDB(t *testing.T) {
	f := newTypingDBFixture(t)
	f.Exec(t, "UPDATE chat_message SET task_id=NULL WHERE id=$1", f.messageID)
	api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}
	first := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	first.Add(context.Background(), f.inst, f.sessionID, "crashed", "", f.messageID)
	f.Exec(t, "UPDATE channel_typing_reaction SET created_at=now()-interval '3 minutes' WHERE chat_message_id=$1", f.messageID)
	next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "channel_ingested": true})
	first.Add(context.Background(), f.inst, f.sessionID, "next", "", uuidFromString(t, next))
	running := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "running", "task_id": f.taskID, "channel_ingested": true})
	first.Add(context.Background(), f.inst, f.sessionID, "running", "", uuidFromString(t, running))
	f.Exec(t, "UPDATE channel_typing_reaction SET created_at=now()-interval '3 minutes' WHERE chat_message_id=$1", running)
	// A fresh process has no local states or timers, only the shared database.
	fresh := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	fresh.Reconcile(context.Background(), pgtype.UUID{})
	if len(api.remote["crashed"]) != 0 || len(api.remote["next"]) != 1 || len(api.remote["running"]) != 1 {
		t.Fatalf("wrong cleanup scope: %+v", api.remote)
	}
	var settled bool
	f.QueryRow(t, "SELECT channel_typing_settled FROM chat_message WHERE id=$1", f.messageID).Scan(&settled)
	if settled {
		t.Fatal("visual expiry must not settle or prevent future input processing")
	}
}

// FIXME(test-integration): Cleanup retention is bounded even without source rows,
// a working credential, a successful API call, or a known Add response.
func TestTypingRetentionErasesCredentialsAndStopsRetriesDB(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown_add", true: "permanent_failure"}[known], func(t *testing.T) {
			f := newTypingDBFixture(t)
			id, ws := uuid.NewString(), uuid.NewString()
			reaction := ""
			if known {
				reaction = "remote-reaction"
			}
			f.Exec(t, `INSERT INTO channel_typing_reaction(id,workspace_id,chat_session_id,chat_message_id,installation_id,channel_message_id,installation_snapshot,reaction_id,add_finished,cleanup_required,created_at,attempts,quota_slot)
   VALUES($1,$2,$3,$4,$5,'deleted','{"app_secret_enc":"synthetic-ciphertext"}',$6,$7,true,now()-interval '8 days',10000,1)`, id, ws, f.sessionID, f.messageID, f.inst.ID, reaction, known)
			f.Cleanup(t, "DELETE FROM channel_typing_reaction WHERE id=$1", id)
			api := &fakeTypingAPIClient{}
			fresh := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
			// Eligibility stops at the deadline even if maintenance has not run yet.
			fresh.Reconcile(context.Background(), f.sessionID)
			if len(api.deleteCalled)+len(api.listCalled) != 0 {
				t.Fatal("expired credentials used for remote cleanup")
			}
			fresh.maintainCleanup(context.Background())
			var abandoned, redacted, cleaned bool
			f.QueryRow(t, `SELECT abandoned_at IS NOT NULL,installation_snapshot='{}'::jsonb,cleaned_at IS NOT NULL FROM channel_typing_reaction WHERE id=$1`, id).Scan(&abandoned, &redacted, &cleaned)
			if !abandoned || !redacted || cleaned {
				t.Fatalf("failed cleanup not preserved distinctly: abandoned=%v redacted=%v cleaned=%v", abandoned, redacted, cleaned)
			}
			_, err := f.store.FinishChannelTypingReactionAdd(context.Background(), db.FinishChannelTypingReactionAddParams{ID: uuidFromString(t, id), ReactionID: "late", CleanupRequired: true})
			if err != pgx.ErrNoRows {
				t.Fatalf("late Add resurrected expired ledger: %v", err)
			}
			fresh.Reconcile(context.Background(), f.sessionID)
			if len(api.deleteCalled)+len(api.listCalled) != 0 {
				t.Fatal("abandoned cleanup retried")
			}
			f.Exec(t, "UPDATE channel_typing_reaction SET abandoned_at=now()-interval '8 days' WHERE id=$1", id)
			fresh.maintainCleanup(context.Background())
			var exists bool
			f.QueryRow(t, "SELECT EXISTS(SELECT 1 FROM channel_typing_reaction WHERE id=$1)", id).Scan(&exists)
			if exists {
				t.Fatal("expired non-secret tombstone was not pruned")
			}
		})
	}
}

// FIXME(test-integration): The unique quota index arbitrates concurrent replicas;
// the losing Add is skipped instead of creating an untracked remote reaction.
func TestTypingWorkspaceQuotaConcurrentDB(t *testing.T) {
	f := newTypingDBFixture(t)
	f.Exec(t, `INSERT INTO channel_typing_reaction(id,workspace_id,chat_session_id,chat_message_id,installation_id,channel_message_id,installation_snapshot,quota_slot)
 SELECT gen_random_uuid(),$1,$2,$3,$4,'occupied','{}',slot FROM generate_series(1,999) slot`, f.inst.WorkspaceID, f.sessionID, f.messageID, f.inst.ID)
	api := &crossProcessReactionAPI{fakeTypingAPIClient: &fakeTypingAPIClient{}, remote: make(map[string][]MessageReaction)}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger()).Add(context.Background(), f.inst, f.sessionID, uuid.NewString(), "", f.messageID)
		}()
	}
	close(start)
	wg.Wait()
	var pending int
	f.QueryRow(t, "SELECT count(*) FROM channel_typing_reaction WHERE workspace_id=$1 AND cleaned_at IS NULL AND abandoned_at IS NULL", f.inst.WorkspaceID).Scan(&pending)
	if pending != 1000 || len(api.remote) != 1 {
		t.Fatalf("quota bypass: pending=%d remoteAdds=%d", pending, len(api.remote))
	}
	other := newTypingDBFixture(t)
	NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, other.store, newDiscardLogger()).Add(context.Background(), other.inst, other.sessionID, "neighbor", "", other.messageID)
	if len(api.remote["neighbor"]) != 1 {
		t.Fatal("workspace quota affected neighbor")
	}
	f.Exec(t, "UPDATE channel_typing_reaction SET created_at=now()-interval '8 days' WHERE workspace_id=$1", f.inst.WorkspaceID)
	m := NewTypingIndicatorManager(api, fakeTypingCreds{secret: "test"}, f.store, newDiscardLogger())
	m.maintainCleanup(context.Background())
	m.Add(context.Background(), f.inst, f.sessionID, "after-expiry", "", f.messageID)
	if len(api.remote["after-expiry"]) != 1 {
		t.Fatal("terminal slots were not released")
	}
}
