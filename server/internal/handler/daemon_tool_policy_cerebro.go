package handler

// CEREBRO-PATCH(daemon-tool-policy-cerebro): TECH-3173 — per-tool enforcement for
// LOCAL CLI runtimes (Claude, Codex, Cursor, Gemini) spawned by the daemon.
//
// The gateway executor already gates every tool call through the authoritative
// Policy Decision Service. Local CLIs run on a Mac and never
// pass through that executor, so an Ask/Deny row in the permission table never
// reaches them. This file is the daemon-side seam that closes that gap: a local
// runtime's provider-native before-call hook calls ResolveDaemonToolPolicy,
// which resolves the SAME toolpolicy.Store chain and,
// on Ask, raises an approval in the SAME inbox the gateway gate uses — one
// model, not a parallel one.
//
// Local-runtime enforcement is always on. The former rollout flags were
// removed once all supported adapters had contract coverage (FIR-3401).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/localtoolpolicy"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
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
	TaskID          string         `json:"task_id"`
	ClaimGeneration int64          `json:"claim_generation"`
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
// approval_id? }. On Ask the daemon long-polls
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	const mode = "enforce"

	agentID := pgtype.UUID{}
	if req.AgentID != "" {
		parsed, parseErr := util.ParseUUID(req.AgentID)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
		agentID = parsed
	}
	taskID, err := util.ParseUUID(req.TaskID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	// CEREBRO-PATCH(daemon-tool-policy-mandate-key): FIR-3403 — the mandate snapshots
	// canonical capability keys (built-ins as "tools:<Name>"); match under the same
	// PolicyToolKey the policy-chain resolve below uses, not the bare hook tool name.
	toolKey, resourcePattern := localtoolpolicy.ProviderToolCallForAgent(r.Context(), h.DB, wsUUID, agentID, req.ToolName, req.ResourcePattern, req.Args) // CEREBRO-PATCH(cursor-tool-policy-key): FIR-3729 normalize Cursor hook names/resources to its runtime inventory.
	req.ResourcePattern = resourcePattern
	if h.taskMandateEnforcementEnabled(r.Context(), wsUUID) {
		if err := taskmandate.NewStoreDB(h.DB).AuthorizeClaimGeneration(r.Context(), taskID, wsUUID, agentID, req.ClaimGeneration, localtoolpolicy.ProviderMandateToolKey(toolKey)); err != nil { // CEREBRO-PATCH(connection-task-mandate-key): FIR-3828 match local workspace Connection hooks to the shared dispatch identity.
			verdict := taskmandate.VerdictForError(err)
			h.recordDaemonTaskMandateDenial(r.Context(), wsUUID, agentID, taskID, req.ToolName, toolKey, verdict)
			slog.Warn("local tool-policy: Task Mandate denied call",
				"workspace_id", workspaceID,
				"agent_id", req.AgentID,
				"task_id", req.TaskID,
				"tool", req.ToolName,
				"verdict_code", string(verdict.Code),
				"recovery_action", string(verdict.RecoveryAction),
			)
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": false, "decision": string(localtoolpolicy.KindDeny),
				"mode": mode, "enforced": true, "would_block": true,
				"reason": verdict.Message, "verdict": verdict,
			})
			return
		}
	}

	// The chain's Runtime and User/Group layers key on the agent's runtime and
	// owner. Resolve them server-side from the agent row (never trust the caller),
	// matching the canonical Gateway decision context. Lookup failure fails closed.
	var runtimeID, ownerID pgtype.UUID
	if agentID.Valid {
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil {
			slog.Warn("local tool-policy: agent lookup failed — failing closed",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", err)
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": false, "decision": string(localtoolpolicy.KindDeny),
				"mode": mode, "enforced": true, "would_block": true,
				"reason": "agent lookup failed",
			})
			return
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
	store := toolpolicy.NewStoreDB(h.DB, h.CerebroQueries)
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
	// This is the local-runtime twin of the gateway's general tool-policy gate.
	// ResolveDeclared selects the key's declared contract and applies the
	// workspace member-override setting only to openable permissions.
	resolveAt := func(pattern string) (toolpolicy.Effective, error) {
		return store.ResolveDeclared(r.Context(), toolpolicy.Query{
			WorkspaceID:     wsUUID,
			ToolKey:         toolKey,
			ResourcePattern: pattern,
			RuntimeID:       runtimeID,
			AgentID:         agentID,
			RequestContext:  reqCtx,
			Eval:            celEval,
			UserID:          ownerID,
			Base:            toolpolicy.SettingAllow,
		})
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
			slog.Warn("local tool-policy: connection resolve failed — failing closed",
				"workspace_id", workspaceID, "agent_id", req.AgentID, "tool", req.ToolName, "error", cErr)
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": false, "decision": string(localtoolpolicy.KindDeny),
				"mode": mode, "enforced": true, "would_block": true,
				"reason": "connection permission check failed",
			})
			return
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

	decision := localtoolpolicy.Decide(eff)

	// FIR-3091 punkt 8 fase 3: usage log — one row per ENFORCED local-CLI
	// verdict, so the permission detail page can show every time the tool's
	// policy was applied on a local runtime. Observe is a dry run (nothing is
	// applied) and records nothing; the connection fold above is recorded under
	// its own connection:<name> key when it decided. Best-effort.
	if decision.Enforced {
		store.RecordUsage(r.Context(), toolpolicy.UsageParams{
			WorkspaceID:      wsUUID,
			ToolKey:          toolKey,
			EnforcementPoint: "local_cli",
			SubjectType:      "agent",
			SubjectID:        agentID,
			Resource:         strings.TrimSpace(req.ResourcePattern),
			Decision:         eff.Setting,
			DecidedBy:        string(eff.DecidedBy),
		})
		if connName != "" {
			store.RecordUsage(r.Context(), toolpolicy.UsageParams{
				WorkspaceID:      wsUUID,
				ToolKey:          toolpolicy.ConnectionToolKey(connName),
				EnforcementPoint: "local_cli",
				SubjectType:      "agent",
				SubjectID:        agentID,
				Resource:         req.ToolName,
				Decision:         eff.Setting,
				DecidedBy:        string(eff.DecidedBy),
			})
		}
	}

	// Ask: raise (or rejoin) one shared-inbox approval and return
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

	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":     decision.Allowed,
		"decision":    string(decision.Kind),
		"mode":        mode,
		"enforced":    decision.Enforced,
		"would_block": decision.WouldBlock,
		"observed":    string(eff.Setting),
		"reason":      eff.Reason,
	})
}

