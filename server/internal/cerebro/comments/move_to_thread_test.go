package comments

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// dedupeUUIDs parses, rejects garbage, and de-duplicates while preserving
// first-seen order.
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

// planMove is the whole re-parent decision of the move (FIR-3880): which
// comments join the new thread, and where the comments left behind end up.
// Nothing is copied and nothing is rewritten, so every case below asserts on
// parent_id writes only.
//
// Fixture thread, chronological: r (root) → a, b, c (replies), plus d nested
// under b so the nested-reply cases have something to walk.
func testThread() []commentNode {
	return []commentNode{
		{ID: "r", ParentID: ""},
		{ID: "a", ParentID: "r"},
		{ID: "b", ParentID: "r"},
		{ID: "c", ParentID: "r"},
		{ID: "d", ParentID: "b"},
	}
}

func assertPlan(t *testing.T, got []parentAssignment, want []parentAssignment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("want %d assignments %v, got %d: %v", len(want), want, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("assignment %d: want %+v, got %+v", i, want[i], got[i])
		}
	}
}

func TestPlanMove(t *testing.T) {
	t.Run("picked replies become their own thread, oldest is the root", func(t *testing.T) {
		got := planMove(testThread(), []string{"b", "c"})
		// b is promoted to a root and c hangs under it. d cannot follow b —
		// it was not picked — so it is re-homed to b's old parent r.
		assertPlan(t, got, []parentAssignment{
			{ID: "b", ParentID: ""},
			{ID: "c", ParentID: "b"},
			{ID: "d", ParentID: "r"},
		})
	})

	t.Run("leaves no write on untouched comments", func(t *testing.T) {
		for _, a := range planMove(testThread(), []string{"b", "c"}) {
			if a.ID == "r" || a.ID == "a" {
				t.Fatalf("untouched comment %s was rewritten: %+v", a.ID, a)
			}
		}
	})

	t.Run("moving the root re-roots what is left on the oldest survivor", func(t *testing.T) {
		got := planMove(testThread(), []string{"r", "a"})
		// r is already a root and a already hangs under it, so the moving pair
		// needs no write at all. b (oldest comment left whose parent moved)
		// becomes the new root of the old thread, c joins it, d stays under b.
		assertPlan(t, got, []parentAssignment{
			{ID: "b", ParentID: ""},
			{ID: "c", ParentID: "b"},
		})
	})

	t.Run("a nested reply follows its picked parent", func(t *testing.T) {
		got := planMove(testThread(), []string{"b", "d"})
		// d already hangs under b, so promoting b carries d along with no
		// write of its own — and no comment is left behind pointing at b.
		assertPlan(t, got, []parentAssignment{
			{ID: "b", ParentID: ""},
		})
	})

	t.Run("moving a single reply just promotes it", func(t *testing.T) {
		assertPlan(t, planMove(testThread(), []string{"c"}), []parentAssignment{
			{ID: "c", ParentID: ""},
		})
	})

	t.Run("moving only the root re-homes its replies onto the oldest of them", func(t *testing.T) {
		// r keeps its place as a root; the replies it leaves behind re-root on
		// a, the oldest of them. d is untouched — its parent b stays.
		assertPlan(t, planMove(testThread(), []string{"r"}), []parentAssignment{
			{ID: "a", ParentID: ""},
			{ID: "b", ParentID: "a"},
			{ID: "c", ParentID: "a"},
		})
	})

	t.Run("a parent is always written before its children", func(t *testing.T) {
		got := planMove(testThread(), []string{"a", "b", "c"})
		written := map[string]bool{}
		for _, x := range got {
			for _, y := range got {
				if y.ID == x.ParentID && !written[y.ID] {
					t.Fatalf("%s is written after its child %s", y.ID, x.ID)
				}
			}
			written[x.ID] = true
		}
	})

	t.Run("empty pick plans nothing", func(t *testing.T) {
		if got := planMove(testThread(), nil); len(got) != 0 {
			t.Fatalf("want no assignments, got %v", got)
		}
	})
}
