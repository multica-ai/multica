package handler

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestBuildSearchQuery_OpenCommentCandidates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}

	token := "opencandidates" + uuid.NewString()[:8]
	var open, closed []string
	for _, status := range []string{"todo", "blocked", "done", "cancelled", "verified"} {
		id := dbfx.Issue(t, "unrelated", testutil.Cols{"status": status})
		dbfx.Comment(t, id, token+" alpha")
		dbfx.Comment(t, id, "beta")
		if status == "todo" || status == "blocked" {
			open = append(open, id)
		} else {
			closed = append(closed, id)
		}
	}
	for _, includeClosed := range []bool{false, true} {
		t.Run(fmt.Sprintf("include_closed=%t", includeClosed), func(t *testing.T) {
			query, args := buildSearchQuery(
				token+" alpha beta",
				[]string{token, "alpha", "beta"},
				0,
				false,
				includeClosed,
				[]string{"done", "cancelled", "verified"},
			)
			args[3], args[len(args)-2], args[len(args)-1] = parseUUID(testWorkspaceID), 20, 0
			// Inspect the candidate CTE directly: checking final search rows alone
			// cannot detect wasted aggregation of terminal issues' comments.
			end := strings.Index(query, "\n\tSELECT i.id")
			if end < 0 {
				t.Fatal("missing final search projection")
			}
			rows, err := testPool.Query(context.Background(), query[:end]+" SELECT issue_id::text FROM comment_matches", args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			for _, id := range open {
				if !slices.Contains(ids, id) {
					t.Errorf("open issue %s lost its cross-comment candidates", id)
				}
			}
			for _, id := range closed {
				if slices.Contains(ids, id) != includeClosed {
					t.Errorf("terminal issue %s in candidates = %t, want %t", id, slices.Contains(ids, id), includeClosed)
				}
			}
		})
	}
}
