// Package webhooks implements the generic event-driven webhook ingress for
// the PUL-102 multi-PR cascade autonomy feature. PR2 ships the dormant
// router skeleton — Source interface, HMAC verification, /webhooks/{source}
// endpoint, stub adapters — without writing to the database. PR3 fills in
// the real GitHub adapter and persistence to cascade_retrigger (added by
// PR1's migration 072). PR4 builds the background worker that consumes
// those rows.
//
// Design contract from the plan (rev 3 commit 2adceac):
//   - Handler returns 200 in well under 1s (target p99 ≤ 1s). The router
//     does HMAC + payload-version check + normalize only; no heavy work
//     happens synchronously.
//   - HMAC verification accepts the current secret AND, during rotation,
//     a previous secret — enabling 24h zero-downtime key rotation per
//     the secret-rotation procedure in the plan.
//   - Generic by design (E1). GitHub is the first of N sources. Linear,
//     Slack, GitLab, etc. plug in by registering a new Source.
//
// PR2 is wired behind the MULTICA_CASCADE_WEBHOOK_ENABLED env var: when
// the flag is off the route is not registered at all, so the dormant
// scaffold cannot accept or process any request in production until
// PR8's rollout flips the flag.
package webhooks

import (
	"time"

	"github.com/google/uuid"
)

// EventType enumerates the normalized event types a Source adapter can emit.
// The string values intentionally match the cascade_retrigger.event_type
// CHECK constraint in migration 072 — PR4's worker persists rows with the
// same string verbatim, so a typo here would surface as a CHECK violation
// at insert time, which is exactly the safety net we want.
const (
	EventTypeCIFailure     = "ci_failure"
	EventTypePRMerged      = "pr_merged"
	EventTypePRReviewChange = "pr_review_change"
	EventTypePRTitleEdit   = "pr_title_edit"
)

// TriggerEvent is the source-agnostic shape that every Source adapter must
// produce from its native webhook payload. Once a TriggerEvent leaves the
// adapter the rest of the cascade pipeline (PR3 persistence, PR4 worker,
// PR5 agent context) sees only this type — switching sources later only
// requires writing a new adapter.
//
// Lookup-related fields (PRTitle, Branch) carry the data the worker uses
// to resolve "which issue does this PR belong to":
//   - Primary lookup: parse [PUL-N] regex out of PRTitle.
//   - Fallback (G4): branch name `agent-N/pul-N-…`.
// Both fields are required so the worker can fall back without re-hitting
// GitHub on a title-edit event.
type TriggerEvent struct {
	// EventID is the idempotency key for cascade_retrigger.event_id UNIQUE.
	// Source adapters MUST derive it deterministically from the upstream
	// delivery identifier — e.g. GitHub's X-GitHub-Delivery header — so
	// re-deliveries collide on insert and become harmless no-ops.
	// PR3 uses a UUIDv5 (namespace = source name, name = delivery id) to
	// guarantee determinism without persisting a separate dedup table on
	// the way in.
	EventID uuid.UUID

	// Source matches the registered Source.Name() (also the URL path
	// segment in /webhooks/{source}). Used in logs and in worker scope
	// filters.
	Source string

	// EventType is one of the EventType* constants. Matches the
	// cascade_retrigger.event_type CHECK constraint.
	EventType string

	// PRURL / PRNumber / PRTitle / HeadSHA / Branch identify the PR the
	// event is about. All five are required; PR4's lookup logic needs
	// PRTitle (primary) and Branch (fallback). Zero values are not
	// permitted — adapters that cannot fill them should return
	// (nil, ErrSchemaMismatch) to make the missing-field failure loud.
	PRURL    string
	PRNumber int
	PRTitle  string
	HeadSHA  string
	Branch   string

	// ReceivedAt is stamped by the router at the moment the request
	// arrives. Distinct from cascade_retrigger.fired_at (DB-side now())
	// — having both gives PR8 observability a way to measure
	// router-to-persist latency for the p99 ≤ 1s SLO.
	ReceivedAt time.Time
}
