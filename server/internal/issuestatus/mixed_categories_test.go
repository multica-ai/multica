package issuestatus

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPreBackfillCategoriesPreserveLifecycleAndWire(t *testing.T) {
	ctx := context.Background()
	for _, old := range Canonical() {
		t.Run(old, func(t *testing.T) {
			category, _ := CategoryForBehavior(old)
			key := "custom_" + old
			// Do not use custom(): that fixture already normalizes its input.
			q := newFakeQuerier(db.IssueStatus{Key: key, Category: old, WorkspaceID: testWorkspace})
			behavior := key
			if old == Done || old == Cancelled {
				behavior = old
			}
			if got := Effective(ctx, q, testWorkspace, key); got != behavior {
				t.Fatalf("Effective = %s, want %s", got, behavior)
			}
			if got := Category(ctx, q, testWorkspace, key); got != category {
				t.Fatalf("Category = %s, want %s", got, category)
			}
			r := NewResolver(testWorkspace)
			if got := r.Effective(ctx, q, key); got != behavior {
				t.Fatalf("resolver Effective = %s", got)
			}
			if got := r.Category(ctx, q, key); got != category {
				t.Fatalf("resolver Category = %s", got)
			}
			keys, err := CustomKeyCategories(ctx, q, testWorkspace)
			if err != nil || keys[key] != category {
				t.Fatalf("custom category map = %v, %v", keys, err)
			}
			if WireCategory(key, old) != WireCategory(key, category) {
				t.Fatal("backfill changes the wire bucket")
			}
		})
	}
}
