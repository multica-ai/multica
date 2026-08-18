package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderCapabilityMatrixMatchesFactory(t *testing.T) {
	t.Parallel()

	capabilities := AllProviderCapabilities()
	if len(capabilities) != len(SupportedTypes) {
		t.Fatalf("capability rows = %d, SupportedTypes = %d", len(capabilities), len(SupportedTypes))
	}
	seen := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		if capability.Type == "" || seen[capability.Type] {
			t.Fatalf("duplicate or empty provider capability %q", capability.Type)
		}
		seen[capability.Type] = true
		if !capability.NativeSkillCatalog {
			t.Errorf("%s must declare a native skill catalog", capability.Type)
		}
		if capability.RuntimeConfigFile == "" || capability.WorkspaceSkillPath == "" {
			t.Errorf("%s is missing a runtime or workspace skill path", capability.Type)
		}
		if capability.UserSkillHomeRelative == "" {
			t.Errorf("%s is missing its user skill root", capability.Type)
		}
		if capability.SandboxApproval == "" || capability.Compatibility == "" {
			t.Errorf("%s is missing sandbox/approval or compatibility metadata", capability.Type)
		}
		if capability.SupportsMCPConfig && capability.MCPConfigSource == "" {
			t.Errorf("%s is missing its MCP config source", capability.Type)
		}
		if _, err := New(capability.Type, Config{}); err != nil {
			t.Errorf("matrix provider %s is not constructible: %v", capability.Type, err)
		}
	}
	for _, provider := range SupportedTypes {
		if !seen[provider] {
			t.Errorf("SupportedTypes contains %q without a capability row", provider)
		}
	}
}

func TestProviderCapabilityMatrixMatchesFrontendMCPMirror(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "packages", "core", "agents", "mcp-support.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read frontend MCP mirror: %v", err)
	}
	text := string(raw)
	for _, capability := range providerCapabilityTable {
		hasProvider := strings.Contains(text, fmt.Sprintf("  %q,", capability.Type))
		if hasProvider != capability.SupportsMCPConfig {
			t.Errorf("frontend MCP mirror for %s = %v, matrix = %v", capability.Type, hasProvider, capability.SupportsMCPConfig)
		}
	}
}

func TestProviderCapabilityMatrixDocumentationIsGenerated(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "docs", "provider-capabilities.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated capability documentation: %v", err)
	}
	if got, want := string(raw), ProviderCapabilityMatrixMarkdown(); got != want {
		t.Fatalf("generated capability documentation is stale")
	}
}

func TestOpenClawInstructionDeliveryIsVersionGated(t *testing.T) {
	t.Parallel()

	if !InstructionNeedsInline("openclaw", "2026.5.4") {
		t.Fatal("OpenClaw legacy versions must retain inline instructions")
	}
	for _, version := range []string{OpenClawMinimumSupportedVersion, "2026.6.0", "openclaw v2027.1.0"} {
		if InstructionNeedsInline("openclaw", version) {
			t.Fatalf("OpenClaw %s must use the single file-based path", version)
		}
	}
	if !InstructionNeedsInline("openclaw", "") {
		t.Fatal("unknown OpenClaw version must fail closed to the legacy inline path")
	}
}

func TestVersionAtLeastParsesCLIOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		version string
		minimum string
		want    bool
	}{
		{"2026.5.5", "2026.5.5", true},
		{"openclaw v2026.6.0 c37871e", "2026.5.5", true},
		{"2026.5.4", "2026.5.5", false},
		{"unknown", "2026.5.5", false},
	}
	for _, tc := range cases {
		if got := VersionAtLeast(tc.version, tc.minimum); got != tc.want {
			t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", tc.version, tc.minimum, got, tc.want)
		}
	}
}
