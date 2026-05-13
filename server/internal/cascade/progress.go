// Package cascade holds value types and helpers for the event-driven
// multi-PR autonomy feature (PUL-102). This package is consumed across
// the service layer, the cascade webhook worker (PR4), the agent-side
// skill helpers (PR5), the dashboard endpoint (PR7), and the
// reconciliation cron (PR8).
//
// PR1 ships only the Progress value type that backs the
// issue.cascade_progress JSONB column. Subsequent PRs add the
// background worker, lookup, loop guard, dashboard, etc.
package cascade

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Progress is the application-level shape of issue.cascade_progress. The
// column is JSONB so the schema can extend without a migration; this
// struct is the canonical decoder so consumers never have to hand-parse.
//
// Fields are documented inline. The on-disk JSON keys are snake_case to
// match the rest of the API surface (multica frontend reads these via
// /api/cascades in PR7).
type Progress struct {
	// TotalPRs is the planned number of PRs in this cascade, read from
	// the plan markdown frontmatter `total_prs` field by the agent
	// (PR5). Used by the completion detector (G7) and the dashboard
	// progress badge.
	TotalPRs int `json:"total_prs"`

	// CurrentStep is the 1-indexed position the agent is currently on.
	// On atomic init (PR5 A4) this is set to 1 before the first
	// `gh pr create`; the agent bumps it after each subsequent
	// pr_merged event when starting the next PR.
	CurrentStep int `json:"current_step"`

	// LastPRNumber is the GitHub PR number of the most recently
	// touched PR. NULL on a freshly initialized cascade before PR1 is
	// opened — represented as zero, since GitHub PR numbers start at
	// 1. Zero is the sentinel for "no PR yet".
	LastPRNumber int `json:"last_pr_number,omitempty"`

	// LastPRMergedAt is set when a pr_merged event was processed for
	// LastPRNumber. Distinct from LastEventAt on the issue row
	// because the cron uses LastEventAt for staleness checks but the
	// UI displays "PR1 merged 2h ago" specifically.
	LastPRMergedAt *time.Time `json:"last_pr_merged_at,omitempty"`

	// LastEventType records the trigger type of the most recent
	// processed event. Used by the dashboard to render a contextual
	// label ("waiting on CI", "review requested", "rebase conflict",
	// etc.). Values match cascade_retrigger.event_type plus a few
	// agent-driven internal events.
	LastEventType string `json:"last_event_type,omitempty"`
}

// ErrProgressInvalid signals that a Progress value violates an
// invariant. Returned by Validate and the unmarshal helpers so callers
// can decide whether to treat the column as corrupt (alert + skip) or
// reset it (atomic-init race recovery).
var ErrProgressInvalid = errors.New("cascade: progress invalid")

// Validate checks the basic invariants the cascade state machine
// assumes hold. Called by:
//   - the atomic-init query in PR5 (sanity check on what the agent
//     wrote before persisting),
//   - the background worker in PR4 (defensive check before deciding
//     to spawn vs pause),
//   - the dashboard endpoint in PR7 (defensive read-side check; on
//     failure the row is rendered with a warning badge instead of
//     crashing the page).
//
// Invariants:
//   - TotalPRs >= 1 (a cascade with zero planned PRs is nonsensical).
//   - 1 <= CurrentStep <= TotalPRs (CurrentStep is 1-indexed; a
//     cascade past TotalPRs should have transitioned to
//     cascade_state='completed').
//   - LastPRNumber >= 0 (zero allowed as sentinel for pre-PR1 state).
func (p Progress) Validate() error {
	if p.TotalPRs < 1 {
		return fmt.Errorf("%w: total_prs must be >= 1, got %d", ErrProgressInvalid, p.TotalPRs)
	}
	if p.CurrentStep < 1 {
		return fmt.Errorf("%w: current_step must be >= 1, got %d", ErrProgressInvalid, p.CurrentStep)
	}
	if p.CurrentStep > p.TotalPRs {
		return fmt.Errorf("%w: current_step %d exceeds total_prs %d", ErrProgressInvalid, p.CurrentStep, p.TotalPRs)
	}
	if p.LastPRNumber < 0 {
		return fmt.Errorf("%w: last_pr_number must be >= 0, got %d", ErrProgressInvalid, p.LastPRNumber)
	}
	return nil
}

// IsComplete reports whether the cascade has finished all planned PRs.
// PR4's worker calls this after processing a pr_merged event to decide
// whether to flip cascade_state to 'completed' and post the completion
// push notification (PR6).
func (p Progress) IsComplete() bool {
	return p.TotalPRs >= 1 && p.CurrentStep >= p.TotalPRs && p.LastPRMergedAt != nil
}

// Marshal serializes Progress into the JSONB on-disk form. Wraps
// encoding/json so callers don't need to import it everywhere, and so
// future migrations to a compacter encoding (msgpack, etc.) flip in
// one place. Validate runs first — Marshal refuses to persist an
// invalid Progress.
func (p Progress) Marshal() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// UnmarshalProgress decodes the JSONB form back into a Progress.
// Validate runs after decode. A nil or empty input returns the zero
// Progress with no error so callers can treat NULL cascade_progress
// columns uniformly. An invalid JSON payload returns a non-nil error
// wrapping ErrProgressInvalid via the validate step.
func UnmarshalProgress(raw []byte) (Progress, error) {
	if len(raw) == 0 {
		return Progress{}, nil
	}
	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil {
		return Progress{}, fmt.Errorf("cascade: decode progress: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Progress{}, err
	}
	return p, nil
}
