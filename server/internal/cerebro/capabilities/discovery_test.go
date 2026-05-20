package capabilities

import (
	"encoding/json"
	"testing"
)

// TestFor_FourProvidersHaveRealTools is the JEH-1873 acceptance test: at
// least four runtimes must report a non-empty tool list. If someone removes
// a provider from the registry, this test catches it before the change
// reaches Multica's runtime view.
func TestFor_FourProvidersHaveRealTools(t *testing.T) {
	required := []string{"claude", "codex", "cursor", "gemini"}
	for _, p := range required {
		s := For(p)
		if len(s.Tools) == 0 {
			t.Errorf("provider %q: expected non-empty Tools, got %v", p, s.Tools)
		}
		if s.DiscoveryMethod != "static" {
			t.Errorf("provider %q: expected DiscoveryMethod=static, got %q", p, s.DiscoveryMethod)
		}
	}
}

// TestFor_UnknownProviderUnmapped — callers always get a typed Set back.
// Persona's UI iterates over the slices and would crash on a nil field; we
// also want the "unmapped" signal so a drift-alarm can flag the new runtime.
func TestFor_UnknownProviderUnmapped(t *testing.T) {
	s := For("made-up-runtime")
	if s.DiscoveryMethod != "unmapped" {
		t.Errorf("DiscoveryMethod: got %q, want unmapped", s.DiscoveryMethod)
	}
	if s.Tools == nil || s.MCPServers == nil {
		t.Errorf("Tools/MCPServers must be non-nil empty slices, got tools=%v mcp=%v", s.Tools, s.MCPServers)
	}
	if len(s.Providers) != 1 || s.Providers[0] != "made-up-runtime" {
		t.Errorf("Providers should echo input, got %v", s.Providers)
	}
}

// TestFor_Returns_Copy — callers must be able to sort/mutate the returned
// slices without poisoning the registry for the next caller.
func TestFor_Returns_Copy(t *testing.T) {
	a := For("claude")
	if len(a.Tools) == 0 {
		t.Fatal("claude tools missing — fixture changed")
	}
	a.Tools[0] = "MUTATED"
	b := For("claude")
	if b.Tools[0] == "MUTATED" {
		t.Errorf("registry leaked through cloneSet — second call saw mutated value")
	}
}

// TestEnrichWithAgentMCPServers — per-agent mcp_config replaces (not merges)
// the static MCP list and bumps DiscoveryMethod so the UI can show "this
// row came from the agent's config, not the static default".
func TestEnrichWithAgentMCPServers(t *testing.T) {
	t.Run("replaces and sorts", func(t *testing.T) {
		s := For("claude")
		s = EnrichWithAgentMCPServers(s, []string{"supabase", "github"})
		if got, want := s.MCPServers, []string{"github", "supabase"}; !equal(got, want) {
			t.Errorf("MCPServers: got %v, want %v", got, want)
		}
		if s.DiscoveryMethod != "agent_config" {
			t.Errorf("DiscoveryMethod: got %q, want agent_config", s.DiscoveryMethod)
		}
	})
	t.Run("empty input leaves Set unchanged", func(t *testing.T) {
		s := For("claude")
		original := append([]string{}, s.MCPServers...)
		s = EnrichWithAgentMCPServers(s, nil)
		if !equal(s.MCPServers, original) {
			t.Errorf("nil names mutated MCPServers: got %v, want %v", s.MCPServers, original)
		}
		if s.DiscoveryMethod != "static" {
			t.Errorf("DiscoveryMethod: got %q, want static", s.DiscoveryMethod)
		}
	})
}

// TestAsMap_LegacyShape — the daemon's register payload and persona-attrs
// push read these exact keys today. If the shape ever drifts, Persona's UI
// goes blank silently. Lock the contract.
func TestAsMap_LegacyShape(t *testing.T) {
	s := For("claude")
	m := AsMap(s)

	for _, key := range []string{"providers", "tools", "mcp_servers"} {
		v, ok := m[key]
		if !ok {
			t.Errorf("AsMap missing required key %q", key)
			continue
		}
		if _, ok := v.([]string); !ok {
			t.Errorf("AsMap[%q] type: got %T, want []string", key, v)
		}
	}
	// Optional keys appear only when populated, but when they do, the type
	// must still be []string — Persona's UI iterates over them.
	if hooks, ok := m["hooks"].([]string); ok {
		if len(hooks) == 0 {
			t.Error("hooks should be omitted when empty, not present as empty slice")
		}
	}

	// Wire compatibility: the legacy map round-trips through JSON without
	// surfacing nil arrays (which would deserialise as `null` and crash
	// Persona's UI). Marshalling here also catches accidental
	// non-serialisable values added in a future patch.
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("AsMap not JSON-marshalable: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if back["tools"] == nil {
		t.Error("tools must serialise as an array, not null")
	}
}

func TestKnownProviders_Sorted(t *testing.T) {
	got := KnownProviders()
	if len(got) < 4 {
		t.Fatalf("KnownProviders must list at least the 4 curated providers, got %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("KnownProviders not sorted at index %d: %v", i, got)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
