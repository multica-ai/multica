package runtime

// connection_tool_resolver_brief.go (FIR-2441 — the Flip, slice 1) points the
// claim-time brief at the UNIFIED ConnectionToolResolver instead of the api-only
// APIConnectionResolver. It is the first of the three api consumers to move onto
// Resolve; the cloud executor and the local handler follow in later slices.
//
// The brief only needs the api half (the mcp_http tools already reach the brief
// through the tool-policy chain in handler.cerebroEffectiveToolsForBrief), so it
// reads out.APITools — the callable handles Resolve carries verbatim from the
// reused APIConnectionResolver. Semantics are therefore identical to the old
// direct path when the flag is on, and both return nil when it is off, so the
// wiring swap is reversible: flag off ⇒ no observable change.
//
// It lives here, in the runtime package that already imports handler, so the
// handler package never imports runtime back (that would be a cycle) — the same
// reason api_connection_resolver_brief.go lives here. The router wires the
// *ConnectionToolResolver in as handler.APIConnectionBrief.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/handler"
)

// Compile-time proof the unified resolver satisfies the same brief seam the
// api-only resolver did, so the router can swap one for the other.
var _ handler.CerebroAPIConnectionBriefResolver = (*ConnectionToolResolver)(nil)

// APIConnectionToolsForBrief resolves the agent's granted api-connection endpoint
// tools through the unified Resolve and maps its APITools to the handler brief
// shape. Name is the exact tool name the agent calls verbatim, so the rendered
// brief lists the callable identity, not a decorated label.
//
// InitiatorID threads the delegated task initiator (on_behalf_of, FIR-2441) into
// the resolve so the brief matches the cloud call-time guard (apiEndpointSetting),
// which now also passes the initiator as a tighten-only Deny-only layer: a
// member-denied endpoint is hidden from the brief exactly as the call path refuses
// it. Zero when there is no delegation, so the non-delegated path is unchanged.
func (r *ConnectionToolResolver) APIConnectionToolsForBrief(ctx context.Context, ident handler.CerebroAPIConnectionBriefIdentity) []handler.CerebroAPIConnectionBriefTool {
	out := r.Resolve(ctx, ConnectionIdentity{
		WorkspaceID: ident.WorkspaceID,
		RuntimeID:   ident.RuntimeID,
		AgentID:     ident.AgentID,
		OwnerID:     ident.OwnerID,
		InitiatorID: ident.InitiatorID,
	})
	if len(out.APITools) == 0 {
		return nil
	}
	briefs := make([]handler.CerebroAPIConnectionBriefTool, 0, len(out.APITools))
	for _, v := range out.APITools {
		briefs = append(briefs, handler.CerebroAPIConnectionBriefTool{
			Connection:  v.Tool.ConnectionName(),
			Name:        v.Tool.Name(),
			Description: v.Tool.Description(),
			Verdict:     string(v.Verdict),
		})
	}
	return briefs
}
