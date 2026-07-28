package runtime

// connection_tool_resolver_brief.go (FIR-2441 — the Flip, slice 1) points the
// claim-time brief at the UNIFIED ConnectionToolResolver instead of the api-only
// APIConnectionResolver. It is the first of the three api consumers to move onto
// Resolve; the cloud executor and the local handler follow in later slices.
//
// The claim consumes both halves. API endpoints are server-dispatched, while
// Allow-only mcp_http connections are injected directly into the local runtime.
// Runtime inventory can lag that per-task injection, so this adapter returns
// both exact callable identities for the brief and immutable task mandate.
//
// It lives here, in the runtime package that already imports handler, so the
// handler package never imports runtime back (that would be a cycle) — the same
// reason api_connection_resolver_brief.go lives here. The router wires the
// *ConnectionToolResolver in as handler.APIConnectionBrief.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/handler"
)

// Compile-time proof the unified resolver satisfies the same brief seam the
// api-only resolver did, so the router can swap one for the other.
var _ handler.CerebroAPIConnectionBriefResolver = (*ConnectionToolResolver)(nil)

// APIConnectionToolsForBrief resolves every connection tool injected into this
// task and maps it to the handler's claim shape. Name is the exact identity the
// local hook observes and the task mandate authorizes.
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
		if len(out.Tools) == 0 {
			return nil
		}
	}
	briefs := make([]handler.CerebroAPIConnectionBriefTool, 0, len(out.APITools)+len(out.Tools))
	for _, v := range out.APITools {
		briefs = append(briefs, handler.CerebroAPIConnectionBriefTool{
			Connection:  v.Tool.ConnectionName(),
			Name:        v.Tool.Name(),
			Description: v.Tool.Description(),
			Verdict:     string(v.Verdict),
		})
	}
	withheld := make(map[string]bool)
	for _, evaluation := range out.Evaluations {
		if evaluation.Type == connections.TypeMCPHTTP && evaluation.Outcome == evaluationRelayWithheld {
			withheld[evaluation.Connection] = true
		}
	}
	for _, tool := range out.Tools {
		if tool.Type != connections.TypeMCPHTTP || tool.Verdict != toolpolicy.SettingAllow || withheld[tool.Connection] {
			continue
		}
		briefs = append(briefs, handler.CerebroAPIConnectionBriefTool{
			Connection:    tool.Connection,
			Name:          toolpolicy.MCPToolToken(tool.Connection, tool.Name),
			Verdict:       string(tool.Verdict),
			MandatePrefix: taskmandate.MCPServerWildcard(toolpolicy.MCPToolToken(tool.Connection, tool.Name)),
		})
	}
	return briefs
}
