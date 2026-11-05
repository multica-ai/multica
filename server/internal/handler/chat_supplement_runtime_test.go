package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChatSupplementRuntimeDeletionPrunesReceipts(t *testing.T) {
	f := newChatSupplementFixture(t, "codex", true)
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "guidance").Want(201)
	dbfx.Exec(t, `UPDATE agent SET kind='system', system_key='rev_review_probe' WHERE id=$1`, f.agent)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, f.task)
	testutil.Call(t, testHandler.DeleteAgentRuntime, withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+f.runtime, nil), "runtimeId", f.runtime)).Want(200)
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, f.session); n != 0 {
		t.Fatalf("cascade did not run: %d", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE chat_session_id=$1`, f.session); n != 0 {
		t.Fatalf("orphan chat receipts after runtime deletion: %d", n)
	}
}

// Both teardown modes are shared by manual/profile deletion and offline GC.
// The transaction must remove only system-chat receipts, retaining user chats.
func TestChatSupplementRuntimeTeardownScope(t *testing.T) {
	for _, cancelTasks := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelTasks), func(t *testing.T) {
			system := newChatSupplementFixture(t, "codex", true)
			user := newChatSupplementFixture(t, "codex", true)
			chatSupplementRequest(t, system, "0199a4e8-22ce-7b01-bba5-111111111111", "system guidance").Want(201)
			chatSupplementRequest(t, user, "0199a4e8-22ce-7b01-bba5-111111111111", "user guidance").Want(201)
			dbfx.Exec(t, `UPDATE agent SET kind='system', system_key='receipt_cleanup' WHERE id=$1`, system.agent)
			dbfx.Exec(t, `UPDATE agent SET runtime_id=$1, archived_at=now() WHERE id=$2`, system.runtime, user.agent)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id IN ($1,$2)`, system.task, user.task)
			tx, err := testPool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			_, err = service.TeardownRuntime(t.Context(), db.New(tx), parseUUID(system.runtime), service.RuntimeTeardownOptions{CancelNonTerminalTasks: cancelTasks})
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE chat_session_id=$1`, system.session); n != 0 {
				t.Fatalf("system receipts: %d", n)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_supplement WHERE chat_session_id=$1`, user.session); n != 1 {
				t.Fatalf("user receipts: %d", n)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, user.session); n != 1 {
				t.Fatal("user history deleted")
			}
		})
	}
}
