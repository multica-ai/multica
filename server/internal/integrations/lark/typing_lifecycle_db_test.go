package lark

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type typingDBFixture struct {
	*dbfx.Fixture
	runtimeID                    string
	store                        *ChannelStore
	inst                         Installation
	sessionID, messageID, taskID pgtype.UUID
}

func newTypingDBFixture(t *testing.T) *typingDBFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required for durable typing lifecycle regression")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fx := dbfx.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Typing tester", "typing-"+suffix+"@multica.test")
	fx.WorkspaceID = fx.Workspace(t, "Typing workspace", "typing-"+suffix)
	runtimeID := fx.Runtime(t, "Typing runtime")
	agentID := fx.Agent(t, "Typing agent", runtimeID)
	instID := fx.Insert(t, "channel_installation", dbfx.Cols{
		"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "feishu",
		"installer_user_id": fx.UserID, "config": dbfx.Raw(`'{"app_id":"typing-app","region":"feishu"}'::jsonb`),
	})
	sid := fx.ChatSession(t, agentID)
	fx.Insert(t, "channel_chat_session_binding", dbfx.Cols{
		"chat_session_id": sid, "installation_id": instID, "channel_type": "feishu", "channel_chat_id": "typing-room", "chat_type": "group",
	})
	task := fx.Task(t, agentID, dbfx.Cols{"chat_session_id": sid, "status": "running", "runtime_id": runtimeID})
	message := fx.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": sid, "role": "user", "content": "work", "task_id": task, "channel_ingested": true})
	store := NewChannelStore(db.New(pool))
	inst, err := store.GetLarkInstallation(context.Background(), uuidFromString(t, instID))
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_typing_reaction WHERE workspace_id=$1", fx.WorkspaceID)
	return &typingDBFixture{Fixture: fx, runtimeID: runtimeID, store: store, inst: inst, sessionID: uuidFromString(t, sid), messageID: uuidFromString(t, message), taskID: uuidFromString(t, task)}
}

// FIXME(test-integration): The shared committed input/task relation, including
// debounce and rollback, is the cross-process lifecycle contract.
func TestTypingInputLifecycleDB(t *testing.T) {
	for _, scenario := range []string{"pending debounce", "running", "completed", "failed", "cancelled", "missing task", "deleted input", "deleted session", "archived session", "revoked installation", "foreign workspace", "foreign installation", "foreign task workspace", "foreign task session", "web input", "rollback", "next turn", "coalesced inputs"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTypingDBFixture(t)
			ctx := context.Background()
			arg := db.IsChannelMessageTypingActiveParams{MessageID: f.messageID, ChatSessionID: f.sessionID, WorkspaceID: f.inst.WorkspaceID, InstallationID: f.inst.ID, ChannelType: channelTypeFeishu}
			want := false
			switch scenario {
			case "pending debounce":
				f.Exec(t, "UPDATE chat_message SET task_id=NULL WHERE id=$1", f.messageID)
				want = true
			case "running":
				want = true
			case "completed", "failed", "cancelled":
				f.Exec(t, "UPDATE agent_task_queue SET status=$2 WHERE id=$1", f.taskID, scenario)
			case "missing task":
				f.Exec(t, "DELETE FROM agent_task_queue WHERE id=$1", f.taskID)
			case "deleted input":
				f.Exec(t, "DELETE FROM chat_message WHERE id=$1", f.messageID)
			case "deleted session":
				f.Exec(t, "DELETE FROM chat_session WHERE id=$1", f.sessionID)
			case "archived session":
				f.Exec(t, "UPDATE chat_session SET status='archived' WHERE id=$1", f.sessionID)
			case "revoked installation":
				f.Exec(t, "UPDATE channel_installation SET status='revoked' WHERE id=$1", f.inst.ID)
			case "foreign workspace":
				arg.WorkspaceID = uuidFromString(t, uuid.NewString())
			case "foreign installation":
				arg.InstallationID = uuidFromString(t, uuid.NewString())
			case "foreign task workspace":
				other := f.Workspace(t, "Other", "other-"+uuid.NewString())
				otherAgent := dbfx.New(f.Pool, other, f.UserID).Agent(t, "Other agent", "")
				f.Exec(t, "UPDATE agent_task_queue SET agent_id=$2 WHERE id=$1", f.taskID, otherAgent)
			case "foreign task session":
				other := f.ChatSession(t, uuidString(f.inst.AgentID))
				f.Exec(t, "UPDATE agent_task_queue SET chat_session_id=$2 WHERE id=$1", f.taskID, other)
			case "web input":
				f.Exec(t, "UPDATE chat_message SET channel_ingested=false WHERE id=$1", f.messageID)
			case "rollback":
				tx, err := f.Pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err = tx.Exec(ctx, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", f.taskID); err != nil {
					t.Fatal(err)
				}
				// A separate process must not observe an uncommitted terminal state.
				live, err := f.store.IsChannelMessageTypingActive(ctx, arg)
				if err != nil || !live {
					t.Fatalf("uncommitted cancellation leaked: active=%v err=%v", live, err)
				}
				if err = tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				want = true
			case "next turn":
				f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.taskID)
				nextTask := f.Task(t, uuidString(f.inst.AgentID), dbfx.Cols{"chat_session_id": f.sessionID, "status": "running", "runtime_id": f.runtimeID})
				next := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "next", "task_id": nextTask, "channel_ingested": true})
				nextArg := arg
				nextArg.MessageID = uuidFromString(t, next)
				live, err := f.store.IsChannelMessageTypingActive(ctx, nextArg)
				if err != nil || !live {
					t.Fatalf("next input not active: %v %v", live, err)
				}
			case "coalesced inputs":
				// The delivery trigger is only one message in a sealed batch. Every
				// other input must still follow the same terminal task without delivery.
				other := f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": f.sessionID, "role": "user", "content": "coalesced", "task_id": f.taskID, "channel_ingested": true})
				arg.MessageID = uuidFromString(t, other)
				f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.taskID)
			}
			active, err := f.store.IsChannelMessageTypingActive(ctx, arg)
			if err != nil || active != want {
				t.Fatalf("active=%v want=%v err=%v", active, want, err)
			}
		})
	}
}
