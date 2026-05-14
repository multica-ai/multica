// CEREBRO-PATCH(persona-mask-activity-cerebro): JEH-1190 — net-new cerebro
// file. Sits alongside upstream activity.go to keep the upstream-zone diff
// to two single-line wiring patches in ListTimeline.
//
// JEH-1190: persona-mask application for the issue timeline.
//
// Sibling of activity.go (upstream). Two kinds of historical PII can sit
// in a timeline payload:
//
//  1. Comment content — already classified per-row on the comment table
//     (JEH-1188). Comment entries in the timeline go through the same
//     decision the standalone ListComments handler runs, so a caller
//     without pii grant cannot read a sensitive comment via the timeline
//     either.
//
//  2. Historical field values inside activity_log.details — e.g. the
//     pre-change title of a pii-classified issue lives in a
//     `title_changed` row's `details.from`. If the caller can no longer
//     see the issue's current title, the row's `from`/`to` must be
//     redacted for the same reason.
//
// Status / priority / due-date / assignee changes are not PII and skip
// the persona round-trip entirely — see the JEH-1190 acceptkriterier.

package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maskTimelineForCaller runs the timeline redaction pipeline: comment
// entries first (per-row classification), then activity entries (the
// row's referenced field decides the kind / field-class pair).
//
// The helper is a no-op when persona is not configured — matches the
// pre-JEH-1190 baseline so a workspace without persona keeps seeing the
// full timeline.
func (h *Handler) maskTimelineForCaller(
	r *http.Request,
	issue db.Issue,
	comments []db.Comment,
	activities []db.ActivityLog,
	entries []TimelineEntry,
) []TimelineEntry {
	if h == nil || h.PersonaMask == nil || len(entries) == 0 {
		return entries
	}
	entries = h.maskTimelineCommentsForCaller(r, issue, comments, entries)
	entries = h.maskTimelineActivitiesForCaller(r, issue, activities, entries)
	return entries
}

// maskTimelineCommentsForCaller is the timeline analogue of
// maskCommentsForCaller (CommentResponse). It runs the same per-row
// decision against entries whose Type=="comment", drops denied rows,
// and blanks Content on mask. Activity entries are passed through
// untouched.
//
// The deny-drops semantics matches JEH-1188: leaking the existence of a
// pii-classified comment via the timeline is itself a signal we don't
// want a non-pii actor to see.
func (h *Handler) maskTimelineCommentsForCaller(
	r *http.Request,
	issue db.Issue,
	comments []db.Comment,
	entries []TimelineEntry,
) []TimelineEntry {
	if len(comments) == 0 {
		return entries
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}

	classByID := make(map[string]string, len(comments))
	for _, c := range comments {
		cls := c.Classification
		if cls == "" {
			cls = "internal"
		}
		classByID[uuidToString(c.ID)] = cls
	}

	out := entries[:0]
	for i := range entries {
		if entries[i].Type != "comment" {
			out = append(out, entries[i])
			continue
		}
		classification, known := classByID[entries[i].ID]
		if !known {
			// Comment row exists in the timeline but wasn't in the
			// classification map — keep the entry so we don't silently
			// drop rows the persona side hasn't classified yet.
			out = append(out, entries[i])
			continue
		}
		dec := h.maskDecide(r.Context(),
			actorID, personaActionRead, personaKindComment, entries[i].ID,
			classification, commentFieldClasses(classification))
		if !dec.Allowed {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, entries[i].ID, classification)
			continue
		}
		if len(dec.MaskedFields) > 0 {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, entries[i].ID, classification)
			applyTimelineCommentMask(&entries[i], dec.MaskedFields)
		}
		out = append(out, entries[i])
	}
	return out
}

// applyTimelineCommentMask zeros the content pointer in place. Mirrors
// applyCommentMask but works on TimelineEntry's *string Content field.
func applyTimelineCommentMask(e *TimelineEntry, masked []string) {
	for _, f := range masked {
		if strings.EqualFold(f, personaFieldContent) {
			if e.Content != nil {
				empty := ""
				e.Content = &empty
			}
		}
	}
}

// activityFieldRef tells the mask layer which (kind, field) an activity
// row refers to — i.e. which classification governs the historical
// from/to values in details. An empty kind OR empty field means the row
// has no PII to redact (status/priority/assignee changes, task lifecycle
// events, unknown actions).
//
// Per the JEH-1190 acceptkriterier:
//   - status/priority/due_date/assignee changes are intentionally not
//     PII even when the underlying row is pii-classified — the audit
//     trail of "Mia changed status from todo→done" carries no personal
//     information.
//   - title and description changes ARE PII when the issue is pii-
//     classified — the historical title in details.from is the same
//     pii string the masked GET would hide.
//
// The map is the single seam to extend when activity_listeners learns
// to log a new field — pair the action with its (kind, field) and the
// mask pipeline handles redaction automatically.
type activityFieldRef struct {
	Kind  string
	Field string
}

