package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type chatWakeupFixture struct {
	fx      *testutil.Fixture
	q       *db.Queries
	svc     *ChatWakeupService
	clock   time.Time
	session string
	source  db.AgentTaskQueue
	events  []protocol.ChatWakeupChangedPayload
}

// newChatWakeupFixture builds a chat whose running turn asked the agent to
// report back, so the turn can create wakeups the way the CLI does.
func newChatWakeupFixture(t *testing.T) *chatWakeupFixture {
	t.Helper()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, "", "")
	suffix := fmt.Sprint(time.Now().UnixNano())
	owner := fx.User(t, "Wakeup owner", "chat-wakeup-"+suffix+"@example.test")
	workspace := fx.Workspace(t, "Chat wakeups", "chat-wakeup-"+suffix, testutil.Cols{"issue_prefix": "WK"})
	fx.UserID, fx.WorkspaceID = owner, workspace
	fx.Member(t, workspace, owner, "owner")
	runtime := fx.Runtime(t, "Wakeup runtime")
	agent := fx.Agent(t, "Wakeup agent", runtime)
	session := fx.ChatSession(t, agent)
	sourceID := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "chat_session_id": session, "status": "running"})
	fx.Exec(t, "UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1", sourceID)
	fx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": session, "role": "user", "content": "Tell me when the release ships", "task_id": sourceID,
	})
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, "DELETE FROM chat_message WHERE chat_session_id = $1", session)
		pool.Exec(ctx, "DELETE FROM agent_task_queue WHERE chat_session_id = $1", session)
		pool.Exec(ctx, "DELETE FROM chat_wakeup WHERE chat_session_id = $1", session)
	})

	q := db.New(pool)
	source, err := q.GetAgentTask(context.Background(), util.MustParseUUID(sourceID))
	if err != nil {
		t.Fatal(err)
	}
	f := &chatWakeupFixture{fx: fx, q: q, session: session, source: source, clock: time.Now()}
	bus := events.New()
	bus.Subscribe(protocol.EventChatWakeupChanged, func(e events.Event) {
		if e.ChatSessionOwnerID != owner {
			t.Errorf("wakeup event left the owner's scope: %+v", e)
		}
		f.events = append(f.events, e.Payload.(protocol.ChatWakeupChangedPayload))
	})
	f.svc = &ChatWakeupService{
		Tasks: &TaskService{Queries: q, TxStarter: pool, Bus: bus},
		now:   func() time.Time { return f.clock },
	}
	return f
}

func (f *chatWakeupFixture) at(t time.Time) { f.clock = t }

func (f *chatWakeupFixture) create(t *testing.T, in ChatWakeupInput) db.ChatWakeup {
	t.Helper()
	if in.Title == "" {
		in.Title = "Release shipped"
	}
	if in.Instruction == "" {
		in.Instruction = "Tell the user what shipped"
	}
	w, err := f.svc.Create(context.Background(), f.source, in)
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}
	return w
}

