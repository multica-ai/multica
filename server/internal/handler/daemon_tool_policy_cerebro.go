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
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

	// CEREBRO-PATCH(daemon-tool-policy-cerebro): TECH-2563 — resolve under the
	// canonical capability key, not the bare Claude tool name. The permissions
	// screen authors a built-in-tool Deny under "tools:<Name>" (claudehook
	// .PolicyToolKey); a bare-name lookup never matched it, so a Deny on Bash/
	// WebFetch/Edit silently let the tool through on local runtimes.
	store := toolpolicy.NewStoreFromQueries(h.CerebroQueries)
	toolKey := claudehook.PolicyToolKey(req.ToolName)
	// Capability-wide row (resource_pattern "") — the shape an "All tools" Deny is
	// written under, and the same shape the gateway gate resolves. A concrete
	// resource (a Bash binary, a WebFetch URL) additionally resolves its exact
	// pattern and may only TIGHTEN the capability-wide verdict, never loosen it.
	// The request attributes a Condition (the WHEN layer) is matched against:
	// the host the call targets, parsed from the resource it names. A rule with
	// a host-allowlist Condition only bites when the call's host is on the list;
	// rows without a Condition (every row today) ignore this entirely.
	// The action verb is derived from the canonical tool key so an action-scoped
	// Condition can bite once repo/credential capabilities resolve through this
	// chain under a verbed dotted key ("repo.checkout" → "checkout"). The keys this
	// gate sees today are "tools:<Name>" and "mcp__…", which carry no verbed
	// namespace, so ActionOf yields "" and no Actions term matches — the derivation
	// is wired and inert until a verbed key flows here, never altering behaviour.
	reqCtx := toolpolicy.RequestContext{
		Host:   toolpolicy.HostOf(req.ResourcePattern),
		Action: toolpolicy.ActionOf(toolKey),
	}
	// The CEL evaluator for the Expr escape hatch is injected only when the
	// default-OFF cerebro_policy_cel flag is on for the workspace; otherwise Eval
	// stays nil and an Expr-bearing row is undecidable and fails closed by effect.
	// No row carries an Expr today, so this is behaviour-preserving until enabled.
	// Resolved once and reused across the capability-wide and per-resource calls.
	celEval := h.daemonPolicyCELEvaluator(r.Context(), wsUUID)
	// This is the local-runtime twin of the gateway's GENERAL tool-policy gate, so
	// it resolves through ResolveGeneral behind the default-on cerebro_member_override
	// flag: on → the member-override model (a member may loosen an inherited group/
	// workspace default), off → identical to the tighten-only Resolve. Resolved once
	// and reused across the capability-wide and per-resource calls so both see the
	// same model. The deny-by-default agent-browser sandbox gate below keeps calling
	// Resolve directly and never consults this flag.
	memberOverride := h.daemonMemberOverrideEnabled(r.Context(), wsUUID)
	resolveAt := func(pattern string) (toolpolicy.Effective, error) {
		return store.ResolveGeneral(r.Context(), toolpolicy.Query{
			WorkspaceID:     wsUUID,
			ToolKey:         toolKey,
			ResourcePattern: pattern,
			RuntimeID:       runtimeID,
			AgentID:         agentID,
			RequestContext:  reqCtx,
			Eval:            celEval,
			UserID:          ownerID,
			Base:            toolpolicy.SettingAllow,
		}, memberOverride)
	}
	eff, err := resolveAt("")
	if err != nil {
		slog.Error("local tool-policy resolve failed",
			"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", err)
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if rp := strings.TrimSpace(req.ResourcePattern); rp != "" {
		if effSpec, sErr := resolveAt(rp); sErr == nil && toolpolicy.MoreRestrictive(effSpec.Setting, eff.Setting) == effSpec.Setting && effSpec.Setting != eff.Setting {
			eff = effSpec
		}
	}

	// CEREBRO-PATCH(daemon-connection-tool-ask): TECH-3498 — workspace-connection
	// MCP tools resolve through a separate connection:<name> chain (per-tool rows
	// keyed on the bare tool name), which the generic tools:<name> resolve above
	// never matches (PolicyToolKey keeps the mcp__ name as-is). Fold the connection
	// verdict in by TIGHTENING, so an Ask/Deny authored on a connection tool is
	// enforced on the local runtime exactly as on the gateway. Built-in tools (no
	// mcp__ prefix) skip this entirely.
	connName := ""
	if connTool := connectionToolFromName(req.ToolName); connTool != "" {
		connEff, resolvedConn, cErr := store.ConnectionToolEffective(r.Context(), wsUUID, runtimeID, agentID, ownerID, connTool)
		if cErr != nil {
			if mode == localtoolpolicy.ModeEnforce {
				slog.Warn("local tool-policy: connection resolve failed — failing closed",
					"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", cErr)
				writeJSON(w, http.StatusOK, map[string]any{
					"allowed": false, "decision": string(localtoolpolicy.KindDeny),
					"mode": string(mode), "enforced": true, "would_block": true,
					"reason": "connection permission check failed",
				})
				return
			}
			slog.Warn("local tool-policy observe: connection resolve failed — keeping base verdict",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", cErr)
		} else {
			// Record the deciding connection name so the ask context can surface
			// "which integration" even when the connection verdict does not tighten
			// (e.g. an Ask folded with an equal base) — TECH-3498.
			connName = resolvedConn
			if toolpolicy.MoreRestrictive(connEff, eff.Setting) == connEff && connEff != eff.Setting {
				eff = toolpolicy.Effective{Setting: connEff, Reason: "workspace connection permission"}
			}
		}
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
		allowed, askDecision, approvalID, askErr := h.toolPolicyAsk(r.Context(), wsUUID, agentID, req.ToolName, req.ResourcePattern, connName, eff.Reason, req.Args)
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

// agentBrowserToolKey is the capability key agent-browser is registered under.
// capabilities.For("claude").Tools lists "agent-browser", which the capability
// mirror writes to cerebro_capability as "tools:agent-browser" (same shape as
// PolicyToolKey produces). The sandbox unix-socket gate keys on it (FIR-1428).
const agentBrowserToolKey = "tools:agent-browser"

// resolveAgentBrowserAllowed reports whether the agent-browser local CLI may run
// for this agent — i.e. whether the daemon should open the sandbox Unix-socket
// bind agent-browser needs to drive Chrome (FIR-1428).
//
// Unlike the generic tool-policy table (Base=Allow), agent-browser is resolved
// with Base=Deny: an unconfigured workspace keeps the socket sealed, matching
// the security requirement that browser access is OFF until explicitly granted.
// Only an explicit allow/ask at some layer opens it; deny — or no grant at all —
// keeps it shut. A resolve error fails closed (sealed), never open.
func (h *Handler) resolveAgentBrowserAllowed(ctx context.Context, wsID, runtimeID, agentID, ownerID pgtype.UUID) bool {
	store := toolpolicy.NewStoreFromQueries(h.CerebroQueries)
	eff, err := store.Resolve(ctx, toolpolicy.Query{
		WorkspaceID: wsID,
		ToolKey:     agentBrowserToolKey,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      ownerID,
		Base:        toolpolicy.SettingDeny,
	})
	if err != nil {
		slog.Warn("agent-browser sandbox gate: resolve failed — keeping socket sealed",
			"workspace_id", util.UUIDToString(wsID), "agent_id", util.UUIDToString(agentID), "error", err)
		return false
	}
	return eff.Setting == toolpolicy.SettingAllow || eff.Setting == toolpolicy.SettingAsk
}

// withAgentBrowserSandbox returns the runtime sandbox policy JSON with
// allow_agent_browser=true merged in, so the daemon's buildSandboxConfig opens
// the Unix-socket bind and ~/.agent-browser write rule for this claim (FIR-1428).
// An empty/invalid input policy yields a fresh object carrying just the flag.
// On a marshal error it returns the original policy unchanged (fail closed: the
// daemon keeps the socket sealed) rather than dropping the rest of the policy.
func withAgentBrowserSandbox(policy json.RawMessage) json.RawMessage {
	obj := map[string]any{}
	if len(policy) > 0 {
		if err := json.Unmarshal(policy, &obj); err != nil {
			slog.Warn("agent-browser sandbox gate: existing sandbox policy is not an object — keeping socket sealed")
			return policy
		}
	}
	obj["allow_agent_browser"] = true
	merged, err := json.Marshal(obj)
	if err != nil {
		return policy
	}
	return merged
}

// connectionToolFromName extracts the bare MCP tool name from a Claude tool name
// of shape "mcp__<connection>__<tool>". Returns "" for any non-MCP (built-in)
// tool name, so built-in tools skip the connection chain entirely. It mirrors the
// token parsing in toolpolicy.ConnectionToolDenied so the daemon and the claim-
// time --disallowedTools path agree on which segment is the tool.
func connectionToolFromName(toolName string) string {
	rest, ok := strings.CutPrefix(toolName, "mcp__")
	if !ok {
		return ""
	}
	_, tool, ok := strings.Cut(rest, "__")
	if !ok {
		return ""
	}
	return tool
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

// daemonMemberOverrideEnabled reports whether the default-on cerebro_member_override
// flag is on for the workspace — the switch that makes the local-runtime GENERAL
// tool-policy gate resolve through the member-override model (a member may loosen
// an inherited group default) instead of the pure tighten-only chain. It mirrors
// the gateway twin (runtime.memberOverrideEnabled) so a local CLI and a gateway
// tool call see the same model. A lookup miss or DB error resolves to OFF (the
// flag's default): a path that can LOOSEN access must never switch on by accident.
// Only the general gate calls this; the agent-browser sandbox gate stays on Resolve.
func (h *Handler) daemonMemberOverrideEnabled(ctx context.Context, wsID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	enabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     toolpolicy.FlagMemberOverride,
	})
	if err != nil || !enabled {
		return false // no override or DB error → default OFF → pure tighten-only
	}
	return true
}

// daemonPolicyCELEvaluator returns the shared CEL evaluator when the default-OFF
// cerebro_policy_cel flag is enabled for the workspace, else nil — mirroring the
// gateway gate (runtime.FlagPolicyCEL) so a local CLI and a gateway tool call see
// the same Expr behaviour. nil leaves Query.Eval unset, so an Expr condition is
// undecidable and fails closed; a lookup miss or error resolves to OFF (the
// flag's default), never switching the expression path on by accident.
func (h *Handler) daemonPolicyCELEvaluator(ctx context.Context, wsID pgtype.UUID) toolpolicy.ExprEvaluator {
	if h.CerebroQueries == nil {
		return nil
	}
	enabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     toolpolicy.FlagPolicyCEL,
	})
	if err != nil || !enabled {
		return nil // no override or DB error → default OFF → Expr stays undecidable
	}
	eval, err := toolpolicy.SharedCELEvaluator()
	if err != nil {
		slog.Warn("local tool-policy: CEL evaluator unavailable — Expr conditions fail closed",
			"workspace_id", util.UUIDToString(wsID), "error", err)
		return nil
	}
	return eval.Eval
}

