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
// Milestones:
//   - 1 (PR #355): table, lookup, pre-enqueue gate (expiry).
//   - 2 (PR #372): permission downscoping — sub-issue can only narrow parent scope.
//   - 3 (this PR): spend-ceiling thresholds: warn 70 %, degrade 90 %, pause 100 %.
package agentpass

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
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

	// DecisionDenyExhausted means the accumulated spend across the issue
	// tree has reached the pass ceiling. The gate also flips the pass
	// status to 'exhausted' so the same lookup short-circuits next time.
	DecisionDenyExhausted Decision = "deny_exhausted"
)

// SpendLevel categorises how close the issue tree is to its spend ceiling.
type SpendLevel string

const (
	SpendLevelOK        SpendLevel = "ok"        // below 70 %
	SpendLevelWarn      SpendLevel = "warn"       // 70–89 %
	SpendLevelDegrade   SpendLevel = "degrade"    // 90–99 %
	SpendLevelExhausted SpendLevel = "exhausted"  // ≥ 100 %
)

// SpendInfo is populated whenever a pass has a spend ceiling and the gate
// evaluated the current spend. It is non-nil even for allow decisions so
// that callers can post threshold alerts without a second round-trip.
type SpendInfo struct {
	Level         SpendLevel
	SpentMicros   int64
	CeilingMicros int64
}

// Result carries both the decision and the pass that produced it (nil
// when no pass was found). Callers log the pass ID so an operator can
// inspect what blocked a given task.
type Result struct {
	Decision  Decision
	Pass      *cerebrodb.CerebroAgentPass
	SpendInfo *SpendInfo // non-nil when a ceiling exists and was evaluated
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
	GetExhaustedAgentPassForAgentIssue(ctx context.Context, arg cerebrodb.GetActiveAgentPassForAgentIssueParams) (cerebrodb.CerebroAgentPass, error)
	MarkAgentPassStatus(ctx context.Context, arg cerebrodb.MarkAgentPassStatusParams) (cerebrodb.CerebroAgentPass, error)
	GetCerebroAgentPass(ctx context.Context, id pgtype.UUID) (cerebrodb.CerebroAgentPass, error)
	SumIssueTreeUsageByModel(ctx context.Context, id pgtype.UUID) ([]cerebrodb.SumIssueTreeUsageByModelRow, error)
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

	passParams := cerebrodb.GetActiveAgentPassForAgentIssueParams{AgentID: agentID, IssueID: issueID}
	pass, err := s.queries.GetActiveAgentPassForAgentIssue(ctx, passParams)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No active pass. Check for a previously exhausted pass — if one
			// exists, subsequent enqueues must still be blocked until the
			// ceiling is raised or a new pass is issued.
			exhausted, exErr := s.queries.GetExhaustedAgentPassForAgentIssue(ctx, passParams)
			if exErr != nil {
				if errors.Is(exErr, pgx.ErrNoRows) {
					return Result{Decision: DecisionAllow}, nil
				}
				return Result{Decision: DecisionAllow}, fmt.Errorf("lookup exhausted pass: %w", exErr)
			}
			return Result{Decision: DecisionDenyExhausted, Pass: &exhausted}, nil
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

	// Spend-ceiling check (JEH-1327 milestone 3).
	if pass.SpendCeilingMicros.Valid && pass.SpendCeilingMicros.Int64 > 0 {
		spentMicros, spendErr := s.sumIssueTreeSpendMicros(ctx, pass.IssueID)
		if spendErr != nil {
			// Default-allow on spend-query error. Logged so an operator can
			// correlate with the slog warning at the call site.
			return Result{Decision: DecisionAllow, Pass: &pass}, fmt.Errorf("compute spend: %w", spendErr)
		}
		level := classifySpendLevel(spentMicros, pass.SpendCeilingMicros.Int64)
		spend := &SpendInfo{
			Level:         level,
			SpentMicros:   spentMicros,
			CeilingMicros: pass.SpendCeilingMicros.Int64,
		}
		if level == SpendLevelExhausted {
			if _, mErr := s.queries.MarkAgentPassStatus(ctx, cerebrodb.MarkAgentPassStatusParams{
				ID:     pass.ID,
				Status: StatusExhausted,
			}); mErr != nil && !errors.Is(mErr, pgx.ErrNoRows) {
				return Result{Decision: DecisionDenyExhausted, Pass: &pass, SpendInfo: spend},
					fmt.Errorf("mark exhausted: %w", mErr)
			}
			pass.Status = StatusExhausted
			return Result{Decision: DecisionDenyExhausted, Pass: &pass, SpendInfo: spend}, nil
		}
		return Result{Decision: DecisionAllow, Pass: &pass, SpendInfo: spend}, nil
	}

	return Result{Decision: DecisionAllow, Pass: &pass}, nil
}

// sumIssueTreeSpendMicros returns the total accumulated cost in micro-USD for
// all task_usage rows whose tasks belong to the issue subtree rooted at issueID.
// Token counts are priced per-model using the pkg/pricing table.
func (s *Service) sumIssueTreeSpendMicros(ctx context.Context, issueID pgtype.UUID) (int64, error) {
	rows, err := s.queries.SumIssueTreeUsageByModel(ctx, issueID)
	if err != nil {
		return 0, err
	}
	var totalCents int64
	for _, row := range rows {
		totalCents += pricing.ComputeCents(row.Model, pricing.Usage{
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
		})
	}
	// 1 USD-cent = 10 000 micro-USD (1 USD = 100 cents = 1 000 000 micros).
	return totalCents * 10_000, nil
}

// classifySpendLevel maps (spent, ceiling) to a SpendLevel threshold.
func classifySpendLevel(spentMicros, ceilingMicros int64) SpendLevel {
	if ceilingMicros <= 0 {
		return SpendLevelOK
	}
	pct := float64(spentMicros) / float64(ceilingMicros)
	switch {
	case pct >= 1.0:
		return SpendLevelExhausted
	case pct >= 0.90:
		return SpendLevelDegrade
	case pct >= 0.70:
		return SpendLevelWarn
	default:
		return SpendLevelOK
	}
}

// SpendPct returns the spend as an integer percentage (rounded up, capped at 999).
func (si *SpendInfo) SpendPct() int {
	if si == nil || si.CeilingMicros <= 0 {
		return 0
	}
	pct := math.Ceil(float64(si.SpentMicros) / float64(si.CeilingMicros) * 100)
	if pct > 999 {
		pct = 999
	}
	return int(pct)
}

// BlockEnqueue is kept for callers that only need a block/allow signal.
// Returns "" when the gate allows, or a stable deny-reason token otherwise.
// Prefer EvaluateEnqueue when the caller also needs spend threshold info.
func (s *Service) BlockEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) string {
	res, err := s.EvaluateForEnqueue(ctx, agentID, issueID)
	if err != nil {
		slog.Warn("agent-pass gate lookup failed; allowing", "error", err)
		return ""
	}
	return res.DenyReason()
}

// EvaluateEnqueue is the primary gate method used by service.AgentPassGate.
// It returns both the deny reason (empty = allowed) and any spend threshold
// info so the service layer can post structured comments without a second
// round-trip.
func (s *Service) EvaluateEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) (string, *SpendInfo, error) {
	res, err := s.EvaluateForEnqueue(ctx, agentID, issueID)
	if err != nil {
		slog.Warn("agent-pass gate lookup failed; allowing", "error", err)
		return "", res.SpendInfo, err
	}
	return res.DenyReason(), res.SpendInfo, nil
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