func (f *chatWakeupFixture) process(t *testing.T, id pgtype.UUID) db.ChatWakeup {
	t.Helper()
	ctx := context.Background()
	if err := f.svc.process(ctx, id); err != nil {
		t.Fatalf("process wakeup: %v", err)
	}
	w, err := f.q.GetChatWakeup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// turn returns the task a wakeup started and its single hidden input row.
func (f *chatWakeupFixture) turn(t *testing.T, w db.ChatWakeup) (db.AgentTaskQueue, db.ChatMessage) {
	t.Helper()
	ctx := context.Background()
	if !w.LastTaskID.Valid {
		t.Fatalf("wakeup started no turn: %+v", w)
	}
	task, err := f.q.GetAgentTask(ctx, w.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.ChatSessionID != w.ChatSessionID || task.Status != "queued" {
		t.Fatalf("wakeup turn = %+v, want a queued task in the same conversation", task)
	}
	rows, err := f.q.ListChatInputMessages(ctx, chatInputOwnerID(task))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].MessageKind != protocol.ChatMessageKindWakeup || rows[0].WakeupID != w.ID {
		t.Fatalf("wakeup turn input = %+v", rows)
	}
	msg, err := f.q.GetChatMessage(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return task, msg
}

func (f *chatWakeupFixture) dueNow(t *testing.T, id pgtype.UUID) {
	t.Helper()
	f.fx.Exec(t, "UPDATE chat_wakeup SET next_eval_at = now() - interval '1 second' WHERE id = $1", util.UUIDToString(id))
}

func TestChatWakeupReminderStartsTurnInConversation(t *testing.T) {
	f := newChatWakeupFixture(t)
	start := time.Now().Add(-time.Hour)
	f.at(start)
	w := f.create(t, ChatWakeupInput{AfterSeconds: 600})
	if w.Kind != "at" || !w.ExpiresAt.Time.Equal(w.FireAt.Time) || !w.SourceMessageID.Valid || w.SourceTaskID != f.source.ID {
		t.Fatalf("created reminder = %+v", w)
	}

	f.at(start.Add(5 * time.Minute))
	if got := f.process(t, w.ID); got.Status != "active" || got.LastTaskID.Valid {
		t.Fatalf("reminder fired early: %+v", got)
	}

	f.at(time.Now())
	if err := f.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := f.q.GetChatWakeup(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "fired" || got.EndReason.String != ChatWakeupReasonTime {
		t.Fatalf("reminder after its time = %+v", got)
	}
	task, input := f.turn(t, got)
	if input.ChannelIngested {
		t.Fatal("a web-only chat must not mark the wakeup turn for channel delivery")
	}
	for _, want := range []string{"[WAKEUP]", "> Tell me when the release ships", "Tell the user what shipped", "Ask before anything irreversible"} {
		if !strings.Contains(input.Content, want) {
			t.Fatalf("turn input lacks %q:\n%s", want, input.Content)
		}
	}
	pending, err := f.q.ListPendingChatTasksForSession(context.Background(), task.ChatSessionID)
	if err != nil {
		t.Fatal(err)
	}
	queued := pending[len(pending)-1]
	if queued.ID != task.ID || queued.MessageKind != protocol.ChatMessageKindWakeup || queued.WakeupID != w.ID {
		t.Fatalf("pending queue = %+v, want the wakeup turn tagged as one", pending)
	}
	last := f.events[len(f.events)-1]
	if last.Status != "fired" || last.TaskID != util.UUIDToString(task.ID) {
		t.Fatalf("last wakeup event = %+v", last)
	}
}

func TestChatWakeupConditionFiresOnceItHolds(t *testing.T) {
	f := newChatWakeupFixture(t)
	issue := f.fx.Issue(t, "Ship the release")
	cond, _ := json.Marshal(map[string]any{"type": "issue", "issue_id": issue, "state": "done"})
	w := f.create(t, ChatWakeupInput{Condition: cond})
	var stored ChatWakeupCondition
	if err := json.Unmarshal(w.Condition, &stored); err != nil || stored.Identifier == "" {
		t.Fatalf("stored condition = %s (%v), want a normalized one", w.Condition, err)
	}

	if got := f.process(t, w.ID); got.Status != "active" || got.LastTaskID.Valid {
		t.Fatalf("condition fired before it held: %+v", got)
	}

	f.fx.Exec(t, "UPDATE issue SET status = 'done' WHERE id = $1", issue)
	f.dueNow(t, w.ID)
	got := f.process(t, w.ID)
	if got.Status != "fired" || got.EndReason.String != ChatWakeupReasonCondition {
		t.Fatalf("condition after it held = %+v", got)
	}
	_, input := f.turn(t, got)
	if !strings.Contains(input.Content, "Observed by the platform") || !strings.Contains(input.Content, "the condition now holds") {
		t.Fatalf("turn input lacks the observation:\n%s", input.Content)
	}

	bad, _ := json.Marshal(map[string]any{"type": "issue", "issue_id": util.UUIDToString(w.ID), "state": "done"})
	if _, err := f.svc.Create(context.Background(), f.source, ChatWakeupInput{Title: "x", Instruction: "y", Condition: bad}); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("unknown issue: err = %v, want an input error", err)
	}
}

func TestChatWakeupCheckInStaysSilent(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	f.at(time.Now().Add(-35 * time.Minute))
	w := f.create(t, ChatWakeupInput{IntervalSeconds: 1800})

	f.at(time.Now())
	first := f.process(t, w.ID)
	if first.Status != "active" || first.CheckCount != 1 || !first.NextEvalAt.Time.After(time.Now()) {
		t.Fatalf("after the first check = %+v", first)
	}
	firstTask, _ := f.turn(t, first)

	if _, err := f.svc.CheckIn(ctx, firstTask, w.ID, "Release still building"); err != nil {
		t.Fatalf("check in: %v", err)
	}
	result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "Nothing new yet."})
	row, err := f.svc.Tasks.writeChatCompletionOutcome(ctx, f.q, firstTask, result)
	if err != nil || row != nil {
		t.Fatalf("silent check wrote a reply: row=%+v err=%v", row, err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM chat_message WHERE task_id = $1", util.UUIDToString(firstTask.ID)); n != 0 {
		t.Fatalf("silent check left %d message(s) in the conversation", n)
	}

	// The next check waits for the previous one to finish, then speaks up.
	f.dueNow(t, w.ID)
	if busy := f.process(t, w.ID); busy.CheckCount != 1 {
		t.Fatalf("a check started while the previous one ran: %+v", busy)
	}
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", util.UUIDToString(firstTask.ID))
	f.dueNow(t, w.ID)
	second := f.process(t, w.ID)
	if second.CheckCount != 2 || second.LastTaskID == firstTask.ID || second.LastCheckNote.Valid {
		t.Fatalf("after the second check = %+v", second)
	}
	secondTask, _ := f.turn(t, second)
	if _, err := f.svc.CheckIn(ctx, firstTask, w.ID, "late"); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("check-in from an earlier turn: err = %v", err)
	}
	result, _ = json.Marshal(protocol.TaskCompletedPayload{Output: "The release shipped."})
	row, err = f.svc.Tasks.writeChatCompletionOutcome(ctx, f.q, secondTask, result)
	if err != nil || row == nil || row.Content != "The release shipped." {
		t.Fatalf("reporting check: row=%+v err=%v", row, err)
	}
	if _, err := f.svc.Finish(ctx, secondTask, w.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.q.GetChatWakeup(ctx, w.ID); got.Status != "fired" || got.EndReason.String != "finished" {
		t.Fatalf("finished check = %+v", got)
	}
}

func TestChatWakeupDeadlineWakesOnce(t *testing.T) {
	f := newChatWakeupFixture(t)
	issue := f.fx.Issue(t, "Never finishes")
	cond, _ := json.Marshal(map[string]any{"type": "issue", "issue_id": issue, "state": "done"})
	f.at(time.Now().Add(-25 * time.Hour))
	w := f.create(t, ChatWakeupInput{Condition: cond})

	f.at(time.Now())
	f.dueNow(t, w.ID)
	got := f.process(t, w.ID)
	if got.Status != "expired" || got.EndReason.String != "expired" {
		t.Fatalf("past its deadline = %+v", got)
	}
	_, input := f.turn(t, got)
	if !strings.Contains(input.Content, "deadline") || !strings.Contains(input.Content, "ask whether to keep waiting") {
		t.Fatalf("deadline turn input:\n%s", input.Content)
	}
	f.dueNow(t, w.ID)
	if again := f.process(t, w.ID); again.LastTaskID != got.LastTaskID {
		t.Fatalf("an expired wakeup started another turn: %+v", again)
	}
}

func TestChatWakeupLimitsCancelAndArchive(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	var ids []pgtype.UUID
	for i := 0; i < ChatWakeupMaxPerSession; i++ {
		ids = append(ids, f.create(t, ChatWakeupInput{AfterSeconds: int64(3600 + i)}).ID)
	}
	if _, err := f.svc.Create(ctx, f.source, ChatWakeupInput{Title: "one more", Instruction: "x", AfterSeconds: 60}); !errors.Is(err, ErrChatWakeupLimit) {
		t.Fatalf("11th wakeup: err = %v, want the limit", err)
	}

	other := util.MustParseUUID(f.fx.ChatSession(t, util.UUIDToString(f.source.AgentID)))
	if _, err := f.svc.Cancel(ctx, other, ids[0], "user"); !errors.Is(err, ErrChatWakeupNotFound) {
		t.Fatalf("cancel from another conversation: err = %v", err)
	}
	cancelled, err := f.svc.Cancel(ctx, f.source.ChatSessionID, ids[0], "user")
	if err != nil || cancelled.Status != "cancelled" || cancelled.EndReason.String != "cancelled_by_user" {
		t.Fatalf("cancel = %+v, %v", cancelled, err)
	}
	if _, err := f.svc.Update(ctx, f.source, ids[0], ChatWakeupInput{Title: "x", Instruction: "y", AfterSeconds: 60}); !errors.Is(err, ErrChatWakeupEnded) {
		t.Fatalf("update after cancel: err = %v", err)
	}
	cond, _ := json.Marshal(map[string]any{"type": "run", "task_id": util.UUIDToString(f.source.ID)})
	if _, err := f.svc.Update(ctx, f.source, ids[1], ChatWakeupInput{Title: "x", Instruction: "y", Condition: cond}); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("changing a reminder into a condition: err = %v", err)
	}

	// A reminder due after the conversation was archived ends instead of running.
	f.fx.Exec(t, "UPDATE chat_session SET status = 'archived' WHERE id = $1", f.session)
	f.dueNow(t, ids[1])
	f.fx.Exec(t, "UPDATE chat_wakeup SET fire_at = now() - interval '1 second', expires_at = now() - interval '1 second' WHERE id = $1", util.UUIDToString(ids[1]))
	if got := f.process(t, ids[1]); got.Status != "cancelled" || got.EndReason.String != "session_archived" || got.LastTaskID.Valid {
		t.Fatalf("reminder in an archived conversation = %+v", got)
	}
	f.svc.EndForArchivedSession(ctx, f.source.ChatSessionID)
	if n := f.fx.Count(t, "SELECT count(*) FROM chat_wakeup WHERE chat_session_id = $1 AND status = 'active'", f.session); n != 0 {
		t.Fatalf("%d wakeup(s) still active after archive", n)
	}
}

