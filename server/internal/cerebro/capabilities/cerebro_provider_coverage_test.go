// CEREBRO-PATCH(capabilities-provider-coverage): FIR-3762 follow-up — a runtime
// provider that ships without a providerRegistry row is a silent total lockout,
// not a missing default. staticFallback returns empty ToolProtocols, the protocol
// check in toolaccess.exposureEffective then fails for EVERY capability, the task
// mandate is issued empty, and taskmandate.Authorize denies every call for the
// whole run. The agent starts and cannot read its own issue.
//
// Pi hit exactly that. So did hermes, opencode and antigravity — 14 of 39 runtimes
// in the Firtal workspace reported discovery_method="unmapped" with zero tools when
// this test was written. This test turns the next occurrence into a red build.
//
// The supported-provider list is read out of agent.New's own error message rather
// than copied here, so adding a backend to agent.New automatically extends this
// contract instead of quietly bypassing it.
package capabilities

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func agentSupportedProviders(t *testing.T) []string {
	t.Helper()
	_, err := agent.New("__no_such_provider__", agent.Config{})
	if err == nil {
		t.Fatal("agent.New accepted an unknown provider; cannot derive the supported list")
	}
	_, list, ok := strings.Cut(err.Error(), "(supported:")
	if !ok {
		t.Fatalf("agent.New error no longer carries the supported list: %v", err)
	}
	list = strings.TrimSuffix(strings.TrimSpace(list), ")")
	out := make([]string, 0, 16)
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	if len(out) < 10 {
		t.Fatalf("derived only %d providers from agent.New (%q); the parse is wrong", len(out), list)
	}
	return out
}

// TestEveryAgentProviderIsMapped is the invariant: every provider a runtime can
// actually run must have a curated row. An unmapped provider exposes no tools at
// all, which reads as "permissions are wrong" and is impossible to diagnose from
// the outside.
func TestEveryAgentProviderIsMapped(t *testing.T) {
	for _, provider := range agentSupportedProviders(t) {
		set := For(provider)
		if set.DiscoveryMethod == "unmapped" {
			t.Errorf("provider %q has no providerRegistry row: every tool call would be denied for the whole run", provider)
			continue
		}
		if len(set.ToolProtocols) == 0 {
			t.Errorf("provider %q declares no tool protocol: toolaccess.exposureEffective would reject every capability", provider)
		}
	}
}

// TestNoRegistryRowIsProtocolless guards the same invariant from the other side:
// a row may legitimately carry an empty static tool list (the inventory comes from
// the measured path), but never an empty protocol list.
func TestNoRegistryRowIsProtocolless(t *testing.T) {
	for _, provider := range KnownProviders() {
		if len(For(provider).ToolProtocols) == 0 {
			t.Errorf("registry row %q declares no tool protocol; a row without one is a silent total lockout", provider)
		}
	}
}