// toolPolicyAsk raises a local-runtime tool call that hit "Ask" against the
// shared inbox and reports the outcome the daemon should act on. It uses
// EvaluateDecisionReusing (non-blocking) so the daemon's short-timeout HTTP
// request is never held open and a retried tool call rejoins its open ask
// instead of piling up duplicates — the same machinery and rejoining semantics
// as repoCheckoutAsk. The requester is the agent making the call; the resource
// is the tool's resource pattern (binary / path / URL).
func (h *Handler) toolPolicyAsk(ctx context.Context, wsID, agentID pgtype.UUID, toolName, resourcePattern, connection, reason string, args map[string]any) (allowed bool, decision string, approvalID pgtype.UUID, err error) {
	// CEREBRO-PATCH(daemon-connection-ask-context): TECH-3498 — a human-readable
	// purpose for the inbox: connection tools name the integration, everything
	// else names the tool, so the approval card shows what/which/why.
	purpose := fmt.Sprintf("Agent requested tool %q", toolName)
	if connection != "" {
		purpose = fmt.Sprintf("Agent wants to use tool %q on connection %q", toolName, connection)
	}
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
			"tool_name":       toolName,
			"resource":        resourcePattern,
			"connection":      connection,
			"args":            args,
			"connection_tool": connection != "",
			"purpose":         purpose,
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