func TestChatWakeupAnswersTheSenderNotTheSessionOwner(t *testing.T) {
	f := newChatWakeupFixture(t)
	sender := f.fx.User(t, "Group sender", fmt.Sprintf("sender-%d@example.test", time.Now().UnixNano()))
	f.fx.Member(t, f.fx.WorkspaceID, sender, "member")
	f.fx.Exec(t, "UPDATE agent_task_queue SET initiator_user_id = $1, originator_user_id = $1, accountable_user_id = $1 WHERE id = $2", sender, util.UUIDToString(f.source.ID))
	f.source.InitiatorUserID = util.MustParseUUID(sender)
	f.source.OriginatorUserID = f.source.InitiatorUserID
	f.fx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", sender, util.UUIDToString(f.source.AgentID))

	w := f.create(t, ChatWakeupInput{AfterSeconds: 60})
	if w.RequesterID != f.source.InitiatorUserID {
		t.Fatalf("requester = %s, want the sender %s", util.UUIDToString(w.RequesterID), sender)
	}
	f.at(f.clock.Add(2 * time.Minute))
	task, _ := f.turn(t, f.process(t, w.ID))
	if task.InitiatorUserID != w.RequesterID {
		t.Fatalf("wakeup turn initiator = %s, want the sender", util.UUIDToString(task.InitiatorUserID))
	}
}

