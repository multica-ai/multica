package runtime

// Approval enforcement gate wiring for the server-owned agent runtime
// (FIR-2193). This is the missing coupling: before this file, the permgate /
// permissions / approvals stack existed but nothing called it from a real
// agent tool execution, so requires_approval never produced an inbox ask and
// Pending was always empty.
//
// guardToolCall is the single choke-point every tool dispatch in the gateway
// tool loops now passes through. Its default behaviour is a pure no-op: when
// no gate is configured (the default) it returns allowed=true without touching
// the resolver, so the fleet is completely unaffected. The gate is only
// activated by MaybeEnableApprovalGate, which the server wires solely when the
// CEREBRO_APPROVAL_GATE_ENABLED env flag is set — a controlled, default-off
// rollout that can be scoped to a single agent via CEREBRO_APPROVAL_GATE_AGENTS.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// toolCapabilityKey maps a runtime tool name to the curated capability key the
// permission engine matches on (see packages/cerebro-permissions/core/
// capability-catalog.ts and server/internal/cerebro/permissions).
//
// Only genuinely dangerous tools are mapped. Internal Multica CRUD (reading and
// writing issues/projects/comments) returns "" and stays UNGATED, on purpose:
// the resolver default-denies any capability without a matching grant, so
// gating reads would brick a controlled agent the moment the gate is enabled.
// Mapping just the dangerous actions keeps the blast radius to exactly the
// actions an operator wants to hold behind approval.
func toolCapabilityKey(toolName string) string {
	switch toolName {
	case "web_fetch", "firtal_bq_query":
		// Reaches a host outside Multica over the network.
		return "network.external"
	case "credential_list":
		// Reads/uses stored secrets.
		return "credentials.read"
	case "gogcli_sheets_write":
		// Writes data to an external system (Google Sheets).
		return "prod.write"
	default:
		return ""
	}
}

// decodeToolArgs best-effort parses a raw tool-arguments JSON string into a map
// for the approval ask context. A parse failure yields an empty map — the
// arguments are informational for the human reviewer, never part of the
// allow/deny decision (which keys only on the capability).
func decodeToolArgs(raw string) map[string]any {
	args := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return args
	}
	_ = json.Unmarshal([]byte(raw), &args)
	return args
}

