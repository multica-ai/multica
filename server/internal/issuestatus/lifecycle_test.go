package issuestatus

import (
	"context"
	"testing"
)

func TestCustomLifecycleDoesNotGrantBuiltInBehavior(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ key, category, behavior string }{
		{"awaiting_response", CategoryStarted, "awaiting_response"},
		{"custom_active", CategoryStarted, "custom_active"},
		{"custom_blocked", CategoryStarted, "custom_blocked"},
		{"later", CategoryUnstarted, "later"},
		{"shipped", CategoryDone, Done},
		{"abandoned", CategoryClosed, Cancelled},
	} {
		t.Run(tc.key, func(t *testing.T) {
			q := newFakeQuerier(custom(tc.key, tc.category))
			if got := Effective(ctx, q, testWorkspace, tc.key); got != tc.behavior {
				t.Fatalf("behavior = %q, want %q", got, tc.behavior)
			}
			if got := Category(ctx, q, testWorkspace, tc.key); got != tc.category {
				t.Fatalf("category = %q", got)
			}
			r := NewResolver(testWorkspace)
			if got := r.Effective(ctx, q, tc.key); got != tc.behavior {
				t.Fatalf("batch behavior = %q", got)
			}
			if got := r.Category(ctx, q, tc.key); got != tc.category {
				t.Fatalf("batch category = %q", got)
			}
			// Compatibility is only a wire encoding: it must round-trip lifecycle,
			// without restoring old custom review/blocked/parking privileges.
			wire := WireCategory(tc.key, tc.category)
			if !IsBuiltIn(wire) {
				t.Fatalf("old client cannot decode %q", wire)
			}
			if got, ok := ParseCategory(wire); !ok || got != tc.category {
				t.Fatalf("wire category lost lifecycle: %q", wire)
			}
		})
	}
}

func TestCategoryBoundaryAcceptsOldAndCurrentEnums(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"backlog", "unstarted"}, {"todo", "unstarted"}, {"in_progress", "started"},
		{"in_review", "started"}, {"blocked", "started"}, {"done", "done"}, {"cancelled", "closed"},
		{"completed", "done"}, {"canceled", "closed"},
		{"unstarted", "unstarted"}, {"started", "started"}, {"closed", "closed"},
	} {
		if got, ok := ParseCategory(tc.input); !ok || got != tc.want {
			t.Errorf("%s => %s, %v", tc.input, got, ok)
		}
	}
	if _, ok := ParseCategory("unknown"); ok {
		t.Fatal("unknown category accepted")
	}
	for _, key := range Canonical() {
		category, _ := CategoryForBehavior(key)
		if WireCategory(key, category) != key {
			t.Fatalf("built-in wire changed: %s", key)
		}
	}
}