func TestChatWakeupEndsWhenTheRequesterMayNoLongerRunTheAgent(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	member := f.fx.User(t, "Member", fmt.Sprintf("member-%d@example.test", time.Now().UnixNano()))
	f.fx.Member(t, f.fx.WorkspaceID, member, "member")
	f.fx.Exec(t, "UPDATE agent_task_queue SET initiator_user_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.ID))
	f.source.InitiatorUserID = util.MustParseUUID(member)
	f.fx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.AgentID))
	w := f.create(t, ChatWakeupInput{AfterSeconds: 60})

	f.fx.Exec(t, "UPDATE agent SET owner_id = $1, permission_mode = 'private' WHERE id = $2", f.fx.UserID, util.UUIDToString(f.source.AgentID))
	if allowed, err := f.svc.Tasks.MemberMayInvokeAgent(ctx, f.source.AgentID, w.RequesterID); err != nil || allowed {
		t.Fatalf("fixture: member still may run the agent (allowed=%v, err=%v)", allowed, err)
	}
	f.at(f.clock.Add(2 * time.Minute))
	got := f.process(t, w.ID)
	if got.LastTaskID.Valid || got.Status != "failed" || got.EndReason.String != "permission_revoked" {
		t.Fatalf("wakeup after the requester lost access = %+v", got)
	}
}

