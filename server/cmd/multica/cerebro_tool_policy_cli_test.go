// CEREBRO-PATCH(cerebro-tool-policy-cli): FIR-1609 cerebro-only file — tests for
// the pure rendering helpers of the `multica tool-policy` CLI.
package main

import "testing"

func TestTPVerdict(t *testing.T) {
	cases := map[string]string{
		"allow":   "ALLOWED",
		"ask":     "ASK (approval required)",
		"deny":    "BLOCKED",
		"inherit": "inherit (no explicit rule)",
		"":        "inherit (no explicit rule)",
		"weird":   "WEIRD",
	}
	for in, want := range cases {
		if got := tpVerdict(in); got != want {
			t.Errorf("tpVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTPLayerStack(t *testing.T) {
	allow := "allow"
	deny := "deny"

	if got := tpLayerStack(tpLayers{}); got != "(all inherit / default)" {
		t.Errorf("empty layers = %q, want default sentinel", got)
	}

	got := tpLayerStack(tpLayers{Workspace: &allow, Agent: &deny})
	if got != "workspace=allow, agent=deny" {
		t.Errorf("layer stack = %q, want ordered workspace then agent", got)
	}
}
