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

	// reuse* drive FindReusable: when reuseOK is true the stored reuseRow is
	// returned as a match; reuseErr forces a lookup failure.
	reuseRow         cerebrodb.CerebroApprovalRequest
	reuseOK          bool
	reuseErr         error
	findReusableCall int
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

func (f *fakeApprovals) FindReusable(ctx context.Context, q approvals.ReusableQuery) (cerebrodb.CerebroApprovalRequest, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findReusableCall++
	if f.reuseErr != nil {
		return cerebrodb.CerebroApprovalRequest{}, false, f.reuseErr
	}
	if !f.reuseOK {
		return cerebrodb.CerebroApprovalRequest{}, false, nil
	}
	return f.reuseRow, true, nil
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

// --- EvaluateDecision / GuardDecision (caller-resolved verdict) -------------
// These power the FIR-2230 tool-policy gate: the chain resolves the verdict and
// the gate only handles the inbox + await. The Resolver is deliberately nil to
// prove it is never consulted on this path.

func TestEvaluateDecision_AllowNoAskNoResolver(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap} // no Resolver on purpose
	out, err := g.EvaluateDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionAllow, Reason: "Allowed by runtime default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeAllowed {
		t.Fatalf("got %q, want allowed", out.Outcome)
	}
	if ap.intakes != 0 {
		t.Fatalf("allow must not create an ask, got %d", ap.intakes)
	}
}

func TestEvaluateDecision_DenyNoAsk(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap}
	out, err := g.EvaluateDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionDeny, Reason: "Capped by user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied", out.Outcome)
	}
	if !out.Outcome.Stops() {
		t.Fatal("deny must stop the action")
	}
	if ap.intakes != 0 {
		t.Fatalf("deny must not create an ask, got %d", ap.intakes)
	}
}

func TestEvaluateDecision_NeedsApprovalCreatesAsk(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap}
	out, err := g.EvaluateDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "Ask by agent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomePending {
		t.Fatalf("got %q, want pending", out.Outcome)
	}
	if ap.intakes != 1 {
		t.Fatalf("needs_approval must create exactly one ask, got %d", ap.intakes)
	}
	if !out.ApprovalID.Valid {
		t.Fatal("pending result must carry a valid approval id")
	}
}

func TestEvaluateDecision_NilApprovalsFailsClosed(t *testing.T) {
	g := &Gate{} // neither Resolver nor Approvals
	out, err := g.EvaluateDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionAllow})
	if err == nil {
		t.Fatal("expected error when gate is not configured")
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied (fail closed)", out.Outcome)
	}
}

func TestGuardDecision_AskApprovedContinues(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}
	go func() {
		time.Sleep(5 * time.Millisecond)
		ap.setStatus(approvals.StatusApproved)
	}()
	out, err := g.GuardDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "Ask by agent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeApproved || out.Outcome.Stops() {
		t.Fatalf("got %q, want approved (continue)", out.Outcome)
	}
}

func TestGuardDecision_AskRejectedStops(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: 2 * time.Second}
	go func() {
		time.Sleep(5 * time.Millisecond)
		ap.setStatus(approvals.StatusRejected)
	}()
	out, err := g.GuardDecision(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "Ask by agent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeRejected || !out.Outcome.Stops() {
		t.Fatalf("got %q, want rejected (stop)", out.Outcome)
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

// --- EvaluateDecisionReusing (FIR-2586 dedup) -------------------------------

func needsApproval() permissions.Decision {
	return permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "approval required"}
}

// No reusable ask exists → behaves exactly like EvaluateDecision: create one.
func TestEvaluateDecisionReusing_NoMatchCreatesAsk(t *testing.T) {
	ap := &fakeApprovals{reuseOK: false}
	g := &Gate{Approvals: ap}

	out, err := g.EvaluateDecisionReusing(context.Background(), baseReq(), needsApproval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomePending {
		t.Fatalf("got %q, want pending", out.Outcome)
	}
	if ap.findReusableCall != 1 {
		t.Fatalf("must consult FindReusable once, got %d", ap.findReusableCall)
	}
	if ap.intakes != 1 {
		t.Fatalf("no reusable match must create exactly one ask, got %d", ap.intakes)
	}
}

// A still-pending ask for the same request → rejoin it, do NOT create a new one.
func TestEvaluateDecisionReusing_PendingMatchRejoinsNoDuplicate(t *testing.T) {
	existing := cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: approvals.StatusPending, Reason: "already asked"}
	ap := &fakeApprovals{reuseOK: true, reuseRow: existing}
	g := &Gate{Approvals: ap}

	out, err := g.EvaluateDecisionReusing(context.Background(), baseReq(), needsApproval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomePending {
		t.Fatalf("got %q, want pending", out.Outcome)
	}
	if out.ApprovalID != existing.ID {
		t.Fatal("must return the existing ask id, not a fresh one")
	}
	if ap.intakes != 0 {
		t.Fatalf("a pending match must NOT create a duplicate ask, got %d intakes", ap.intakes)
	}
}

// An ask approved after the daemon's earlier poll budget → the retry is
// honoured (allowed) instead of raising yet another ask.
func TestEvaluateDecisionReusing_ApprovedMatchAllowsRetry(t *testing.T) {
	existing := cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: approvals.StatusApproved, Reason: "granted"}
	ap := &fakeApprovals{reuseOK: true, reuseRow: existing}
	g := &Gate{Approvals: ap}

	out, err := g.EvaluateDecisionReusing(context.Background(), baseReq(), needsApproval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeApproved {
		t.Fatalf("got %q, want approved", out.Outcome)
	}
	if out.ApprovalID != existing.ID {
		t.Fatal("must return the approved ask id")
	}
	if ap.intakes != 0 {
		t.Fatalf("an approved match must NOT create a new ask, got %d intakes", ap.intakes)
	}
}

// A lookup failure must fail closed and loud, never silently deny or duplicate.
func TestEvaluateDecisionReusing_LookupErrorFailsClosed(t *testing.T) {
	ap := &fakeApprovals{reuseErr: errors.New("db down")}
	g := &Gate{Approvals: ap}

	out, err := g.EvaluateDecisionReusing(context.Background(), baseReq(), needsApproval())
	if err == nil {
		t.Fatal("lookup error must surface")
	}
	if out.Outcome != OutcomeDenied {
		t.Fatalf("got %q, want denied", out.Outcome)
	}
	if ap.intakes != 0 {
		t.Fatalf("must not create an ask on lookup failure, got %d", ap.intakes)
	}
}

// Allow/Deny decisions skip the reuse lookup entirely (no needs_approval, no ask).
func TestEvaluateDecisionReusing_AllowSkipsLookup(t *testing.T) {
	ap := &fakeApprovals{}
	g := &Gate{Approvals: ap}

	out, err := g.EvaluateDecisionReusing(context.Background(), baseReq(),
		permissions.Decision{Kind: permissions.DecisionAllow, Reason: "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Outcome != OutcomeAllowed {
		t.Fatalf("got %q, want allowed", out.Outcome)
	}
	if ap.findReusableCall != 0 {
		t.Fatalf("allow must not consult FindReusable, got %d", ap.findReusableCall)
	}
	if ap.intakes != 0 {
		t.Fatalf("allow must not create an ask, got %d", ap.intakes)
	}
}
