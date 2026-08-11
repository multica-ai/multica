package service

import (
	"testing"
	"time"
)

func TestSelectMemoryItemsFiltersStateAndTTL(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	items := []MemoryDocketItem{
		{ID: "active-1", State: MemoryItemActive, Priority: 50, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)},
		{ID: "expired-1", State: MemoryItemActive, Priority: 90, ExpiresAt: now.Add(-time.Minute)},
		{ID: "withdrawn-1", State: MemoryItemWithdrawn, Priority: 100, WithdrawnAt: now},
		{ID: "superseded-1", State: MemoryItemSuperseded, Priority: 80},
	}
	res := SelectMemoryItems(MemoryDocketSelection{Items: items, Now: now, MaxItems: 10})
	if len(res.Selected) != 1 || res.Selected[0].ItemID != "active-1" {
		t.Fatalf("selected = %+v, want only active-1", res.Selected)
	}
	if len(res.ExpiredRefs) != 1 || len(res.WithdrawnRefs) != 1 {
		t.Fatalf("expired=%v withdrawn=%v", res.ExpiredRefs, res.WithdrawnRefs)
	}
}

func TestSelectMemoryItemsDedupeAndCompression(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	items := []MemoryDocketItem{
		{ID: "a", State: MemoryItemActive, Priority: 10, DedupeKey: "k1", CreatedAt: now},
		{ID: "b", State: MemoryItemActive, Priority: 20, DedupeKey: "k1", CreatedAt: now},
		{ID: "c", State: MemoryItemActive, Priority: 30, CreatedAt: now},
		{ID: "d", State: MemoryItemActive, Priority: 40, CreatedAt: now},
	}
	res := SelectMemoryItems(MemoryDocketSelection{Items: items, Now: now, MaxItems: 2})
	if !res.Compressed {
		t.Fatal("expected compression")
	}
	// dedupe keeps highest priority of k1 (b=20); then cap at 2 -> d(40), c(30)
	if len(res.Selected) != 2 {
		t.Fatalf("selected len = %d, want 2", len(res.Selected))
	}
	// order must be priority desc
	if res.Selected[0].ItemID != "d" || res.Selected[1].ItemID != "c" {
		t.Fatalf("order = %+v, want [d c]", res.Selected)
	}
}

func TestSelectMemoryItemsDeterministicOrder(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	items := []MemoryDocketItem{
		{ID: "a", State: MemoryItemActive, Priority: 10, CreatedAt: now.Add(2 * time.Minute)},
		{ID: "b", State: MemoryItemActive, Priority: 10, CreatedAt: now.Add(time.Minute)},
		{ID: "c", State: MemoryItemActive, Priority: 10, CreatedAt: now},
	}
	res := SelectMemoryItems(MemoryDocketSelection{Items: items, Now: now, MaxItems: 10})
	// equal priority -> created_at asc: c, b, a
	if len(res.Selected) != 3 || res.Selected[0].ItemID != "c" || res.Selected[1].ItemID != "b" || res.Selected[2].ItemID != "a" {
		t.Fatalf("order = %+v, want [c b a]", res.Selected)
	}
}

func TestWireMemoryAttachmentRefsOnly(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	sel := SelectMemoryItems(MemoryDocketSelection{
		MemoryPolicy: "required", MaxItems: 5, Now: now,
		Items: []MemoryDocketItem{
			{ID: "i1", State: MemoryItemActive, Kind: "decision", SourceRef: "comment:2d41b8", Priority: 100, SHA256: "abcd", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		},
	})
	att := WireMemoryAttachment("att-ref", "exec-1", "run-1", "docket-1", 3, "workspace", nil, "issue", "sub-1", "v1", sel.Selected, now, nil)
	if att.SchemaVersion != 1 || len(att.SelectedItemRefs) != 1 {
		t.Fatalf("unexpected attachment: %+v", att)
	}
	ref := att.SelectedItemRefs[0]
	if ref.ItemID != "i1" || ref.SHA256 != "abcd" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	// no raw content ever in the attachment
	if att.PolicyVersion == "" {
		t.Fatal("policy_version must be present")
	}
}
