package agent

import "testing"

// TestDiscoveryCacheKeyScopesByExecutable pins the key shape: an empty path
// keeps the bare provider key (what a built-in on PATH resolves to), and two
// executables of the same protocol family never collapse onto one key.
func TestDiscoveryCacheKeyScopesByExecutable(t *testing.T) {
	if got, want := discoveryCacheKey("hermes", Command{}), "hermes"; got != want {
		t.Fatalf("discoveryCacheKey(hermes, \"\") = %q, want %q", got, want)
	}
	builtin := discoveryCacheKey("hermes", Command{Path: "/usr/local/bin/hermes"})
	custom := discoveryCacheKey("hermes", Command{Path: "/usr/local/bin/jcode"})
	if builtin == custom {
		t.Fatalf("same key %q for two different executables", builtin)
	}
	if builtin == "hermes" || custom == "hermes" {
		t.Fatalf("a path-scoped key must differ from the bare provider key: %q / %q", builtin, custom)
	}
}

// TestCachedDiscoveryDoesNotShareMemoAcrossExecutables is the reason the key
// carries the path (MUL-5789). A host can run a built-in runtime and a custom
// runtime profile of the same protocol family at once — the reporter's own
// `MULTICA_HERMES_PATH` workaround produces exactly that pair. With a
// provider-only key the first discovery to land would answer for both for the
// full TTL, reintroducing the catalog/binary mismatch the daemon-side fix
// removes.
func TestCachedDiscoveryDoesNotShareMemoAcrossExecutables(t *testing.T) {
	builtinKey := discoveryCacheKey("hermes", Command{Path: "/usr/local/bin/hermes"})
	customKey := discoveryCacheKey("hermes", Command{Path: "/usr/local/bin/jcode"})
	reset := func() {
		modelCacheMu.Lock()
		delete(modelCache, builtinKey)
		delete(modelCache, customKey)
		modelCacheMu.Unlock()
	}
	reset()
	t.Cleanup(reset)

	discover := func(id string, calls *int) func() (Catalog, error) {
		return func() (Catalog, error) {
			*calls++
			return Catalog{Models: []Model{{ID: id}}}, nil
		}
	}

	var builtinCalls, customCalls int
	if _, err := cachedDiscovery(builtinKey, discover("hermes-model", &builtinCalls)); err != nil {
		t.Fatalf("cachedDiscovery(builtin): %v", err)
	}

	got, err := cachedDiscovery(customKey, discover("jcode-model", &customCalls))
	if err != nil {
		t.Fatalf("cachedDiscovery(custom): %v", err)
	}
	if customCalls != 1 {
		t.Fatalf("custom executable must run its own discovery, got %d calls", customCalls)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "jcode-model" {
		t.Fatalf("custom key served the built-in catalog: %+v", got.Models)
	}

	// The built-in entry is still memoized under its own key.
	if _, err := cachedDiscovery(builtinKey, discover("hermes-model", &builtinCalls)); err != nil {
		t.Fatalf("cachedDiscovery(builtin, second): %v", err)
	}
	if builtinCalls != 1 {
		t.Fatalf("built-in discovery should have been served from cache, got %d calls", builtinCalls)
	}
}
