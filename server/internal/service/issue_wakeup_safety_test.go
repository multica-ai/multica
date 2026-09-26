package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func finishWakeupRuns(t *testing.T, f principalFixture, id pgtype.UUID) {
	t.Helper()
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1 AND status IN ('queued','dispatched','running')", util.UUIDToString(id))
}

func TestWakeupMaxFiresPausesAfterTheCap(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", MaxFires: 2,
		EventTypes: []string{"comment.created"}, Instruction: "Answer new comments"})
	for i := range 3 {
		f.Comment(t, util.UUIDToString(issue), "question")
		w = wakeTick(t, f, s, w.ID)
		if i < 2 {
			finishWakeupRuns(t, f, w.ID)
		}
	}
	if n := wakeRuns(t, f, w.ID); n != 2 {
		t.Fatalf("capped rule started %d runs, want 2", n)
	}
	if w.Enabled || w.PausedReason.String != wakeupPausedMaxFires || w.DisabledAt.Valid {
		t.Fatalf("rule after cap: enabled=%t reason=%q disabled_at=%v", w.Enabled, w.PausedReason.String, w.DisabledAt)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_paused' AND details->>'reason'='max_fires'", issue); n != 1 {
		t.Fatalf("pause timeline entries = %d, want 1", n)
	}
	// Turning it back on starts a new count.
	w, err := s.Enable(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Enabled || w.FireCount != 0 || w.PausedReason.Valid {
		t.Fatalf("re-enabled rule = enabled %t count %d reason %v", w.Enabled, w.FireCount, w.PausedReason)
	}
}

func TestWakeupBurstPausesTheRule(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous",
		EventTypes: []string{"comment.created"}, Instruction: "Answer new comments"})
	for range wakeupHourlyRunLimit {
		f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "completed", "context": `{"wakeup_id":"` + util.UUIDToString(w.ID) + `"}`,
			"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	}
	f.Comment(t, util.UUIDToString(issue), "one more")
	got := wakeTick(t, f, s, w.ID)
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND status='queued'", util.UUIDToString(w.ID)); n != 0 {
		t.Fatalf("burst still queued %d runs", n)
	}
	if got.Enabled || got.PausedReason.String != wakeupPausedRate || !got.DisabledAt.Valid {
		t.Fatalf("rule after burst: enabled=%t reason=%q disabled_at=%v", got.Enabled, got.PausedReason.String, got.DisabledAt)
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatalf("paused rule kept %d pending inputs", n)
	}
	// "Wake now" is refused until someone turns the rule back on.
	if err := s.Trigger(context.Background(), issue, w.ID, parseTestUUID(t, f.UserID)); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("wake now on a paused rule: %v", err)
	}
}

func TestWakeupLoopBetweenRulesPausesTheRule(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	a := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous",
		EventTypes: []string{"comment.created"}, Instruction: "Reply to the other agent"})
	b := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous",
		EventTypes: []string{"task.completed"}, Instruction: "Unrelated rule"})
	chain := func(ids ...pgtype.UUID) string {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = util.UUIDToString(id)
		}
		raw, _ := json.Marshal(out)
		return string(raw)
	}
	// The other side of the review round is another agent: an agent's own
	// comments never wake it.
	var runtime string
	f.QueryRow(t, "SELECT runtime_id::text FROM agent WHERE id=$1", agent).Scan(&runtime)
	reviewer := f.Agent(t, "Reviewer", runtime)
	run := func(ctx string) string {
		return f.Task(t, reviewer, testutil.Cols{"issue_id": issue, "status": "running", "context": ctx, "runtime_id": runtime})
	}
	// Once through the loop is a normal review round: A's run wakes B's
	// agent, whose comment wakes A again.
	once := run(`{"wakeup_id":"` + util.UUIDToString(b.ID) + `","wakeup_chain":` + chain(a.ID, b.ID) + `}`)
	f.Comment(t, util.UUIDToString(issue), "round one", testutil.Cols{"author_type": "agent", "author_id": reviewer, "source_task_id": once})
	got := wakeTick(t, f, s, a.ID)
	if !got.Enabled || wakeRuns(t, f, a.ID) != 1 {
		t.Fatalf("first round was blocked: enabled=%t runs=%d", got.Enabled, wakeRuns(t, f, a.ID))
	}
	var stored struct {
		Chain []string `json:"wakeup_chain"`
	}
	task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(task.Context, &stored)
	if strings.Join(stored.Chain, ",") != strings.Join([]string{util.UUIDToString(a.ID), util.UUIDToString(b.ID), util.UUIDToString(a.ID)}, ",") {
		t.Fatalf("run chain = %v", stored.Chain)
	}
	finishWakeupRuns(t, f, a.ID)
	// The third pass through A without a person in between is a loop.
	again := run(`{"wakeup_id":"` + util.UUIDToString(b.ID) + `","wakeup_chain":` + chain(a.ID, b.ID, a.ID, b.ID) + `}`)
	f.Comment(t, util.UUIDToString(issue), "round two", testutil.Cols{"author_type": "agent", "author_id": reviewer, "source_task_id": again})
	got = wakeTick(t, f, s, a.ID)
	if got.Enabled || got.PausedReason.String != wakeupPausedLoop || wakeRuns(t, f, a.ID) != 1 {
		t.Fatalf("loop not stopped: enabled=%t reason=%q runs=%d", got.Enabled, got.PausedReason.String, wakeRuns(t, f, a.ID))
	}
	// A person's comment starts a fresh chain once the rule is back on.
	got, err = s.Enable(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, a.ID, WakeupEnableInput{Revision: got.Revision})
	if err != nil {
		t.Fatal(err)
	}
	f.Comment(t, util.UUIDToString(issue), "from a person")
	if got = wakeTick(t, f, s, a.ID); !got.Enabled || wakeRuns(t, f, a.ID) != 2 {
		t.Fatalf("member comment after re-enable: enabled=%t runs=%d", got.Enabled, wakeRuns(t, f, a.ID))
	}
}

