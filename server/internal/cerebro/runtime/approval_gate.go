package runtime

// Approval enforcement gate wiring for the server-owned agent runtime
// (FIR-2193). This is the missing coupling: before this file, the permgate /
// permissions / approvals stack existed but nothing called it from a real
// agent tool execution, so requires_approval never produced an inbox ask and
// Pending was always empty.
//
// guardToolCall is the single choke-point every tool dispatch in the gateway
// tool loops now passes through. The Policy Decision Service always decides
// access; this file only supplies the shared approval inbox for Ask outcomes.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaction"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
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
	case "web_fetch", "firtal_registry":
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

// askContext builds the verbatim Context map attached to an approval ask so the
// human reviewer sees what the agent is trying to do: the tool, which connection
// it targets (empty for non-connection tools), the originating task, the issue
// the run is working on, the human who triggered the run, the raw args, a
// connection_tool flag, and a plain-language purpose string (TECH-3498). issue_id
// is the robust source the approvals handler uses to resolve a human-readable
// issue identifier + title, independent of task_id; trigger_user_id /
// trigger_user_name name the member whose message/task started the run, so the
// inbox can show "requested by <member>".
func askContext(toolName, connName, taskID, issueID, triggerUserID, triggerUserName string, args map[string]any, connectionTool bool) map[string]any {
	var purpose string
	if connectionTool && connName != "" {
		purpose = fmt.Sprintf("Agent wants to use tool %q on connection %q", toolName, connName)
	} else {
		purpose = fmt.Sprintf("Agent requested tool %q", toolName)
	}
	return map[string]any{
		"tool_name":         toolName,
		"connection":        connName,
		"task_id":           taskID,
		"issue_id":          issueID,
		"trigger_user_id":   triggerUserID,
		"trigger_user_name": triggerUserName,
		"args":              args,
		"connection_tool":   connectionTool,
		"purpose":           purpose,
	}
}

// connectionToolSetting resolves the effective Allow/Ask/Deny verdict for a
// workspace-connection per-tool rule, through the same chain the permissions
// screen writes. The customer-service MCP tools are dispatched server-side on
// this runtime, so the daemon's --disallowedTools never sees them — this is the
// firtal-gateway half of connection enforcement (TECH-3174 Deny, TECH-3498 Ask).
//
// Fail closed on a missing resolver or DB/lookup error. This is the authoritative
// call-time gate, so an unresolved connection verdict must produce a visible
// denial instead of silently granting the call.
// It returns the resolved setting plus the connection name of the deciding row
// ("" when the tool is not a connection tool), so the caller can surface "which
// integration" in the approval context (TECH-3498).
func (e *FirtalGatewayExecutor) connectionToolSetting(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	reg *Registry,
	toolName string,
	meta GatewayRequestMeta,
) (toolpolicy.Setting, string) {
	if !agentID.Valid || toolName == "" {
		return toolpolicy.SettingAllow, ""
	}
	resourceName, connectionName, connectionTool := connectionPolicyTarget(reg, toolName)
	if !connectionTool {
		return toolpolicy.SettingAllow, ""
	}
	if e.connDeny == nil || e.queries == nil {
		e.logger.Warn("connection policy: resolver unavailable — blocking",
			"agent_id", meta.AgentID, "tool", toolName)
		return toolpolicy.SettingDeny, connectionName
	}
	agent, err := e.queries.GetAgent(ctx, agentID)
	if err != nil {
		e.logger.Warn("connection policy: agent lookup failed — blocking",
			"agent_id", meta.AgentID, "tool", toolName, "error", err)
		return toolpolicy.SettingDeny, connectionName
	}
	eff, connName, err := e.connDeny.ConnectionToolEffective(ctx, workspaceID, agent.RuntimeID, agentID, agent.OwnerID, resourceName)
	if err != nil {
		e.logger.Warn("connection policy: resolve failed — blocking",
			"agent_id", meta.AgentID, "tool", toolName, "error", err)
		return toolpolicy.SettingDeny, connectionName
	}
	return eff, connName
}

