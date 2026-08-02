package handler

// daemon_effective_tools_cerebro.go (FIR-2312) resolves, at task-claim time,
// the set of NON-CLI tools an agent may actually use — MCP tools and connection
// tools — and ships them in the claim response as `effective_tools`. The daemon
// copies them into the agent brief's `## Available Commands` block via
// execenv.cerebroToolsBrief, so an agent finally sees the connections and MCP
// tools it has, not just the static `multica` CLI list.
//
// This is the single server-side chokepoint for the pattern: it reuses the same
// toolaccess resolver the admin "effective access" UI uses
// (h.runtimeToolAccess.ListEffectiveTools), which already folds the full
// tool-policy chain (Workspace › Runtime › Agent › Group › User). Any future
// tool family that flows through the runtime tool inventory + tool-policy chain
// is surfaced here automatically — no new wiring per tool. See
// docs/agents/agent-tool-brief.md.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentTaskToolEntry is one non-CLI tool shipped to the daemon for the brief.
// Family groups it under a heading ("MCP tools" / "Connections" / "Platform
// tools"); Name is the exact tool name the agent calls; Description is the
// model-facing one-liner; Verdict is "allow" or "ask".
type AgentTaskToolEntry struct {
	// CEREBRO-PATCH(connection-instructions-claim): FIR-2760 attaches guidance only after tool-policy filtering.
	Family       string `json:"family"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Verdict      string `json:"verdict,omitempty"`
	Connection   string `json:"connection,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type CerebroConnectionInstructionsBriefResolver interface {
	// CEREBRO-PATCH(connection-instructions-claim): The resolver receives only exposed connection names.
	ConnectionInstructionsForBrief(context.Context, pgtype.UUID, []string) map[string]string
}

// CerebroAPIConnectionBriefResolver (FIR-2388) resolves the api-type connection
// endpoint tools an agent is granted, for the claim-time brief. It is the SAME
// resolver the cloud gateway and the local MCP handler use, so a tool listed in
// the brief is a tool the agent can actually call. It is an injected interface —
// not a direct call — because the concrete resolver lives in the cerebro/runtime
// package, which imports this handler package; the handler importing runtime back
// would be a cycle. The router wires *cerebro/runtime.APIConnectionResolver here.
type CerebroAPIConnectionBriefResolver interface {
	APIConnectionToolsForBrief(ctx context.Context, ident CerebroAPIConnectionBriefIdentity) []CerebroAPIConnectionBriefTool
}

// CerebroAPIConnectionBriefIdentity is the actor the endpoints are gated for. It
// mirrors runtime.APIConnectionIdentity but is declared here so the interface has
// no runtime-package types in its signature (keeping the cycle broken).
//
// InitiatorID is the delegated task initiator (on_behalf_of, FIR-2441): a
// tighten-only layer the endpoint gate honours Deny-only, so the brief lists a
// member-denied tool as hidden exactly as the call path refuses it. Zero when
// there is no delegation.
type CerebroAPIConnectionBriefIdentity struct {
	WorkspaceID pgtype.UUID
	RuntimeID   pgtype.UUID
	AgentID     pgtype.UUID
	OwnerID     pgtype.UUID
	InitiatorID pgtype.UUID
}

// CerebroAPIConnectionBriefTool is one resolved endpoint tool for the brief.
// Name is the exact tool name the agent calls verbatim; Verdict is "allow" or
// "ask".
type CerebroAPIConnectionBriefTool struct {
	Connection    string
	Name          string
	Description   string
	Verdict       string
	MandatePrefix string
}

