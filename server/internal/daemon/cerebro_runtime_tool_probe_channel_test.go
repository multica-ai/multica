package daemon

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// The daemon starts a task's claude with an injected `multica` MCP server, but
// probes the CLI standalone — so Multica's own actions were absent from the
// capability register, absent from the task mandate, and every mcp__multica__*
// call was refused with "task mandate denied the call" (FIR-4047).
func TestClaudeInventoryMeasuresTheMulticaMCPChannel(t *testing.T) {
	entry, ok := providerInventories["claude"]
	if !ok {
		t.Fatal("claude has no inventory entry")
	}
	if len(entry.Channels) == 0 {
		t.Fatal("claude carries no measured Multica channel; mcp__multica__* calls are denied by the task mandate")
	}
	found := false
	for _, channel := range entry.Channels {
		if channel.Prefix != "mcp__multica__" {
			continue
		}
		if channel.Probe == nil {
			t.Fatal("the Multica channel has no probe, so nothing is measured")
		}
		if strings.TrimSpace(channel.Reason) == "" {
			t.Fatal("the Multica channel has no Reason, so a future reader cannot re-verify it")
		}
		found = true
	}
	if !found {
		t.Fatalf("claude channels = %+v, want one spelled mcp__multica__", entry.Channels)
	}
}

// Claude Code spells an MCP tool "mcp__<server>__<tool>", so a bare measured
// name produces a capability row no call ever matches.
func TestProbeProviderToolsAppliesTheChannelPrefix(t *testing.T) {
	original := providerInventories["claude"]
	t.Cleanup(func() { providerInventories["claude"] = original })

	providerInventories["claude"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return []string{"Bash", "Read"}, nil
		},
		Channels: []toolChannel{{
			Probe: func(context.Context, string) ([]string, error) {
				return []string{"create_artifact", "request_approval"}, nil
			},
			Prefix: "mcp__multica__",
			Reason: "test",
		}},
	}

	got, ok := probeProviderTools("claude", "")
	if !ok {
		t.Fatal("probeProviderTools reported not measured")
	}
	for _, want := range []string{"Bash", "Read", "mcp__multica__create_artifact", "mcp__multica__request_approval"} {
		if !slices.Contains(got, want) {
			t.Fatalf("tools = %v, want %q present", got, want)
		}
	}
}

// A channel that cannot start must leave the primary measurement intact: an
// empty inventory denies every call the runtime makes.
func TestProbeProviderToolsKeepsMeasuredWhenChannelFails(t *testing.T) {
	original := providerInventories["claude"]
	t.Cleanup(func() { providerInventories["claude"] = original })

	providerInventories["claude"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return []string{"Bash"}, nil
		},
		Channels: []toolChannel{{
			Probe: func(context.Context, string) ([]string, error) {
				return nil, context.DeadlineExceeded
			},
			Prefix: "mcp__multica__",
			Reason: "test",
		}},
	}

	got, ok := probeProviderTools("claude", "")
	if !ok {
		t.Fatal("a failed channel must not turn a good measurement into not-measured")
	}
	if !slices.Contains(got, "Bash") {
		t.Fatalf("tools = %v, want the primary measurement kept", got)
	}
}

// A name reachable through both channels must not produce two capability rows.
func TestProbeProviderToolsDedupesChannelAgainstMeasured(t *testing.T) {
	original := providerInventories["claude"]
	t.Cleanup(func() { providerInventories["claude"] = original })

	providerInventories["claude"] = providerInventory{
		Probe: func(context.Context, string) ([]string, error) {
			return []string{"mcp__multica__get_issue"}, nil
		},
		Channels: []toolChannel{{
			Probe: func(context.Context, string) ([]string, error) {
				return []string{"get_issue"}, nil
			},
			Prefix: "mcp__multica__",
			Reason: "test",
		}},
	}

	got, _ := probeProviderTools("claude", "")
	count := 0
	for _, name := range got {
		if name == "mcp__multica__get_issue" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("tools = %v, want exactly one mcp__multica__get_issue row", got)
	}
}
