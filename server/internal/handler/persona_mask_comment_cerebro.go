// JEH-1173: persona-mask application for the comment read paths.
//
// Sibling of comment.go (upstream). Each redacted comment becomes one
// row in cerebro_persona_mask_audit so operators can answer "did Mia
// read the legal comment?" without grepping logs.

package handler

import (
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// applyCommentMask nils every field in `masked` on resp. Content is the
// only sensitive field on the comment response shape today; future labels
// can promote attachments / reactions by extending the switch. Setting
// the pointer to nil serialises as `"content": null` (JEH-1215) — the
// previous `""` overload was indistinguishable from a legitimately empty
// body and leaked the fact that a real value once existed.
func applyCommentMask(resp *CommentResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldContent:
			resp.Content = nil
		}
	}
}

// maskCommentsForCaller filters a slice of comments in place: comments
// whose persona decision is `deny` are dropped from the slice, comments
// with non-empty MaskedFields have those fields zeroed. Each non-allow
// outcome writes one row to cerebro_persona_mask_audit.
//
// Dropping (rather than blanking) denied comments matches the JEH-1079
// scope precision for thread reads: leaking a redacted-comment shape
// reveals that a sensitive comment exists, which is itself a signal.
// Members-list keeps the row because the existence of a member is
// already public via other endpoints — comments don't have that
// independent leak.
func (h *Handler) maskCommentsForCaller(r *http.Request, issue db.Issue, comments []db.Comment, resp []CommentResponse) []CommentResponse {
	if h == nil || h.PersonaMask == nil || len(comments) == 0 {
		return resp
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}

	out := resp[:0]
	for i, c := range comments {
		classification := c.Classification
		if classification == "" {
			classification = "internal"
		}
		dec := h.maskDecide(r.Context(),
			actorID, personaActionRead, personaKindComment, uuidToString(c.ID),
			classification, commentFieldClasses(classification))
		if !dec.Allowed {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, uuidToString(c.ID), classification)
			continue
		}
		if len(dec.MaskedFields) > 0 {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, uuidToString(c.ID), classification)
			applyCommentMask(&resp[i], dec.MaskedFields)
		}
		out = append(out, resp[i])
	}
	return out
}

// maskSingleCommentForCaller mirrors maskIssueForCaller: ok=true means
// render, ok=false means caller already wrote 404. Used by future
// single-comment GET endpoints; ListComments uses the slice form above.
func (h *Handler) maskSingleCommentForCaller(w http.ResponseWriter, r *http.Request, comment db.Comment, resp *CommentResponse) bool {
	if h == nil || h.PersonaMask == nil {
		return true
	}
	workspaceID := uuidToString(comment.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	classification := comment.Classification
	if classification == "" {
		classification = "internal"
	}
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindComment, uuidToString(comment.ID),
		classification, commentFieldClasses(classification))
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindComment, uuidToString(comment.ID), classification)
		writeError(w, http.StatusNotFound, "comment not found")
		return false
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindComment, uuidToString(comment.ID), classification)
		applyCommentMask(resp, dec.MaskedFields)
	}
	return true
}

func commentFieldClasses(classification string) map[string]string {
	if classification == "" || classification == "public" || classification == "internal" {
		return nil
	}
	return map[string]string{personaFieldContent: classification}
}

// applyCommentEntryMask is the TimelineEntry counterpart to
// applyCommentMask. Timeline entries use *string for Content (so activity
// entries can omit it), so a masked comment-entry sets Content to nil
// rather than an empty string. Behaviourally aligned with the issue mask
// (Description set to nil) — the field disappears from the JSON payload.
func applyCommentEntryMask(entry *TimelineEntry, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldContent:
			entry.Content = nil
		}
	}
}

// maskCommentEntriesForCaller is the timeline-side mirror of
// maskCommentsForCaller. The timeline endpoint returns a heterogeneous
// stream (comments + activities), so the helper operates on the
// already-built TimelineEntry slice — deny drops the entry from the
// stream entirely (same JEH-1079 reasoning: a redacted-comment shape
// would still leak the existence of a sensitive comment), while masked
// fields are zeroed in place. `entries` must be the slice produced by
// commentsToEntries(r, comments) so indices line up 1:1.
func (h *Handler) maskCommentEntriesForCaller(r *http.Request, issue db.Issue, comments []db.Comment, entries []TimelineEntry) []TimelineEntry {
	if h == nil || h.PersonaMask == nil || len(comments) == 0 {
		return entries
	}
	if len(entries) != len(comments) {
		return entries
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}

	out := entries[:0]
	for i, c := range comments {
		classification := c.Classification
		if classification == "" {
			classification = "internal"
		}
		dec := h.maskDecide(r.Context(),
			actorID, personaActionRead, personaKindComment, uuidToString(c.ID),
			classification, commentFieldClasses(classification))
		if !dec.Allowed {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, uuidToString(c.ID), classification)
			continue
		}
		if len(dec.MaskedFields) > 0 {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindComment, uuidToString(c.ID), classification)
			applyCommentEntryMask(&entries[i], dec.MaskedFields)
		}
		out = append(out, entries[i])
	}
	return out
}