func TestChatWakeupHonorsPrivateProjects(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	member := f.fx.User(t, "Member", fmt.Sprintf("member-%d@example.test", time.Now().UnixNano()))
	f.fx.Member(t, f.fx.WorkspaceID, member, "member")
	f.fx.Exec(t, "UPDATE agent_task_queue SET initiator_user_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.ID))
	f.source.InitiatorUserID = util.MustParseUUID(member)
	f.fx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.AgentID))

	secret := f.fx.Project(t, "Secret", testutil.Cols{"visibility": "private"})
	hidden := f.fx.Issue(t, "Secret issue", testutil.Cols{"project_id": secret})
	if access, err := projectaccess.ForPrincipals(ctx, f.q, util.MustParseUUID(f.fx.WorkspaceID), util.MustParseUUID(member)); err != nil || access.CanSeeProject(util.MustParseUUID(secret)) {
		t.Fatalf("fixture: member can see the private project (err=%v)", err)
	}
	cond, _ := json.Marshal(map[string]any{"type": "issue_reply", "issue_id": hidden})
	if _, err := f.svc.Create(ctx, f.source, ChatWakeupInput{Title: "Watch", Instruction: "Report the reply", Condition: cond}); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("watching a hidden issue: err = %v, want an input error", err)
	}

	// Visible when created, private by the time it fires.
	project := f.fx.Project(t, "Open for now")
	issue := f.fx.Issue(t, "Watch then hide", testutil.Cols{"project_id": project})
	cond, _ = json.Marshal(map[string]any{"type": "issue_reply", "issue_id": issue})
	w := f.create(t, ChatWakeupInput{Condition: cond})
	f.fx.Exec(t, "UPDATE project SET visibility = 'private' WHERE id = $1", project)
	f.fx.Insert(t, "comment", testutil.Cols{"workspace_id": f.fx.WorkspaceID, "issue_id": issue, "author_id": f.fx.UserID, "author_type": "member", "content": "posted after it went private"})
	f.dueNow(t, w.ID)
	got := f.process(t, w.ID)
	if got.LastTaskID.Valid || got.Status != "failed" || got.EndReason.String != "access_lost" {
		t.Fatalf("wakeup watching a now-private issue = %+v", got)
	}
}

