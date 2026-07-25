// Package permgate connects a caller-resolved permission decision to the
// approval inbox (FIR-2193).
//
// Before this package existed the resolver could return
// permissions.DecisionNeedsApproval, but nothing acted on it: in practice the
// outcome was silently dropped or turned into a blank deny, so the inbox never
// received a real ask and Pending was always empty. permgate closes that gap.
//
// The gate does three things in one place so every call site behaves
// identically:
//
//   - EvaluateDecision — accept the canonical resolver's allow/deny/ask verdict.
//   - On Ask, materialise a pending ask in the approval inbox
//     (approvals.Service.Intake) so a human sees it under /approvals.
//   - Await — block the agent's action until the human approves, rejects, or
//     the ask expires. Approve continues the action; reject/expiry stops it.
//
// The waiter polls the approval row rather than holding a channel so it works
// across processes: the runtime hook calls the HTTP gate, then long-polls the
// approval status from a different process than the one that decided it.
//
// All three states are explicit. needs_approval is NEVER converted into a deny
// without an inbox entry — that conversion was the original bug.
package permgate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
)

// Outcome is the gate's final verdict for one guarded action.
type Outcome string

const (
	// OutcomeAllowed — a grant matched and no approval was required.
	OutcomeAllowed Outcome = "allowed"
	// OutcomeDenied — no grant matched (or the resolver explicitly denied).
	OutcomeDenied Outcome = "denied"
	// OutcomePending — approval is required and an ask was created; the caller
	// must Await (or the agent must poll) for the human decision. Returned by
	// Evaluate, never by Guard (Guard always resolves to a terminal outcome).
	OutcomePending Outcome = "pending"
	// OutcomeApproved — needs_approval, and a human approved. Action continues.
	OutcomeApproved Outcome = "approved"
	// OutcomeRejected — needs_approval, and a human rejected. Action stops.
	OutcomeRejected Outcome = "rejected"
	// OutcomeExpired — the ask was not decided before its deadline. Fail-closed:
	// treat as a stop, but the audit trail shows nobody acted in time.
	OutcomeExpired Outcome = "expired"
	// OutcomeTimedOut — the caller's wait budget elapsed while the ask was still
	// pending (the ask itself may still be open in the inbox). Fail-closed.
	OutcomeTimedOut Outcome = "timed_out"
)

// Stops reports whether an outcome means the guarded action must NOT proceed.
func (o Outcome) Stops() bool {
	switch o {
	case OutcomeAllowed, OutcomeApproved:
		return false
	default:
		return true
	}
}

// Result is what Evaluate/Guard return.
type Result struct {
	Outcome    Outcome
	Decision   permissions.Decision
	ApprovalID pgtype.UUID // valid when an inbox ask was created
	Reason     string
}

// Request is the input to the gate: who is acting, through which agent, on what
// capability/resource, plus the metadata the inbox ask needs to be meaningful.
type Request struct {
	Permission permissions.Request

	// Requester identifies the principal recorded on the ask. For an agent
	// action this is the agent; for a member-initiated action it is the member.
	RequesterType string // approvals.RequesterMember | approvals.RequesterAgent
	RequesterID   pgtype.UUID

	// Surface records where the action came from (api/cli/ui/mcp/system).
	Surface string

	// Context is attached verbatim to the ask so the human reviewer sees what
	// the agent is trying to do (tool name, command, url, …).
	Context map[string]any

	// ApprovalTTL bounds how long the ask stays open before it auto-expires.
	// Zero means "use the gate's default"; a negative value means "never
	// expires".
	ApprovalTTL time.Duration
}

// Defaults for the waiter. The runtime hook overrides these via Gate fields.
const (
	DefaultPollInterval = 2 * time.Second
	DefaultWaitTimeout  = 5 * time.Minute
	DefaultApprovalTTL  = 15 * time.Minute
)

// approvalsIface is the slice of approvals.Service the gate needs.
type approvalsIface interface {
	Intake(ctx context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error)
	Get(ctx context.Context, id, workspaceID pgtype.UUID) (cerebrodb.CerebroApprovalRequest, error)
	FindReusable(ctx context.Context, q approvals.ReusableQuery) (cerebrodb.CerebroApprovalRequest, bool, error)
}

// Gate wires caller-resolved permission decisions to the approval inbox.
type Gate struct {
	Approvals approvalsIface

	// PollInterval / WaitTimeout configure Await. Zero falls back to the
	// Default* constants, so a zero-value Gate is usable.
	PollInterval time.Duration
	WaitTimeout  time.Duration
	// ApprovalTTL is the default ask lifetime when Request.ApprovalTTL is zero.
	ApprovalTTL time.Duration

	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
}

// New constructs a Gate from the approval service. Permission resolution stays
// at the feature's canonical call-time gate; this type only owns ask lifecycle.
func New(a *approvals.Service) *Gate {
	return &Gate{Approvals: a}
}

