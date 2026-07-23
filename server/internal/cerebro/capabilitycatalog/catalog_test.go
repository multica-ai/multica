package capabilitycatalog

import (
	"errors"
	"reflect"
	"testing"
)

func TestCatalogResolvesEveryKnownKeyFormAndRoundTrips(t *testing.T) {
	t.Parallel()

	catalog, err := New(
		PlatformTool("create_issue"),
		BridgeTool("create_issue"),
		GatewayNativeTool("firtal_registry"),
		WebFetch(),
		Connection("atlas", "mcp_http"),
		MCPConnectionTool("atlas", "search"),
		APIConnectionEndpoint("agentvault", "GET", "/v1/status", "agentvault__get_v1_status"),
		RuntimeNativeTool("codex", "apply_patch", "tools:apply_patch"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name     string
		key      Key
		wantID   string
		wantKind Relation
	}{
		{"platform policy key", PolicyKey("create_issue", "", "platform"), "platform:create_issue", RelationCanonical},
		{"gateway bridge tool", BridgeKey("create_issue"), "platform:create_issue", RelationAlias},
		{"gateway native tool", GatewayKey("firtal_registry"), "gateway:firtal_registry", RelationCanonical},
		{"gateway web fetch", GatewayKey("web_fetch"), "tool:web_fetch", RelationCanonical},
		{"gateway web fetch policy", PolicyKey("web_fetch", "", "gateway"), "tool:web_fetch", RelationAlias},
		{"claude web fetch runtime", RuntimeKey("claude", "WebFetch"), "tool:web_fetch", RelationAlias},
		{"claude web fetch policy", PolicyKey("tools:WebFetch", "", "runtime_report"), "tool:web_fetch", RelationAlias},
		{"gemini web fetch runtime", RuntimeKey("gemini", "web_fetch"), "tool:web_fetch", RelationVariant},
		{"gemini web fetch policy", PolicyKey("tools:web_fetch", "", "runtime_report"), "tool:web_fetch", RelationVariant},
		{"connection wide key", PolicyKey("connection:atlas", "", "connection"), "connection:atlas", RelationCanonical},
		{"mcp callable token", MCPKey("atlas", "search"), "connection:atlas:mcp:search", RelationCanonical},
		{"mcp policy tuple", PolicyKey("connection:atlas", "search", "connection-tool"), "connection:atlas:mcp:search", RelationAlias},
		{"api callable name", APIEndpointKey("agentvault__get_v1_status"), "connection:agentvault:api:GET:/v1/status", RelationCanonical},
		{"api policy tuple", PolicyKey("connection:agentvault", "GET /v1/status", "connection-endpoint"), "connection:agentvault:api:GET:/v1/status", RelationAlias},
		{"codex runtime tool", RuntimeKey("codex", "apply_patch"), "runtime:codex:apply_patch", RelationCanonical},
		{"codex policy key", RuntimePolicyKey("codex", "tools:apply_patch", "runtime_report"), "runtime:codex:apply_patch", RelationAlias},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, ok := catalog.Resolve(tt.key)
			if !ok {
				t.Fatalf("Resolve(%+v) did not find a mapping", tt.key)
			}
			if resolved.Capability.ID != tt.wantID {
				t.Errorf("Resolve(%+v).Capability.ID = %q, want %q", tt.key, resolved.Capability.ID, tt.wantID)
			}
			if resolved.Relation != tt.wantKind {
				t.Errorf("Resolve(%+v).Relation = %q, want %q", tt.key, resolved.Relation, tt.wantKind)
			}

			forms, ok := catalog.Forms(tt.wantID)
			if !ok {
				t.Fatalf("Forms(%q) did not find the canonical capability", tt.wantID)
			}
			found := false
			for _, form := range forms {
				if form.Key == tt.key && form.Relation == tt.wantKind {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Forms(%q) does not contain round-trip mapping for %+v", tt.wantID, tt.key)
			}
		})
	}
}

func TestCatalogRejectsAliasMappedToMultipleCapabilities(t *testing.T) {
	t.Parallel()

	shared := PolicyKey("tools:Read", "", "runtime_report")
	_, err := New(
		Capability{ID: "runtime:claude:Read", Family: FamilyRuntime, Forms: []Form{Canonical(shared)}},
		Capability{ID: "runtime:other:Read", Family: FamilyRuntime, Forms: []Form{
			Canonical(RuntimeKey("other", "Read")),
			Alias(shared),
		}},
	)
	if !errors.Is(err, ErrAmbiguousKey) {
		t.Fatalf("New() error = %v, want ErrAmbiguousKey", err)
	}
}

func TestCatalogRejectsCapabilityWithoutExactlyOneCanonicalForm(t *testing.T) {
	t.Parallel()

	tests := []Capability{
		{ID: "missing", Family: FamilyGateway, Forms: []Form{Alias(GatewayKey("missing"))}},
		{ID: "duplicate", Family: FamilyGateway, Forms: []Form{Canonical(GatewayKey("one")), Canonical(GatewayKey("two"))}},
	}
	for _, capability := range tests {
		if _, err := New(capability); !errors.Is(err, ErrCanonicalForm) {
			t.Errorf("New(%q) error = %v, want ErrCanonicalForm", capability.ID, err)
		}
	}
}

