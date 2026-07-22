package toolaccess

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
)

func TestEffectiveToolWireShapeHasNoLegacyRuntimeGrant(t *testing.T) {
	body, err := json.Marshal(EffectiveTool{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "runtime_grant") {
		t.Fatalf("legacy runtime_grant field returned: %s", body)
	}
}

func TestDescriptorForCapabilityUsesCanonicalKeyAndScanSource(t *testing.T) {
	desc := DescriptorForCapability(capabilityregistry.View{
		Key: "github.create_issue", Title: "Create issue", Description: "Creates an issue", Source: "scan",
	})
	if desc.ToolKey != "github.create_issue" || desc.DisplayName != "Create issue" || desc.Source != "mcp" {
		t.Fatalf("unexpected descriptor: %#v", desc)
	}
	if len(desc.Protocols) != 2 || desc.RiskClass != "write" {
		t.Fatalf("unexpected capability classification: %#v", desc)
	}
}

func TestRuntimeProtocolsCloudFallback(t *testing.T) {
	got := RuntimeProtocols("cloud", "firtal-gateway", nil)
	if len(got.RuntimeProtocols) != 1 || got.RuntimeProtocols[0] != "native_tool_loop" || !got.SupportsAsk {
		t.Fatalf("unexpected cloud fallback: %#v", got)
	}
}

func TestCredentialDescriptorFailsClosed(t *testing.T) {
	state := CredentialForDescriptor(Descriptor{RiskClass: "credential"})
	if state.Effective != CredentialRequired {
		t.Fatalf("credential tool must require the credential gate: %#v", state)
	}
}
