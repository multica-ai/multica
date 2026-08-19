// Package tagaccess projects VIBES membership authority into Multica and
// exposes the single fail-closed authorization seam used by Tag callers.
//
// A signed legacy Multica credential is never sufficient here. Authorize
// requires a server-side Tag session and Workspace grant whose account epoch,
// Membership generation, and authority version still match a healthy active
// projection. Missing state, storage errors, gaps, conflicts, and stale grants
// all deny access. Projection continuity is tracked once per Workspace; only a
// verified complete snapshot or reconcile delivery may bootstrap above version
// one or skip a missing global authority version.
package tagaccess