// recordDaemonTaskMandateDenial mirrors the local-runtime call-time rejection
// into the same append-only ledger the Gateway uses. Capabilities and the drift
// watcher read this observation so a tool that Permissions allows but the
// immutable Task Mandate rejects is visible as real access drift.
func (h *Handler) recordDaemonTaskMandateDenial(ctx context.Context, workspaceID, agentID, taskID pgtype.UUID, observedToolName, capabilityID string, verdict taskmandate.Verdict) {
	if h == nil || h.Queries == nil || h.CerebroQueries == nil || !agentID.Valid {
		return
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return
	}
	observedToolName = strings.TrimSpace(observedToolName)
	if observedToolName == "" {
		return
	}
	if err := h.CerebroQueries.AppendCerebroAccessDecisionLedger(ctx, cerebrodb.AppendCerebroAccessDecisionLedgerParams{
		WorkspaceID:           workspaceID,
		AgentID:               agentID,
		RuntimeID:             agent.RuntimeID,
		TaskID:                taskID,
		ObservedToolName:      observedToolName,
		CanonicalCapabilityID: pgtype.Text{String: strings.TrimSpace(capabilityID), Valid: strings.TrimSpace(capabilityID) != ""},
		LegacyDecision:        string(accessdecision.DecisionDeny),
		LegacyPath:            "local_tool_policy",
		ShadowDecision:        string(accessdecision.DecisionDeny),
		PolicyDecision:        string(accessdecision.PolicyError),
		EvidenceLevel:         string(availabilityevidence.LevelDiscovered),
		Differs:               false,
		Reason:                fmt.Sprintf("task mandate %s: %s", verdict.Code, verdict.Message),
	}); err != nil {
		slog.Warn("local tool-policy: could not record Task Mandate denial",
			"workspace_id", util.UUIDToString(workspaceID), "agent_id", util.UUIDToString(agentID),
			"tool", observedToolName, "error", err)
	}
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
	store := toolpolicy.NewStoreDB(h.DB, h.CerebroQueries)
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
	// FIR-3091 punkt 8 fase 3: usage log — one row per applied sandbox verdict,
	// so the permission detail page can show every time tools:agent-browser was
	// enforced. Best-effort after the decision resolved.
	store.RecordUsage(ctx, toolpolicy.UsageParams{
		WorkspaceID:      wsID,
		ToolKey:          agentBrowserToolKey,
		EnforcementPoint: "agent_browser_sandbox",
		SubjectType:      "agent",
		SubjectID:        agentID,
		Decision:         eff.Setting,
		DecidedBy:        string(eff.DecidedBy),
	})
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

// daemonMemberOverrideEnabled reports whether cerebro_member_override is on for
// the workspace — the switch that makes the local-runtime GENERAL tool-policy
// gate resolve through the member-override model (a member may loosen an
// inherited group default) instead of the pure tighten-only chain. It mirrors
// the gateway twin (runtime.memberOverrideEnabled) so a local CLI and a gateway
// tool call see the same model. The registry default is ON
// (packages/cerebro-feature-flags/registry.ts), but a lookup miss or DB error
// still resolves to OFF here: a path that can LOOSEN access must never switch
// on by accident just because a workspace row is missing (registry-vs-missing-
// row gap tracked in FIR-3176). Only the general gate calls this; the
// agent-browser sandbox gate stays on Resolve.
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