func connectionPolicyTarget(reg *Registry, toolName string) (resourceName, connectionName string, ok bool) {
	if reg == nil {
		return "", "", false
	}
	tool, found := reg.Get(toolName)
	if !found {
		return "", "", false
	}
	switch concrete := tool.(type) {
	case *gatewayMCPTool:
		return concrete.toolName, concrete.connectionName, true
	case *CustomerServiceMCPTool:
		return concrete.Name(), "customer-service-mcp", true
	default:
		return "", "", false
	}
}

// guardConnectionAsk routes an Ask verdict on a workspace-connection tool through
// the shared approval gate: it creates an inbox request and BLOCKS until a human
// approves (continue) or rejects/expires (stop), reusing the canonical
// GuardDecision machinery. A gate error fails closed —
// an Ask is never silently downgraded to allow (TECH-3498).
func (e *FirtalGatewayExecutor) guardConnectionAsk(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName, connName string,
	args map[string]any,
	meta GatewayRequestMeta,
) (bool, string) {
	req := permgate.Request{
		Permission: permissions.Request{
			WorkspaceID: workspaceID,
			Actor:       permissions.Actor{Type: "agent", ID: agentID},
			Agent:       agentID,
			Capability:  toolName,
			Resource:    toolName,
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   agentID,
		Surface:       approvals.SurfaceSystem,
		Context:       askContext(toolName, connName, meta.TaskID, meta.IssueID, meta.TriggerUserID, meta.TriggerUserName, args, true),
	}
	decision := permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: "connection tool requires approval"}
	// GuardDecisionReusing: a still-valid period-grant (an approved row with a
	// future expires_at) short-circuits to allow without raising a new ask, so a
	// time-boxed grant covers subsequent gateway calls for the same tool (TECH-3498).
	res, err := e.gate.GuardDecisionReusing(ctx, req, decision)
	if err != nil {
		e.logger.Warn("connection Ask gate error — failing closed",
			"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName, "error", err)
		return false, fmt.Sprintf("permission gate error: %v", err)
	}
	e.logger.Info("connection tool Ask decision (TECH-3498)",
		"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool_name", toolName,
		"outcome", string(res.Outcome), "reason", res.Reason)
	if res.Outcome.Stops() {
		reason := res.Reason
		if reason == "" {
			reason = "connection tool approval was not granted"
		}
		return false, reason
	}
	return true, ""
}

// guardToolCall is the enforcement choke-point. It returns (allowed, reason).
//
// Policy Decision Service is authoritative and fail-closed for every call.
// Connection-specific Deny and Ask plus create_issue's platform-action gate
// remain stricter safety floors on top of an Allow verdict.
func (e *FirtalGatewayExecutor) guardToolCall(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	args map[string]any,
	reg *Registry,
	meta GatewayRequestMeta,
) (allowed bool, reason string) {
	if e == nil {
		return true, ""
	}
	// The task mandate is the immutable per-run contract. Check it at this
	// shared dispatch choke-point so every Gateway transport, including the
	// create_issue early-return path, performs a fresh expiry and membership
	// read immediately before execution.
	if e.taskMandates != nil && e.taskMandateEnforcementEnabled(ctx, workspaceID) {
		taskID := optionalGatewayUUID(meta.TaskID)
		if !taskID.Valid {
			return e.denyTaskMandateCall(ctx, agentID, workspaceID, reg, meta, toolName, taskmandate.ErrMissing)
		}
		var err error
		if authorizer, ok := e.taskMandates.(taskMandateGenerationAuthorizer); ok {
			if meta.TaskMandateGeneration <= 0 {
				err = taskmandate.ErrStaleClaimGeneration
			} else {
				err = authorizer.AuthorizeClaimGeneration(ctx, taskID, workspaceID, agentID, meta.TaskMandateGeneration, toolName)
			}
		} else {
			err = e.taskMandates.Authorize(ctx, taskID, workspaceID, agentID, toolName)
		}
		if err != nil {
			return e.denyTaskMandateCall(ctx, agentID, workspaceID, reg, meta, toolName, err)
		}
	}
	var decision, connNameLog string
	if toolName == "create_issue" {
		entry := e.decideAccess(ctx, agentID, workspaceID, toolName, reg, meta, gatewayPolicyRequestContext(toolName, args))
		if entry.Decision != accessdecision.DecisionAllow {
			return false, entry.Reason
		}
		decision = "policy_decision_service+platform_action"
		return e.guardPlatformAction(ctx, agentID, workspaceID, toolName, args, meta)
	}
	// FIR-2243 B1: emit one structured runtime decision line per tool call, tying
	// the tool to the permission verdict it ran under — for EVERY call, including
	// the default-off common path that previously logged nothing. decision is set
	// at each return below; the defer reads the final named (allowed, reason).
	defer func() {
		e.logToolDecision(meta, toolName, connNameLog, decision, allowed, reason)
	}()
	// Workspace-connection per-tool policy (TECH-3174 Deny, TECH-3498 Ask). Resolve
	// the verdict once. Deny is always-on — it runs even when the approval gate is
	// off (e.gate == nil), because connection tools (e.g. the customer-service MCP
	// tools) are dispatched server-side on this runtime and never reach the daemon's
	// --disallowedTools. A Deny here makes the tool uncallable regardless of the
	// approval-gate rollout. Ask needs the approval inbox, so it is routed below
	// alongside the rest of the Ask machinery (a no-op when the gate is off).
	connSetting, connName := e.connectionToolSetting(ctx, agentID, workspaceID, reg, toolName, meta)
	// FIR-2166 C PR3: fold in the API-connection ENDPOINT verdict for API-type
	// connection tools (api_connection_tools.go), resolved through the same chain
	// via ConnectionEndpointEffective. connectionToolSetting above keys on the bare
	// tool name and only matches MCP per-tool rows, so it returns Allow for an API
	// endpoint tool — this is where an API endpoint's Deny/Ask is applied.
	// Most-restrictive wins, so the two verdicts compose safely.
	// failClosed=true: this is the authoritative call-time guard in front of the
	// secrets box, so an unresolved endpoint verdict denies the call rather than
	// allowing it (FIR-2166 C review fix).
	epSetting, epConn := e.apiEndpointSetting(ctx, agentID, workspaceID, reg, toolName, true, meta)
	if epSetting != toolpolicy.SettingAllow {
		if connName == "" {
			connName = epConn
		}
		connSetting = toolpolicy.MoreRestrictive(connSetting, epSetting)
	}
	connNameLog = connName
	// FIR-3091 punkt 8 fase 3: usage log — a connection rule that decided this
	// call (connName is only set when a rule tightened past the allow baseline)
	// is recorded under its connection:<name> permission key. Best-effort.
	if connName != "" && e.connDeny != nil {
		e.connDeny.RecordUsage(ctx, toolpolicy.UsageParams{
			WorkspaceID:      workspaceID,
			ToolKey:          toolpolicy.ConnectionToolKey(connName),
			EnforcementPoint: "gateway_connection_tool",
			SubjectType:      "agent",
			SubjectID:        agentID,
			Resource:         toolName,
			Decision:         connSetting,
		})
	}
	if connSetting == toolpolicy.SettingDeny {
		e.logger.Info("connection tool blocked by per-tool Deny (TECH-3174)",
			"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool", toolName)
		decision = "deny_connection"
		return false, fmt.Sprintf("tool %q is denied for this agent by a connection permission", toolName)
	}

	entry := e.decideAccess(ctx, agentID, workspaceID, toolName, reg, meta, gatewayPolicyRequestContext(toolName, args))
	decision = "policy_decision_service"
	if entry.PolicyDecision == accessdecision.PolicyAsk {
		decision = "policy_decision_service+ask"
		return e.guardCanonicalAsk(ctx, agentID, workspaceID, toolName, args, meta, entry.Reason)
	}
	if entry.Decision != accessdecision.DecisionAllow {
		return false, entry.Reason
	}
	// Connection Ask remains a safety floor on top of the authoritative Policy
	// Decision Service. When the approval inbox is active, preserve the existing
	// blocking approval flow; API endpoint Ask already fails closed above when it
	// is inactive, while MCP Ask keeps its established inactive-inbox behavior.
	if connSetting == toolpolicy.SettingAsk && e.approvalInboxActive(ctx, workspaceID) {
		decision = "policy_decision_service+ask_connection"
		return e.guardConnectionAsk(ctx, agentID, workspaceID, toolName, connName, args, meta)
	}
	// FIR-2388: an API-connection ENDPOINT set to Ask fronts the secrets box and is
	// dispatched server-side on this runtime, so it MUST reach the approval inbox
	// before it runs — it must never be silently downgraded to Allow by an inactive
	// approval gate. Unlike an ordinary Ask (a UX pause) or an MCP-connection Ask
	// (relayed, TECH-3498 "no-op when the gate is off"), letting this through
	// unapproved would dispatch a credentialed secrets call the agent should have
	// had to get approved. So if the inbox will not run for this call (gate off or
	// workspace approval flag off), fail
	// CLOSED — mirroring the local MCP path, which 403s an Ask endpoint. When the
	// inbox IS active the Ask flows to guardConnectionAsk below via connSetting.
	if epSetting == toolpolicy.SettingAsk && !e.approvalInboxActive(ctx, workspaceID) {
		e.logger.Info("api endpoint Ask blocked — approval inbox inactive (FIR-2388)",
			"task_id", meta.TaskID, "agent_id", meta.AgentID, "tool", toolName, "connection", connName)
		decision = "deny_ask_inbox_inactive"
		return false, fmt.Sprintf("tool %q requires human approval, which is not available for this run", toolName)
	}
	return true, ""
}

func (e *FirtalGatewayExecutor) denyTaskMandateCall(ctx context.Context, agentID, workspaceID pgtype.UUID, reg *Registry, meta GatewayRequestMeta, toolName string, err error) (bool, string) {
	verdict := taskmandate.VerdictForError(err)
	reason := taskmandate.VerdictJSON(err)
	e.recordTaskMandateDenial(ctx, agentID, workspaceID, toolName, reg, meta, verdict)
	if e != nil && e.logger != nil {
		e.logger.Warn("runtime tool decision (FIR-2243 B1)",
			"event", "tool_call_decision",
			"tool", toolName,
			"decision", "task_mandate",
			"allowed", false,
			"verdict_code", string(verdict.Code),
			"recovery_action", string(verdict.RecoveryAction),
			"agent_id", meta.AgentID,
			"agent_name", meta.AgentName,
			"task_id", meta.TaskID,
			"issue_id", meta.IssueID,
			"surface", meta.Surface,
			"reason", reason,
		)
	}
	return false, reason
}

func (e *FirtalGatewayExecutor) taskMandateEnforcementEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if e == nil {
		return false
	}
	if e.taskMandateEnforcement != nil {
		return e.taskMandateEnforcement(ctx, workspaceID)
	}
	if e.cerebro == nil {
		return false
	}
	return taskmandate.EnforcementEnabled(ctx, e.cerebro, workspaceID)
}

