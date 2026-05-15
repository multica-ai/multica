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
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// AgentPassGate is the seam the task service uses to consult the
// cerebro/agentpass package without importing it directly. Set on
// TaskService.AgentPass from main.go; nil-safe at every call site.
type AgentPassGate interface {
	// BlockEnqueue returns a stable deny-reason token when the gate
	// refuses the (agent, issue) pair, or "" when it allows. The
	// concrete cerebro/agentpass.Service implements this on top of
	// its richer Result type; only the stable string crosses the
	// interface boundary so the upstream service package never sees
	// the cerebro Decision enum.
	BlockEnqueue(ctx context.Context, agentID, issueID pgtype.UUID) (denyReason string)
}

// blockedByAgentPass is the call-site helper used by the enqueue paths.
// Returns ("", false) when:
//   - the gate is not wired (nil) — keeps existing flows untouched until
//     cerebro is fully rolled out;
//   - the gate allows.
//
// Returns (reason, true) when the gate refuses. Callers wrap the reason
// in a user-facing error and log it at the call site.
func (s *TaskService) blockedByAgentPass(ctx context.Context, agentID, issueID pgtype.UUID) (string, bool) {
	if s == nil || s.AgentPass == nil {
		return "", false
	}
	reason := s.AgentPass.BlockEnqueue(ctx, agentID, issueID)
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
