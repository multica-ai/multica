package handler

// CEREBRO-PATCH(daemon-tool-policy-cerebro): TECH-3173 — per-tool enforcement for
// LOCAL CLI runtimes (Claude, Codex, Cursor, Gemini) spawned by the daemon.
//
// The gateway executor already gates every tool call through the unified
// tool-policy chain (guardToolCallViaPolicy). Local CLIs run on a Mac and never
// pass through that executor, so an Ask/Deny row in the permission table never
// reaches them. This file is the daemon-side seam that closes that gap: a local
// runtime's PreToolUse hook (Claude) or native approval channel (Codex) calls
// ResolveDaemonToolPolicy, which resolves the SAME toolpolicy.Store chain and,
// on Ask, raises an approval in the SAME inbox the gateway gate uses — one
// model, not a parallel one.
//
// Staged rollout (the security requirement) is driven by WORKSPACE SETTINGS —
// the cerebro feature-flag model, the same place cerebro_approval_gate lives —
// NOT an environment variable, so an admin stages it from Settings → Features
// with no daemon or server restart (Jesper's requirement, TECH-3173):
//   - cerebro_local_tool_policy         off → ModeOff (default, no behaviour change)
//   - cerebro_local_tool_policy on, ..._enforce off → ModeObserve (resolve + log, allow)
//   - cerebro_local_tool_policy on, ..._enforce on  → ModeEnforce (act on the verdict)
// The pure off/observe/enforce decision lives in
// server/internal/cerebro/localtoolpolicy; this file is only the IO seam
// (read the settings stage, resolve, raise the inbox ask), mirroring
// repo_approval_cerebro.go.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/localtoolpolicy"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// daemonToolPolicyResolveRequest is the body a local-runtime seam posts for one
// tool call. tool_name is the policy key (the CLI tool name, e.g. "Bash" or an
// "mcp__server__action"); resource_pattern is the optional per-resource scope
// (a shell binary, a file path, a URL) the chain can refine on; args is
// informational context attached to the inbox ask, never part of the decision.
type daemonToolPolicyResolveRequest struct {
	AgentID         string         `json:"agent_id"`
	ToolName        string         `json:"tool_name"`
	ResourcePattern string         `json:"resource_pattern"`
	Args            map[string]any `json:"args"`
}

// ResolveDaemonToolPolicy resolves one local-runtime tool call against the
// unified per-tool chain and reports the decision the hook must act on.
//
// POST /api/daemon/workspaces/{workspaceId}/tool-policy/resolve
//
// Response: { allowed, decision, mode, enforced, would_block, observed, reason,
// approval_id? }. On an enforce-stage Ask the daemon long-polls
// PollDaemonToolPolicyApproval with approval_id until it reaches a terminal
// status, exactly as the repo-checkout flow does.
func (h *Handler) ResolveDaemonToolPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	var req daemonToolPolicyResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ToolName) == "" {
		writeError(w, http.StatusBadRequest, "tool_name is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Rollout stage comes from workspace settings (the cerebro feature-flag
	// model), resolved server-side — an admin stages off → observe → enforce
	// from Settings → Features with no daemon or server restart (TECH-3173).
	mode := h.localToolPolicyMode(r.Context(), wsUUID)

	// Off is the default and the fast path: never resolve, never log, never
	// touch the inbox — a local CLI spawns exactly as it did before this feature.
	if mode == localtoolpolicy.ModeOff {
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed": true, "decision": string(localtoolpolicy.KindAllow),
			"mode": string(mode), "enforced": false, "would_block": false,
		})
		return
	}

	agentID := pgtype.UUID{}
	if req.AgentID != "" {
		parsed, parseErr := util.ParseUUID(req.AgentID)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
		agentID = parsed
	}

	// The chain's Runtime and User/Group layers key on the agent's runtime and
	// owner. Resolve them server-side from the agent row (never trust the
	// caller), exactly as guardToolCallViaPolicy does. A lookup failure fails
	// closed under enforce so a missing user-ceiling can't under-enforce; under
	// observe we log and resolve with what we have, since a dry run must never
	// stall work.
	var runtimeID, ownerID pgtype.UUID
	if agentID.Valid {
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil {
			if mode == localtoolpolicy.ModeEnforce {
				slog.Warn("local tool-policy: agent lookup failed — failing closed",
					"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", err)
				writeJSON(w, http.StatusOK, map[string]any{
					"allowed": false, "decision": string(localtoolpolicy.KindDeny),
					"mode": string(mode), "enforced": true, "would_block": true,
					"reason": "agent lookup failed",
				})
				return
			}
			slog.Warn("local tool-policy observe: agent lookup failed — resolving without runtime/owner",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", err)
		} else {
			runtimeID = agent.RuntimeID
			ownerID = agent.OwnerID
		}
	}

	eff, err := toolpolicy.NewStoreFromQueries(h.CerebroQueries).Resolve(r.Context(), toolpolicy.Query{
		WorkspaceID:     wsUUID,
		ToolKey:         req.ToolName,
		ResourcePattern: req.ResourcePattern,
		RuntimeID:       runtimeID,
		AgentID:         agentID,
		UserID:          ownerID,
		Base:            toolpolicy.SettingAllow,
	})
	if err != nil {
		slog.Error("local tool-policy resolve failed",
			"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", err)
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}

	decision := localtoolpolicy.Decide(mode, eff)

	// Enforce-stage Ask: raise (or rejoin) one shared-inbox approval and return
	// its decision + id so the daemon can long-poll. Gate off while enforce is on
	// is a misconfiguration — fail closed rather than silently allow.
	if decision.NeedsApproval() {
		if h.ApprovalGate == nil {
			slog.Warn("local tool-policy enforce: Ask verdict but approval gate disabled — failing closed",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName)
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": false, "decision": string(localtoolpolicy.KindDeny),
				"mode": string(mode), "enforced": true, "would_block": true,
				"reason": "approval gate disabled",
			})
			return
		}
		allowed, askDecision, approvalID, askErr := h.toolPolicyAsk(r.Context(), wsUUID, agentID, req.ToolName, req.ResourcePattern, eff.Reason, req.Args)
		if askErr != nil {
			slog.Error("local tool-policy approval ask failed",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", askErr)
			writeError(w, http.StatusInternalServerError, "could not create approval request")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed":     allowed,
			"decision":    askDecision,
			"approval_id": util.UUIDToString(approvalID),
			"mode":        string(mode),
			"enforced":    true,
			"would_block": true,
			"observed":    string(eff.Setting),
			"reason":      eff.Reason,
		})
		return
	}

	// Observe dry run: surface what an enforce would have blocked so an operator
	// can watch a machine's would-block stream before flipping to enforce.
	if mode == localtoolpolicy.ModeObserve && decision.WouldBlock {
		slog.Info("local tool-policy OBSERVE would-block",
			"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName,
			"resource", req.ResourcePattern, "setting", string(eff.Setting), "reason", eff.Reason)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":     decision.Allowed,
		"decision":    string(decision.Kind),
		"mode":        string(mode),
		"enforced":    decision.Enforced,
		"would_block": decision.WouldBlock,
		"observed":    string(eff.Setting),
		"reason":      eff.Reason,
	})
}

