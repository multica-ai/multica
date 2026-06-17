package cost_optimization

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mkUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[15] = b
	u.Valid = true
	return u
}

func ids(comments []db.Comment) []byte {
	out := make([]byte, 0, len(comments))
	for _, c := range comments {
		out = append(out, c.ID.Bytes[15])
	}
	return out
}

// On a channel, scoping keeps only the triggering thread (root + its replies)
// and drops comments belonging to other threads in the same channel.
func TestScopeSnapshotToTriggerThread_ChannelKeepsOnlyTriggerThread(t *testing.T) {
	root := mkUUID(1)
	reply := mkUUID(2)        // reply in the trigger thread (this is the trigger)
	otherRoot := mkUUID(3)    // a different conversation in the same channel
	otherReply := mkUUID(4)   // its reply
	siblingReply := mkUUID(5) // another reply in the trigger thread

	comments := []db.Comment{
		{ID: root},
		{ID: reply, ParentID: root},
		{ID: otherRoot},
		{ID: otherReply, ParentID: otherRoot},
		{ID: siblingReply, ParentID: root},
	}

	got := ScopeSnapshotToTriggerThread("channel", comments, reply)
	gotIDs := ids(got)

	want := map[byte]bool{1: true, 2: true, 5: true}
	if len(gotIDs) != len(want) {
		t.Fatalf("expected %d comments in trigger thread, got %d (%v)", len(want), len(gotIDs), gotIDs)
	}
	for _, id := range gotIDs {
		if !want[id] {
			t.Fatalf("unexpected comment %d leaked from another thread: %v", id, gotIDs)
		}
	}
}

// When the agent is mentioned in a top-level channel message, the thread root is
// that message itself; its replies come along, other threads do not.
func TestScopeSnapshotToTriggerThread_TopLevelTriggerUsesSelfAsRoot(t *testing.T) {
	trigger := mkUUID(1)
	triggerReply := mkUUID(2)
	other := mkUUID(3)

	comments := []db.Comment{
		{ID: trigger},
		{ID: triggerReply, ParentID: trigger},
		{ID: other},
	}

	got := ids(ScopeSnapshotToTriggerThread("channel", comments, trigger))
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("expected root+reply [1 2], got %v", got)
	}
}

// Non-channel surfaces (issues, DMs) and unresolvable triggers are returned
// unchanged so existing behavior is untouched.
func TestScopeSnapshotToTriggerThread_LeavesNonChannelUntouched(t *testing.T) {
	comments := []db.Comment{{ID: mkUUID(1)}, {ID: mkUUID(2), ParentID: mkUUID(1)}, {ID: mkUUID(3)}}
	trigger := mkUUID(2)

	for _, kind := range []string{"issue", "dm", ""} {
		if got := ScopeSnapshotToTriggerThread(kind, comments, trigger); len(got) != len(comments) {
			t.Fatalf("kind %q: expected untouched (%d), got %d", kind, len(comments), len(got))
		}
	}

	// Invalid trigger → unchanged even on a channel.
	if got := ScopeSnapshotToTriggerThread("channel", comments, pgtype.UUID{}); len(got) != len(comments) {
		t.Fatalf("invalid trigger: expected untouched (%d), got %d", len(comments), len(got))
	}

	// Trigger not present in the slice → unchanged (don't guess).
	if got := ScopeSnapshotToTriggerThread("channel", comments, mkUUID(99)); len(got) != len(comments) {
		t.Fatalf("absent trigger: expected untouched (%d), got %d", len(comments), len(got))
	}
}