// cerebroEffectiveToolsForBrief resolves the agent's exposed non-CLI tools for
// the brief. Returns nil (no section) when the tool-access service is not wired,
// the agent is unknown, or nothing is exposed — all expected, non-error cases.
// The brief is best-effort, so a resolution error is swallowed here (no section);
// the claim path handles it via cerebroEffectiveToolsForClaim's error return.
func (h *Handler) cerebroEffectiveToolsForBrief(ctx context.Context, runtime db.AgentRuntime, agent *TaskAgentData, initiatorType, initiatorID string) []AgentTaskToolEntry {
	tools, _, _ := h.cerebroEffectiveToolsForClaim(ctx, runtime, agent, initiatorType, initiatorID)
	return tools
}

// cerebroEffectiveToolsForClaim resolves the final claim-time tool exposure once
// and returns the human-readable brief entries, the exact callable names that may
// be snapshotted into the task mandate, and an error.
//
// FIR-3403 (TRIN 1b): the mandate is a fail-closed allowlist — an EMPTY mandate
// denies every tool (Authorize returns ErrToolDeny for all), including built-in
// Bash/Read/Edit. So "resolved to zero tools" and "could not resolve" must not
// collapse to the same nil result: the former is a legitimate empty mandate, the
// latter must NOT silently issue a total-lockout mandate. The two cases where the
// resolver was actually consulted and FAILED (a malformed agent id, or a
// ListEffectiveTools error) therefore return a non-nil error, so the claim path
// fails the claim (task retries) instead of bricking the run. The
// resolver-not-wired / nil-agent cases stay non-error (the feature is simply
// unavailable — unchanged behaviour), as does a genuine zero-tool resolution.
func (h *Handler) cerebroEffectiveToolsForClaim(ctx context.Context, runtime db.AgentRuntime, agent *TaskAgentData, initiatorType, initiatorID string) ([]AgentTaskToolEntry, []string, error) {
	if h == nil || h.runtimeToolAccess == nil || agent == nil {
		return nil, nil, nil
	}
	agentID, err := util.ParseUUID(agent.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("cerebro effective tools: parse agent id %q: %w", agent.ID, err)
	}
	// A failed runtime inventory must be visible at claim time. If the daemon
	// could not measure a provider and no curated fallback exists, issuing an
	// empty finalized mandate would turn a discovery outage into a silent total
	// lockout. The claim retries instead, while the runtime capability snapshot
	// remains explicitly marked not_measured for operators.
	caps := normalizedRuntimeCapabilities(runtime.Provider, runtime.Capabilities, runtime.ToolsConfig)
	if method, _ := caps["discovery_method"].(string); method == "not_measured" && len(anyStringSlice(caps["tools"])) == 0 {
		return nil, nil, fmt.Errorf("cerebro effective tools: provider inventory not measured for %q", runtime.Provider)
	}

	// Actor layers (FIR-2441). The agent owner and the delegated member (the task
	// initiator, on_behalf_of) are two DISTINCT policy layers — matching the claim-
	// time connection resolver (connection_tool_resolver.userCeiling), so the brief
	// lists exactly what the run can call (list == callable):
	//   - User layer  = the fail-closed intersection of owner and initiator: no
	//     initiator → the owner; owner == initiator → that user; owner != initiator
	//     → the zero UUID, so a personal grant on either party never leaks to the
	//     other.
	//   - on_behalf_of = the initiator member, resolved tighten-only, so a member
	//     denied a tool across every agent they drive is reflected here too.
	var ownerID, initiatorMember pgtype.UUID
	if strings.TrimSpace(agent.OwnerID) != "" {
		if u, err := util.ParseUUID(agent.OwnerID); err == nil {
			ownerID = u
		}
	}
	if initiatorType == "member" && strings.TrimSpace(initiatorID) != "" {
		if u, err := util.ParseUUID(initiatorID); err == nil {
			initiatorMember = u
		}
	}
	userID := ownerID
	if initiatorMember.Valid {
		if !(ownerID.Valid && ownerID.Bytes == initiatorMember.Bytes) {
			// Owner != initiator (or owner unset): the intersection is empty, so the
			// user layer is fail-closed (no user-scoped grant applies to either).
			userID = pgtype.UUID{}
		}
	}

	// FIR-3781 instrumentation: ListEffectiveTools resolves the tool-policy chain
	// once per capability, so its cost is N x (per-capability DB work). build_ms on
	// the claim endpoint brackets the whole response build; this line isolates the
	// resolver's share and reports N, which is what tells N-grew apart from
	// per-item-got-slower. Logged above 250ms so a healthy claim stays quiet.
	resolveStart := time.Now()
	rows, err := h.runtimeToolAccess.ListEffectiveTools(ctx, RuntimeToolAccessQuery{
		WorkspaceID:         runtime.WorkspaceID,
		RuntimeID:           runtime.ID,
		RuntimeMode:         runtime.RuntimeMode,
		RuntimeProvider:     runtime.Provider,
		RuntimeCapabilities: marshalRuntimeCapabilities(normalizedRuntimeCapabilities(runtime.Provider, runtime.Capabilities, runtime.ToolsConfig)),
		AgentID:             agentID,
		UserID:              userID,
		OnBehalfOfID:        initiatorMember,
	})
	if err != nil {
		// FIR-3403 (TRIN 1b): a resolution error is NOT "zero tools". Returning an
		// empty mandate here would deny every tool (including built-ins) for the
		// whole run. Surface the error so the claim path fails the claim and the
		// task retries, rather than issuing a silent total-lockout mandate.
		return nil, nil, fmt.Errorf("cerebro effective tools: list effective tools: %w", err)
	}
	if elapsed := time.Since(resolveStart); elapsed > 250*time.Millisecond {
		perTool := int64(0)
		if len(rows) > 0 {
			perTool = elapsed.Milliseconds() / int64(len(rows))
		}
		slog.Info("claim tool resolve slow",
			"runtime_id", uuidToString(runtime.ID),
			"agent_id", agent.ID,
			"total_ms", elapsed.Milliseconds(),
			"capabilities", len(rows),
			"per_capability_ms", perTool,
		)
	}

	out := make([]AgentTaskToolEntry, 0, len(rows))
	mandateTools := make([]string, 0, len(rows))
	var supplementalMandateTools []string
	for _, v := range rows {
		if !v.ExposureEffective.Effective {
			continue // not actually exposed to this agent → omit
		}
		name := strings.TrimSpace(v.Descriptor.DisplayName)
		if name == "" {
			name = strings.TrimSpace(v.Descriptor.ToolKey)
		}
		if name == "" {
			continue
		}
		callableName := strings.TrimSpace(v.Inventory.ToolName)
		if strings.EqualFold(v.Descriptor.Source, "mcp") {
			callableName = claudehook.CanonicalToolName(callableName)
		}
		if callableName == "" {
			callableName = name
		}
		mandateTools = append(mandateTools, callableName)
		family := "Platform tools"
		server := strings.TrimSpace(v.Inventory.MCPServerName)
		switch {
		case server != "":
			family = "Connections"
			name = server + " / " + name
		case v.Descriptor.Source == "mcp":
			family = "MCP tools"
		}
		out = append(out, AgentTaskToolEntry{
			Family:      family,
			Name:        name,
			Description: strings.TrimSpace(v.Descriptor.Description),
			Verdict:     strings.TrimSpace(v.Policy.Effective),
			Connection:  server,
		})
	}

	// FIR-2388: append the api-type connection endpoint tools this agent is
	// granted. The tool-policy chain above (ListEffectiveTools) covers MCP and
	// platform tools but NOT api-connection endpoints — those are a separate,
	// server-side-dispatched family gated by ConnectionEndpointEffective. Resolve
	// them through the SAME shared resolver the cloud gateway and local MCP handler
	// use, so "listed in the brief" == "callable" for every runtime. The endpoint
	// gate keys on the agent's OWNER as the user layer (matching the cloud call-
	// time guard, apiEndpointSetting) PLUS the delegated initiator as the tighten-
	// only on_behalf_of layer (FIR-2441), so the brief cannot show a tool the call
	// path would then refuse — including a member-level Deny on the initiator.
	if h.APIConnectionBrief != nil {
		seen := make(map[string]struct{}, len(mandateTools))
		for _, callableName := range mandateTools {
			seen[callableName] = struct{}{}
		}
		apiTools := h.APIConnectionBrief.APIConnectionToolsForBrief(ctx, CerebroAPIConnectionBriefIdentity{
			WorkspaceID: runtime.WorkspaceID,
			RuntimeID:   runtime.ID,
			AgentID:     agentID,
			OwnerID:     ownerID,
			InitiatorID: initiatorMember,
		})
		for _, t := range apiTools {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			if prefix := strings.TrimSpace(t.MandatePrefix); prefix != "" {
				if _, dup := seen[prefix]; !dup {
					seen[prefix] = struct{}{}
					supplementalMandateTools = append(supplementalMandateTools, prefix)
				}
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			mandateTools = append(mandateTools, name)
			verdict := strings.TrimSpace(t.Verdict)
			if verdict == "" {
				verdict = "allow"
			}
			out = append(out, AgentTaskToolEntry{
				Family:      "Connections",
				Name:        name,
				Description: strings.TrimSpace(t.Description),
				Verdict:     verdict,
				Connection:  strings.TrimSpace(t.Connection),
			})
		}
	}
	if h.ConnectionInstructionsBrief != nil {
		// CEREBRO-PATCH(connection-instructions-claim): Never load guidance for a denied/hidden connection.
		names, seen := []string{}, map[string]bool{}
		for _, e := range out {
			if e.Connection != "" && !seen[e.Connection] {
				seen[e.Connection] = true
				names = append(names, e.Connection)
			}
		}
		instructions := h.ConnectionInstructionsBrief.ConnectionInstructionsForBrief(ctx, runtime.WorkspaceID, names)
		for i := range out {
			out[i].Instructions = strings.TrimSpace(instructions[out[i].Connection])
		}
	}

	if len(out) == 0 {
		// Resolved successfully to zero exposed tools — a legitimate empty result,
		// distinct from the error returns above. The caller may issue an empty
		// mandate for it (no error).
		return nil, nil, nil
	}
	// Exact callable identities stay positionally aligned with out. Supplemental
	// raw-server wildcards follow them so SessionMode filtering cannot replace an
	// offered callable with its wildcard authorization identity.
	mandateTools = append(mandateTools, supplementalMandateTools...)
	return out, mandateTools, nil
}

func filterClaimToolsForSessionMode(tools []AgentTaskToolEntry, mandateTools, allowedTools []string) ([]AgentTaskToolEntry, []string) {
	if len(allowedTools) == 0 {
		return tools, mandateTools
	}
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		allowed[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	filteredTools := tools[:0]
	filteredMandate := mandateTools[:0]
	for i, tool := range tools {
		_, displayAllowed := allowed[strings.ToLower(strings.TrimSpace(tool.Name))]
		callableName := ""
		callableAllowed := false
		if i < len(mandateTools) {
			callableName = mandateTools[i]
			_, callableAllowed = allowed[strings.ToLower(strings.TrimSpace(callableName))]
			if !callableAllowed {
				if wildcard := taskmandate.MCPServerWildcard(callableName); wildcard != "" {
					_, callableAllowed = allowed[strings.ToLower(wildcard)]
				}
			}
		}
		if displayAllowed || callableAllowed {
			filteredTools = append(filteredTools, tool)
			if callableName != "" {
				filteredMandate = append(filteredMandate, callableName)
			}
		}
	}
	for _, supplemental := range mandateTools[min(len(tools), len(mandateTools)):] {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(supplemental))]; ok {
			filteredMandate = append(filteredMandate, supplemental)
		}
	}
	return filteredTools, filteredMandate
}