// localToolPolicyMode resolves the staged-rollout Mode for a workspace from the
// cerebro feature-flag settings (TECH-3173) — NOT an env var, so the stage is
// changed from Settings → Features with no restart. cerebro_local_tool_policy is
// the on/off master; cerebro_local_tool_policy_enforce flips observe→enforce once
// on. Both default OFF, and a workspace-flag lookup failure folds to OFF, so the
// endpoint is fail-safe to "no behaviour change".
func (h *Handler) localToolPolicyMode(ctx context.Context, wsID pgtype.UUID) localtoolpolicy.Mode {
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, wsID)
	if err != nil {
		slog.Warn("local tool-policy: workspace flag lookup failed — staying OFF",
			"workspace_id", util.UUIDToString(wsID), "error", err)
		return localtoolpolicy.ModeOff
	}
	var enabled, enforce bool
	for _, row := range rows {
		switch row.FlagKey {
		case "cerebro_local_tool_policy":
			enabled = row.Enabled
		case "cerebro_local_tool_policy_enforce":
			enforce = row.Enabled
		}
	}
	return localtoolpolicy.ModeFromFlags(enabled, enforce)
}

// toolPolicyAsk raises a local-runtime tool call that hit "Ask" against the
// shared inbox and reports the outcome the daemon should act on. It uses
// EvaluateDecisionReusing (non-blocking) so the daemon's short-timeout HTTP
// request is never held open and a retried tool call rejoins its open ask
// instead of piling up duplicates — the same machinery and rejoining semantics
// as repoCheckoutAsk. The requester is the agent making the call; the resource
// is the tool's resource pattern (binary / path / URL).
func (h *Handler) toolPolicyAsk(ctx context.Context, wsID, agentID pgtype.UUID, toolName, resourcePattern, reason string, args map[string]any) (allowed bool, decision string, approvalID pgtype.UUID, err error) {
	req := permgate.Request{
		Permission: permissions.Request{
			WorkspaceID: wsID,
			Actor:       permissions.Actor{Type: "agent", ID: agentID},
			Agent:       agentID,
			Capability:  toolName,
			Resource:    resourcePattern,
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   agentID,
		Surface:       approvals.SurfaceSystem,
		Context: map[string]any{
			"tool_name": toolName,
			"resource":  resourcePattern,
			"args":      args,
		},
	}
	res, err := h.ApprovalGate.EvaluateDecisionReusing(ctx, req, permissions.Decision{
		Kind:   permissions.DecisionNeedsApproval,
		Reason: reason,
	})
	if err != nil {
		return false, "", pgtype.UUID{}, err
	}
	switch res.Outcome {
	case permgate.OutcomeApproved, permgate.OutcomeAllowed:
		return true, "allow", res.ApprovalID, nil
	case permgate.OutcomePending:
		return false, "pending", res.ApprovalID, nil
	default:
		return false, "deny", res.ApprovalID, nil
	}
}

// PollDaemonToolPolicyApproval reports the current status of a local-runtime
// tool approval so the daemon hook can long-poll it. Single-shot and
// non-blocking; it reuses the repo-checkout outcome mapping (pending stays
// pending, approved → allow, every other terminal → deny / fail-closed).
//
// GET /api/daemon/workspaces/{workspaceId}/tool-policy/resolve/{approvalId}
func (h *Handler) PollDaemonToolPolicyApproval(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	approvalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "approvalId"), "approval_id")
	if !ok {
		return
	}
	if h.ApprovalGate == nil {
		writeJSON(w, http.StatusOK, map[string]any{"allowed": false, "decision": "deny", "reason": "approval gate disabled"})
		return
	}

	outcome, err := h.ApprovalGate.Status(r.Context(), wsUUID, approvalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approval status lookup failed")
		return
	}

	allowed, decision, reason := repoApprovalOutcomeToAnswer(outcome)
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":  allowed,
		"decision": decision,
		"reason":   reason,
	})
}
