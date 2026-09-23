package handler

import (
	"net/url"

	"github.com/multica-ai/multica/server/internal/issuequery"
)

// The `triage` filter on the issue read endpoints (MUL-7189 §2.4).
//
// Every issue read that returns a SET is scoped: by default it is a work
// surface and carries `triage_state IS NULL`, and a request that asks for
// Triage by name gets the queue instead. Nothing returns both — see
// issuequery.Filter for why, and SearchIssues for the one surface that does.
//
// The parameter is deliberately not a tri-state. "All issues, Triage included"
// has no product surface, and offering it would make the default something a
// caller could opt out of by accident.

// parseIssueTriageScope reports whether the request asked for the Triage queue
// rather than the work surface. Anything other than the exact string "true" —
// including an absent parameter — is the work surface, which is what keeps a
// malformed value from widening a list.
func parseIssueTriageScope(values url.Values) bool {
	return values.Get("triage") == "true"
}

// issueTriageWhere is the predicate to append to a dynamically built issue
// WHERE clause, for the scope the request asked for. alias is the issue table's
// alias in that query.
func issueTriageWhere(values url.Values, alias string) string {
	return issuequery.Filter(alias, parseIssueTriageScope(values))
}