func (e *FirtalGatewayExecutor) guardCanonicalAsk(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	args map[string]any,
	meta GatewayRequestMeta,
	reason string,
) (bool, string) {
	if e.gate == nil || meta.TriggerUserID == "" {
		return false, "tool requires human approval, which is not available for this run"
	}
	res, err := e.gate.GuardDecision(ctx, permgate.Request{
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
			"issue_id":  meta.IssueID,
			"args":      args,
			"reason":    reason,
		},
	}, permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: reason})
	if err != nil {
		return false, fmt.Sprintf("permission gate error: %v", err)
	}
	if res.Outcome.Stops() {
		if res.Reason != "" {
			return false, res.Reason
		}
		return false, reason
	}
	return true, ""
}

func (e *FirtalGatewayExecutor) guardPlatformAction(ctx context.Context, agentID, workspaceID pgtype.UUID, toolName string, args map[string]any, meta GatewayRequestMeta) (bool, string) {
	if e.platformActionGate != nil {
		var taskID pgtype.UUID
		if meta.TaskID != "" {
			taskID, _ = util.ParseUUID(meta.TaskID)
		}
		result, err := e.platformActionGate.AuthorizeAndWait(ctx, platformaction.Request{
			WorkspaceID: workspaceID, AgentID: agentID, TaskID: taskID,
			Capability: toolName, Resource: gatewayPlatformActionResource(args), Surface: "firtal_gateway",
			Context: args, IsSystem: meta.TriggerUserID == "",
		})
		if err != nil {
			return false, err.Error()
		}
		if result.Outcome == permgate.OutcomeAllowed || result.Outcome == permgate.OutcomeApproved {
			return true, ""
		}
		return false, result.Reason
	}
	// Tests and partially wired executors still fail closed from the real policy
	// store; production always wires platformActionGate in main.
	if e.toolPolicy == nil {
		return false, "platform action gate unavailable"
	}
	agent, err := e.queries.GetAgent(ctx, agentID)
	if err != nil {
		return false, err.Error()
	}
	effective, err := e.toolPolicy.ResolveDeclared(ctx, toolpolicy.Query{
		WorkspaceID: workspaceID, ToolKey: toolName, RuntimeID: agent.RuntimeID,
		AgentID: agentID, UserID: agent.OwnerID, Base: toolpolicy.SettingAllow,
	})
	if err != nil {
		return false, err.Error()
	}
	if effective.Setting == toolpolicy.SettingAllow {
		return true, ""
	}
	return false, effective.Reason
}

