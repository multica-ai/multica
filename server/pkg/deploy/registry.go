package deploy

import (
	"fmt"
	"sort"
	"sync"
)

// registry holds every Adapter the binary has imported. Populated via
// Register from each adapter file's init(); reads after init are
// concurrency-safe because the map is only read from there onwards.
//
// The mutex is overkill for the production path (init runs before main),
// but the test suite calls Register from t.Run subtests in parallel —
// the lock keeps the race detector happy without forcing each test to
// reset state by hand.
var (
	regMu    sync.RWMutex
	registry = map[string]Adapter{}
)

// Register adds an adapter to the registry. Panics if two adapters
// declare the same Name() — it's an unrecoverable misconfiguration that
// should surface at boot.
func Register(a Adapter) {
	if a == nil {
		panic("deploy.Register: adapter is nil")
	}
	name := a.Name()
	if name == "" {
		panic("deploy.Register: adapter Name() returned empty string")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("deploy.Register: duplicate adapter name %q", name))
	}
	registry[name] = a
}

// Get returns the adapter for the given kind. Returns ErrUnknownAdapter
// (wrapped with the kind for error context) when not registered.
func Get(kind string) (Adapter, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, kind)
	}
	return a, nil
}

// Names returns every registered adapter name in sorted order. Used by
// the GET /api/deploy/adapters endpoint so the frontend can render a
// dropdown without hardcoding the list.
func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// PollableNames returns every registered adapter whose SupportsPoll() is
// true. The periodic poller passes this list into
// ListDeployEnvironmentsByAdapter so we don't iterate envs that can't be
// polled (e.g. generic_webhook).
func PollableNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name, a := range registry {
		if a.SupportsPoll() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ResetForTest empties the registry. Tests use it to start each case
// from a clean state when they want to register a stub adapter.
func ResetForTest() {
	regMu.Lock()
	defer regMu.Unlock()
	registry = map[string]Adapter{}
}
