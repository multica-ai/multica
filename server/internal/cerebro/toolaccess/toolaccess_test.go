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

func (s platformPolicyStub) ResolvePermission(_ context.Context, in toolpolicy.Query, actor platformaccess.Actor) (toolpolicy.Effective, error) {
	return toolpolicy.ResolvePermission(toolpolicy.Input{Settings: map[toolpolicy.Layer]toolpolicy.Setting{
		toolpolicy.LayerAgent: s.settings[in.ToolKey],
	}}, in.ToolKey, actor), nil
}

type declaredContractPolicyStub struct{}

func (declaredContractPolicyStub) Resolve(_ context.Context, _ toolpolicy.Query) (toolpolicy.Effective, error) {
	return toolpolicy.Effective{Setting: toolpolicy.SettingAllow}, nil
}

func (declaredContractPolicyStub) ResolvePermission(_ context.Context, _ toolpolicy.Query, _ platformaccess.Actor) (toolpolicy.Effective, error) {
	return toolpolicy.Effective{Setting: toolpolicy.SettingDeny}, nil
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

func TestDescriptorForCapabilityTreatsDirectRuntimeMCPAsMCP(t *testing.T) {
	desc := DescriptorForCapability(capabilityregistry.View{
		Key: "mcp__github__create_issue", Title: "create_issue", Source: "runtime_report",
	})
	if desc.ToolKey != "mcp__github__create_issue" || desc.Source != "mcp" {
		t.Fatalf("unexpected direct MCP descriptor: %#v", desc)
	}
	if len(desc.Protocols) != 2 || desc.Protocols[0] != "mcp_stdio" {
		t.Fatalf("direct MCP tool must require an MCP protocol: %#v", desc)
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
		{Key: "tools:personal-browser", Title: "Personal browser", Source: "builtin"},
		{Key: "tools:test-as-user", Title: "Test as user", Source: "builtin"},
	}}, platformPolicyStub{settings: map[string]toolpolicy.Setting{
		"tools:personal-browser": toolpolicy.SettingAllow,
		"tools:test-as-user":     toolpolicy.SettingAllow,
	}})

	rows, err := service.ListEffectiveTools(context.Background(), Query{
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         pgtype.UUID{Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"list_workflow_hooks":    AccessAllow,
		"create_workflow_hook":   AccessDeny,
		"publish_workflow_hook":  AccessDeny,
		"tools:personal-browser": AccessAllow,
		"tools:test-as-user":     AccessDeny,
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

func TestListEffectiveToolsUsesDeclaredPermissionContractForOrdinaryTools(t *testing.T) {
	service := New(capabilityListStub{views: []capabilityregistry.View{{
		Key: "mcp__github__create_issue", Title: "Create issue", Source: "scan",
	}}}, declaredContractPolicyStub{})

	rows, err := service.ListEffectiveTools(context.Background(), Query{
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         pgtype.UUID{Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if got := rows[0].Policy.Effective; got != AccessDeny {
		t.Fatalf("ordinary tool policy = %q, want %q from declared permission contract", got, AccessDeny)
	}
	if rows[0].ExposureEffective.Effective {
		t.Fatal("ordinary tool must not be exposed when declared permission contract denies it")
	}
}
