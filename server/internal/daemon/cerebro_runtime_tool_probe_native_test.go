package daemon

import (
	"context"
	"slices"
	"testing"
)

// Pi dispatches its own tools from an internal registry that never crosses the
// Multica MCP wire, so the probe is blind to them by construction. An unlisted
// name is denied at call time no matter what the policy chain says — that is
// how agent Mette ended up able to read her issue but not run `echo hi`.
func TestPiInventoryIncludesItsOwnDispatchedTools(t *testing.T) {
	entry, ok := providerInventories["pi"]
	if !ok {
		t.Fatal("pi has no inventory entry")
	}
	if !slices.Contains(entry.Native, "bash") {
		t.Fatalf("pi Native = %v, want it to carry the tool Pi actually calls", entry.Native)
	}
}

func TestProbeProviderToolsUnionsNativeOntoMeasured(t *testing.T) {
	original := providerInventories["pi"]
	t.Cleanup(func() { providerInventories["pi"] = original })

	providerInventories["pi"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return []string{"get_issue", "add_comment"}, nil
		},
		Native: []string{"bash"},
	}

	got, ok := probeProviderTools("pi", "")
	if !ok {
		t.Fatal("probeProviderTools reported not measured")
	}
	for _, want := range []string{"get_issue", "add_comment", "bash"} {
		if !slices.Contains(got, want) {
			t.Fatalf("tools = %v, want %q present", got, want)
		}
	}
}

// A name in both channels must not produce two capability rows.
func TestProbeProviderToolsDedupesNativeAgainstMeasured(t *testing.T) {
	original := providerInventories["pi"]
	t.Cleanup(func() { providerInventories["pi"] = original })

	providerInventories["pi"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return []string{"bash", "get_issue"}, nil
		},
		Native: []string{"bash"},
	}

	got, _ := probeProviderTools("pi", "")
	count := 0
	for _, name := range got {
		if name == "bash" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("tools = %v, want exactly one bash row", got)
	}
}

// A failed probe must stay "not measured" rather than reporting a Native-only
// inventory: a one-tool table would look like a permission decision instead of
// a measurement failure, which is the exact confusion this file exists to end.
func TestProbeProviderToolsDoesNotReportNativeAloneOnProbeFailure(t *testing.T) {
	original := providerInventories["pi"]
	t.Cleanup(func() { providerInventories["pi"] = original })

	providerInventories["pi"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return nil, context.DeadlineExceeded
		},
		Native: []string{"bash"},
	}

	if got, ok := probeProviderTools("pi", ""); ok {
		t.Fatalf("probeProviderTools = %v, true; want not-measured on probe failure", got)
	}
}

// Providers without a blind second channel must be untouched.
func TestProvidersWithoutNativeAreUnchanged(t *testing.T) {
	for _, provider := range []string{"claude", "opencode"} {
		if entry := providerInventories[provider]; len(entry.Native) != 0 {
			t.Fatalf("providerInventories[%q].Native = %v, want empty", provider, entry.Native)
		}
	}
}
