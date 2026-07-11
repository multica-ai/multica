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
		CategoryCredentials: true, CategoryConnections: true, CategoryWorkflows: true,
		CategoryChannels: true, CategoryReadAccess: true,
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
		// FIR-2175 phase 3: every capability ships a Chinese description too, so the
		// permission table is self-explanatory in both app languages and the en/zh
		// pair can never silently drift apart.
		if strings.TrimSpace(c.DescriptionZh) == "" {
			t.Errorf("capability %q has blank Chinese description (DescriptionZh)", c.Key)
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

// TestDelegatedOverrideCopyStatesOwnerAdminExemption pins the copy clarified in
// FIR-2351 (PR 2077), which shipped under the no-test-needed escape hatch — this
// is the test that should have accompanied it. Both delegated-override
// capabilities must state BOTH halves of the rule, in both app languages: a
// capability holder can never target their own access row, and workspace
// owners/admins are exempt (they can always change anyone's access, including
// their own). Dropping either half re-creates the ambiguity that made an owner
// believe they could not change their own permissions.
func TestDelegatedOverrideCopyStatesOwnerAdminExemption(t *testing.T) {
	byKey := map[string]Capability{}
	for _, c := range All() {
		byKey[c.Key] = c
	}
	for _, key := range []string{"manage_group_overrides", "manage_workspace_overrides"} {
		c, ok := byKey[key]
		if !ok {
			t.Fatalf("capability %q missing from catalog", key)
		}
		requireCopyPhrases(t, key, "Description", c.Description,
			"Can never be used on your own access",
			"owners and admins are not limited by this",
			"including their own",
		)
		requireCopyPhrases(t, key, "DescriptionZh", c.DescriptionZh,
			"永远不能用于你自己的访问权限",
			"工作区所有者和管理员不受此限制",
			"包括自己的",
		)
	}
}

func requireCopyPhrases(t *testing.T, key, field, text string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if !strings.Contains(text, p) {
			t.Errorf("capability %q: %s no longer states %q — the copy must keep both the self-target ban and the owner/admin exemption (FIR-2351)", key, field, p)
		}
	}
}

// TestSurfacedKeys guards the agent-start surface allowlist (FIR-3091 slice 4):
// exactly the four "start someone else's agent" capabilities are marked Surfaced
// so they can appear in the Permissions table behind the light agent-start gate
// WITHOUT opening the whole platform catalog. Every surfaced key must be a real
// catalog entry, and SurfacedKeys() must agree with the Surfaced field.
func TestSurfacedKeys(t *testing.T) {
	want := map[string]bool{
		"trigger_other_agent":   true,
		"rerun_issue":           true,
		"schedule_agent_wakeup": true,
		"trigger_autopilot":     true,
	}

	// SurfacedKeys() returns exactly the wanted set (no more, no less).
	got := map[string]bool{}
	for _, k := range SurfacedKeys() {
		if got[k] {
			t.Errorf("SurfacedKeys() returned duplicate %q", k)
		}
		got[k] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("SurfacedKeys() missing %q", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("SurfacedKeys() has unexpected %q — surface only the agent-start family", k)
		}
	}

	// The Surfaced field on the catalog must agree with SurfacedKeys(), and every
	// surfaced key must be a real, enforced/known catalog entry.
	inCatalog := map[string]bool{}
	for _, c := range All() {
		inCatalog[c.Key] = true
		if c.Surfaced != want[c.Key] {
			t.Errorf("capability %q Surfaced = %v, want %v", c.Key, c.Surfaced, want[c.Key])
		}
	}
	for k := range want {
		if !inCatalog[k] {
			t.Errorf("surfaced key %q is not a catalog entry", k)
		}
	}
}