// guardToolCall is the enforcement choke-point. It returns (allowed, reason).
//
// Default-off: when e.gate is nil it allows immediately without any lookup.
// Controlled scope: when gateAgents is non-empty, agents outside the set are
// allowed without a lookup. Ungated tools (toolCapabilityKey == "") are always
// allowed. Otherwise it asks the gate, which on requires_approval creates an
// inbox ask and BLOCKS until a human approves (continue), rejects/expires
// (stop), or the wait budget elapses (stop, fail-closed). A gate error also
// fails closed — needs_approval is never silently downgraded to allow.
func (e *FirtalGatewayExecutor) guardToolCall(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	args map[string]any,
	meta GatewayRequestMeta,
) (bool, string) {
	if e == nil || e.gate == nil {
		return true, ""
	}
	if len(e.gateAgents) > 0 {
		if _, ok := e.gateAgents[agentID.Bytes]; !ok || !agentID.Valid {
			return true, ""
		}
	}

	// FIR-2230: when the unified per-tool policy chain is wired, it — not the
	// capability resolver — decides this call. Every tool is resolved through
	// Runtime › Agent › Group › User; unconfigured tools fall back to the chain's
	// Base default (Allow), so the blast radius is exactly the tools an operator
	// gave an explicit Ask/Deny row.
	if e.toolPolicy != nil {
		return e.guardToolCallViaPolicy(ctx, agentID, workspaceID, toolName, args, meta)
	}

	capKey := toolCapabilityKey(toolName)
	if capKey == "" {
		return true, ""
	}

	req := permgate.Request{
		Permission: permissions.Request{
			WorkspaceID: workspaceID,
			Actor:       permissions.Actor{Type: "agent", ID: agentID},
			Agent:       agentID,
			Capability:  capKey,
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   agentID,
		Surface:       approvals.SurfaceSystem,
		Context: map[string]any{
			"tool_name": toolName,
			"task_id":   meta.TaskID,
			"args":      args,
		},
	}

	res, err := e.gate.Guard(ctx, req)
	if err != nil {
		e.logger.Warn("approval gate error — failing closed",
			"task_id", meta.TaskID,
			"agent_id", meta.AgentID,
			"tool_name", toolName,
			"capability", capKey,
			"error", err,
		)
		return false, fmt.Sprintf("permission gate error: %v", err)
	}
	e.logger.Info("approval gate decision",
		"task_id", meta.TaskID,
		"agent_id", meta.AgentID,
		"tool_name", toolName,
		"capability", capKey,
		"outcome", string(res.Outcome),
		"reason", res.Reason,
	)
	if res.Outcome.Stops() {
		reason := res.Reason
		if reason == "" {
			reason = string(res.Outcome)
		}
		return false, reason
	}
	return true, ""
}

// guardToolCallViaPolicy enforces the FIR-2230 per-tool permission chain for one
// tool call. It resolves the tool through Runtime › Agent › Group › User for the
// agent's owner — the principal whose ceiling the agent runs under — and maps
// the Effective verdict: Allow proceeds immediately, Deny stops, Ask creates an
// inbox request and BLOCKS until a human approves (continue) or rejects/expires
// (stop), reusing the same gate machinery as the capability path. Any lookup or
// gate error fails closed.
func (e *FirtalGatewayExecutor) guardToolCallViaPolicy(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	args map[string]any,
	meta GatewayRequestMeta,
) (bool, string) {
	// The chain's user ceiling is the agent's owner; the runtime layer is the
	// agent's runtime. Both come from the agent row. resolveGroupIDs (in the
	// store) expands the owner's real groups for the Group layer.
	var runtimeID, ownerID pgtype.UUID
	if agentID.Valid {
		agent, err := e.queries.GetAgent(ctx, agentID)
		if err != nil {
			e.logger.Warn("tool-policy gate: agent lookup failed — failing closed",
				"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName, "error", err)
			return false, fmt.Sprintf("tool-policy gate error: %v", err)
		}
		runtimeID = agent.RuntimeID
		ownerID = agent.OwnerID
	}

	eff, err := e.toolPolicy.Resolve(ctx, toolpolicy.Query{
		WorkspaceID: workspaceID,
		ToolKey:     toolName,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      ownerID,
	})
	if err != nil {
		e.logger.Warn("tool-policy gate: resolve failed — failing closed",
			"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName, "error", err)
		return false, fmt.Sprintf("tool-policy gate error: %v", err)
	}

	decision := toolPolicyDecision(eff)
	if decision.Kind == permissions.DecisionAllow {
		// Fast path: no ask, no await, no inbox row for an allowed tool.
		return true, ""
	}

	req := permgate.Request{
		Permission: permissions.Request{
			WorkspaceID: workspaceID,
			Actor:       permissions.Actor{Type: "agent", ID: agentID},
			Agent:       agentID,
			Capability:  toolName,
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   agentID,
		Surface:       approvals.SurfaceSystem,
		Context: map[string]any{
			"tool_name": toolName,
			"task_id":   meta.TaskID,
			"args":      args,
			"effective": string(eff.Setting),
			"reason":    eff.Reason,
		},
	}

	res, err := e.gate.GuardDecision(ctx, req, decision)
	if err != nil {
		e.logger.Warn("tool-policy gate error — failing closed",
			"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName,
			"effective", string(eff.Setting), "error", err)
		return false, fmt.Sprintf("permission gate error: %v", err)
	}
	e.logger.Info("tool-policy gate decision",
		"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName,
		"effective", string(eff.Setting), "outcome", string(res.Outcome), "reason", eff.Reason)
	if res.Outcome.Stops() {
		reason := res.Reason
		if reason == "" {
			reason = eff.Reason
		}
		if reason == "" {
			reason = string(res.Outcome)
		}
		return false, reason
	}
	return true, ""
}

// toolPolicyDecision maps a resolved tool-policy verdict to the permission
// decision the gate acts on: Allow → allow, Ask → needs_approval (inbox + wait),
// Deny → deny. Resolve never returns Inherit; any unexpected value fails closed.
func toolPolicyDecision(eff toolpolicy.Effective) permissions.Decision {
	switch eff.Setting {
	case toolpolicy.SettingAllow:
		return permissions.Decision{Kind: permissions.DecisionAllow, Reason: eff.Reason}
	case toolpolicy.SettingAsk:
		return permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: eff.Reason}
	case toolpolicy.SettingDeny:
		return permissions.Decision{Kind: permissions.DecisionDeny, Reason: eff.Reason}
	default:
		return permissions.Decision{Kind: permissions.DecisionDeny, Reason: "unresolved tool policy"}
	}
}

// EnableApprovalGate activates the enforcement gate on this executor, scoped to
// the given agent allowlist (empty = all agents). Calling it is what turns the
// default no-op into real enforcement; the server only calls it under the env
// flag, so the zero-value executor stays unchanged.
func (e *FirtalGatewayExecutor) EnableApprovalGate(gate *permgate.Gate, agentAllowlist []pgtype.UUID) {
	if e == nil {
		return
	}
	e.gate = gate
	if len(agentAllowlist) == 0 {
		e.gateAgents = nil
		return
	}
	allow := make(map[[16]byte]struct{}, len(agentAllowlist))
	for _, id := range agentAllowlist {
		if id.Valid {
			allow[id.Bytes] = struct{}{}
		}
	}
	e.gateAgents = allow
}

// Approval gate env knobs. All default to off / unset so production behaviour
// is unchanged until an operator opts in.
const (
	envApprovalGateEnabled = "CEREBRO_APPROVAL_GATE_ENABLED"
	envApprovalGateAgents  = "CEREBRO_APPROVAL_GATE_AGENTS"
	envApprovalGateWait    = "CEREBRO_APPROVAL_GATE_WAIT"
	envApprovalGateTTL     = "CEREBRO_APPROVAL_GATE_TTL"
	// envApprovalGateMode selects which engine resolves each tool call:
	// "toolpolicy" (FIR-2230 unified per-tool chain) or the default capability
	// resolver (FIR-2193). Unset/anything else = capability, so prod is unchanged.
	envApprovalGateMode = "CEREBRO_APPROVAL_GATE_MODE"

	defaultApprovalGateWait = 10 * time.Minute
	defaultApprovalGateTTL  = 30 * time.Minute
)

// BuildApprovalGate constructs the shared approval enforcement gate — the single
// "resolve → on Ask create an inbox request → await the human decision" seam
// (FIR-2586). It returns nil when CEREBRO_APPROVAL_GATE_ENABLED is off, so every
// caller keeps its prior behaviour until an operator opts in. The same gate type
// backs the agent tool loop (MaybeEnableApprovalGate), the daemon repo checkout
// (Handler.ApprovalGate) and credential governance, so a needs-approval verdict
// from any of them lands in the one /approvals inbox instead of a silent block.
func BuildApprovalGate(cerebroQueries *cerebrodb.Queries, tx approvals.TxStarter, bus *events.Bus) *permgate.Gate {
	if !approvalGateEnvEnabled() {
		return nil
	}
	resolver := permissions.New(cerebroQueries)
	approvalsSvc := approvals.New(cerebroQueries, tx, bus)
	gate := permgate.New(resolver, approvalsSvc)
	gate.WaitTimeout = durationFromEnv(defaultApprovalGateWait, envApprovalGateWait)
	gate.ApprovalTTL = durationFromEnv(defaultApprovalGateTTL, envApprovalGateTTL)
	return gate
}

// MaybeEnableApprovalGate reads the env flags and, only when
// CEREBRO_APPROVAL_GATE_ENABLED is truthy, constructs the resolver + approvals
// service + gate and enables it on the executor. When the flag is off it is a
// no-op: tool execution behaves exactly as before. Invalid agent UUIDs in the
// allowlist are skipped with a warning rather than failing startup.
func MaybeEnableApprovalGate(e *FirtalGatewayExecutor, cerebroQueries *cerebrodb.Queries, tx approvals.TxStarter, bus *events.Bus) {
	if e == nil {
		return
	}
	gate := BuildApprovalGate(cerebroQueries, tx, bus)
	if gate == nil {
		return
	}

	// Tool-policy mode resolves every tool through the unified Runtime › Agent ›
	// Group › User chain so the permission table's Allow/Ask/Deny rows gate real
	// tool calls; the gate above still provides the shared inbox + await. Default
	// (capability) keeps the prior FIR-2193 resolver path.
	toolPolicyMode := approvalGateModeToolPolicy()
	if toolPolicyMode {
		e.toolPolicy = toolpolicy.NewStoreFromQueries(cerebroQueries)
	}

	var allowlist []pgtype.UUID
	if raw := strings.TrimSpace(os.Getenv(envApprovalGateAgents)); raw != "" {
		for _, part := range splitCSV(raw) {
			id, err := util.ParseUUID(part)
			if err != nil {
				e.logger.Warn("approval gate: skipping invalid agent id",
					"value", part, "error", err)
				continue
			}
			allowlist = append(allowlist, id)
		}
	}

	e.EnableApprovalGate(gate, allowlist)
	mode := "capability"
	if toolPolicyMode {
		mode = "toolpolicy"
	}
	e.logger.Info("approval enforcement gate ENABLED (controlled rollout)",
		"resolver_mode", mode,
		"scoped_agents", len(allowlist),
		"wait_timeout", gate.WaitTimeout.String(),
		"approval_ttl", gate.ApprovalTTL.String(),
	)
}

func approvalGateEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envApprovalGateEnabled))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// approvalGateModeToolPolicy reports whether the gate should resolve through the
// FIR-2230 per-tool policy chain instead of the default capability resolver.
func approvalGateModeToolPolicy() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envApprovalGateMode))) {
	case "toolpolicy", "tool-policy", "tool_policy":
		return true
	default:
		return false
	}
}
