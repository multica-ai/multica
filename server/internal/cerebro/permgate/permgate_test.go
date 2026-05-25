package permgate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
)

// --- fakes ------------------------------------------------------------------

type fakeResolver struct {
	decision permissions.Decision
	err      error
	calls    int
}

func (f *fakeResolver) Can(ctx context.Context, req permissions.Request) (permissions.Decision, error) {
	f.calls++
	return f.decision, f.err
}

// fakeApprovals records intakes and serves Get from an in-memory row whose
// status can be flipped by the test to simulate a human decision.
type fakeApprovals struct {
	mu         sync.Mutex
	intakes    int
	intakeErr  error
	row        cerebrodb.CerebroApprovalRequest
	getErr     error
	getCalls   int
	lastIntake approvals.IntakeParams
}

func (f *fakeApprovals) Intake(ctx context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intakes++
	f.lastIntake = p
	if f.intakeErr != nil {
		return cerebrodb.CerebroApprovalRequest{}, f.intakeErr
	}
	f.row = cerebrodb.CerebroApprovalRequest{
		ID:          newUUID(),
		WorkspaceID: p.WorkspaceID,
		Capability:  p.Capability,
		Resource:    p.Resource,
		Status:      approvals.StatusPending,
	}
	return f.row, nil
}

func (f *fakeApprovals) Get(ctx context.Context, id, workspaceID pgtype.UUID) (cerebrodb.CerebroApprovalRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return cerebrodb.CerebroApprovalRequest{}, f.getErr
	}
	return f.row, nil
}

func (f *fakeApprovals) setStatus(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.row.Status = s
}

func newUUID() pgtype.UUID {
	var u pgtype.UUID
	u.Bytes = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	u.Valid = true
	return u
}

func baseReq() Request {
	return Request{
		Permission: permissions.Request{
			WorkspaceID: newUUID(),
			Actor:       permissions.Actor{Type: "agent", ID: newUUID()},
			Capability:  "shell.exec",
			Resource:    "curl",
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   newUUID(),
		Surface:       approvals.SurfaceSystem,
		Context:       map[string]any{"tool": "Bash", "command": "curl https://example.com"},
	}
}

// --- Evaluate ---------------------------------------------------------------

func TestEvaluate_AllowDoesNotCreateAsk(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionAllow, Reason: "grant matched"}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap}

	out, err := g.Evaluate(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeAllowed {
		t.Fatalf("got %q, want allowed", out.Outcome)
	}
	if ap.intakes != 0 {
		t.Fatalf("allow must not create an ask, got %d intakes", ap.intakes)
	}
}

func TestEvaluate_DenyDoesNotCreateAsk(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionDeny, Reason: "no matching grant"}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap}

	out, err := g.Evaluate(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied", out.Outcome)
	}
	if ap.intakes != 0 {
		t.Fatalf("deny must not create an ask, got %d intakes", ap.intakes)
	}
}

func TestEvaluate_NeedsApprovalCreatesAsk(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{
		Kind:            permissions.DecisionNeedsApproval,
		Reason:          "approval required",
		MatchedGrantIDs: []pgtype.UUID{newUUID()},
	}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap}

	out, err := g.Evaluate(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomePending {
		t.Fatalf("got %q, want pending", out.Outcome)
	}
	if !out.ApprovalID.Valid {
		t.Fatal("pending result must carry a valid ApprovalID")
	}
	if ap.intakes != 1 {
		t.Fatalf("needs_approval must create exactly one ask, got %d", ap.intakes)
	}
	// The ask must carry the agent/capability/resource context the reviewer needs.
	if ap.lastIntake.Capability != "shell.exec" || ap.lastIntake.Resource != "curl" {
		t.Fatalf("ask lost request detail: %+v", ap.lastIntake)
	}
	if ap.lastIntake.Context["command"] != "curl https://example.com" {
		t.Fatalf("ask lost context: %+v", ap.lastIntake.Context)
	}
}

