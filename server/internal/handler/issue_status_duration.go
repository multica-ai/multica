package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// StatusDurationEntry is the total wall-clock time an issue has spent on one
// status, summed across every visit to it.
//
// Seconds rather than millis: the UI renders a single coarse unit ("9s",
// "56min", "11d") and the underlying activity timestamps are only meaningful to
// about the second anyway, so millisecond precision would be false precision.
type StatusDurationEntry struct {
	Status  string `json:"status"`
	Seconds int64  `json:"seconds"`
	// Current marks the status the issue sits on right now. Its Seconds keeps
	// growing, so the client can label it as still-running rather than final.
	Current bool `json:"current"`
}

// StatusDurationsResponse is the "time in status" aggregate for one issue.
type StatusDurationsResponse struct {
	Entries []StatusDurationEntry `json:"entries"`
	// ComputedAt is the instant the open (current) segment was closed off for
	// this response. Clients that tick the live segment locally add
	// now-ComputedAt rather than guessing at request latency.
	ComputedAt string `json:"computed_at"`
	// Partial is true when the issue predates status-change logging, so the
	// aggregate is a single synthetic bucket rather than reconstructed history.
	Partial bool `json:"partial"`
}

// statusChange is the storage-independent shape aggregateStatusDurations works
// on, so the aggregation can be unit-tested without a database.
type statusChange struct {
	From string
	To   string
	At   time.Time
}

// aggregateStatusDurations reconstructs per-status residency from an issue's
// ordered status-change history.
//
// The model is a partition of [createdAt, now] into half-open segments. Each
// change closes the running segment and opens the next one:
//
//	created ──seg0──▶ change1 ──seg1──▶ change2 ──seg2──▶ now
//
// Attribution rules, and why each is what it is:
//
//   - Segment 0 belongs to changes[0].From, not to any stored "initial status"
//     column — there isn't one. The first transition's `from` is the only
//     record of what the issue was created as, and it is exact.
//   - The final open segment is attributed to currentStatus (the issue row),
//     NOT to the last change's `To`. Activity rows are written by a
//     best-effort async event-bus listener, so a status write can land in the
//     issue row without ever producing an activity. When the two disagree we
//     know an unlogged transition happened at an unknown instant; the tail is
//     given to the issue row so the status the user is looking at never reads
//     as "0s", and the last logged status is still listed (at zero) so the
//     transition is not erased from the history. Either choice misplaces the
//     same amount of time, so the tiebreak is which one reads correctly.
//   - Ordering of the result is by FIRST time each status was entered, so the
//     list reads as the issue's life story top-to-bottom and stays stable as
//     durations grow. Sorting by duration instead would make rows jump around
//     between renders.
//
// Repeat visits to the same status accumulate into one row: the question the
// UI answers is "how long in code review", not "how long in the third visit to
// code review".
//
// Returns partial=true when there is no recorded history at all, in which case
// the entire lifetime is attributed to the current status. That covers issues
// created before status-change logging existed as well as issues that have
// genuinely never moved; the two are indistinguishable from here, and the
// single-bucket answer is correct for the second and the best available
// approximation for the first.
func aggregateStatusDurations(
	createdAt time.Time,
	currentStatus string,
	changes []statusChange,
	now time.Time,
) ([]StatusDurationEntry, bool) {
	partial := len(changes) == 0

	// Phase 1 — lay the lifetime out as (status, start) boundaries. Building the
	// full list before folding it keeps the "what was held when" question
	// separate from the "how long" arithmetic; an earlier version fused the two
	// and silently dropped the last logged status.
	type segment struct {
		status string
		start  time.Time
	}
	var segments []segment
	if len(changes) == 0 {
		segments = append(segments, segment{currentStatus, createdAt})
	} else {
		// The first transition's `from` is the only surviving record of what
		// the issue was created as.
		segments = append(segments, segment{changes[0].From, createdAt})
		for _, c := range changes {
			segments = append(segments, segment{c.To, c.At})
		}
		// An unlogged transition: hand the open tail to the issue row, leaving
		// the last logged status present but zero-length.
		if last := segments[len(segments)-1]; last.status != currentStatus {
			segments = append(segments, segment{currentStatus, last.start})
		}
	}

	// Phase 2 — fold adjacent boundaries into per-status totals.
	totals := map[string]int64{}
	order := []string{}

	cursor := createdAt
	for i, seg := range segments {
		// A clock skew or backdated row can order a boundary before its
		// predecessor. Clamp the cursor forward rather than subtracting, so one
		// bad timestamp costs a zero-length segment instead of a negative
		// contribution that eats real time from another status.
		start := seg.start
		if start.Before(cursor) {
			start = cursor
		}
		cursor = start

		end := now
		if i+1 < len(segments) {
			end = segments[i+1].start
			if end.Before(cursor) {
				end = cursor
			}
		}

		if seg.status == "" {
			// A transition with no recorded endpoint. Dropping it is better than
			// inventing a bucket keyed on "": the UI has no label to render.
			cursor = end
			continue
		}
		if _, seen := totals[seg.status]; !seen {
			order = append(order, seg.status)
			totals[seg.status] = 0
		}
		if d := end.Sub(start); d > 0 {
			totals[seg.status] += int64(d / time.Second)
		}
		cursor = end
	}

	entries := make([]StatusDurationEntry, 0, len(order))
	for _, status := range order {
		entries = append(entries, StatusDurationEntry{
			Status:  status,
			Seconds: totals[status],
			Current: status == currentStatus,
		})
	}
	return entries, partial
}

// GetIssueStatusDurations returns how long an issue has spent in each status,
// aggregated across repeat visits and ordered by first entry.
//
// Backed by a dedicated uncapped query rather than the issue timeline: see
// ListStatusChangesForIssue for why the timeline's newest-N cap makes it
// unusable as a source for this aggregate.
func (h *Handler) GetIssueStatusDurations(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	rows, err := h.Queries.ListStatusChangesForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list status changes")
		return
	}

	changes := make([]statusChange, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid {
			continue
		}
		changes = append(changes, statusChange{
			From: row.FromStatus,
			To:   row.ToStatus,
			At:   row.CreatedAt.Time,
		})
	}

	createdAt := issue.CreatedAt.Time
	now := time.Now().UTC()
	// An issue with no valid created_at cannot anchor segment 0. Falling back
	// to `now` yields zero-length segments rather than a lifetime measured
	// from the zero time (year 1), which would render as ~2000y.
	if !issue.CreatedAt.Valid {
		createdAt = now
	}

	entries, partial := aggregateStatusDurations(createdAt, issue.Status, changes, now)

	writeJSON(w, http.StatusOK, StatusDurationsResponse{
		Entries:    entries,
		ComputedAt: now.Format(time.RFC3339),
		Partial:    partial,
	})
}
