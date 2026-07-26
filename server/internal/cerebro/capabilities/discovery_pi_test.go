package capabilities

import (
	"slices"
	"testing"
)

// TestFor_PiIsMapped is the FIR-3762 regression test. Pi agents could start
// under mandatory tool-policy enforcement but every single tool call — including
// read-only ones — came back "task mandate denied the call".
//
// The cause was this registry: "pi" had no row, so For("pi") fell through to
// staticFallback, whose ToolProtocols is empty. toolaccess.exposureEffective
// requires a supported protocol, so no tool was ever exposed at claim time, the
// task mandate was issued with an empty allowlist, and taskmandate.Authorize
// denied everything.
//
// Before the fix this test fails on both assertions.
func TestFor_PiIsMapped(t *testing.T) {
	s := For("pi")

	if s.DiscoveryMethod != "static" {
		t.Errorf("provider \"pi\": expected DiscoveryMethod=static (a curated row), got %q — pi fell through to staticFallback", s.DiscoveryMethod)
	}
	if !slices.Contains(s.ToolProtocols, "mcp_stdio") {
		t.Errorf("provider \"pi\": expected ToolProtocols to contain \"mcp_stdio\", got %v — the Pi harness talks MCP over stdio, and an empty protocol list denies every tool", s.ToolProtocols)
	}
}

// TestProviderRegistry_EveryProviderDeclaresAToolProtocol guards the invariant
// the Pi outage broke: a runtime we ship agents on must declare how Cerebro may
// hand it tools. A row with no protocol exposes nothing, which reaches the agent
// as a total lockout rather than as a visible configuration error.
func TestProviderRegistry_EveryProviderDeclaresAToolProtocol(t *testing.T) {
	for provider, set := range providerRegistry {
		if len(set.ToolProtocols) == 0 {
			t.Errorf("provider %q: ToolProtocols is empty — every tool call for this runtime will be denied at claim time", provider)
		}
	}
}
