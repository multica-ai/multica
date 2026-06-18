package driftwatch

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

func TestComputeObservedCapabilities_MarksOnlyPreviouslyUnseenToolsNew(t *testing.T) {
	usage := []cerebrodb.ListAgentObservedToolUsageBetweenRow{
		{Tool: "Bash", Uses: 2, FirstUsed: testTS(2026, 6, 18, 1), LastUsed: testTS(2026, 6, 18, 2)},
		{Tool: "DixaGetConversation", Uses: 1, FirstUsed: testTS(2026, 6, 18, 3), LastUsed: testTS(2026, 6, 18, 3)},
		{Tool: "DropTable", Uses: 1, FirstUsed: testTS(2026, 6, 18, 4), LastUsed: testTS(2026, 6, 18, 4)},
	}
	known := map[string]bool{"bash": true}
	perm := map[string]string{
		"bash":                "allow",
		"dixagetconversation": "ask",
		"droptable":           "deny",
	}

	observed, newlyFound, drifting := computeObservedCapabilities(usage, perm, known)

	if len(observed) != 3 {
		t.Fatalf("observed len = %d, want 3", len(observed))
	}
	if len(newlyFound) != 2 {
		t.Fatalf("newlyFound len = %d, want 2 (%+v)", len(newlyFound), newlyFound)
	}
	if newlyFound[0].Name != "DixaGetConversation" || newlyFound[0].Status != statusNeedsApproval {
		t.Fatalf("first new capability = %+v", newlyFound[0])
	}
	if newlyFound[1].Name != "DropTable" || newlyFound[1].Status != statusBlocked {
		t.Fatalf("second new capability = %+v", newlyFound[1])
	}
	if len(drifting) != 1 || drifting[0].Name != "DropTable" {
		t.Fatalf("drifting = %+v, want only DropTable", drifting)
	}
}

func TestNextNightlyRun(t *testing.T) {
	loc := time.FixedZone("test", 60*60)
	before := time.Date(2026, 6, 18, 1, 30, 0, 0, loc)
	got := nextNightlyRun(before, 2, loc)
	want := time.Date(2026, 6, 18, 2, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("before 02:00 next = %s, want %s", got, want)
	}

	after := time.Date(2026, 6, 18, 2, 1, 0, 0, loc)
	got = nextNightlyRun(after, 2, loc)
	want = time.Date(2026, 6, 19, 2, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("after 02:00 next = %s, want %s", got, want)
	}
}

func testTS(year int, month time.Month, day, hour int) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Date(year, month, day, hour, 0, 0, 0, time.UTC), Valid: true}
}
