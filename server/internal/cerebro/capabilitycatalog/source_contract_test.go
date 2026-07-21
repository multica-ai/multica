package capabilitycatalog_test

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilities"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestCurrentSourceKeyFormsAreCovered(t *testing.T) {
	t.Parallel()

	platformTools := platformcatalog.Keys()
	bridgeTools := runtime.MulticaMCPToolMatrix()
	bridgeNames := make([]string, 0, len(bridgeTools))
	for _, tool := range bridgeTools {
		bridgeNames = append(bridgeNames, tool.Name)
	}
	gatewayNativeNames := runtime.GatewayNativeToolNames()

	runtimes := make([]capabilitycatalog.RuntimeInventory, 0, len(capabilities.KnownProviders()))
	for _, provider := range capabilities.KnownProviders() {
		set := capabilities.For(provider)
		runtimes = append(runtimes, capabilitycatalog.RuntimeInventory{Provider: provider, Tools: set.Tools})
	}

	apiConnection := connections.Connection{
		Name:    "agentvault",
		Type:    connections.TypeAPI,
		URL:     "http://agentvault.internal",
		Enabled: true,
		EndpointPermissions: []connections.EndpointPermission{
			{Path: "/v1/status", Methods: []string{"GET"}},
		},
	}
	apiTools := runtime.BuildAPIConnectionTools([]connections.Connection{apiConnection}, nil)
	if len(apiTools) != 1 {
		t.Fatalf("BuildAPIConnectionTools() returned %d tools, want 1", len(apiTools))
	}

	catalog, err := capabilitycatalog.Build(capabilitycatalog.Inventory{
		PlatformTools:      platformTools,
		GatewayNativeTools: gatewayNativeNames,
		BridgeTools:        bridgeNames,
		Connections: []capabilitycatalog.ConnectionInventory{
			{Name: "atlas", Kind: connections.TypeMCPHTTP, MCPTools: []string{"search"}},
			{Name: apiConnection.Name, Kind: apiConnection.Type, APIEndpoints: []capabilitycatalog.APIEndpointInventory{
				{Method: "GET", Path: "/v1/status", ExposedName: apiTools[0].Name()},
			}},
		},
		Runtimes: runtimes,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, name := range platformTools {
		key := capabilitycatalog.PolicyKey(name, "", "platform")
		if _, ok := catalog.Resolve(key); !ok {
			t.Errorf("platform source form is unmapped: tool=%q", name)
		}
	}
	for _, name := range bridgeNames {
		key := capabilitycatalog.BridgeKey(name)
		if _, ok := catalog.Resolve(key); !ok {
			t.Errorf("bridge source form is unmapped: tool=%q", name)
		}
	}
	for _, name := range gatewayNativeNames {
		key := capabilitycatalog.GatewayKey(name)
		if _, ok := catalog.Resolve(key); !ok {
			t.Errorf("gateway source form is unmapped: tool=%q", name)
		}
	}

	known := []capabilitycatalog.Key{
		capabilitycatalog.PolicyKey(platformTools[0], "", "platform"),
		capabilitycatalog.BridgeKey(bridgeNames[0]),
		capabilitycatalog.GatewayKey("web_fetch"),
		capabilitycatalog.PolicyKey(claudehook.PolicyToolKey("WebFetch"), "", "runtime_report"),
		capabilitycatalog.RuntimePolicyKey("claude", claudehook.PolicyToolKey("WebFetch"), "runtime_report"),
		capabilitycatalog.PolicyKey(connections.CapabilityKey("atlas"), "", "connection"),
		capabilitycatalog.MCPKey("atlas", "search"),
		capabilitycatalog.PolicyKey(connections.CapabilityKey("atlas"), "search", "connection-tool"),
		capabilitycatalog.APIEndpointKey(apiTools[0].Name()),
		capabilitycatalog.PolicyKey(connections.CapabilityKey("agentvault"), "GET /v1/status", "connection-endpoint"),
	}
	if got := toolpolicy.MCPToolToken("atlas", "search"); got != known[6].Value {
		t.Fatalf("MCPKey value = %q, live MCPToolToken = %q", known[6].Value, got)
	}
	if got := catalog.UnmappedPolicyKeys(known); len(got) != 0 {
		t.Fatalf("current source forms are unmapped: %#v", got)
	}

	for _, runtimeInventory := range runtimes {
		for _, tool := range runtimeInventory.Tools {
			policyKey := claudehook.PolicyToolKey(tool)
			key := capabilitycatalog.RuntimePolicyKey(runtimeInventory.Provider, policyKey, "runtime_report")
			if _, ok := catalog.Resolve(key); !ok {
				t.Errorf("runtime source form is unmapped: provider=%q tool=%q policy=%q", runtimeInventory.Provider, tool, policyKey)
			}
		}
	}
}
