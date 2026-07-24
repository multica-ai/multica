package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The webhook create_issue repair (ensureWebhookCreateIssueTask) must always leave
// the run bound (via CAS) to a task PROVEN to be its dispatched task, regardless of
// how long ago the crash happened or what the issue status is (MUL-4809 §4.1 P0-2).

// TestWebhookRepairBindsDispatchedTaskAfterWindow covers delayed recovery: the
// dispatched task was enqueued (stamped) but the bind crashed, and reclaim happens
// long after issue creation. A time-window heuristic would never find it; precise
// provenance binds it.
func TestWebhookRepairBindsDispatchedTaskAfterWindow(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)
	ap, err := svc.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}

	// Dispatched task queued 20 minutes ago, stamped, but run.task_id never bound.
	dispatched := insertTask(agentID, -20*time.Minute, "queued", run.ID)

	if err := svc.ensureWebhookCreateIssueTask(ctx, ap, &run); err != nil {
		t.Fatalf("ensureWebhookCreateIssueTask: %v", err)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if !got.TaskID.Valid || got.TaskID.Bytes != dispatched.ID.Bytes {
		t.Fatalf("webhook repair did not bind the (aged) dispatched task: task_id valid=%v", got.TaskID.Valid)
	}
	if got.Status != "running" {
		t.Fatalf("webhook repair did not advance the run to running: status=%q", got.Status)
	}
}

// TestWebhookRepairIgnoresStrayCommentTask covers the in-window stray comment: a
// (settled) unstamped comment task exists on the issue. The repair must NOT adopt it
// (that was the old "any issue task exists → success" bug); it enqueues a fresh
// dispatched task carrying this run's provenance and binds that instead.
func TestWebhookRepairIgnoresStrayCommentTask(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)
	ap, err := svc.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}

	// A settled (completed) unstamped comment task — the old code returned success
	// on "any issue task exists" and left the run unbound forever.
	comment := insertTask(agentID, 0, "completed", pgtype.UUID{})

	if err := svc.ensureWebhookCreateIssueTask(ctx, ap, &run); err != nil {
		t.Fatalf("ensureWebhookCreateIssueTask: %v", err)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if !got.TaskID.Valid {
		t.Fatal("webhook repair left the run unbound")
	}
	if got.TaskID.Bytes == comment.ID.Bytes {
		t.Fatal("webhook repair bound the run to an unstamped stray comment task")
	}
	// The bound task must be a real dispatched task carrying this run's provenance.
	bound, err := svc.Queries.GetAgentTask(ctx, got.TaskID)
	if err != nil {
		t.Fatalf("get bound task: %v", err)
	}
	if !bound.DispatchedAutopilotRunID.Valid || bound.DispatchedAutopilotRunID.Bytes != run.ID.Bytes {
		t.Fatal("webhook repair bound a task without this run's provenance stamp")
	}
}

// TestWebhookRepairFailsRunOnPendingCollision covers the convergence closure for the
// rarest race (MUL-4809 §4.1 P0-3): the agent already holds its one pending task per
// (issue, agent) — an unstamped comment task — so the autopilot's dispatched task
// can't be enqueued. The repair must NOT bind the run to that unrelated pending task,
// and must NOT leave the run permanently active. It closes the loop by failing the
// run with a traceable dispatch-collision reason; `run` (a pointer) reflects that so
// the webhook worker records the delivery as failed rather than dispatched.
func TestWebhookRepairFailsRunOnPendingCollision(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)
	ap, err := svc.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}

	pending := insertTask(agentID, 0, "queued", pgtype.UUID{}) // pending unstamped comment task holds the slot

	if err := svc.ensureWebhookCreateIssueTask(ctx, ap, &run); err != nil {
		t.Fatalf("ensureWebhookCreateIssueTask must not error on a pending-task collision: %v", err)
	}
	// The run must be terminally failed (not active/unbound) and the failure reason
	// must be traceable.
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("pending collision did not converge the run to failed: status=%q", got.Status)
	}
	if !got.FailureReason.Valid || !strings.Contains(strings.ToLower(got.FailureReason.String), "collision") {
		t.Fatalf("dispatch-collision reason not traceable: %q", got.FailureReason.String)
	}
	if got.TaskID.Bytes == pending.ID.Bytes {
		t.Fatal("webhook repair misattributed the run to the unrelated pending comment task")
	}
	// The pointer the caller passes must reflect the terminal status so the worker
	// records delivery=failed, not delivery=dispatched.
	if run.Status != "failed" {
		t.Fatalf("collision did not propagate to the caller's run: status=%q", run.Status)
	}
}

