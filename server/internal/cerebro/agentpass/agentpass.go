// Package agentpass implements the agent-pass control plane for JEH-1327.
//
// An agent-pass is an explicit, machine-readable mandate that "agent A may
// act on issue I (or its subtree) within scope S, up to spend ceiling C,
// until expiresAt T". The package owns:
//
//   - the gate that decides whether a queued/claimed task is allowed to
//     run, given the active pass for (agent, issue) at evaluation time;
//   - the lifecycle transitions that mark a pass expired/exhausted when
//     the gate observes the terminal condition.
//
// This is the FIRST milestone of JEH-1327: the table, the lookup, and the
// pre-enqueue gate. Downscoping enforcement (sub-issue narrows parent
// scope) and spend-aware decisions (70/90/100 % thresholds) are explicit
// out-of-scope follow-ups — see the issue.
package agentpass

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Pass status values mirror the cerebro_agent_pass CHECK constraint.
const (
	StatusActive    = "active"
	StatusRevoked   = "revoked"
	StatusExpired   = "expired"
	StatusExhausted = "exhausted"
)

// Issuer subject types stored in cerebro_agent_pass.issuer_type.
const (
	IssuerMember = "member"
	IssuerAgent  = "agent"
	IssuerSystem = "system"
)

// Decision is the outcome of an agent-pass evaluation.
type Decision string

const (
	// DecisionAllow means either no pass exists (default-allow during
	// rollout) or the pass is active and within its bounds.
	DecisionAllow Decision = "allow"

	// DecisionDenyExpired means a pass exists but its expires_at is in
	// the past at evaluation time. The gate also flips the pass status
	// to 'expired' so the same lookup short-circuits next time.
	DecisionDenyExpired Decision = "deny_expired"

	// DecisionDenyExhausted is reserved for the spend-ceiling check that
	// lands in a follow-up PR. The gate code path is in place so callers
	// can already pattern-match on it.
	DecisionDenyExhausted Decision = "deny_exhausted"
)

// Result carries both the decision and the pass that produced it (nil
// when no pass was found). Callers log the pass ID so an operator can
// inspect what blocked a given task.
type Result struct {
	Decision Decision
	Pass     *cerebrodb.CerebroAgentPass
}

// Errors surfaced by the service. Keep the set minimal so callers can
// distinguish "no pass exists" (sql.ErrNoRows is treated as allow) from
// real failures.
var (
	ErrNilQueries = errors.New("agentpass: cerebrodb queries are nil")
)

// Now is an injectable time source so tests can advance the clock past
// expires_at without sleeping. Production code never overrides it.
type Now func() time.Time

// Queries is the minimal cerebrodb surface the agent-pass gate needs.
// Defining it as an interface lets tests substitute a fake without
// spinning up Postgres. *cerebrodb.Queries satisfies it implicitly.
type Queries interface {
	GetActiveAgentPassForAgentIssue(ctx context.Context, arg cerebrodb.GetActiveAgentPassForAgentIssueParams) (cerebrodb.CerebroAgentPass, error)
	MarkAgentPassStatus(ctx context.Context, arg cerebrodb.MarkAgentPassStatusParams) (cerebrodb.CerebroAgentPass, error)
}

// Service is the gate + lifecycle owner. Construct with New; the
// dependencies are explicit so we can wire stubs in tests.
type Service struct {
	queries Queries
	now     Now
}

// New constructs a Service. The now function defaults to time.Now when
// nil — production wiring just passes a nil now.
func New(queries Queries, now Now) (*Service, error) {
	if queries == nil {
		return nil, ErrNilQueries
	}
	if now == nil {
		now = time.Now
	}
	return &Service{queries: queries, now: now}, nil
}

// EvaluateForEnqueue is the pre-enqueue gate. Returns DecisionAllow when
// no pass is found (default-allow during rollout), or when the pass is
// active and within bounds. Returns a Deny* decision otherwise; in that
// case the pass status is also flipped to the corresponding terminal
// state so the same lookup short-circuits on the next call.
//
// Caller contract:
//
//   - issueID may be zero (pgtype.UUID{Valid:false}). Chat tasks have no
//     issue; the gate returns DecisionAllow for them so chat is never
//     blocked by an issue-bound pass.
//   - The function must NOT return an error to abort the caller's flow;
//     transient DB hiccups are logged at the call site and treated as
//     "allow". The error return is reserved for programmer errors
//     (nil queries) that should panic in dev.
func (s *Service) EvaluateForEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) (Result, error) {
	if s == nil || s.queries == nil {
		return Result{Decision: DecisionAllow}, ErrNilQueries
	}
	if !agentID.Valid || !issueID.Valid {
		return Result{Decision: DecisionAllow}, nil
	}

	pass, err := s.queries.GetActiveAgentPassForAgentIssue(ctx, cerebrodb.GetActiveAgentPassForAgentIssueParams{
		AgentID: agentID,
		IssueID: issueID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{Decision: DecisionAllow}, nil
		}
		return Result{Decision: DecisionAllow}, fmt.Errorf("lookup active pass: %w", err)
	}

	if pass.ExpiresAt.Valid && !pass.ExpiresAt.Time.After(s.now()) {
		// Pass expired. Flip status so future lookups skip the row,
		// then deny. The MarkAgentPassStatus update is a no-op if a
		// concurrent revoke already moved the row out of 'active'.
		if _, mErr := s.queries.MarkAgentPassStatus(ctx, cerebrodb.MarkAgentPassStatusParams{
			ID:     pass.ID,
			Status: StatusExpired,
		}); mErr != nil && !errors.Is(mErr, pgx.ErrNoRows) {
			// Surface the error to the caller — but the decision
			// is still deny: a pass we know is expired must not
			// gate-allow just because the status flip failed.
			return Result{Decision: DecisionDenyExpired, Pass: &pass}, fmt.Errorf("mark expired: %w", mErr)
		}
		pass.Status = StatusExpired
		return Result{Decision: DecisionDenyExpired, Pass: &pass}, nil
	}

	// TODO(JEH-1327 follow-up): spend-ceiling check.
	//
	// When SpendCeilingMicros is set and the accumulated cost across
	// (agent, issue-subtree) reaches the ceiling, return
	// DecisionDenyExhausted and flip status to 'exhausted'. The query
	// joins task_usage_daily_rollup on issue_id IN (descendants of
	// pass.IssueID). Tracked separately to keep this PR focused on
	// schema + decision point.

	return Result{Decision: DecisionAllow, Pass: &pass}, nil
}

// BlockEnqueue is the cross-package seam used by the upstream service
// package via the service.AgentPassGate interface. Returns "" when the
// gate allows (no pass, or pass within bounds), or a stable token
// otherwise. A transient DB hiccup on the lookup is logged here and
// treated as allow, so the gate cannot silently block all enqueues
// during a Postgres blip.
func (s *Service) BlockEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) string {
	res, err := s.EvaluateForEnqueue(ctx, agentID, issueID)
	if err != nil {
		// Default-allow on lookup error. Logged at WARN so an
		// operator can correlate with the slog warning at the
		// EvaluateForEnqueue call site.
		slog.Warn("agent-pass gate lookup failed; allowing", "error", err)
		return ""
	}
	return res.DenyReason()
}

// DenyReason returns a short stable token describing why a Result is a
// deny. Callers use this in slog fields and in the structured comment
// posted on the blocked issue. Returns the empty string for allow.
func (r Result) DenyReason() string {
	switch r.Decision {
	case DecisionDenyExpired:
		return "agent_pass_expired"
	case DecisionDenyExhausted:
		return "agent_pass_spend_exhausted"
	default:
		return ""
	}
}
