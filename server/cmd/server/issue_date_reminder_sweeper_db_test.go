package main

// CEREBRO-PATCH(issue-date-reminders): DB-backed regression test proving that a
// single assignee with a malformed timezone string does NOT abort the whole
// reminder sweep. Before cerebro_safe_timezone() guarded the AT TIME ZONE cast,
// one bad "user".timezone row raised inside ClaimArrivingDateReminders and rolled
// back the statement — silencing reminders for every user that tick.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createDueTodayIssue inserts an open issue assigned to assigneeID with
// due_date = now(), so it falls on "today" in any timezone. Returns the UUID.
func createDueTodayIssue(t *testing.T, workspaceID, assigneeID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		WITH bump AS (
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, kind,
		                   creator_type, creator_id, assignee_type, assignee_id,
		                   due_date, position, number)
		SELECT $1, 'date reminder tz test', 'todo', 'medium', 'issue',
		       'member', $2, 'member', $2, now(), 0, issue_counter FROM bump
		RETURNING id
	`, workspaceID, assigneeID).Scan(&issueID)
	if err != nil {
		t.Fatalf("createDueTodayIssue: %v", err)
	}
	return issueID
}

func setUserTimezone(t *testing.T, userID, tz string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE "user" SET timezone = $1 WHERE id = $2`, tz, userID); err != nil {
		t.Fatalf("setUserTimezone: %v", err)
	}
}

// TestClaimArrivingDateReminders_BadTimezoneDoesNotAbortSweep is the regression
// guard for the FIR-2412 timezone-hardening fix. A user with a garbage timezone
// and a user with a valid timezone both have an issue due today; the sweep must
// succeed and still return the valid user's reminder.
func TestClaimArrivingDateReminders_BadTimezoneDoesNotAbortSweep(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	goodEmail := "date-reminder-good-tz@multica.ai"
	badEmail := "date-reminder-bad-tz@multica.ai"
	cleanupTestUser(t, goodEmail)
	cleanupTestUser(t, badEmail)

	goodUser := createTestUser(t, goodEmail)
	badUser := createTestUser(t, badEmail)
	setUserTimezone(t, goodUser, "Europe/Copenhagen")
	setUserTimezone(t, badUser, "Not/AZone") // malformed — would crash the old query

	goodIssue := createDueTodayIssue(t, testWorkspaceID, goodUser)
	badIssue := createDueTodayIssue(t, testWorkspaceID, badUser)

	t.Cleanup(func() {
		cleanupTestIssue(t, goodIssue) // ON DELETE CASCADE clears claim rows
		cleanupTestIssue(t, badIssue)
		cleanupTestUser(t, goodEmail)
		cleanupTestUser(t, badEmail)
	})

	rows, err := queries.ClaimArrivingDateReminders(ctx, 200)
	if err != nil {
		t.Fatalf("ClaimArrivingDateReminders returned error (bad timezone aborted the sweep): %v", err)
	}

	var sawGood, sawBad bool
	for _, r := range rows {
		switch util.UUIDToString(r.IssueID) {
		case goodIssue:
			sawGood = true
		case badIssue:
			sawBad = true
		}
	}
	if !sawGood {
		t.Errorf("valid-timezone issue %s was not returned — the sweep dropped it", goodIssue)
	}
	// The bad-timezone user still gets a reminder, computed under the
	// Europe/Copenhagen fallback rather than failing the whole run.
	if !sawBad {
		t.Errorf("bad-timezone issue %s was not returned — expected Copenhagen fallback to still claim it", badIssue)
	}
}

// CEREBRO-PATCH(issue-date-reminders): regression guard for the Copenhagen default.
// TestCerebroSafeTimezone_DefaultsToCopenhagen pins the FIR-2412 follow-up fix:
// an unset or malformed user timezone must resolve to Europe/Copenhagen (the
// fork's home offset, shared by all Scandinavian zones) so the day-of reminder
// lands on the correct Danish calendar day, while a valid timezone passes
// through unchanged.
func TestCerebroSafeTimezone_DefaultsToCopenhagen(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		in   any // string or nil (NULL)
		want string
	}{
		{"null", nil, "Europe/Copenhagen"},
		{"empty", "", "Europe/Copenhagen"},
		{"malformed", "Not/AZone", "Europe/Copenhagen"},
		{"valid passthrough", "America/New_York", "America/New_York"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if err := testPool.QueryRow(ctx,
				`SELECT cerebro_safe_timezone($1)`, tc.in).Scan(&got); err != nil {
				t.Fatalf("cerebro_safe_timezone(%v): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("cerebro_safe_timezone(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