func gatewayPlatformActionResource(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("request:%x", sha256.Sum256(raw))
}

// logToolDecision emits the FIR-2243 B1 runtime audit line: one structured record
// per tool call tying the tool to the permission decision it ran under and the run
// that made it (agent/task/issue/surface), plus the deciding connection when the
// verdict came from a connection rule. It fires for EVERY call — including the
// default-off allow path that previously logged nothing — so the audit trail can
// answer "which agent ran which tool, on which issue, and was it allowed". A
// blocked call logs at Warn, an allowed call at Info. Field names match the B2
// gateway-trace line so the registry can consume both consistently.
func (e *FirtalGatewayExecutor) logToolDecision(meta GatewayRequestMeta, toolName, connName, decision string, allowed bool, reason string) {
	if e == nil || e.logger == nil {
		return
	}
	attrs := []any{
		"event", "tool_call_decision",
		"tool", toolName,
		"connection", connName,
		"decision", decision,
		"allowed", allowed,
		"agent_id", meta.AgentID,
		"agent_name", meta.AgentName,
		"task_id", meta.TaskID,
		"issue_id", meta.IssueID,
		"surface", meta.Surface,
	}
	if allowed {
		e.logger.Info("runtime tool decision (FIR-2243 B1)", attrs...)
		return
	}
	e.logger.Warn("runtime tool decision (FIR-2243 B1)", append(attrs, "reason", reason)...)
}

