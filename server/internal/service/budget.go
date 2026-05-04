package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BudgetService enforces JEH-327 cost caps at task claim time across
// three axes: workspace daily hard kill, per-agent daily cap, and
// per-user daily cap. Each axis can be configured independently;
// per-user enforcement also consults the member's
// budget_enforcement_enabled toggle (migration 9008) so an admin can
// exempt a single user without touching the workspace default.
type BudgetService struct {
	Queries *db.Queries
}

func NewBudgetService(q *db.Queries) *BudgetService {
	return &BudgetService{Queries: q}
}

// PreClaimDecision is the verdict for a single pending task. When
// Allowed is false, Reason is a short human-readable string suitable
// for surfacing as the task's cancellation reason.
type PreClaimDecision struct {
	Allowed bool
	Reason  string
}

// CheckPreClaim returns whether a task may be claimed for a given
// (workspace, agent, triggering user) tuple given today's spend.
// triggeredByUserID may be invalid (zero pgtype.UUID) for
// system-triggered tasks (autopilots, schedulers); per-user enforcement
// is then skipped while the workspace and agent axes still apply.
//
// Decision tree:
//   - workspace has no config row OR configured_at IS NULL
//     → allow (workspace owner hasn't completed setup wizard yet,
//     enforcing here would brick every existing workspace)
//   - workspace_daily_hard_kill_cents set AND today's workspace spend
//     >= cap → deny "workspace daily hard kill exceeded"
//   - agent has effective daily cap (override OR workspace default)
//     AND today's agent spend >= cap → deny "agent daily cap exceeded"
//   - triggering member exists, has budget_enforcement_enabled, and
//     has effective user daily cap with spend >= cap
//     → deny "user daily cap exceeded"
//   - otherwise → allow
func (s *BudgetService) CheckPreClaim(ctx context.Context, workspaceID, agentID, triggeredByUserID pgtype.UUID) PreClaimDecision {
	if !workspaceID.Valid {
		return PreClaimDecision{Allowed: true}
	}

	cfg, err := s.Queries.GetWorkspaceBudgetConfig(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreClaimDecision{Allowed: true}
	}
	if err != nil {
		// Don't block claims on a transient DB error — surface via
		// caller logging instead.
		return PreClaimDecision{Allowed: true}
	}
	if !cfg.ConfiguredAt.Valid {
		return PreClaimDecision{Allowed: true}
	}

	dayStart := todayUTCStart()

	if cfg.WorkspaceDailyHardKillCents.Valid {
		spent := s.spentInWindow(ctx, workspaceID, "workspace", workspaceID, "day", dayStart)
		if spent >= cfg.WorkspaceDailyHardKillCents.Int64 {
			return PreClaimDecision{Allowed: false, Reason: "workspace daily hard kill exceeded"}
		}
	}

	if agentCap := s.effectiveAgentDailyCap(ctx, workspaceID, agentID, cfg); agentCap.Valid {
		spent := s.spentInWindow(ctx, workspaceID, "agent", agentID, "day", dayStart)
		if spent >= agentCap.Int64 {
			return PreClaimDecision{Allowed: false, Reason: "agent daily cap exceeded"}
		}
	}

	if triggeredByUserID.Valid && s.userBudgetEnforced(ctx, workspaceID, triggeredByUserID) {
		if userCap := s.effectiveUserDailyCap(ctx, workspaceID, triggeredByUserID, cfg); userCap.Valid {
			spent := s.spentInWindow(ctx, workspaceID, "user", triggeredByUserID, "day", dayStart)
			if spent >= userCap.Int64 {
				return PreClaimDecision{Allowed: false, Reason: "user daily cap exceeded"}
			}
		}
	}

	return PreClaimDecision{Allowed: true}
}

// effectiveAgentDailyCap returns the agent override if present and
// set, otherwise the workspace default. Both can be NULL (Int8.Valid
// = false) which the caller treats as "no cap".
func (s *BudgetService) effectiveAgentDailyCap(ctx context.Context, workspaceID, agentID pgtype.UUID, cfg db.WorkspaceBudgetConfig) pgtype.Int8 {
	if !agentID.Valid {
		return cfg.DefaultAgentDailyCapCents
	}
	override, err := s.Queries.GetAgentBudgetOverride(ctx, db.GetAgentBudgetOverrideParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
	})
	if err == nil && override.DailyCapCents.Valid {
		return override.DailyCapCents
	}
	return cfg.DefaultAgentDailyCapCents
}

// effectiveUserDailyCap returns the user override if present and set,
// otherwise the workspace default. Both can be NULL (Int8.Valid =
// false) which the caller treats as "no cap".
func (s *BudgetService) effectiveUserDailyCap(ctx context.Context, workspaceID, userID pgtype.UUID, cfg db.WorkspaceBudgetConfig) pgtype.Int8 {
	override, err := s.Queries.GetUserBudgetOverride(ctx, db.GetUserBudgetOverrideParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err == nil && override.DailyCapCents.Valid {
		return override.DailyCapCents
	}
	return cfg.DefaultUserDailyCapCents
}

// userBudgetEnforced returns true unless the member row says otherwise.
// Missing member (deleted or cross-workspace) → enforce by default; a
// stale task tied to a removed member shouldn't bypass caps.
func (s *BudgetService) userBudgetEnforced(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return true
	}
	if !member.BudgetEnforcementEnabled {
		slog.Debug("user budget enforcement disabled by member toggle",
			"member_id", uuidToString(member.ID),
			"user_id", uuidToString(userID),
		)
	}
	return member.BudgetEnforcementEnabled
}

// spentInWindow returns 0 when no row exists or on lookup error —
// "no row" is the steady state for new (scope, window) combinations
// before the first task usage hits.
func (s *BudgetService) spentInWindow(ctx context.Context, workspaceID pgtype.UUID, scopeType string, scopeID pgtype.UUID, windowType string, windowStart pgtype.Date) int64 {
	state, err := s.Queries.GetBudgetState(ctx, db.GetBudgetStateParams{
		WorkspaceID: workspaceID,
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		WindowType:  windowType,
		WindowStart: windowStart,
	})
	if err != nil {
		return 0
	}
	return state.CentsSpent
}

func todayUTCStart() pgtype.Date {
	t := time.Now().UTC()
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	v, err := u.Value()
	if err != nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
