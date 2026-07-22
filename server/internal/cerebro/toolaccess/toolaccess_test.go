package toolaccess

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

type capabilityListStub struct {
	views []capabilityregistry.View
}

func (s capabilityListStub) List(context.Context, pgtype.UUID, *capabilityregistry.Subject, []string) ([]capabilityregistry.View, error) {
	return s.views, nil
}

type platformPolicyStub struct {
	settings map[string]toolpolicy.Setting
}

func (s platformPolicyStub) Resolve(_ context.Context, in toolpolicy.Query) (toolpolicy.Effective, error) {
	return toolpolicy.Effective{Setting: s.settings[in.ToolKey]}, nil
}

func (s platformPolicyStub) ResolvePlatformAction(_ context.Context, in toolpolicy.Query, enforcement platformaccess.Enforcement, actor platformaccess.Actor) (toolpolicy.Effective, error) {
	return toolpolicy.ResolvePlatformAction(toolpolicy.Input{Settings: map[toolpolicy.Layer]toolpolicy.Setting{
		toolpolicy.LayerAgent: s.settings[in.ToolKey],
	}}, enforcement, actor), nil
}

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

func TestListEffectiveToolsUsesPlatformActionContractForWorkflowHooks(t *testing.T) {
	service := New(capabilityListStub{views: []capabilityregistry.View{
		{Key: "list_workflow_hooks", Title: "List workflow hooks", Source: "builtin"},
		{Key: "create_workflow_hook", Title: "Create workflow hook", Source: "builtin"},
		{Key: "publish_workflow_hook", Title: "Publish workflow hook", Source: "builtin"},
	}}, platformPolicyStub{settings: map[string]toolpolicy.Setting{}})

	rows, err := service.ListEffectiveTools(context.Background(), Query{
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         pgtype.UUID{Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"list_workflow_hooks":   AccessAllow,
		"create_workflow_hook":  AccessDeny,
		"publish_workflow_hook": AccessDeny,
	}
	for _, row := range rows {
		if got, ok := want[row.Descriptor.ToolKey]; ok {
			if row.Policy.Effective != got {
				t.Errorf("%s policy = %q, want %q", row.Descriptor.ToolKey, row.Policy.Effective, got)
			}
			delete(want, row.Descriptor.ToolKey)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing workflow hook rows: %v", want)
	}
}
