package runtime

// connection_tool_resolver_executor.go (FIR-2441 — the Flip, slice 2) points the
// CLOUD EXECUTOR at the unified ConnectionToolResolver instead of the api-only
// APIConnectionResolver. It is the second of the three api consumers to move onto
// Resolve; the claim brief moved first (connection_tool_resolver_brief.go, #2000)
// and the local handler follows in slice 3.
//
// The executor only registers the api half (out.APITools) — the *APIConnectionTool
// handles the task registry dispatches, exactly the shape ListForAgent returned
// before. The mcp_http half of Resolve is inert on this path for now (the cloud
// runtime reaches mcp_http connections through the injected --mcp-config, not the
// registry); the executor simply ignores out.MCPServers/out.Deny this slice.
//
// Parity by construction: connectionToolResolver reuses the SAME
// apiConnectionResolver() the executor called before as the api sub-component, so
// out.APITools is byte-for-byte the ListForAgent(...) verdict list — identical
// callable handles, same flag gate (Resolve returns the zero value when
// cerebro_api_connection_tools is off, just as ListForAgent did). Flag off ⇒ no
// observable change, so the swap is reversible.

// connectionToolResolver builds the unified ConnectionToolResolver from the
// executor's own pool-backed stores, mirroring apiConnectionResolver(). The api
// half is the reused APIConnectionResolver (Category A); the mcp_http half reads
// enabled connections via apiConnStore and per-tool verdicts via connDeny (the
// always-on toolpolicy store) so "listed == callable" holds for both types. A nil
// entryFor defaults to the direct-URL relay entry a cloud runtime uses. A nil
// apiConnStore/connDeny (feature not wired) makes Resolve yield no tools, exactly
// as the old apiConnectionResolver().ListForAgent guard did.
func (e *FirtalGatewayExecutor) connectionToolResolver() *ConnectionToolResolver {
	if e == nil {
		return nil
	}
	return NewConnectionToolResolver(
		e.apiConnectionResolver(),
		e.apiConnStore,
		e.connDeny,
		e.cerebro,
		nil, // defaultMCPEntry — the direct-URL entry a cloud runtime uses
		e.logger,
	)
}