// TestWebhookDeliveryDispatchFailsRunOnPendingCollision is the worker-level closure
// (MUL-4809 §4.1 P0-3): driving the exact entry point the webhook worker calls
// (DispatchAutopilotForWebhookDelivery) on a reclaimed delivery whose dispatched
// enqueue hits a pending-task collision must return a FAILED run. The worker maps a
// failed run to delivery=failed, so the forbidden `delivery=dispatched && run
// active/unbound` state can never occur.
func TestWebhookDeliveryDispatchFailsRunOnPendingCollision(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)
	ap, err := svc.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}

	// Link the run to a webhook delivery so the reclaim path finds it by delivery id.
	var deliveryID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`UPDATE autopilot_run SET webhook_delivery_id = gen_random_uuid() WHERE id = $1 RETURNING webhook_delivery_id`,
		run.ID).Scan(&deliveryID); err != nil {
		t.Fatalf("set webhook_delivery_id: %v", err)
	}

	// A pending comment task holds the (issue, agent) slot → the dispatched enqueue collides.
	insertTask(agentID, 0, "queued", pgtype.UUID{})

	got, err := svc.DispatchAutopilotForWebhookDelivery(ctx, ap, pgtype.UUID{}, []byte("{}"), deliveryID)
	if err != nil {
		t.Fatalf("DispatchAutopilotForWebhookDelivery: %v", err)
	}
	if got == nil {
		t.Fatal("DispatchAutopilotForWebhookDelivery returned nil run on collision")
	}
	if got.Status != "failed" {
		t.Fatalf("collision must return a failed run (worker then records delivery=failed), got status=%q", got.Status)
	}
	stored, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.Status != "failed" {
		t.Fatalf("run left active after collision: status=%q", stored.Status)
	}
	if stored.TaskID.Valid {
		t.Fatalf("run bound to the stray pending task on collision: task_id=%x", stored.TaskID.Bytes)
	}
}

// TestWebhookRepairProceedsWhenIssueInReview covers the status-token bug: an issue
// sitting in in_review/blocked must NOT stop the repair. The run is finalized by
// task outcome, so the repair still enqueues + binds the dispatched task rather than
// declaring the delivery handled and leaving the run hung (MUL-4809 §4.1 P0-2).
func TestWebhookRepairProceedsWhenIssueInReview(t *testing.T) {
	ctx := context.Background()
	svc, _, run, pool, _ := newCreateIssueRunFixture(t)
	ap, err := svc.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_review' WHERE id = $1`, run.IssueID); err != nil {
		t.Fatalf("set issue in_review: %v", err)
	}

	if err := svc.ensureWebhookCreateIssueTask(ctx, ap, &run); err != nil {
		t.Fatalf("ensureWebhookCreateIssueTask: %v", err)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if !got.TaskID.Valid || got.Status != "running" {
		t.Fatalf("webhook repair was blocked by issue status: task_id valid=%v status=%q", got.TaskID.Valid, got.Status)
	}
	var bound db.AgentTaskQueue
	if bound, err = svc.Queries.GetAgentTask(ctx, got.TaskID); err != nil {
		t.Fatalf("get bound task: %v", err)
	}
	if !bound.DispatchedAutopilotRunID.Valid || bound.DispatchedAutopilotRunID.Bytes != run.ID.Bytes {
		t.Fatal("repaired task missing provenance stamp")
	}
}
