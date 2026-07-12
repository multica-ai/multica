package agentoffice

import "testing"

func TestFoldChangeRequestCounts(t *testing.T) {
	var c ChangeRequestCounts
	foldChangeRequestCounts(&c, "pending", 2)
	foldChangeRequestCounts(&c, "approved", 3)
	foldChangeRequestCounts(&c, "rejected", 1)
	foldChangeRequestCounts(&c, "merged", 4)
	// An unknown/future status still counts toward Total, never disappears.
	foldChangeRequestCounts(&c, "superseded", 5)

	if c.Pending != 2 || c.Approved != 3 || c.Rejected != 1 || c.Merged != 4 {
		t.Fatalf("per-status wrong: %+v", c)
	}
	if c.Total != 15 {
		t.Fatalf("Total = %d, want 15 (unknown status must still count)", c.Total)
	}
}

func TestApproverAccumulatorFoldsAndKeepsOrder(t *testing.T) {
	acc := newApproverAccumulator()
	// Alice: first seen, later name backfilled from a row that carries it.
	acc.add("alice", "", "approved", 2)
	acc.add("bob", "Bob", "rejected", 1)
	acc.add("alice", "Alice", "merged", 1)
	// Empty reviewer id (should never happen for reviewed rows) is ignored.
	acc.add("", "Nobody", "approved", 9)

	got := acc.result()
	if len(got) != 2 {
		t.Fatalf("expected 2 approvers, got %d (%+v)", len(got), got)
	}
	// First-seen order: alice before bob.
	if got[0].UserID != "alice" || got[1].UserID != "bob" {
		t.Fatalf("order not stable/first-seen: %+v", got)
	}
	alice := got[0]
	if alice.Name != "Alice" {
		t.Fatalf("name not backfilled: %q", alice.Name)
	}
	if alice.Approved != 2 || alice.Merged != 1 || alice.Total != 3 {
		t.Fatalf("alice tallies wrong: %+v", alice)
	}
	if got[1].Rejected != 1 || got[1].Total != 1 {
		t.Fatalf("bob tallies wrong: %+v", got[1])
	}
}

func TestApproverAccumulatorResultNeverNil(t *testing.T) {
	got := newApproverAccumulator().result()
	if got == nil {
		t.Fatal("result() must be non-nil so it JSON-encodes as [] not null")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestSummarizeDrift(t *testing.T) {
	findings := []LintFinding{
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "warning"},
		{Severity: "info"},
		{Severity: "mystery"}, // counted in Total only
	}
	d := summarizeDrift(findings)
	if d.Total != 5 || d.Errors != 1 || d.Warnings != 2 || d.Infos != 1 {
		t.Fatalf("drift summary wrong: %+v", d)
	}
	empty := summarizeDrift(nil)
	if empty.Total != 0 || empty.Errors != 0 {
		t.Fatalf("empty drift summary wrong: %+v", empty)
	}
}
