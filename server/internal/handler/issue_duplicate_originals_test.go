package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A page resolves the originals its duplicates point at in one read, the way
// it loads their labels (MUL-7349). Per-row reads made a page of N duplicates
// of N different issues cost N extra round trips.

const (
	queryIssueRefs = "-- name: ListIssueRefsInWorkspace :many"
	queryIssueRef  = "-- name: GetIssueRefInWorkspace :one"
)

// issueRefSpy passes every statement through to the real pool and records the
// original lookups: how many ids each batched read was given, and how many
// single reads ran.
type issueRefSpy struct {
	inner db.DBTX

	mu          sync.Mutex
	batchedIDs  []int
	singleReads int
}

func (s *issueRefSpy) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.inner.Exec(ctx, sql, args...)
}

func (s *issueRefSpy) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, queryIssueRefs) {
		s.mu.Lock()
		s.batchedIDs = append(s.batchedIDs, uuidArgLen(args, 1))
		s.mu.Unlock()
	}
	return s.inner.Query(ctx, sql, args...)
}

func (s *issueRefSpy) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, queryIssueRef) {
		s.mu.Lock()
		s.singleReads++
		s.mu.Unlock()
	}
	return s.inner.QueryRow(ctx, sql, args...)
}

func (s *issueRefSpy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchedIDs = nil
	s.singleReads = 0
}

func (s *issueRefSpy) snapshot() (batched []int, single int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.batchedIDs...), s.singleReads
}

func TestDuplicateOriginalsResolveInOneReadPerPage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	spy := &issueRefSpy{inner: testPool}
	h := New(db.New(spy), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService,
		nil, nil, analytics.NoopClient{}, Config{})

	project := dbfx.Project(t, "dup-originals-page")
	inProject := func(cols testutil.Cols) testutil.Cols {
		cols["project_id"] = testutil.Raw("'" + project + "'::uuid")
		return cols
	}
	markedAs := func(title, originalID, status string) string {
		return dbfx.Issue(t, title, inProject(testutil.Cols{
			"status":                status,
			"duplicate_of_issue_id": testutil.Raw("'" + originalID + "'::uuid"),
		}))
	}

	originals := []string{
		dbfx.Issue(t, "dup-originals-first"),
		dbfx.Issue(t, "dup-originals-second"),
		dbfx.Issue(t, "dup-originals-third"),
	}
	gone := "00000000-0000-4000-8000-00000000beef"
	want := map[string]string{
		markedAs("dup-originals-needle a", originals[0], "cancelled"): originals[0],
		markedAs("dup-originals-needle b", originals[1], "cancelled"): originals[1],
		markedAs("dup-originals-needle c", originals[2], "cancelled"): originals[2],
		// Shares the first original, so it adds no id to the read.
		markedAs("dup-originals-needle d", originals[0], "cancelled"): originals[0],
		// An original an older server deleted is looked up with the rest and
		// then left off the response.
		markedAs("dup-originals-needle e", gone, "cancelled"): "",
		// A pointer an older server left on a reopened issue is no mark, so
		// it is not looked up at all.
		markedAs("dup-originals-needle f", originals[1], "todo"): "",
	}
	// first, second, third and the deleted one; the shared and reopened
	// pointers add nothing.
	const wantIDs = 4

	type page struct {
		Issues []IssueResponse `json:"issues"`
	}
	for _, tc := range []struct {
		name string
		call func() page
	}{
		{"list", func() page {
			var out page
			testutil.Call(t, h.ListIssues, newRequest("GET", "/api/issues?project_id="+project+"&limit=50", nil)).
				Want(http.StatusOK).JSON(&out)
			return out
		}},
		{"search", func() page {
			var out page
			testutil.Call(t, h.SearchIssues, newRequest("GET", "/api/issues/search?q=dup-originals-needle&include_closed=true&limit=50", nil)).
				Want(http.StatusOK).JSON(&out)
			return out
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy.reset()
			out := tc.call()

			batched, single := spy.snapshot()
			if len(batched) != 1 || batched[0] != wantIDs || single != 0 {
				t.Fatalf("original reads: batched ids per read = %v, single reads = %d; want one read of %d ids and none single",
					batched, single, wantIDs)
			}

			var seen []string
			for _, issue := range out.Issues {
				wantOriginal, ok := want[issue.ID]
				if !ok {
					continue
				}
				seen = append(seen, issue.ID)
				got := ""
				if issue.DuplicateOf != nil {
					got = issue.DuplicateOf.ID
				}
				if got != wantOriginal {
					t.Errorf("%s: duplicate_of = %q, want %q", issue.Title, got, wantOriginal)
				}
			}
			if len(seen) != len(want) {
				sort.Strings(seen)
				t.Fatalf("page carried %d of the %d seeded issues: %v", len(seen), len(want), seen)
			}
		})
	}
}

// A response built on its own, with nothing passed up front, still resolves
// its original.
func TestDuplicateOriginalResolvesWithoutPriming(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-unprimed-original")
	duplicate := seedDuplicate(t, "dup-unprimed-duplicate", original)
	row, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(duplicate))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	resp := issueToResponse(row, "MUL")
	testHandler.newStatusCategoryFiller(context.Background(), row.WorkspaceID)(&resp)
	if resp.DuplicateOf == nil || resp.DuplicateOf.ID != original {
		t.Fatalf("duplicate_of = %+v, want %s", resp.DuplicateOf, original)
	}
}
