// Package permguard is the JEH-1171 regression guard for permission gates.
//
// Three tests sit on top of this package — one per surface — and assert that
// every mutating operation in the system is either (a) gated by an explicit
// permission check, or (b) explicitly exempted with a reason. The point is
// to make it impossible to add a new mutating HTTP route, MCP tool, or CLI
// command without making a conscious decision about who can call it.
//
// The mechanism is a single JSON file, inventory.json, that lists every
// known operation. Tests diff the actual surface (extracted from the live
// chi router, the registered MCP tool list, and the cobra command tree)
// against this inventory and fail on drift.
//
// See README.md for the exemption mechanism and the failure-recovery
// playbook.
package permguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Surface identifies which inventory slice an entry lives under.
type Surface string

const (
	SurfaceHTTP Surface = "http"
	SurfaceMCP  Surface = "mcp"
	SurfaceCLI  Surface = "cli"
)

// Entry is one operation in the inventory.
//
// Exactly one of Gate and Exempt must be non-empty. The test fails when
// both are empty (unclassified) or both are set (ambiguous).
//
// Gate names the permission check that runs for this operation. For HTTP
// routes this is the in-process check (e.g. "create_agent", "workspace_member",
// "project_access"). For MCP tools and CLI commands the gate is typically a
// "via:<route>" pointer to the HTTP route the operation proxies to — the gate
// fires server-side when the request reaches that route.
//
// Exempt explains why no gate is needed. Pre-auth endpoints, local-only
// daemon commands, and known-gap entries waiting on a follow-up issue all
// belong here. The reason is mandatory and must name a concrete cause
// (a follow-up issue ID is fine — "TODO" alone is not).
type Entry struct {
	ID     string `json:"id"`
	Gate   string `json:"gate,omitempty"`
	Exempt string `json:"exempt,omitempty"`
}

// Inventory is the on-disk shape of inventory.json.
type Inventory struct {
	HTTP []Entry `json:"http"`
	MCP  []Entry `json:"mcp"`
	CLI  []Entry `json:"cli"`
}

// Load reads inventory.json from the package directory and returns the parsed
// inventory. The location is resolved relative to this source file so the
// inventory travels with the package no matter where the test is invoked
// from.
func Load() (*Inventory, error) {
	path, err := inventoryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &inv, nil
}

// InventoryPath returns the absolute path to inventory.json. Exported so
// tests can echo the path in failure messages.
func InventoryPath() (string, error) { return inventoryPath() }

func inventoryPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "inventory.json"), nil
}

// Entries returns the inventory slice for the given surface.
func (i *Inventory) Entries(s Surface) []Entry {
	switch s {
	case SurfaceHTTP:
		return i.HTTP
	case SurfaceMCP:
		return i.MCP
	case SurfaceCLI:
		return i.CLI
	default:
		return nil
	}
}

// Result describes the outcome of comparing an actual surface to the
// inventory. Empty Missing, Stale, Unclassified, and Ambiguous slices mean
// the surface is regression-clean.
type Result struct {
	// Missing entries are operations that exist in code but not in the
	// inventory. Fix by appending an entry to inventory.json.
	Missing []string
	// Stale entries are operations in the inventory that no longer exist in
	// code. Fix by removing the entry.
	Stale []string
	// Unclassified entries are in the inventory but have neither Gate nor
	// Exempt set. Pick one — see README.md.
	Unclassified []string
	// Ambiguous entries have both Gate and Exempt set. Pick one.
	Ambiguous []string
}

// Clean reports whether the surface is regression-clean (no drift).
func (r Result) Clean() bool {
	return len(r.Missing) == 0 && len(r.Stale) == 0 && len(r.Unclassified) == 0 && len(r.Ambiguous) == 0
}

// Report renders a human-readable summary. The string is empty when the
// result is clean.
func (r Result) Report(surface Surface) string {
	if r.Clean() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "permission-gate regression on surface %q:\n", surface)
	if len(r.Missing) > 0 {
		fmt.Fprintf(&b, "\n  %d operation(s) exist in code but not in inventory.json — add an entry for each:\n", len(r.Missing))
		for _, id := range r.Missing {
			fmt.Fprintf(&b, "    + %s\n", id)
		}
	}
	if len(r.Stale) > 0 {
		fmt.Fprintf(&b, "\n  %d inventory entr(y/ies) no longer exist in code — remove them:\n", len(r.Stale))
		for _, id := range r.Stale {
			fmt.Fprintf(&b, "    - %s\n", id)
		}
	}
	if len(r.Unclassified) > 0 {
		fmt.Fprintf(&b, "\n  %d inventory entr(y/ies) have neither gate nor exempt — pick one (see README.md):\n", len(r.Unclassified))
		for _, id := range r.Unclassified {
			fmt.Fprintf(&b, "    ? %s\n", id)
		}
	}
	if len(r.Ambiguous) > 0 {
		fmt.Fprintf(&b, "\n  %d inventory entr(y/ies) have BOTH gate and exempt set — pick one:\n", len(r.Ambiguous))
		for _, id := range r.Ambiguous {
			fmt.Fprintf(&b, "    ! %s\n", id)
		}
	}
	return b.String()
}

// Diff compares the given actual set of operation IDs against the inventory
// slice for surface s. It is the only function tests should call.
func (i *Inventory) Diff(s Surface, actual []string) Result {
	entries := i.Entries(s)

	actualSet := make(map[string]struct{}, len(actual))
	for _, id := range actual {
		actualSet[id] = struct{}{}
	}
	invSet := make(map[string]Entry, len(entries))
	for _, e := range entries {
		invSet[e.ID] = e
	}

	var res Result
	for id := range actualSet {
		if _, ok := invSet[id]; !ok {
			res.Missing = append(res.Missing, id)
		}
	}
	for id, e := range invSet {
		if _, ok := actualSet[id]; !ok {
			res.Stale = append(res.Stale, id)
			continue
		}
		switch {
		case e.Gate == "" && e.Exempt == "":
			res.Unclassified = append(res.Unclassified, id)
		case e.Gate != "" && e.Exempt != "":
			res.Ambiguous = append(res.Ambiguous, id)
		}
	}

	sort.Strings(res.Missing)
	sort.Strings(res.Stale)
	sort.Strings(res.Unclassified)
	sort.Strings(res.Ambiguous)
	return res
}

// MutatingMethods is the canonical set of HTTP verbs considered mutating
// for the purpose of this guard. WebSocket upgrades and read endpoints are
// out of scope — they are gated by the auth middleware and the realtime
// audience filter, not by per-operation capability checks.
func MutatingMethods() []string { return []string{"POST", "PUT", "PATCH", "DELETE"} }

// IsMutating reports whether m belongs to MutatingMethods.
func IsMutating(m string) bool {
	switch m {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}