func (g *Gate) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *Gate) pollInterval() time.Duration {
	if g.PollInterval > 0 {
		return g.PollInterval
	}
	return DefaultPollInterval
}

func (g *Gate) waitTimeout() time.Duration {
	if g.WaitTimeout > 0 {
		return g.WaitTimeout
	}
	return DefaultWaitTimeout
}

// EvaluateDecision maps a decision the caller already resolved through the
// canonical permission engine to the shared inbox lifecycle. It does not block:
// on needs_approval it returns OutcomePending with a valid ApprovalID.
func (g *Gate) EvaluateDecision(ctx context.Context, req Request, decision permissions.Decision) (Result, error) {
	if g == nil || g.Approvals == nil {
		return Result{Outcome: OutcomeDenied, Reason: "gate not configured"},
			errors.New("permgate: gate not configured")
	}
	return g.resultForDecision(ctx, req, decision)
}

// resultForDecision maps a resolved Decision to a gate Result, materialising an
// inbox ask on needs_approval.
func (g *Gate) resultForDecision(ctx context.Context, req Request, decision permissions.Decision) (Result, error) {
	switch decision.Kind {
	case permissions.DecisionAllow:
		return Result{Outcome: OutcomeAllowed, Decision: decision, Reason: decision.Reason}, nil
	case permissions.DecisionDeny:
		return Result{Outcome: OutcomeDenied, Decision: decision, Reason: decision.Reason}, nil
	case permissions.DecisionNeedsApproval:
		ask, err := g.createAsk(ctx, req, decision)
		if err != nil {
			// CRITICAL: do NOT silently deny. If we cannot record the ask we
			// must surface the error so the caller fails closed loudly rather
			// than turning needs_approval into an invisible deny.
			return Result{Outcome: OutcomeDenied, Decision: decision, Reason: "could not create approval ask"}, err
		}
		return Result{Outcome: OutcomePending, Decision: decision, ApprovalID: ask.ID, Reason: decision.Reason}, nil
	default:
		// Unknown decision kind → fail closed.
		return Result{Outcome: OutcomeDenied, Decision: decision, Reason: "unknown decision"},
			errors.New("permgate: unknown decision kind")
	}
}

// EvaluateDecisionReusing is EvaluateDecision with dedup for poll-based callers.
// Before materialising a new ask for a needs_approval decision it looks for a
// still-actionable approval for the same (agent, capability, resource): a
// pending match is returned as OutcomePending with its existing id so a retry
// rejoins the SAME inbox request, and an approved, unexpired match
// short-circuits to OutcomeApproved so the retry is honoured. Only when nothing
// reusable exists does it create a fresh ask. This is what stops a retried repo
// checkout — the daemon long-polls, gives up, the agent retries — from spawning
// a duplicate inbox request each round (FIR-2586). Allow/Deny decisions are
// unchanged. Blocking callers should keep using EvaluateDecision: they hold one
// Await across the whole window, so there is no retry to dedup.
func (g *Gate) EvaluateDecisionReusing(ctx context.Context, req Request, decision permissions.Decision) (Result, error) {
	if g == nil || g.Approvals == nil {
		return Result{Outcome: OutcomeDenied, Reason: "gate not configured"},
			errors.New("permgate: gate not configured")
	}
	if decision.Kind == permissions.DecisionNeedsApproval {
		existing, ok, err := g.Approvals.FindReusable(ctx, approvals.ReusableQuery{
			WorkspaceID: req.Permission.WorkspaceID,
			Agent:       req.Permission.Agent,
			Capability:  req.Permission.Capability,
			Resource:    req.Permission.Resource,
		})
		if err != nil {
			// Fail closed loudly, like createAsk: a lookup error must not silently
			// become a deny or a duplicate.
			return Result{Outcome: OutcomeDenied, Decision: decision, Reason: "could not look up existing approval"}, err
		}
		if ok {
			if existing.Status == approvals.StatusApproved {
				return Result{Outcome: OutcomeApproved, Decision: decision, ApprovalID: existing.ID, Reason: existing.Reason}, nil
			}
			return Result{Outcome: OutcomePending, Decision: decision, ApprovalID: existing.ID, Reason: existing.Reason}, nil
		}
	}
	return g.resultForDecision(ctx, req, decision)
}

// GuardDecision is the synchronous caller-resolved path: EvaluateDecision, then
// Await the human if the result is pending. The runtime tool gate uses it when
// the per-tool policy chain (not the capability resolver) produced the verdict,
// so a tool-policy "Ask" pauses the agent and lands in the same inbox as a
// resolver-driven needs_approval. Like Guard it never returns OutcomePending.
func (g *Gate) GuardDecision(ctx context.Context, req Request, decision permissions.Decision) (Result, error) {
	res, err := g.EvaluateDecision(ctx, req, decision)
	if err != nil || res.Outcome != OutcomePending {
		return res, err
	}
	outcome, werr := g.Await(ctx, req.Permission.WorkspaceID, res.ApprovalID)
	res.Outcome = outcome
	return res, werr
}

