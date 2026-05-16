package service

// CEREBRO-PATCH(service-agent-pass-gate): JEH-1327 pre-enqueue gate for agent-pass.
//
// This file is the upstream-service-side seam for the cerebro/agentpass
// package. It lives in the service package (not in cerebro/) because the
// enqueue path that calls it is itself in service/task.go, which would
// otherwise need a forward import to a cerebro subpackage and create an
// import cycle. The pattern mirrors AutoPauseInvoker — interface here,
// concrete implementation in cerebro/.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	cerebroagentpass "github.com/multica-ai/multica/server/internal/cerebro/agentpass"
	"github.com/multica-ai/multica/server/internal/util"
)

// AgentPassGate is the seam the task service uses to consult the
// cerebro/agentpass package without importing it directly. Set on
// TaskService.AgentPass from main.go; nil-safe at every call site.
type AgentPassGate interface {
	// EvaluateEnqueue returns a deny-reason string (empty = allowed) and
	// optional spend info. SpendInfo is non-nil when a spend ceiling exists
	// and a threshold was crossed (including exhausted/block), so the service
	// can post structured threshold comments without a second round-trip.
	EvaluateEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) (denyReason string, spend *cerebroagentpass.SpendInfo, err error)
}

// blockedByAgentPass is the call-site helper used by the enqueue paths.
// Returns ("", false) when the gate is not wired or allows. Returns (reason,
// true) when the gate refuses. When a spend threshold is crossed (warn or
// degrade), a structured comment is posted on the issue and the task is
// allowed. When the ceiling is exhausted (deny), the comment is posted before
// the block is signalled.
func (s *TaskService) blockedByAgentPass(ctx context.Context, agentID, issueID pgtype.UUID) (string, bool) {
	if s == nil || s.AgentPass == nil {
		return "", false
	}
	reason, spend, err := s.AgentPass.EvaluateEnqueue(ctx, agentID, issueID)
	if err != nil {
		slog.Warn("agent-pass gate lookup failed; allowing",
			"agent_id", util.UUIDToString(agentID),
			"issue_id", util.UUIDToString(issueID),
			"error", err,
		)
		return "", false
	}
	if spend != nil && spend.Level != cerebroagentpass.SpendLevelOK {
		s.postSpendThresholdAlert(ctx, issueID, agentID, spend)
	}
	if reason == "" {
		return "", false
	}
	slog.Info("task enqueue blocked by agent-pass",
		"agent_id", util.UUIDToString(agentID),
		"issue_id", util.UUIDToString(issueID),
		"reason", reason,
	)
	return reason, true
}

// CEREBRO-PATCH(agent-pass-spend-thresholds): JEH-1327 — post structured spend-ceiling alert comment (warn 70%, degrade 90%, pause 100%).
func (s *TaskService) postSpendThresholdAlert(ctx context.Context, issueID, agentID pgtype.UUID, spend *cerebroagentpass.SpendInfo) {
	content := spendAlertComment(spend)
	s.createAgentComment(ctx, issueID, agentID, content, "system", pgtype.UUID{})
}

// spendAlertComment returns the markdown body for a spend threshold comment.
func spendAlertComment(spend *cerebroagentpass.SpendInfo) string {
	pct := spend.SpendPct()
	spentUSD := float64(spend.SpentMicros) / 1_000_000
	ceilUSD := float64(spend.CeilingMicros) / 1_000_000

	switch spend.Level {
	case cerebroagentpass.SpendLevelWarn:
		return fmt.Sprintf(
			"**Spend-advarsel: %d %% af budget brugt.**\n\nBrugt: $%.4f / $%.4f loft.\n\nVed 90 %% øges advarslen. Ved 100 %% stoppes nye opgaver, indtil loftet hæves eller nyt agent-pas udstedes.",
			pct, spentUSD, ceilUSD,
		)
	case cerebroagentpass.SpendLevelDegrade:
		return fmt.Sprintf(
			"**Spend-advarsel: %d %% af budget brugt.**\n\nBrugt: $%.4f / $%.4f loft.\n\nUnder 10 %% af budget tilbage. Nye opgaver stoppes ved 100 %%, indtil loftet hæves eller nyt agent-pas udstedes.",
			pct, spentUSD, ceilUSD,
		)
	case cerebroagentpass.SpendLevelExhausted:
		return fmt.Sprintf(
			"**Spend-loft nået: %d %% af budget brugt.**\n\nBrugt: $%.4f / $%.4f loft.\n\nDenne opgave blev blokeret. Hæv loftet eller udsted et nyt agent-pas for at fortsætte.",
			pct, spentUSD, ceilUSD,
		)
	default:
		return ""
	}
}
