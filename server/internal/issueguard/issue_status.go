package issueguard

// Single source of truth for how an issue status is classified as "closed" or
// "completed".  Before this file existed the `('done','cancelled','archived')`
// and `('done','cancelled')` literals were repeated across SQL and Go, so
// adding a new status meant hand-auditing every site.  Now the classification
// is decided here (and, for SQL, in the migration-236 Postgres functions
// issue_status_is_closed / issue_status_is_completed, which MUST be kept in
// lock-step with these helpers) and every other site derives from it.
//
// Classification:
//
//	closed    = done | cancelled | archived   — leaves default list/board/
//	            search/dedupe; terminal for the stage barrier.
//	completed = done | cancelled              — counts toward done-stats,
//	            ArchiveCompletedInbox, CLI stage Done++.  `archived` is closed
//	            but explicitly NOT completed.
//
// Note: these helpers eliminate the "forgot to update one site" class of bug
// when a status is added.  They cannot prevent a status from being classified
// into the wrong bucket — that is a review/classification decision, not
// something a constant can catch.
//
// Keep in sync with server/migrations/236_issue_status_classifier_functions.*.sql.

// closedStatuses classifies an issue as closed: it disappears from default
// list/board/search and counts as terminal for the stage barrier.
var closedStatuses = []string{"done", "cancelled", "archived"}

// completedStatuses classifies an issue as completed: it counts toward
// done-statistics.  This is a strict subset of closedStatuses (excludes
// archived).
var completedStatuses = []string{"done", "cancelled"}

// IsClosedStatus reports whether s is a closed status
// (done | cancelled | archived).
func IsClosedStatus(s string) bool {
	return containsStatus(closedStatuses, s)
}

// IsCompletedStatus reports whether s is a completed status
// (done | cancelled).  archived returns false: it is closed but not
// completed.
func IsCompletedStatus(s string) bool {
	return containsStatus(completedStatuses, s)
}

func containsStatus(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}