func TestUnmappedPolicyKeysReturnsSortedUniqueKeys(t *testing.T) {
	t.Parallel()

	catalog, err := New(
		PlatformTool("create_issue"),
		WebFetch(),
		MCPConnectionTool("atlas", "search"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got := catalog.UnmappedPolicyKeys([]Key{
		PolicyKey("z_unknown", "", "runtime_report"),
		PolicyKey("create_issue", "", "platform"),
		PolicyKey("a_unknown", "", "runtime_report"),
		PolicyKey("z_unknown", "", "runtime_report"),
		PolicyKey("connection:atlas", "search", "connection-tool"),
	})
	want := []Key{
		PolicyKey("a_unknown", "", "runtime_report"),
		PolicyKey("z_unknown", "", "runtime_report"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnmappedPolicyKeys() = %#v, want %#v", got, want)
	}
}

func TestCatalogReturnsDefensiveCopiesInStableOrder(t *testing.T) {
	t.Parallel()

	catalog, err := New(GatewayNativeTool("zeta"), PlatformTool("alpha"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first := catalog.All()
	if got, want := []string{first[0].ID, first[1].ID}, []string{"gateway:zeta", "platform:alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("All() ids = %v, want %v", got, want)
	}
	first[0].ID = "mutated"
	first[0].Forms[0].Key.Value = "mutated"

	second := catalog.All()
	if second[0].ID != "gateway:zeta" || second[0].Forms[0].Key.Value == "mutated" {
		t.Fatalf("All() leaked mutable catalog state: %#v", second[0])
	}
}

func TestBuildInventoryCoversEveryToolFamily(t *testing.T) {
	t.Parallel()

	inventory := Inventory{
		PlatformTools:      []string{"create_issue", "add_comment"},
		GatewayNativeTools: []string{"web_fetch", "firtal_registry"},
		BridgeTools:        []string{"create_issue", "add_comment"},
		Connections: []ConnectionInventory{
			{Name: "atlas", Kind: "mcp_http", MCPTools: []string{"search", "query_preview"}},
			{Name: "agentvault", Kind: "api", APIEndpoints: []APIEndpointInventory{
				{Method: "GET", Path: "/v1/status", ExposedName: "agentvault__get_v1_status"},
			}},
		},
		Runtimes: []RuntimeInventory{
			{Provider: "claude", Tools: []string{"Bash", "WebFetch"}},
			{Provider: "codex", Tools: []string{"apply_patch", "view"}},
			{Provider: "cursor", Tools: []string{"read_file"}},
			{Provider: "gemini", Tools: []string{"read_file", "web_fetch"}},
		},
	}

	catalog, err := Build(inventory)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	known := []Key{
		PolicyKey("create_issue", "", "platform"),
		BridgeKey("add_comment"),
		GatewayKey("web_fetch"),
		GatewayKey("firtal_registry"),
		PolicyKey("connection:atlas", "", "connection"),
		MCPKey("atlas", "query_preview"),
		PolicyKey("connection:atlas", "search", "connection-tool"),
		APIEndpointKey("agentvault__get_v1_status"),
		PolicyKey("connection:agentvault", "GET /v1/status", "connection-endpoint"),
		RuntimeKey("claude", "Bash"),
		RuntimePolicyKey("claude", "tools:Bash", "runtime_report"),
		RuntimeKey("codex", "apply_patch"),
		RuntimePolicyKey("codex", "tools:apply_patch", "runtime_report"),
		RuntimePolicyKey("cursor", "tools:read_file", "runtime_report"),
		RuntimePolicyKey("gemini", "tools:read_file", "runtime_report"),
		PolicyKey("tools:WebFetch", "", "runtime_report"),
	}
	for _, key := range known {
		if _, ok := catalog.Resolve(key); !ok {
			t.Errorf("Build() left known key unmapped: %+v", key)
		}
	}

	if got := catalog.UnmappedPolicyKeys(known); len(got) != 0 {
		t.Fatalf("UnmappedPolicyKeys(known) = %#v, want empty", got)
	}

	cursor, ok := catalog.Resolve(RuntimePolicyKey("cursor", "tools:read_file", "runtime_report"))
	if !ok {
		t.Fatal("cursor read_file policy key is unmapped")
	}
	gemini, ok := catalog.Resolve(RuntimePolicyKey("gemini", "tools:read_file", "runtime_report"))
	if !ok {
		t.Fatal("gemini read_file policy key is unmapped")
	}
	if cursor.Capability.ID == gemini.Capability.ID {
		t.Fatalf("same raw tool name across runtimes collapsed to %q", cursor.Capability.ID)
	}
}
