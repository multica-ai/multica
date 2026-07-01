package runtime

// api_connection_resolver_brief.go (FIR-2388) adapts the shared
// APIConnectionResolver to the claim-time brief. It lives here, in the runtime
// package — which already imports handler — so the handler package never imports
// runtime back (that would be a cycle). handler.cerebroEffectiveToolsForBrief
// calls this through the injected handler.CerebroAPIConnectionBriefResolver
// interface; the router wires *APIConnectionResolver in as the concrete value.
//
// Because the brief goes through the SAME ListForAgent every other surface uses,
// a tool the agent sees in its prompt is a tool it can actually call — the one
// invariant FIR-2388 exists to guarantee.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/handler"
)

// Compile-time proof the resolver satisfies the brief seam.
var _ handler.CerebroAPIConnectionBriefResolver = (*APIConnectionResolver)(nil)

// APIConnectionToolsForBrief resolves the agent's granted api-connection endpoint
// tools and maps them to the handler brief shape. Name is the exact tool name the
// agent calls verbatim (e.g. infisical_admin__get_api_v3_secrets_raw), so the
// rendered brief lists the callable identity, not a decorated label.
func (r *APIConnectionResolver) APIConnectionToolsForBrief(ctx context.Context, ident handler.CerebroAPIConnectionBriefIdentity) []handler.CerebroAPIConnectionBriefTool {
	verds := r.ListForAgent(ctx, APIConnectionIdentity{
		WorkspaceID: ident.WorkspaceID,
		RuntimeID:   ident.RuntimeID,
		AgentID:     ident.AgentID,
		OwnerID:     ident.OwnerID,
	})
	if len(verds) == 0 {
		return nil
	}
	out := make([]handler.CerebroAPIConnectionBriefTool, 0, len(verds))
	for _, v := range verds {
		out = append(out, handler.CerebroAPIConnectionBriefTool{
			Name:        v.Tool.Name(),
			Description: v.Tool.Description(),
			Verdict:     string(v.Verdict),
		})
	}
	return out
}
