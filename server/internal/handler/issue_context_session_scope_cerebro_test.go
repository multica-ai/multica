package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uid builds a deterministic, valid pgtype.UUID from a single byte so tests can
// assert thread membership without real UUIDs.
func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[15] = b
	u.Valid = true
	return u
}

func TestUUIDEqual(t *testing.T) {
	if !uuidEqual(uid(1), uid(1)) {
		t.Fatal("same uuid should be equal")
	}
	if uuidEqual(uid(1), uid(2)) {
		t.Fatal("different uuids should not be equal")
	}
	if uuidEqual(pgtype.UUID{}, pgtype.UUID{}) {
		t.Fatal("invalid uuids should never be equal")
	}
}

func TestThreadScope_ContainsRootAndReplies(t *testing.T) {
	root := uid(1)
	scope := threadScope{RootID: root}

	// The root comment itself belongs to the thread.
	if !scope.contains(db.Comment{ID: root}) {
		t.Fatal("root must belong to its own thread")
	}
	// A direct reply (parent_id = root) belongs to the thread.
	if !scope.contains(db.Comment{ID: uid(2), ParentID: root}) {
		t.Fatal("reply to root must belong to the thread")
	}
	// An unrelated root comment does NOT belong.
	if scope.contains(db.Comment{ID: uid(3)}) {
		t.Fatal("a different thread root must not belong")
	}
	// A reply to a different thread does NOT belong.
	if scope.contains(db.Comment{ID: uid(4), ParentID: uid(3)}) {
		t.Fatal("a reply in a different thread must not belong")
	}
}

func TestScopeContextComments_KeepsThread(t *testing.T) {
	root := uid(1)
	comments := []db.Comment{
		{ID: root},                     // root — kept
		{ID: uid(2), ParentID: root},   // reply in thread — kept
		{ID: uid(3)},                   // other thread root — dropped
		{ID: uid(4), ParentID: uid(3)}, // reply in other thread — dropped
		{ID: uid(5), ParentID: root},   // reply in thread — kept
	}
	got := scopeContextComments(comments, threadScope{RootID: root})
	if len(got) != 3 {
		t.Fatalf("kept %d comments, want 3", len(got))
	}
	for _, c := range got {
		if !uuidEqual(c.ID, root) && !uuidEqual(c.ParentID, root) {
			t.Fatalf("kept a comment outside the thread: %v", c.ID)
		}
	}
}

func TestScopeContextComments_EmptyInput(t *testing.T) {
	if got := scopeContextComments(nil, threadScope{RootID: uid(1)}); len(got) != 0 {
		t.Fatalf("expected empty result, got %d", len(got))
	}
}