// GuardDecisionReusing is GuardDecision with the FindReusable dedup +
// short-circuit: EvaluateDecisionReusing first, then Await if the result is
// pending. A still-valid prior grant — an approved, unexpired row for the same
// (agent, capability, resource) — short-circuits to OutcomeApproved with no new
// ask, so a time-boxed "approve for a period" grant covers subsequent in-process
// calls (TECH-3498). Like Guard it never returns OutcomePending.
func (g *Gate) GuardDecisionReusing(ctx context.Context, req Request, decision permissions.Decision) (Result, error) {
	res, err := g.EvaluateDecisionReusing(ctx, req, decision)
	if err != nil || res.Outcome != OutcomePending {
		return res, err
	}
	outcome, werr := g.Await(ctx, req.Permission.WorkspaceID, res.ApprovalID)
	res.Outcome = outcome
	return res, werr
}

// Await blocks until the approval row reaches a terminal status, the wait
// budget elapses, or ctx is cancelled. It is process-independent: any decider
// (a human in the inbox, the expiry sweeper) flips the row and Await observes
// it on the next poll.
func (g *Gate) Await(ctx context.Context, workspaceID, approvalID pgtype.UUID) (Outcome, error) {
	if g == nil || g.Approvals == nil {
		return OutcomeDenied, errors.New("permgate: gate not configured")
	}
	deadline := g.clock().Add(g.waitTimeout())
	ticker := time.NewTicker(g.pollInterval())
	defer ticker.Stop()

	for {
		ask, err := g.Approvals.Get(ctx, approvalID, workspaceID)
		if err != nil {
			// A transient read error is not fatal; retry on the next tick. The
			// deadline check below bounds sustained failure.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return OutcomeTimedOut, err
			}
		} else if outcome, terminal := outcomeForStatus(ask.Status); terminal {
			return outcome, nil
		}

		if !g.clock().Before(deadline) {
			return OutcomeTimedOut, nil
		}
		select {
		case <-ctx.Done():
			return OutcomeTimedOut, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Status reports an approval's current outcome in a single, non-blocking read:
// OutcomePending while it is still open, or the terminal outcome once decided.
// The repo-checkout poll endpoint calls it on each daemon tick instead of
// holding the HTTP connection — the daemon's client has a short timeout, so the
// server cannot Await across the whole approval window the way an in-process
// caller can. (FIR-2586)
func (g *Gate) Status(ctx context.Context, workspaceID, approvalID pgtype.UUID) (Outcome, error) {
	if g == nil || g.Approvals == nil {
		return OutcomeDenied, errors.New("permgate: gate not configured")
	}
	ask, err := g.Approvals.Get(ctx, approvalID, workspaceID)
	if err != nil {
		return OutcomePending, err
	}
	outcome, _ := outcomeForStatus(ask.Status)
	return outcome, nil
}

// outcomeForStatus maps an approval row status to a gate outcome. The boolean
// reports whether the status is terminal (the waiter can stop).
func outcomeForStatus(status string) (Outcome, bool) {
	switch status {
	case approvals.StatusApproved:
		return OutcomeApproved, true
	case approvals.StatusRejected:
		return OutcomeRejected, true
	case approvals.StatusExpired:
		return OutcomeExpired, true
	case approvals.StatusCancelled:
		// A cancelled ask means the action should not proceed.
		return OutcomeDenied, true
	case approvals.StatusPending, approvals.StatusDelegated:
		// Delegated asks are still actionable — keep waiting.
		return OutcomePending, false
	default:
		return OutcomePending, false
	}
}

func (g *Gate) createAsk(ctx context.Context, req Request, decision permissions.Decision) (cerebrodb.CerebroApprovalRequest, error) {
	ttl := req.ApprovalTTL
	if ttl == 0 {
		ttl = g.ApprovalTTL
	}
	if ttl == 0 {
		ttl = DefaultApprovalTTL
	}
	var expiresAt pgtype.Timestamptz
	if ttl > 0 {
		expiresAt = pgtype.Timestamptz{Time: g.clock().Add(ttl), Valid: true}
	}

	requesterType := req.RequesterType
	if requesterType == "" {
		requesterType = req.Permission.Actor.Type
	}
	requesterID := req.RequesterID
	if !requesterID.Valid {
		requesterID = req.Permission.Actor.ID
	}

	return g.Approvals.Intake(ctx, approvals.IntakeParams{
		WorkspaceID:     req.Permission.WorkspaceID,
		RequesterType:   requesterType,
		RequesterID:     requesterID,
		Agent:           req.Permission.Agent,
		Capability:      req.Permission.Capability,
		Resource:        req.Permission.Resource,
		Reason:          decision.Reason,
		MatchedGrantIDs: decision.MatchedGrantIDs,
		Context:         req.Context,
		ExpiresAt:       expiresAt,
		Surface:         req.Surface,
	})
}