func TestChatWakeupReplyGoesWhereTheRequestCameFrom(t *testing.T) {
	ctx := context.Background()

	t.Run("web request stays in Multica", func(t *testing.T) {
		f := newChatWakeupFixture(t)
		seedChannelTaskBinding(t, f.fx.Pool, f.session)
		w := f.create(t, ChatWakeupInput{AfterSeconds: 60})
		f.at(f.clock.Add(2 * time.Minute))
		task, input := f.turn(t, f.process(t, w.ID))
		if input.ChannelIngested {
			t.Fatal("a wakeup asked for on Web was routed to the IM channel")
		}
		if _, err := f.q.GetChannelTaskDelivery(ctx, task.ID); err == nil {
			t.Fatal("a wakeup asked for on Web got an IM delivery snapshot")
		}
	})

	t.Run("IM request answers in its thread after /new", func(t *testing.T) {
		f := newChatWakeupFixture(t)
		seedChannelTaskBinding(t, f.fx.Pool, f.session)
		f.fx.Exec(t, `
			INSERT INTO channel_task_delivery (task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type, channel_message_id, channel_thread_id, route_revision)
			SELECT $1, id, installation_id, channel_type, channel_chat_id, chat_type, 'msg-asked', 'thread-asked', route_revision
			FROM channel_chat_session_binding WHERE chat_session_id = $2`, util.UUIDToString(f.source.ID), f.session)
		w := f.create(t, ChatWakeupInput{AfterSeconds: 60})
		f.fx.Exec(t, "UPDATE channel_chat_session_binding SET retired_at = now() WHERE chat_session_id = $1", f.session)

		f.at(f.clock.Add(2 * time.Minute))
		task, input := f.turn(t, f.process(t, w.ID))
		if !input.ChannelIngested {
			t.Fatal("a wakeup asked for in IM was not delivered back to IM")
		}
		delivery, err := f.q.GetChannelTaskDelivery(ctx, task.ID)
		if err != nil {
			t.Fatalf("wakeup turn delivery: %v", err)
		}
		if delivery.ChannelMessageID.String != "msg-asked" || delivery.ChannelThreadID.String != "thread-asked" {
			t.Fatalf("wakeup turn delivery = %+v, want the asking message's thread", delivery)
		}
	})
}

func TestChatWakeupFirstCheckIsNoLaterThanTheDeadline(t *testing.T) {
	now := time.Now()
	p, err := (ChatWakeupInput{IntervalSeconds: 7200, ExpiresInSeconds: 3600}).plan(now)
	if err != nil {
		t.Fatal(err)
	}
	if !p.nextEval.Equal(p.expiresAt) {
		t.Fatalf("first check %s, deadline %s: want the check at the deadline", p.nextEval, p.expiresAt)
	}
}

func TestChatWakeupRetriesAFailedReport(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	w := f.create(t, ChatWakeupInput{AfterSeconds: 60})
	f.at(f.clock.Add(2 * time.Minute))
	fired := f.process(t, w.ID)
	first, _ := f.turn(t, fired)

	if _, err := f.svc.Retry(ctx, f.source.ChatSessionID, w.ID); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("retry while the report is queued: err = %v", err)
	}
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'failed', completed_at = now() WHERE id = $1", util.UUIDToString(first.ID))
	listed, err := f.q.ListChatWakeupsForSession(ctx, f.source.ChatSessionID)
	if err != nil || len(listed) != 1 || listed[0].LastTaskStatus != "failed" {
		t.Fatalf("listed wakeups = %+v (%v), want the failed report visible", listed, err)
	}

	retried, err := f.svc.Retry(ctx, f.source.ChatSessionID, w.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retried.Status != "fired" || retried.LastTaskID == first.ID {
		t.Fatalf("retried wakeup = %+v", retried)
	}
	second, input := f.turn(t, retried)
	if !strings.Contains(input.Content, "Tell the user what shipped") || second.InitiatorUserID != w.RequesterID {
		t.Fatalf("retried turn = %+v\n%s", second, input.Content)
	}

	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", util.UUIDToString(second.ID))
	if _, err := f.svc.Retry(ctx, f.source.ChatSessionID, w.ID); !errors.Is(err, ErrChatWakeupInput) {
		t.Fatalf("retry after a delivered report: err = %v", err)
	}
}

func TestChatWakeupOutlivingItsConversationIsDropped(t *testing.T) {
	f := newChatWakeupFixture(t)
	w := f.create(t, ChatWakeupInput{AfterSeconds: 60})
	f.fx.Exec(t, "DELETE FROM chat_session WHERE id = $1", f.session)
	f.at(f.clock.Add(2 * time.Minute))
	if err := f.svc.process(context.Background(), w.ID); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM chat_wakeup WHERE id = $1", util.UUIDToString(w.ID)); n != 0 {
		t.Fatal("a wakeup whose conversation is gone was kept")
	}
}