// workspaceApprovalGateEnabled checks the cerebro_approval_gate workspace
// feature flag. The workspace-level row uses the all-zero sentinel user_id (see
// feature_flags.sql.go). When no override row exists → use the default (true).
// Any DB error → fail-open (true) so a DB glitch never blocks an active workspace.
func (e *FirtalGatewayExecutor) workspaceApprovalGateEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if e == nil || e.cerebro == nil {
		return true
	}
	enabled, err := e.cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     "cerebro_approval_gate",
	})
	if err != nil {
		return true // no override or DB error → default ON
	}
	return enabled
}

// approvalInboxActive reports whether an Ask verdict on this call will reach
// the approval inbox and block for a human. The workspace feature setting is
// the only authoring switch; there is no second server rollout control.
// FIR-2388 uses it to fail
// CLOSED on an API-connection endpoint Ask when the inbox is inactive: those
// front the secrets box and must never run unapproved.
func (e *FirtalGatewayExecutor) approvalInboxActive(ctx context.Context, workspaceID pgtype.UUID) bool {
	if e == nil || e.gate == nil {
		return false
	}
	return e.workspaceApprovalGateEnabled(ctx, workspaceID)
}

// memberOverrideEnabled reports whether cerebro_member_override is on for the
// workspace — the switch that makes the GENERAL tool-policy gate resolve
// through the member-override model (a member may loosen a group default)
// instead of the pure tighten-only chain. The registry default is ON
// (packages/cerebro-feature-flags/registry.ts), but a DB lookup miss or error
// still resolves to OFF here, like policyCELEvaluator and unlike the always-on
// gates: a path that can LOOSEN access must never switch itself on by accident
// just because a workspace row is missing. The deny-by-default floors never
// call this — they stay on Resolve. (Registry-vs-missing-row gap tracked in
// FIR-3176.)
func (e *FirtalGatewayExecutor) memberOverrideEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if e == nil || e.cerebro == nil {
		return false
	}
	enabled, err := e.cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     toolpolicy.FlagMemberOverride,
	})
	if err != nil || !enabled {
		return false // no override or DB error → default OFF → pure tighten-only
	}
	return true
}

