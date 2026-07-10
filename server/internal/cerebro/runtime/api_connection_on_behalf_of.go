package runtime

// api_connection_on_behalf_of.go (FIR-2668): per-agent delegation on API-type
// connections.
//
// A connection with auth_config.on_behalf_of.enabled dispatches every
// server-side call with the calling agent stamped in the X-On-Behalf-Of header
// ("agent:<uuid>" — the Firtal Data Registry delegation contract, FIR-2564
// fase 1, lib/registry/delegation.ts). The remote API resolves the call to
// THAT agent's own grants: the shared connection key never leaves the Cerebro
// backend and needs no data grants of its own, and per-agent access is managed
// remotely (the key's delegation_allowlist plus the agent's grants) instead of
// handing per-agent keys to runtimes.
//
// Trust and failure posture:
//   - The agent identity is ONLY the authenticated caller (GatewayRequestMeta
//     .AgentID on the cloud gateway tool loops, the mat_-token-resolved
//     identity on the local connection-tools surface) — never a value the
//     model controls.
//   - No agent identity on the context (system runs, human surfaces) → the
//     header is not sent and the call proceeds exactly as before. A
//     delegation-guarded remote API then decides by the shared key's own
//     grants, which for a delegation-only key are none (fail closed remotely).
//   - on_behalf_of takes precedence over session_exchange for agent callers: a
//     delegated agent call runs on the shared key + header, never on the
//     triggering human's exchanged session key, so an agent cannot borrow the
//     human's (broader) access.

import (
	"context"
	"strings"
)

// onBehalfOfHeader is the delegation header the Firtal Data Registry contract
// expects (lib/registry/delegation.ts: ON_BEHALF_OF_HEADER).
const onBehalfOfHeader = "X-On-Behalf-Of"

// connectionAgentKey carries the calling agent's UUID from the tool loop down
// to APIConnectionTool.Call via context.
type connectionAgentKey struct{}

// WithConnectionAgent returns a context carrying the calling agent's id for
// connection dispatch. An empty id returns ctx unchanged.
func WithConnectionAgent(ctx context.Context, agentID string) context.Context {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, connectionAgentKey{}, agentID)
}

// ConnectionAgent returns the calling agent's id carried by the context, or ""
// when the dispatch has no agent caller.
func ConnectionAgent(ctx context.Context) string {
	s, _ := ctx.Value(connectionAgentKey{}).(string)
	return s
}