func TestWakeupTriggerDeleteAndCheckIn(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	member := parseTestUUID(t, f.UserID)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "every", IntervalSeconds: 3600, Instruction: "Check the migration"})
	if err := s.Trigger(ctx, issue, w.ID, member); err != nil {
		t.Fatal(err)
	}
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("wake now started %d runs, want 1", n)
	}
	var taskID pgtype.UUID
	f.QueryRow(t, "SELECT id FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)).Scan(&taskID)
	// Only the running check itself can check in.
	if err := s.CheckIn(ctx, issue, w.ID, taskID, agent, "Nothing changed"); !errors.Is(err, ErrWakeupConflict) {
		t.Fatalf("queued run check-in: %v", err)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", taskID)
	if err := s.CheckIn(ctx, issue, w.ID, taskID, util.UUIDToString(member), "Nothing changed"); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("another actor's check-in: %v", err)
	}
	if err := s.CheckIn(ctx, issue, w.ID, taskID, agent, "  Progress 38%, 0 failures  "); err != nil {
		t.Fatal(err)
	}
	task, err := f.q.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !HasWakeupCheckin(task) {
		t.Fatalf("check-in not stored: %s", task.Context)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_checkin' AND details->>'note'='Progress 38%, 0 failures'", issue); n != 1 {
		t.Fatalf("check-in timeline entries = %d, want 1", n)
	}
	// Deleting withdraws queued runs and removes the rule.
	if err = s.Trigger(ctx, issue, w.ID, member); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, issue, w.ID, member); err != nil {
		t.Fatal(err)
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup WHERE id=$1", w.ID); n != 0 {
		t.Fatal("rule not deleted")
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1 AND status='queued'", util.UUIDToString(w.ID)); n != 0 {
		t.Fatalf("delete left %d queued runs", n)
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='running'", taskID); n != 1 {
		t.Fatal("delete must not stop a started run")
	}
}

func TestWakeupCheckInIsOnlyForScheduledChecks(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Reply"})
	task := f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "running", "context": `{"wakeup_id":"` + util.UUIDToString(w.ID) + `"}`,
		"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	if err := s.CheckIn(context.Background(), issue, w.ID, parseTestUUID(t, task), agent, "no reply needed"); !errors.Is(err, ErrWakeupInput) {
		t.Fatalf("event rule check-in: %v", err)
	}
}

// The check-in exception is narrow: only a run that checked in completes
// without the synthesized fallback comment.
func TestCompleteTaskSkipsFallbackCommentOnlyAfterCheckIn(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	f.Cleanup(t, "DELETE FROM comment WHERE issue_id=$1", issue)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "cron", CronExpression: "0 9 * * *", Instruction: "Daily check"})
	output, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "Checked; nothing new."})
	complete := func(ctxJSON string, withComment bool) int {
		cols := testutil.Cols{"issue_id": issue, "status": "running", "started_at": testutil.Raw("now()"), "context": ctxJSON,
			"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")}
		if withComment {
			cols["trigger_comment_id"] = f.Comment(t, util.UUIDToString(issue), "please check")
		}
		id := parseTestUUID(t, f.Task(t, agent, cols))
		if _, err := s.Tasks.CompleteTask(ctx, id, output, "", "", "", false, "", ""); err != nil {
			t.Fatal(err)
		}
		return f.Count(t, "SELECT count(*) FROM comment WHERE issue_id=$1 AND author_type='agent' AND source_task_id=$2", issue, id)
	}
	wakeupCtx := `{"wakeup_id":"` + util.UUIDToString(w.ID) + `"`
	if n := complete(wakeupCtx+`,"wakeup_checkin":{"note":"nothing new"}}`, false); n != 0 {
		t.Fatalf("checked-in run posted %d fallback comments", n)
	}
	if n := complete(wakeupCtx+`}`, false); n != 1 {
		t.Fatalf("wakeup run without a check-in posted %d fallback comments, want 1", n)
	}
	if n := complete(`{}`, true); n != 1 {
		t.Fatalf("comment-triggered run posted %d fallback comments, want 1", n)
	}
	if n := complete(`{}`, false); n != 1 {
		t.Fatalf("assignment run posted %d fallback comments, want 1", n)
	}
}
