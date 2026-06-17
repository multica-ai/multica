package driftwatch

import (
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestDriftStatus(t *testing.T) {
	cases := []struct {
		perm       string
		hasRow     bool
		wantStatus string
		wantDrift  bool
	}{
		{"allow", true, "", false},
		{"ask", true, "", false},
		{"deny", true, statusBlocked, true},
		{"", false, statusUnmapped, true},     // no policy row → drift
		{"weird", true, statusUnmapped, true}, // enum drift on a present row → unmapped drift
	}
	for _, c := range cases {
		gotStatus, gotDrift := driftStatus(c.perm, c.hasRow)
		if gotStatus != c.wantStatus || gotDrift != c.wantDrift {
			t.Fatalf("driftStatus(%q,%v) = (%q,%v); want (%q,%v)",
				c.perm, c.hasRow, gotStatus, gotDrift, c.wantStatus, c.wantDrift)
		}
	}
}

func TestPermissionLookup_MatchesTitleAndKey(t *testing.T) {
	rows := []cerebrotoolpolicy.TableRow{
		{ToolKey: "bash", Title: "Bash", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow}},
		{ToolKey: "bigquery.query", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingDeny}},
		{ToolKey: "", Title: "", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAsk}}, // skipped
	}
	perm := permissionLookup(rows)
	if perm["bash"] != "allow" {
		t.Fatalf("expected bash→allow, got %q", perm["bash"])
	}
	if perm["bigquery.query"] != "deny" {
		t.Fatalf("expected bigquery.query→deny, got %q", perm["bigquery.query"])
	}
	// "Bash" title and "bash" key collapse to one lowercased entry, so 2 total.
	if len(perm) != 2 {
		t.Fatalf("expected 2 lookup entries, got %d (%v)", len(perm), perm)
	}
}

func TestComputeDrift_OnlyBlockedAndUnmapped(t *testing.T) {
	usage := []cerebrodb.ListAgentObservedToolUsageRow{
		{Tool: "Bash", Uses: 20},     // allowed → no drift
		{Tool: "Read", Uses: 8},      // ask → no drift
		{Tool: "DropTable", Uses: 3}, // deny → blocked drift
		{Tool: "WebFetch", Uses: 9},  // no row → unmapped drift
	}
	perm := map[string]string{"bash": "allow", "read": "ask", "droptable": "deny"}

	drift := computeDrift(usage, perm)

	if len(drift) != 2 {
		t.Fatalf("expected 2 drifting tools, got %d (%+v)", len(drift), drift)
	}
	// Sorted by use count desc: WebFetch (9) before DropTable (3).
	if drift[0].Name != "WebFetch" || drift[0].Status != statusUnmapped {
		t.Fatalf("expected WebFetch unmapped first, got %+v", drift[0])
	}
	if drift[1].Name != "DropTable" || drift[1].Status != statusBlocked || drift[1].Permission != "deny" {
		t.Fatalf("expected DropTable blocked second, got %+v", drift[1])
	}
}

func TestComputeDrift_NoneWhenAllSanctioned(t *testing.T) {
	usage := []cerebrodb.ListAgentObservedToolUsageRow{
		{Tool: "Bash", Uses: 5},
		{Tool: "Edit", Uses: 2},
	}
	perm := map[string]string{"bash": "allow", "edit": "ask"}
	if drift := computeDrift(usage, perm); len(drift) != 0 {
		t.Fatalf("expected no drift, got %+v", drift)
	}
}

func TestDriftSignature_StableAndOrderInsensitive(t *testing.T) {
	a := []DriftTool{
		{Name: "WebFetch", Status: statusUnmapped},
		{Name: "DropTable", Status: statusBlocked},
	}
	b := []DriftTool{ // same set, different order
		{Name: "DropTable", Status: statusBlocked},
		{Name: "WebFetch", Status: statusUnmapped},
	}
	if driftSignature(a) != driftSignature(b) {
		t.Fatalf("signature should be order-insensitive: %q vs %q", driftSignature(a), driftSignature(b))
	}
	// A changed set yields a different signature (so the watcher re-alerts).
	c := []DriftTool{{Name: "DropTable", Status: statusBlocked}}
	if driftSignature(a) == driftSignature(c) {
		t.Fatalf("different drift sets must have different signatures")
	}
}