// policyCELEvaluator returns the shared CEL evaluator when the default-OFF
// cerebro_policy_cel flag is enabled for the workspace, else nil. Returning nil
// leaves Query.Eval unset, so an Expr condition is undecidable and fails closed —
// the behaviour-preserving default. A DB lookup miss or error resolves to OFF
// (the flag's default): an unproven expression-evaluation path must not switch
// itself on by accident.
func (e *FirtalGatewayExecutor) policyCELEvaluator(ctx context.Context, workspaceID pgtype.UUID) toolpolicy.ExprEvaluator {
	if e == nil || e.cerebro == nil {
		return nil
	}
	enabled, err := e.cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     toolpolicy.FlagPolicyCEL,
	})
	if err != nil || !enabled {
		return nil // no override or DB error → default OFF → Expr stays undecidable
	}
	eval, err := toolpolicy.SharedCELEvaluator()
	if err != nil {
		e.logger.Warn("tool-policy gate: CEL evaluator unavailable — Expr conditions fail closed",
			"workspace_id", workspaceID, "error", err)
		return nil
	}
	return eval.Eval
}

// EnableApprovalGate wires the one shared approval seam for every agent.
func (e *FirtalGatewayExecutor) EnableApprovalGate(gate *permgate.Gate) {
	if e == nil {
		return
	}
	e.gate = gate
}

// Operational timing knobs do not change who has access. Access and the Ask
// behavior are authored in Settings → Permissions and Settings → Features.
const (
	envApprovalGateWait = "CEREBRO_APPROVAL_GATE_WAIT"
	envApprovalGateTTL  = "CEREBRO_APPROVAL_GATE_TTL"

	defaultApprovalGateWait = 10 * time.Minute
	defaultApprovalGateTTL  = 30 * time.Minute
)

// BuildApprovalGate constructs the shared approval enforcement gate — the single
// "resolve → on Ask create an inbox request → await the human decision" seam.
// The same gate type backs the agent tool loop, daemon repo checkout, and
// credential governance, so a needs-approval verdict
// from any of them lands in the one /approvals inbox instead of a silent block.
func BuildApprovalGate(cerebroQueries *cerebrodb.Queries, tx approvals.TxStarter, bus *events.Bus) *permgate.Gate {
	approvalsSvc := approvals.New(cerebroQueries, tx, bus)
	gate := permgate.New(approvalsSvc)
	gate.WaitTimeout = durationFromEnv(defaultApprovalGateWait, envApprovalGateWait)
	gate.ApprovalTTL = durationFromEnv(defaultApprovalGateTTL, envApprovalGateTTL)
	return gate
}
