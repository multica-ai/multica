package runtime

// connection_tool_resolver_claim.go (FIR-2441 — the Flip) points the CLAIM-TIME
// mcp_http injection at the UNIFIED ConnectionToolResolver. The claim is the
// fourth real consumer to move onto Resolve, after the brief (slice 1, #2000),
// the cloud executor (slice 2, #2001) and the local connectiontools handler
// (slice 3, #2002).
//
// Unlike those three (which read the api half, out.APITools), the claim consumes
// the mcp_http half: out.MCPServers (the Allow-only raw relay entries, ready to
// merge into --mcp-config) and out.Deny (the --disallowedTools tokens for every
// Deny-verdict mcp_http tool). Both come from the SAME Resolve() call, so the
// injected servers and the deny list can never drift — the whole point of the
// unification.
//
// handled reports whether the resolver was authoritative for this claim:
//
//   - flag ON  ⇒ handled=true, the resolver's MCPServers/Deny ARE the claim
//     decision. The Allow-only rule (Lone #1) applies: a connection with any
//     Ask/Deny tool is withheld from the raw relay, closing the gap where the
//     relay honours no per-tool policy.
//   - flag OFF ⇒ handled=false, the caller (handler.injectClaimConnectionMCP)
//     falls back to the legacy BuildMCPConfig/DisallowedMCPTools path unchanged.
//     Because the claim-time mcp_http injection is a LIVE feature (not behind
//     this flag), that fallback is what keeps the swap reversible: flag off ⇒
//     identical to today.
//
// It lives in the runtime package (which already imports handler) so the handler
// package never imports runtime back — the same no-cycle reason the brief and
// executor adapters live here. The router wires the *ConnectionToolResolver in as
// handler.CerebroConnectionClaimResolver, constructed with a relay-aware entry
// builder so LOCAL runtimes receive Multica relay URLs (FIR-1563), matching the
// legacy claim path's relay behavior.

import (
	"context"
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/handler"
)

// Compile-time proof the unified resolver satisfies the claim seam.
var _ handler.CerebroConnectionClaimResolver = (*ConnectionToolResolver)(nil)

// ClaimConnectionMCP resolves the claim-time mcp_http --mcp-config and
// --disallowedTools deny tokens through the unified Resolve when the feature flag
// is on. InitiatorID is left unset: the claim gates the connection user layer on
// the agent owner only (ceiling = OwnerID), matching the legacy DisallowedMCPTools
// owner resolution and the brief/executor call paths — the non-delegated ceiling.
func (r *ConnectionToolResolver) ClaimConnectionMCP(ctx context.Context, ident handler.CerebroConnectionClaimIdentity) (json.RawMessage, []string, bool) {
	if r == nil || !ident.WorkspaceID.Valid || !ident.AgentID.Valid || !r.flagEnabled(ctx, ident.WorkspaceID) {
		// Not authoritative — the caller runs the legacy path.
		return nil, nil, false
	}
	out := r.Resolve(ctx, ConnectionIdentity{
		WorkspaceID: ident.WorkspaceID,
		RuntimeID:   ident.RuntimeID,
		AgentID:     ident.AgentID,
		OwnerID:     ident.OwnerID,
		// The delegated member (task initiator) gates the tighten-only on_behalf_of
		// policy layer, so a member-denied connection tool is withheld at claim
		// across every agent they drive (FIR-2441).
		InitiatorID: ident.InitiatorID,
	})
	return out.MCPServers, out.Deny, true
}