// The original bug: needs_approval became a blank deny with no inbox entry.
// If intake fails the gate must surface an ERROR (fail closed loudly), never a
// silent allow and never a silent deny that hides the dropped ask.
func TestEvaluate_NeedsApprovalIntakeFailureSurfacesError(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &fakeApprovals{intakeErr: errors.New("db down")}
	g := &Gate{Resolver: res, Approvals: ap}

	out, err := g.Evaluate(context.Background(), baseReq())
	if err == nil {
		t.Fatal("intake failure must return an error so the caller fails closed")
	}
	if !out.Outcome.Stops() {
		t.Fatalf("on intake failure the outcome must stop the action, got %q", out.Outcome)
	}
}

func TestEvaluate_ResolverErrorFailsClosed(t *testing.T) {
	res := &fakeResolver{err: errors.New("grant lookup failed")}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap}

	out, err := g.Evaluate(context.Background(), baseReq())
	if err == nil {
		t.Fatal("resolver error must propagate")
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied on resolver error", out.Outcome)
	}
}

// --- Guard (synchronous wait) ----------------------------------------------

func TestGuard_ApproveContinues(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}

	// Simulate a human approving shortly after the ask is created.
	go func() {
		time.Sleep(5 * time.Millisecond)
		ap.setStatus(approvals.StatusApproved)
	}()

	out, err := g.Guard(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeApproved {
		t.Fatalf("got %q, want approved", out.Outcome)
	}
	if out.Outcome.Stops() {
		t.Fatal("approved must not stop the action")
	}
}

func TestGuard_RejectStops(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}

	go func() {
		time.Sleep(5 * time.Millisecond)
		ap.setStatus(approvals.StatusRejected)
	}()

	out, err := g.Guard(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeRejected {
		t.Fatalf("got %q, want rejected", out.Outcome)
	}
	if !out.Outcome.Stops() {
		t.Fatal("rejected must stop the action")
	}
}

func TestGuard_TimeoutStops(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &fakeApprovals{} // stays pending forever
	g := &Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 20 * time.Millisecond}

	out, err := g.Guard(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeTimedOut {
		t.Fatalf("got %q, want timed_out", out.Outcome)
	}
	if !out.Outcome.Stops() {
		t.Fatal("timed_out must stop the action")
	}
}

func TestGuard_ExpiredStops(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}

	go func() {
		time.Sleep(5 * time.Millisecond)
		ap.setStatus(approvals.StatusExpired)
	}()

	out, err := g.Guard(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeExpired {
		t.Fatalf("got %q, want expired", out.Outcome)
	}
	if !out.Outcome.Stops() {
		t.Fatal("expired must stop the action")
	}
}

func TestGuard_AllowSkipsWait(t *testing.T) {
	res := &fakeResolver{decision: permissions.Decision{Kind: permissions.DecisionAllow}}
	ap := &fakeApprovals{}
	g := &Gate{Resolver: res, Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: time.Second}

	out, err := g.Guard(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeAllowed {
		t.Fatalf("got %q, want allowed", out.Outcome)
	}
	if ap.getCalls != 0 {
		t.Fatalf("allow must not poll for a decision, got %d Get calls", ap.getCalls)
	}
}

// A delegated ask is still actionable — the waiter must keep waiting, then
// resolve when the delegate decides.
func TestAwait_DelegatedKeepsWaitingThenApproves(t *testing.T) {
	ap := &fakeApprovals{row: cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: approvals.StatusDelegated}}
	g := &Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}

	go func() {
		time.Sleep(10 * time.Millisecond)
		ap.setStatus(approvals.StatusApproved)
	}()

	out, err := g.Await(context.Background(), newUUID(), newUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != OutcomeApproved {
		t.Fatalf("got %q, want approved after delegation", out)
	}
}

func TestAwait_ContextCancelStops(t *testing.T) {
	ap := &fakeApprovals{row: cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: approvals.StatusPending}}
	g := &Gate{Approvals: ap, PollInterval: 50 * time.Millisecond, WaitTimeout: time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	out, err := g.Await(ctx, newUUID(), newUUID())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if out != OutcomeTimedOut {
		t.Fatalf("got %q, want timed_out on cancel", out)
	}
}

func TestEvaluate_NilGateFailsClosed(t *testing.T) {
	var g *Gate
	out, err := g.Evaluate(context.Background(), baseReq())
	if err == nil {
		t.Fatal("nil gate must error")
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied", out.Outcome)
	}
}
