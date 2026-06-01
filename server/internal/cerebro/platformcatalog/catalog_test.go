package platformcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCatalogInvariants guards the shape every catalog entry must hold: a stable
// snake_case key, non-blank human labels, a known category, and at least one of
// Ops (a real route) or Evidence (a hardcoded internal check). A broken entry
// would render an unlabelled, unsettable, or untraceable row.
func TestCatalogInvariants(t *testing.T) {
	knownCategory := map[string]bool{
		CategoryIssues: true, CategoryComments: true, CategoryAutopilots: true,
		CategoryArtifacts: true, CategoryAgents: true, CategoryRuntimes: true,
		CategoryGroups: true, CategoryPermissions: true, CategoryProjects: true,
		CategoryWorkspace: true, CategorySkills: true, CategorySquads: true,
		CategoryCredentials: true, CategoryWorkflows: true, CategoryChannels: true,
		CategoryReadAccess: true,
	}
	evidenceRe := regexp.MustCompile(`\.go(:\d+| )`) // "...go:123" or "...go (note)"
	seen := map[string]bool{}
	for _, c := range All() {
		if strings.TrimSpace(c.Key) == "" {
			t.Errorf("capability has blank key: %+v", c)
		}
		if c.Key != strings.ToLower(c.Key) || strings.ContainsAny(c.Key, " -.") {
			t.Errorf("capability key %q must be lower snake_case (no spaces/dashes/dots)", c.Key)
		}
		if seen[c.Key] {
			t.Errorf("duplicate capability key %q", c.Key)
		}
		seen[c.Key] = true
		if strings.TrimSpace(c.Title) == "" {
			t.Errorf("capability %q has blank title", c.Key)
		}
		if strings.TrimSpace(c.Description) == "" {
			t.Errorf("capability %q has blank description", c.Key)
		}
		if !knownCategory[c.Category] {
			t.Errorf("capability %q has unknown category %q", c.Key, c.Category)
		}
		if len(c.Ops) == 0 && len(c.Evidence) == 0 {
			t.Errorf("capability %q has neither Ops (route) nor Evidence (internal check)", c.Key)
		}
		for _, e := range c.Evidence {
			if !evidenceRe.MatchString(e) {
				t.Errorf("capability %q evidence %q must point at a .go file (file.go:line)", c.Key, e)
			}
		}
	}
}

// inventory mirrors the permguard inventory.json shape (only the http surface
// and the id field matter here).
type inventory struct {
	HTTP []struct {
		ID string `json:"id"`
	} `json:"http"`
}

func loadHTTPInventory(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "permguard", "inventory.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read permguard inventory: %v", err)
	}
	var inv inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatalf("parse permguard inventory: %v", err)
	}
	ids := make([]string, 0, len(inv.HTTP))
	for _, e := range inv.HTTP {
		ids = append(ids, e.ID)
	}
	if len(ids) == 0 {
		t.Fatal("permguard inventory has no http operations")
	}
	return ids
}

var mutatingRe = regexp.MustCompile(`^(POST|PUT|PATCH|DELETE) `)

// catalogOps returns the set of every HTTP route owned by some capability.
func catalogOps() map[string]string {
	owner := map[string]string{}
	for _, c := range All() {
		for _, op := range c.Ops {
			owner[op] = c.Key
		}
	}
	return owner
}

// TestCatalogOpsAreRealOperations is the traceability tripwire. Every Op a
// capability claims must be a live route in permguard/inventory.json. An upstream
// rename or removal that orphans the catalog breaks this test.
func TestCatalogOpsAreRealOperations(t *testing.T) {
	httpOps := map[string]bool{}
	for _, id := range loadHTTPInventory(t) {
		httpOps[id] = true
	}
	for op, key := range catalogOps() {
		if !httpOps[op] {
			t.Errorf("capability %q references op %q which is not in permguard/inventory.json http surface", key, op)
		}
	}
}

// TestCatalogOpsAreDisjoint proves the catalog partitions the routes it covers:
// no two capabilities claim the same route, so a stored Allow/Ask/Deny is never
// ambiguous about which route it governs.
func TestCatalogOpsAreDisjoint(t *testing.T) {
	owner := map[string]string{}
	for _, c := range All() {
		for _, op := range c.Ops {
			if prev, ok := owner[op]; ok {
				t.Errorf("op %q claimed by both %q and %q", op, prev, c.Key)
			}
			owner[op] = c.Key
		}
	}
}

// TestExcludedRoutesAreRealAndDisjoint proves the exclusion list is honest: every
// excluded route exists in the inventory, carries a non-blank reason, and is not
// ALSO owned by a capability (a route is covered xor excluded, never both).
func TestExcludedRoutesAreRealAndDisjoint(t *testing.T) {
	httpOps := map[string]bool{}
	for _, id := range loadHTTPInventory(t) {
		httpOps[id] = true
	}
	owner := catalogOps()
	for route, reason := range Excluded() {
		if !httpOps[route] {
			t.Errorf("excluded route %q is not in permguard/inventory.json", route)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("excluded route %q has a blank reason", route)
		}
		if key, ok := owner[route]; ok {
			t.Errorf("route %q is both excluded and owned by capability %q — pick one", route, key)
		}
	}
}

// TestEveryMutatingRouteIsClassified is the completeness gate that makes the
// catalog "exhaustive" by construction (Jesper, 31-05-2026): every mutating HTTP
// route in the inventory must be EITHER owned by a capability OR explicitly
// excluded with a reason. A new un-classified mutating route fails CI here, so
// the catalog cannot silently drift out of completeness.
func TestEveryMutatingRouteIsClassified(t *testing.T) {
	owner := catalogOps()
	excl := Excluded()
	var missing []string
	for _, id := range loadHTTPInventory(t) {
		if !mutatingRe.MatchString(id) {
			continue // GET — read; not a mutating action
		}
		_, owned := owner[id]
		_, isExcluded := excl[id]
		if !owned && !isExcluded {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d mutating route(s) are neither catalogued nor excluded — classify each (add to a capability's Ops or to excluded):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}
