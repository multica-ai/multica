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

// countingPolicyStub records how many times the policy chain is consulted, so a
// test can pin the resolver's COST MODEL and not just its verdicts.
type countingPolicyStub struct {
	resolveCalls           *int
	resolvePermissionCalls *int
}

func (s countingPolicyStub) Resolve(_ context.Context, _ toolpolicy.Query) (toolpolicy.Effective, error) {
	*s.resolveCalls++
	return toolpolicy.Effective{Setting: toolpolicy.SettingAllow}, nil
}

func (s countingPolicyStub) ResolvePermission(_ context.Context, _ toolpolicy.Query, _ platformaccess.Actor) (toolpolicy.Effective, error) {
	*s.resolvePermissionCalls++
	return toolpolicy.Effective{Setting: toolpolicy.SettingAllow}, nil
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

// TestListEffectiveToolsUsesDeclaredPermissionContractForPlatformKeys keeps the
// declared-contract routing for the keys it was built for.
//
// This test previously asserted the same for ORDINARY tools, which is what made
// the FIR-3781 regression invisible: routing an ordinary key through
// ResolvePermission sends it to ResolveDeclared, which resolves ModeOpenable
// whenever cerebro_member_override is on (default on) — so a member row could
// loosen an inherited workspace deny, and the effective tool list an agent
// received in a claim doubled in production. The declared contract is right for
// platform actions and wrong as a security floor for ordinary tools; see
// TestOrdinaryToolsResolveThroughTheTightenOnlyChain for the other half.
func TestListEffectiveToolsUsesDeclaredPermissionContractForPlatformKeys(t *testing.T) {
	const platformKey = "tools:test-as-user"
	if _, special := platformaccess.ForKey(platformKey); !special {
		t.Fatalf("%q is no longer a platform key — pick another one, this test needs a "+
			"key that genuinely routes through the declared contract", platformKey)
	}

	service := New(capabilityListStub{views: []capabilityregistry.View{{
		Key: platformKey, Title: "Test as user", Source: "builtin",
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
		t.Fatalf("platform key policy = %q, want %q from declared permission contract", got, AccessDeny)
	}
	if rows[0].ExposureEffective.Effective {
		t.Fatal("platform key must not be exposed when the declared permission contract denies it")
	}
}

// TestListEffectiveToolsResolvesPolicyOncePerCapability pins the resolver's cost
// model, which the claim path pays on every task handover: the capability
// catalog is read once, and the tool-policy chain is consulted exactly once per
// capability. Cost here is N x (per-capability DB work), so a change that
// resolves more than once per capability — or that adds an uncached per-tool
// lookup inside ResolvePermission — multiplies the claim's response-build time
// by N without touching a single verdict. That is precisely how the 25 July
// outage happened: build_ms went from ~3s to 50-65s with no behavioural change
// visible in any assertion, because every existing test checked verdicts only.
//
// If a future change batches resolution, this test SHOULD fail — update it to
// state the new cost model deliberately rather than letting it drift silently.
func TestListEffectiveToolsResolvesPolicyOncePerCapability(t *testing.T) {
	capabilities := []capabilityregistry.View{
		{Key: "mcp__github__create_issue", Title: "Create issue", Source: "scan"},
		{Key: "mcp__github__list_issues", Title: "List issues", Source: "scan"},
		{Key: "mcp__slack__post", Title: "Post message", Source: "scan"},
		{Key: "tools:personal-browser", Title: "Personal browser", Source: "builtin"},
	}

	var resolveCalls, resolvePermissionCalls int
	service := New(capabilityListStub{views: capabilities}, countingPolicyStub{
		resolveCalls:           &resolveCalls,
		resolvePermissionCalls: &resolvePermissionCalls,
	})

	rows, err := service.ListEffectiveTools(context.Background(), Query{
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         pgtype.UUID{Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(capabilities) {
		t.Fatalf("got %d rows, want %d", len(rows), len(capabilities))
	}

	totalResolves := resolveCalls + resolvePermissionCalls
	if totalResolves != len(capabilities) {
		t.Fatalf("policy chain consulted %d times for %d capabilities (Resolve=%d, ResolvePermission=%d); "+
			"cost must stay 1 resolve per capability",
			totalResolves, len(capabilities), resolveCalls, resolvePermissionCalls)
	}
}