func activityKindAndField(action string) activityFieldRef {
	switch action {
	case "title_changed":
		return activityFieldRef{Kind: personaKindIssue, Field: personaFieldTitle}
	case "description_updated":
		// description_updated rows carry an empty details object today
		// (see activity_listeners.go) — the wiring is in place for the
		// day details starts carrying the historical body.
		return activityFieldRef{Kind: personaKindIssue, Field: personaFieldDescription}
	}
	return activityFieldRef{}
}

// maskTimelineActivitiesForCaller redacts details.from / details.to (and
// the legacy old_value / new_value aliases mentioned in the spec) on
// activity rows whose referenced field is pii for this actor. Rows
// whose action doesn't map to a PII-bearing field — status_changed,
// priority_changed, assignee_changed, due_date_changed, task lifecycle
// events — are passed through untouched.
//
// A row is never dropped here: removing an activity entry would change
// the timeline's row count and tip the actor off that a redaction
// happened. The masked-but-present row carries the same shape as the
// original, with the historical values nulled.
func (h *Handler) maskTimelineActivitiesForCaller(
	r *http.Request,
	issue db.Issue,
	activities []db.ActivityLog,
	entries []TimelineEntry,
) []TimelineEntry {
	if len(activities) == 0 {
		return entries
	}
	// Map activity ID → (kind, field) so we can decide per entry
	// regardless of how mergeTimeline ordered them.
	refByID := make(map[string]activityFieldRef, len(activities))
	for _, a := range activities {
		ref := activityKindAndField(a.Action)
		if ref.Kind == "" || ref.Field == "" {
			continue
		}
		refByID[uuidToString(a.ID)] = ref
	}
	if len(refByID) == 0 {
		return entries
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}

	// All PII-bearing activity actions today reference the issue itself,
	// so we run a single persona check per timeline and reuse the
	// decision for each matching activity entry. If a future action
	// references a member or comment, this collapses to one check per
	// (kind, resourceID) pair — kept simple here because the v1 surface
	// is single-resource.
	classification := issue.Classification
	if classification == "" {
		classification = "internal"
	}
	fieldClasses := issueFieldClasses(classification)
	if fieldClasses == nil {
		// Issue is public / internal → no field-level redaction is
		// possible regardless of the actor's grant; skip the round-trip.
		return entries
	}
	issueID := uuidToString(issue.ID)
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindIssue, issueID,
		classification, fieldClasses)

	maskedSet := make(map[string]struct{}, len(dec.MaskedFields))
	for _, f := range dec.MaskedFields {
		maskedSet[strings.ToLower(f)] = struct{}{}
	}

	audited := false
	for i := range entries {
		if entries[i].Type != "activity" {
			continue
		}
		ref, ok := refByID[entries[i].ID]
		if !ok {
			continue
		}
		needsRedact := false
		switch {
		case !dec.Allowed:
			// Persona denies the issue outright — the actor cannot see
			// the current title either, so the historical title in
			// details has to go. Same logic for description.
			needsRedact = true
		case len(maskedSet) > 0:
			if _, hit := maskedSet[strings.ToLower(ref.Field)]; hit {
				needsRedact = true
			}
		}
		if !needsRedact {
			continue
		}
		if !audited {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindIssue, issueID, classification)
			audited = true
		}
		entries[i].Details = redactActivityDetails(entries[i].Details)
	}
	return entries
}

// redactActivityDetails returns a JSON-encoded copy of `details` with
// the historical value keys (from/to and the old_value/new_value spec
// aliases) set to null. Other keys are preserved so the activity row
// still renders structurally identically.
//
// Returning the input unchanged on parse error keeps the timeline
// renderable: a non-JSON details blob means something is already
// off-shape and the safer outcome is "render as-is" rather than blank
// the whole row.
func redactActivityDetails(details json.RawMessage) json.RawMessage {
	if len(details) == 0 {
		return details
	}
	var parsed map[string]any
	if err := json.Unmarshal(details, &parsed); err != nil {
		return details
	}
	redactedKeys := []string{"from", "to", "old_value", "new_value"}
	changed := false
	for _, k := range redactedKeys {
		if _, present := parsed[k]; present {
			parsed[k] = nil
			changed = true
		}
	}
	if !changed {
		return details
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return details
	}
	return out
}
