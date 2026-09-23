// Package issuequery holds the SQL fragments every issue read path shares.
package issuequery

// WorkSurface is the predicate that keeps Triage entries off a work surface
// (MUL-7189 §2.4).
//
// Triage is an issue attribute, not a status: `issue.triage_state` is NULL for
// an ordinary issue — including one already accepted — and non-NULL while the
// entry is still a proposal nobody has taken on. Lists, boards, tables, counts,
// search and project statistics are about work, so they all carry this one
// predicate rather than each deciding what Triage means for them.
//
// alias is the issue table's alias in the query being built ("i", or "issue"
// when the query does not alias it). Callers append the result as a conjunct.
//
// Not applied when the caller explicitly asks for Triage — the queue page, and
// the `triage` filter on the list and table endpoints. "Excluded by default,
// visible when asked for" is the product rule; this helper only supplies the
// default.
func WorkSurface(alias string) string {
	return alias + ".triage_state IS NULL"
}

// TriageOnly is WorkSurface's complement: the entries a Triage surface shows,
// and nothing else. It backs the explicit `triage` filter.
func TriageOnly(alias string) string {
	return alias + ".triage_state IS NOT NULL"
}

// Filter is the predicate a list, board or table read carries for the Triage
// scope the caller asked for: the work surface by default, the Triage queue
// when the request named it.
//
// The two scopes are disjoint on purpose. "Excluded by default, visible when
// explicitly filtered for" is the product rule, and a Triage surface wants the
// queue and nothing else — a request that asked for Triage is not asking for
// the rest of the workspace alongside it. Search is the exception and does not
// use this: `include_triage` there WIDENS the result set, because finding an
// entry in order to merge into it means seeing it beside ordinary issues.
func Filter(alias string, triageOnly bool) string {
	if triageOnly {
		return TriageOnly(alias)
	}
	return WorkSurface(alias)
}
