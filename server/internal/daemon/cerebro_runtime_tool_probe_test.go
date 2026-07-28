// CEREBRO-PATCH(daemon-runtime-tool-probe): FIR-3762 tests for the per-provider
// tool-inventory probe.
package daemon

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func supportedProviders(t *testing.T) []string {
	t.Helper()
	_, err := agent.New("__no_such_provider__", agent.Config{})
	if err == nil {
		t.Fatal("agent.New accepted an unknown provider; cannot derive the supported list")
	}
	_, list, ok := strings.Cut(err.Error(), "(supported:")
	if !ok {
		t.Fatalf("agent.New error no longer carries the supported list: %v", err)
	}
	out := []string{}
	for _, name := range strings.Split(strings.TrimSuffix(strings.TrimSpace(list), ")"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	if len(out) < 10 {
		t.Fatalf("derived only %d providers from agent.New; the parse is wrong", len(out))
	}
	return out
}

// TestEveryProviderHasAnInventoryEntry is the reason providerInventories is a
// map of decisions rather than a map of probes: a provider with no entry gets an
// empty inventory, which denies every tool call for the whole run without saying
// so anywhere. Adding a backend to agent.New must force a decision here — either
// a probe, or a written reason why one is not possible yet.
func TestEveryProviderHasAnInventoryEntry(t *testing.T) {
	for _, provider := range supportedProviders(t) {
		entry, ok := providerInventories[provider]
		if !ok {
			t.Errorf("provider %q has no providerInventories entry: its runtimes would report an empty tool inventory and every call would be denied", provider)
			continue
		}
		if entry.Probe == nil && strings.TrimSpace(entry.Reason) == "" {
			t.Errorf("provider %q has neither a probe nor a reason; an unexplained gap is what caused the outage", provider)
		}
		if entry.Probe != nil && strings.TrimSpace(entry.Reason) != "" {
			t.Errorf("provider %q carries both a probe and a not-measurable reason; one of them is stale", provider)
		}
	}
}

// TestNoInventoryEntryForUnsupportedProvider keeps the map from accumulating
// entries for providers that no longer exist.
func TestNoInventoryEntryForUnsupportedProvider(t *testing.T) {
	supported := make(map[string]bool)
	for _, provider := range supportedProviders(t) {
		supported[provider] = true
	}
	for provider := range providerInventories {
		if !supported[provider] {
			t.Errorf("providerInventories has an entry for %q, which agent.New does not support", provider)
		}
	}
}

// TestProbeMulticaMCPToolsMeasuresPiSurface is the integration proof for the Pi
// fix: the channel Pi's harness opens must answer with a non-empty tool list, in
// the bare spelling the harness registers and therefore the spelling the mandate
// is keyed on. Skipped when no multica binary is reachable.
func TestProbeMulticaMCPToolsMeasuresPiSurface(t *testing.T) {
	if _, err := exec.LookPath("multica"); err != nil {
		t.Skip("multica binary not on PATH; skipping the live MCP channel probe")
	}
	t.Setenv("MULTICA_PI_HARNESS_COMMAND", "multica")

	tools, err := probeMulticaMCPTools(context.Background(), "")
	if err != nil {
		t.Fatalf("probe multica mcp channel: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("multica mcp channel answered with zero tools; Pi would still be locked out")
	}
	for _, name := range tools {
		if strings.HasPrefix(name, "mcp__") {
			t.Errorf("tool %q is namespaced, but the harness registers local tools under their bare name; the capability key would not match the call", name)
		}
	}
}

func TestParseHermesToolsets(t *testing.T) {
	output := strings.Join([]string{
		"Built-in toolsets (cli):",
		"  ✓ enabled  web  🔍 Web Search & Scraping",
		"  ✓ enabled  terminal  💻 Terminal & Processes",
		"  ✗ disabled  video  🎬 Video Analysis",
		"",
	}, "\n")

	got := parseHermesToolsets(output)
	want := []string{"web", "terminal"}
	if len(got) != len(want) {
		t.Fatalf("parsed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parsed %v, want %v", got, want)
		}
	}
}

// TestUnmeasurableProviderIsFlagged proves the honest-empty behaviour: a provider
// with no probe and no curated tools reports "not measured" rather than an empty
// inventory that reads as a permission decision.
func TestUnmeasurableProviderIsFlagged(t *testing.T) {
	snapshot := cerebroProviderCapabilities("antigravity", "")
	if got := snapshot["discovery_method"]; got != discoveryNotMeasured {
		t.Fatalf("discovery_method = %v, want %q", got, discoveryNotMeasured)
	}
}

// TestCuratedProviderKeepsItsList guards that flagging does not overwrite a
// provider whose static list is real and resolving today.
func TestCuratedProviderKeepsItsList(t *testing.T) {
	snapshot := cerebroProviderCapabilities("cursor", "")
	tools, _ := snapshot["tools"].([]string)
	if len(tools) == 0 {
		t.Fatal("cursor lost its curated tool list")
	}
	if got := snapshot["discovery_method"]; got != "static" {
		t.Fatalf("discovery_method = %v, want static", got)
	}
}

func TestDedupeToolNames(t *testing.T) {
	got := dedupeToolNames([]string{" Bash ", "Bash", "", "Read"})
	want := []string{"Bash", "Read"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("deduped %v, want %v", got, want)
	}
}
