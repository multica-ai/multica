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
	if second.CheckCount != 2 || second.LastTaskID == firstTask.ID {
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
