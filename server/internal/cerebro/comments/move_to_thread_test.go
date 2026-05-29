package comments

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// dedupeUUIDs is the only pure helper in the move-to-thread handler; the rest
// of the flow is DB-bound and covered by the E2E suite. We assert it parses,
// rejects garbage, and de-duplicates while preserving first-seen order.
func TestDedupeUUIDs(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"

	t.Run("dedupes preserving first-seen order", func(t *testing.T) {
		out, err := dedupeUUIDs([]string{a, b, a})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("want 2 unique ids, got %d", len(out))
		}
		if util.UUIDToString(out[0]) != a || util.UUIDToString(out[1]) != b {
			t.Fatalf("order not preserved: got %s, %s", util.UUIDToString(out[0]), util.UUIDToString(out[1]))
		}
	})

	t.Run("rejects an invalid id", func(t *testing.T) {
		if _, err := dedupeUUIDs([]string{a, "not-a-uuid"}); err == nil {
			t.Fatal("expected error for invalid uuid, got nil")
		}
	})

	t.Run("empty input yields empty slice", func(t *testing.T) {
		out, err := dedupeUUIDs(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("want empty, got %d", len(out))
		}
	})
}
