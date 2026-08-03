package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestProbeClaudeToolsUsesInstalledInitInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	path := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"tools\":[\"Read\",\"Skill\",\"ToolSearch\",\"mcp__x__y\"]}'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := probeClaudeTools(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Read", "Skill", "ToolSearch", "mcp__x__y"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("probeClaudeTools() = %v, want %v", got, want)
	}

	caps := providerCapabilities("claude", path)
	if caps["discovery_method"] != "probed" || !reflect.DeepEqual(caps["tools"], got) {
		t.Fatalf("provider capabilities = %#v, want probed installed tools", caps)
	}
}

func TestProviderCapabilitiesPinsProbeObservedAtAcrossCachedCalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	path := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"tools\":[\"Read\"]}'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	first := providerCapabilities("claude", path)
	firstObserved, ok := first["provider_observed_at"].(string)
	if !ok {
		t.Fatalf("first capability snapshot has no provider_observed_at: %#v", first)
	}
	if _, err := time.Parse(time.RFC3339Nano, firstObserved); err != nil {
		t.Fatalf("provider_observed_at = %q: %v", firstObserved, err)
	}
	second := providerCapabilities("claude", path)
	if second["provider_observed_at"] != firstObserved {
		t.Fatalf("cached snapshot observation changed: first %q second %q", firstObserved, second["provider_observed_at"])
	}
}
