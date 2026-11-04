package handler

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A directive blocked after its due date is listed; one blocked before its due
// date, or one that is not a directive, is not.
func TestDigest_BlockedDirectivePastDueIsListed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newDigestEnv(t)
	sq := e.squad(t, "Directive risk")
	late := e.issue(t, sq, "stuck directive", "blocked", testutil.Cols{
		"origin_type": "directive",
		"due_date":    testutil.Raw("now() - interval '3 days'"),
	})
	e.issue(t, sq, "not yet due", "blocked", testutil.Cols{
		"origin_type": "directive",
		"due_date":    testutil.Raw("now() + interval '3 days'"),
	})
	e.issue(t, sq, "plain blocked", "blocked", testutil.Cols{
		"due_date": testutil.Raw("now() - interval '3 days'"),
	})

	d := e.digest(t, time.Now().Add(-24*time.Hour))
	if len(d.BlockedDirectives) != 1 {
		t.Fatalf("blocked directives = %+v, want exactly the stuck one", d.BlockedDirectives)
	}
	got := d.BlockedDirectives[0]
	if got.IssueID != late || got.DaysOverdue < 2 {
		t.Fatalf("blocked directive = %+v, want issue %s overdue at least 2 days", got, late)
	}
}
