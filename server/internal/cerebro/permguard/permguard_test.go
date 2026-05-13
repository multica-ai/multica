package permguard

import (
	"testing"
)

func TestInventoryLoads(t *testing.T) {
	inv, err := Load()
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inv == nil {
		t.Fatal("nil inventory")
	}
}

func TestInventoryEntriesAreClassified(t *testing.T) {
	inv, err := Load()
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	// Walk every entry across every surface and assert it is classified.
	// This catches an entry that was added to the inventory but landed with
	// neither a gate nor an exempt reason — exactly the regression the
	// surface-specific tests catch via Diff, but checked here independently
	// in case a surface test is accidentally skipped.
	for _, s := range []Surface{SurfaceHTTP, SurfaceMCP, SurfaceCLI} {
		for _, e := range inv.Entries(s) {
			if e.ID == "" {
				t.Errorf("[%s] entry with empty id: %+v", s, e)
				continue
			}
			gate := e.Gate != ""
			exempt := e.Exempt != ""
			switch {
			case !gate && !exempt:
				t.Errorf("[%s] %q has neither gate nor exempt — see README.md", s, e.ID)
			case gate && exempt:
				t.Errorf("[%s] %q has both gate (%q) and exempt (%q) — pick one", s, e.ID, e.Gate, e.Exempt)
			}
		}
	}
}

func TestInventoryEntriesAreUnique(t *testing.T) {
	inv, err := Load()
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	for _, s := range []Surface{SurfaceHTTP, SurfaceMCP, SurfaceCLI} {
		seen := map[string]bool{}
		for _, e := range inv.Entries(s) {
			if seen[e.ID] {
				t.Errorf("[%s] duplicate entry: %q", s, e.ID)
			}
			seen[e.ID] = true
		}
	}
}

func TestDiffDetectsRegressions(t *testing.T) {
	inv := &Inventory{
		HTTP: []Entry{
			{ID: "POST /api/known-gated", Gate: "workspace_member"},
			{ID: "POST /api/known-exempt", Exempt: "pre-auth"},
			{ID: "POST /api/unclassified"},
			{ID: "POST /api/ambiguous", Gate: "x", Exempt: "y"},
			{ID: "POST /api/stale", Gate: "x"},
		},
	}
	actual := []string{
		"POST /api/known-gated",
		"POST /api/known-exempt",
		"POST /api/unclassified",
		"POST /api/ambiguous",
		"POST /api/new-route", // missing from inventory
	}
	res := inv.Diff(SurfaceHTTP, actual)
	if len(res.Missing) != 1 || res.Missing[0] != "POST /api/new-route" {
		t.Errorf("Missing = %v, want [POST /api/new-route]", res.Missing)
	}
	if len(res.Stale) != 1 || res.Stale[0] != "POST /api/stale" {
		t.Errorf("Stale = %v, want [POST /api/stale]", res.Stale)
	}
	if len(res.Unclassified) != 1 || res.Unclassified[0] != "POST /api/unclassified" {
		t.Errorf("Unclassified = %v, want [POST /api/unclassified]", res.Unclassified)
	}
	if len(res.Ambiguous) != 1 || res.Ambiguous[0] != "POST /api/ambiguous" {
		t.Errorf("Ambiguous = %v, want [POST /api/ambiguous]", res.Ambiguous)
	}
	if res.Clean() {
		t.Error("Clean() = true, want false")
	}
	report := res.Report(SurfaceHTTP)
	if report == "" {
		t.Error("Report() returned empty string for non-clean result")
	}
}

func TestDiffCleanWhenAligned(t *testing.T) {
	inv := &Inventory{
		HTTP: []Entry{{ID: "POST /a", Gate: "g"}},
	}
	res := inv.Diff(SurfaceHTTP, []string{"POST /a"})
	if !res.Clean() {
		t.Errorf("Clean() = false, want true; %+v", res)
	}
	if r := res.Report(SurfaceHTTP); r != "" {
		t.Errorf("Report = %q, want empty", r)
	}
}
