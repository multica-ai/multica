package handler

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectStatusKeySetsFallBackToBuiltInKeys(t *testing.T) {
	h := *testHandler
	h.Queries = db.New(failQueryDBTX{
		DBTX:   testPool,
		failOn: "FROM issue_status",
		err:    errors.New("status catalog unavailable"),
	})

	got := h.projectStatusKeySets(context.Background(), parseUUID(testWorkspaceID))
	if want := []string{issuestatus.Done, issuestatus.Cancelled}; !slices.Equal(got.terminal, want) {
		t.Fatalf("terminal keys = %v, want built-in fallback %v", got.terminal, want)
	}
	if want := []string{issuestatus.Done}; !slices.Equal(got.completed, want) {
		t.Fatalf("completed keys = %v, want %v", got.completed, want)
	}
	if want := []string{issuestatus.InProgress, issuestatus.InReview, issuestatus.Blocked}; !slices.Equal(sortedCopy(got.started...), sortedCopy(want...)) {
		t.Fatalf("started keys = %v, want %v", got.started, want)
	}
}

func TestNewProjectProgressLeavesCancelledOutOfScope(t *testing.T) {
	// 12 issues: 5 done, 1 cancelled, 3 started, 3 unstarted.
	got := newProjectProgress(12, 6, 5, 3)
	want := ProjectProgress{Unit: "issues", Scope: 11, Completed: 5, Started: 3, Unstarted: 3, Cancelled: 1, Percent: 45}
	if got != want {
		t.Fatalf("progress = %+v, want %+v", got, want)
	}

	if got := newProjectProgress(3, 1, 0, 0); got.Scope != 2 || got.Percent != 0 {
		t.Fatalf("all-open progress = %+v", got)
	}
	// Every remaining issue cancelled: nothing in scope, and no division.
	if got := newProjectProgress(2, 2, 0, 0); got.Scope != 0 || got.Percent != 0 || got.Cancelled != 2 {
		t.Fatalf("all-cancelled progress = %+v", got)
	}
	// 199 of 200 rounds down, so 100 only means finished.
	if got := newProjectProgress(200, 199, 199, 0); got.Percent != 99 {
		t.Fatalf("percent = %d, want 99", got.Percent)
	}
}