// A wakeup turn can sit in the queue while its runtime is offline; the claim
// re-checks it, and so does the claim of an auto-retry of it.
func TestChatWakeupClaimRechecksTheRequester(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	member := f.fx.User(t, "Member", fmt.Sprintf("member-%d@example.test", time.Now().UnixNano()))
	f.fx.Member(t, f.fx.WorkspaceID, member, "member")
	f.fx.Exec(t, "UPDATE agent_task_queue SET initiator_user_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.ID))
	f.source.InitiatorUserID = util.MustParseUUID(member)
	f.fx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", member, util.UUIDToString(f.source.AgentID))

	reminder := f.create(t, ChatWakeupInput{AfterSeconds: 60})
	project := f.fx.Project(t, "Open for now")
	issue := f.fx.Issue(t, "Ship it", testutil.Cols{"project_id": project})
	cond, _ := json.Marshal(map[string]any{"type": "issue", "issue_id": issue, "state": "done"})
	watcher := f.create(t, ChatWakeupInput{Condition: cond})
	f.fx.Exec(t, "UPDATE issue SET status = 'done' WHERE id = $1", issue)

	f.at(f.clock.Add(2 * time.Minute))
	reminderTurn, _ := f.turn(t, f.process(t, reminder.ID))
	f.dueNow(t, watcher.ID)
	watcherTurn, _ := f.turn(t, f.process(t, watcher.ID))
	retryID := f.fx.Task(t, util.UUIDToString(f.source.AgentID), testutil.Cols{
		"runtime_id": util.UUIDToString(f.source.RuntimeID), "chat_session_id": f.session, "status": "queued",
		"chat_input_task_id": util.UUIDToString(reminderTurn.ID),
	})
	autoRetry, err := f.q.GetAgentTask(ctx, util.MustParseUUID(retryID))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []db.AgentTaskQueue{reminderTurn, autoRetry, watcherTurn, f.source} {
		if err := f.svc.CheckClaim(ctx, task); err != nil {
			t.Fatalf("claim of %s while allowed: %v", util.UUIDToString(task.ID), err)
		}
	}

	f.fx.Exec(t, "UPDATE project SET visibility = 'private' WHERE id = $1", project)
	if err := f.svc.CheckClaim(ctx, watcherTurn); !errors.Is(err, ErrChatWakeupClaimForbidden) {
		t.Fatalf("claim after the watched issue went private: err = %v", err)
	}
	f.fx.Exec(t, "UPDATE agent SET owner_id = $1, permission_mode = 'private' WHERE id = $2", f.fx.UserID, util.UUIDToString(f.source.AgentID))
	for _, task := range []db.AgentTaskQueue{reminderTurn, autoRetry} {
		if err := f.svc.CheckClaim(ctx, task); !errors.Is(err, ErrChatWakeupClaimForbidden) {
			t.Fatalf("claim of %s after the requester lost the agent: err = %v", util.UUIDToString(task.ID), err)
		}
	}
}

// finish runs inside the check's turn, before its reply is written, so a turn
// that fails afterwards has not reported and can be retried.
func TestChatWakeupFinishedCheckWhoseReportFailedCanRetry(t *testing.T) {
	f := newChatWakeupFixture(t)
	ctx := context.Background()
	w := f.create(t, ChatWakeupInput{IntervalSeconds: 600, ExpiresInSeconds: 3600})
	f.at(f.clock.Add(11 * time.Minute))
	task, _ := f.turn(t, f.process(t, w.ID))
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'running' WHERE id = $1", util.UUIDToString(task.ID))
	if _, err := f.svc.Finish(ctx, task, w.ID); err != nil {
		t.Fatal(err)
	}
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'failed', completed_at = now() WHERE id = $1", util.UUIDToString(task.ID))

	retried, err := f.svc.Retry(ctx, f.source.ChatSessionID, w.ID)
	if err != nil {
		t.Fatalf("retry of a finished check whose report failed: %v", err)
	}
	if retried.Status != "fired" || retried.LastTaskID == task.ID {
		t.Fatalf("retried check = %+v", retried)
	}
	_, input := f.turn(t, retried)
	if strings.Contains(input.Content, "checkin") || !strings.Contains(input.Content, "retries that report") {
		t.Fatalf("retry of a finished check must ask for the report, not another check:\n%s", input.Content)
	}
}
