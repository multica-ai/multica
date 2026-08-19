// Package tagaccess projects VIBES membership authority into Multica and
// exposes the single fail-closed authorization seam used by Tag callers.
//
// A signed legacy Multica credential is never sufficient here. Authorize
// requires a server-side Tag session and Workspace grant whose account epoch,
// Membership generation, and authority version still match a healthy active
// projection. Missing state, storage errors, gaps, conflicts, and stale grants
// all deny access. Projection continuity is tracked once per Workspace; only a
// verified complete snapshot or reconcile delivery may bootstrap above version
// one or skip a missing global authority version. Production construction uses
// NewAuthenticatedAccess: its canonical HMAC ingress is the only projection
// mutation path, while direct Gate apply calls remain fail closed. Durable
// projection apply and targeted connection-close completion are separate
// receipt stages; the latter is completed only by the #290 close port.
// Session logout and account-ban restrictions arrive through a separate
// authenticated per-user sequence. Its cursor and delivery rows are durable
// access-revocation evidence only; they neither write VIBES identity truth nor
// consume the Workspace-global authority cursor.
package tagaccess
