package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestSessionWindowForComment_NoMarkers_NoScope(t *testing.T) {
	// No session markers = the implicit single "Session 1" covering the whole
	// timeline. The engine must NOT scope: ok=false.
	if _, ok := sessionWindowForComment(nil, ts("2026-06-21T12:00:00Z")); ok {
		t.Fatalf("expected ok=false when there are no session markers")
	}
}

func TestSessionWindowForComment_LatestSessionIsOpen(t *testing.T) {
	starts := []time.Time{
		ts("2026-06-21T10:00:00Z"), // Session 1
		ts("2026-06-21T12:00:00Z"), // Session 2 (latest, open)
	}
	// A comment after the second marker belongs to the open latest session: it
	// has a start but no end (runs "to now").
	w, ok := sessionWindowForComment(starts, ts("2026-06-21T13:30:00Z"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !w.Start.Equal(ts("2026-06-21T12:00:00Z")) {
		t.Fatalf("start = %v, want 12:00", w.Start)
	}
	if w.HasEnd {
		t.Fatalf("latest session must have no end, got end=%v", w.End)
	}
}

func TestSessionWindowForComment_MiddleSessionIsBounded(t *testing.T) {
	starts := []time.Time{
		ts("2026-06-21T10:00:00Z"),
		ts("2026-06-21T12:00:00Z"),
		ts("2026-06-21T14:00:00Z"),
	}
	w, ok := sessionWindowForComment(starts, ts("2026-06-21T13:00:00Z"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !w.Start.Equal(ts("2026-06-21T12:00:00Z")) || !w.End.Equal(ts("2026-06-21T14:00:00Z")) {
		t.Fatalf("window = [%v,%v), want [12:00,14:00)", w.Start, w.End)
	}
	if !w.HasEnd {
		t.Fatal("a non-latest session must be bounded")
	}
}

func TestSessionWindowForComment_BeforeFirstMarker_FallsIntoEarliest(t *testing.T) {
	starts := []time.Time{
		ts("2026-06-21T10:00:00Z"),
		ts("2026-06-21T12:00:00Z"),
	}
	// A comment earlier than the first marker is never orphaned — it belongs to
	// the earliest session.
	w, ok := sessionWindowForComment(starts, ts("2026-06-21T09:00:00Z"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !w.Start.Equal(ts("2026-06-21T10:00:00Z")) {
		t.Fatalf("start = %v, want 10:00 (earliest)", w.Start)
	}
}

func TestSessionWindowForComment_UnsortedInput(t *testing.T) {
	starts := []time.Time{
		ts("2026-06-21T14:00:00Z"),
		ts("2026-06-21T10:00:00Z"),
		ts("2026-06-21T12:00:00Z"),
	}
	w, ok := sessionWindowForComment(starts, ts("2026-06-21T13:00:00Z"))
	if !ok || !w.Start.Equal(ts("2026-06-21T12:00:00Z")) || !w.End.Equal(ts("2026-06-21T14:00:00Z")) {
		t.Fatalf("unsorted input not handled: got ok=%v window=[%v,%v)", ok, w.Start, w.End)
	}
}

func TestSessionWindowContains_HalfOpen(t *testing.T) {
	w := sessionWindow{Start: ts("2026-06-21T12:00:00Z"), End: ts("2026-06-21T14:00:00Z"), HasEnd: true}
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-06-21T11:59:59Z", false}, // before start
		{"2026-06-21T12:00:00Z", true},  // start inclusive
		{"2026-06-21T13:00:00Z", true},  // inside
		{"2026-06-21T14:00:00Z", false}, // end exclusive
		{"2026-06-21T15:00:00Z", false}, // after end
	}
	for _, c := range cases {
		if got := w.contains(ts(c.at)); got != c.want {
			t.Errorf("contains(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestSessionWindowContains_OpenSessionHasNoUpperBound(t *testing.T) {
	w := sessionWindow{Start: ts("2026-06-21T12:00:00Z")}
	if !w.contains(ts("2026-06-21T12:00:00Z")) || !w.contains(ts("2030-01-01T00:00:00Z")) {
		t.Fatal("open session must include everything at or after its start")
	}
	if w.contains(ts("2026-06-21T11:00:00Z")) {
		t.Fatal("open session must still exclude entries before its start")
	}
}

func mkComment(at string) db.Comment {
	return db.Comment{CreatedAt: pgtype.Timestamptz{Time: ts(at), Valid: true}}
}

func TestScopeContextComments_KeepsOnlyWindow(t *testing.T) {
	comments := []db.Comment{
		mkComment("2026-06-21T11:00:00Z"), // before window — dropped
		mkComment("2026-06-21T12:30:00Z"), // inside — kept
		mkComment("2026-06-21T13:30:00Z"), // inside — kept
		mkComment("2026-06-21T14:00:00Z"), // at end (exclusive) — dropped
	}
	w := sessionWindow{Start: ts("2026-06-21T12:00:00Z"), End: ts("2026-06-21T14:00:00Z"), HasEnd: true}
	got := scopeContextComments(comments, w)
	if len(got) != 2 {
		t.Fatalf("kept %d comments, want 2", len(got))
	}
	if !got[0].CreatedAt.Time.Equal(ts("2026-06-21T12:30:00Z")) || !got[1].CreatedAt.Time.Equal(ts("2026-06-21T13:30:00Z")) {
		t.Fatalf("wrong comments kept: %v", got)
	}
}

func TestScopeContextComments_EmptyInput(t *testing.T) {
	if got := scopeContextComments(nil, sessionWindow{Start: ts("2026-06-21T12:00:00Z")}); len(got) != 0 {
		t.Fatalf("expected empty result, got %d", len(got))
	}
}
