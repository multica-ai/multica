package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestChannelAndAutomationUseEachExecutionUser(t *testing.T) {
	f, owner := newPrincipalFixture(t)
	ctx := context.Background()
	other := f.member(t, "channel-sender")
	publisher := f.member(t, "channel-publisher")
	defaultRuntime := f.Runtime(t, "default runtime", testutil.Cols{"provider": "codex"})
	agentID := f.Agent(t, "shared source", defaultRuntime, testutil.Cols{"permission_mode": "public_to"})
	f.InsertNoID(t, "agent_invocation_target", testutil.Cols{
		"agent_id": agentID, "target_type": "member", "target_id": other,
	}, "agent_id=$1 AND target_id=$2", agentID, other)
	personal := f.Runtime(t, "sender personal runtime", testutil.Cols{"provider": "codex", "owner_id": other})
	q := f.q
	if _, err := q.UpsertAgentRuntimePreference(ctx, db.UpsertAgentRuntimePreferenceParams{
		WorkspaceID: util.MustParseUUID(f.WorkspaceID), UserID: util.MustParseUUID(other),
		AgentID: util.MustParseUUID(agentID), RuntimeID: util.MustParseUUID(personal), ModelMode: "runtime_default",
	}); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, `DELETE FROM agent_runtime_preference WHERE agent_id=$1`, agentID)
	session, err := q.CreateChatSession(ctx, db.CreateChatSessionParams{
		ID: dbid.NewV7(), WorkspaceID: util.MustParseUUID(f.WorkspaceID),
		AgentID: util.MustParseUUID(agentID), CreatorID: util.MustParseUUID(owner), Title: "shared channel",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, `DELETE FROM chat_session WHERE id=$1`, session.ID)
	bindingID := f.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": session.ID, "installation_id": dbid.NewV7(),
		"channel_type": "slack", "channel_chat_id": "shared-channel", "chat_type": "p2p",
	})
	f.InsertNoID(t, "channel_chat_context_generation", testutil.Cols{
		"chat_session_id": session.ID, "revision": 1,
		// The shared generation cursor deliberately belongs to the OTHER sender.
		"last_message_id": other + "-message", "last_thread_id": other + "-thread", "last_sender_id": other + "-native",
	}, "chat_session_id=$1", session.ID)
	f.Cleanup(t, `DELETE FROM agent_task_queue WHERE chat_session_id=$1`, session.ID)
	f.Cleanup(t, `DELETE FROM channel_task_delivery WHERE binding_id=$1`, bindingID)
	f.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id=$1`, session.ID)
	svc := f.svc.TaskSvc
	for _, sender := range []string{owner, other} {
		_, err := q.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ID: dbid.NewV7(), ChatSessionID: session.ID, Role: "user", Content: sender,
			ChannelSenderUserID: util.MustParseUUID(sender), ChannelIngested: pgtype.Bool{Bool: true, Valid: true},
			ChannelSourceMessageID: pgtype.Text{String: sender + "-message", Valid: true},
			ChannelSourceThreadID:  pgtype.Text{String: sender + "-thread", Valid: true},
			ChannelSourceSenderID:  pgtype.Text{String: sender + "-native", Valid: true},
			ChannelContextRevision: pgtype.Int8{Int64: 1, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	pending, err := q.ListUnownedChannelChatContextRevisions(ctx, session.ID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("recover both senders within one revision: %+v, err=%v", pending, err)
	}
	pendingSenders := map[pgtype.UUID]bool{}
	for _, partition := range pending {
		if partition.ContextRevision != 1 || !partition.SenderUserID.Valid {
			t.Fatalf("invalid recovery partition: %+v", partition)
		}
		pendingSenders[partition.SenderUserID] = true
	}
	if !pendingSenders[util.MustParseUUID(owner)] || !pendingSenders[util.MustParseUUID(other)] {
		t.Fatalf("recovery merged sender identities: %+v", pending)
	}
	for _, sender := range []string{owner, other} {
		task, err := svc.EnqueueChannelChatTask(ctx, session, util.MustParseUUID(sender), false, 1, util.MustParseUUID(bindingID), 1)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := q.ListChatInputMessages(ctx, task.ChatInputTaskID)
		if err != nil || len(messages) != 1 || messages[0].Content != sender {
			t.Fatalf("sender %s batch mixed: %+v err=%v", sender, messages, err)
		}
		routing, err := ParseRuntimeRouting(task.RuntimeRouting)
		if err != nil || routing.ExecutionUserID != sender {
			t.Fatalf("wrong execution user: %+v %v", routing, err)
		}
		if sender == other && task.RuntimeID != util.MustParseUUID(personal) {
			t.Fatal("second sender used first sender's machine")
		}
		var stored struct {
			Target struct {
				MessageID string `json:"message_id"`
				ThreadID  string `json:"thread_id"`
			} `json:"channel_reply_target"`
		}
		if err := json.Unmarshal(task.Context, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Target.MessageID != sender+"-message" || stored.Target.ThreadID != sender+"-thread" {
			t.Fatal("reply target follows another sender")
		}
		delivery, err := q.GetChannelTaskDelivery(ctx, task.ID)
		if err != nil || delivery.ChannelMessageID.String != sender+"-message" || delivery.ChannelThreadID.String != sender+"-thread" || delivery.ChannelSenderID.String != sender+"-native" {
			t.Fatalf("delivery must use the sealed sender's trigger: %+v, err=%v", delivery, err)
		}
	}
	// An old unowned channel message cannot silently borrow a new user's route.
	if _, err := q.CreateChatMessage(ctx, db.CreateChatMessageParams{ID: dbid.NewV7(), ChatSessionID: session.ID, Role: "user", Content: "unknown old sender", ChannelIngested: pgtype.Bool{Bool: true, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	pending, err = q.ListUnownedChannelChatContextRevisions(ctx, session.ID)
	if err != nil || len(pending) != 1 || pending[0].SenderUserID.Valid {
		t.Fatalf("unknown sender must remain unauthenticated during recovery: %+v, err=%v", pending, err)
	}
	if _, err := svc.EnqueueChatTask(ctx, session, util.MustParseUUID(other), false); !errors.Is(err, ErrTaskRuntimeUnavailable) {
		t.Fatalf("unknown channel sender accepted: %v", err)
	}

	// Admission, runtime selection and attribution all use trigger.created_by.
	// The autopilot creator and latest publisher are two distinct other people.
	autopilotID, triggerID := f.autopilotWithTrigger(t, agentID, owner, other)
	f.Exec(t, `UPDATE autopilot_trigger SET published_by_id=$2 WHERE id=$1`, triggerID, publisher)
	f.Insert(t, "autopilot_rule_version", testutil.Cols{
		"autopilot_id": autopilotID, "workspace_id": f.WorkspaceID,
		"published_by_type": "member", "published_by_id": publisher,
	})
	f.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id=$1`, autopilotID)
	f.Cleanup(t, `DELETE FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id=$1)`, autopilotID)
	run := f.dispatch(t, autopilotID, triggerID)
	task, err := q.GetAgentTask(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := ParseRuntimeRouting(task.RuntimeRouting)
	if err != nil || routing.ExecutionUserID != other || task.RuntimeID != util.MustParseUUID(personal) {
		t.Fatalf("scheduler did not use trigger creator's personal runtime: %v %v", routing, err)
	}
	if task.OriginatorUserID != util.MustParseUUID(other) || task.AccountableUserID != util.MustParseUUID(other) || task.OriginatorSource.String != "trigger_owner" {
		t.Fatalf("trigger-owner attribution changed: originator=%v accountable=%v source=%s", task.OriginatorUserID, task.AccountableUserID, task.OriginatorSource.String)
	}
}
