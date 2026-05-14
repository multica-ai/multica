// JEH-1187: persona-mask application for the workspace-invitation read
// paths.
//
// Sibling of invitation.go (upstream). The invitation has no per-row
// classification column — its sensitive fields are inherent to the kind
// (invitee_email / inviter_email are always pii). The persona policy
// decides whether the actor is the invitee (self-send, accept page) and
// thus implicitly carries pii-grant for that row; the cerebro layer
// never code-hardcodes the "actor == invitee" shortcut.

package handler

import (
	"net/http"
	"strings"
)

const (
	personaKindInvitation    = "multica.invitation"
	personaFieldInviteeEmail = "invitee_email"
	personaFieldInviterEmail = "inviter_email"
)

// applyInvitationMask zeros every field in `masked` on one
// InvitationResponse. Unknown field names are no-ops so persona's
// contract can grow without forcing a handler edit.
func applyInvitationMask(row *InvitationResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldInviteeEmail:
			row.InviteeEmail = ""
		case personaFieldInviterEmail:
			row.InviterEmail = ""
		}
	}
}

// invitationFieldClasses mirrors mask.FieldClassifications(mask.KindInvitation)
// but inlined here so the upstream handler package doesn't import the
// cerebro tree. Tracks the static map in fields.go.
func invitationFieldClasses() map[string]string {
	return map[string]string{
		personaFieldInviteeEmail: "pii",
		personaFieldInviterEmail: "pii",
	}
}

// maskInvitationForCaller runs the persona decision for a single
// invitation render (CreateInvitation response + GetMyInvitation).
// Deny → writes 404 + audit row, returns ok=false (caller MUST exit
// immediately). Allow-with-mask → blanks the listed fields, writes audit
// row, returns ok=true.
//
// invitationID is the resource_id passed to persona; pass the canonical
// UUID-string so persona's policy can key on it (e.g. attribute-mapping
// "actor.email == invitation.invitee_email → pii-grant").
func (h *Handler) maskInvitationForCaller(w http.ResponseWriter, r *http.Request, workspaceID, invitationID string, resp *InvitationResponse) bool {
	if h == nil || h.PersonaMask == nil {
		return true
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindInvitation, invitationID,
		"", invitationFieldClasses())
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindInvitation, invitationID, "")
		writeError(w, http.StatusNotFound, "invitation not found")
		return false
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindInvitation, invitationID, "")
		applyInvitationMask(resp, dec.MaskedFields)
	}
	return true
}

// maskInvitationsListForCaller applies persona's per-row decision to a
// slice of InvitationResponse. Matches the members-list pattern: a deny
// blanks the PII fields but keeps the row identifier so the UI can still
// render a "no access" placeholder. Dropping the row would leak the
// existence of an invitation through the row count, which the issue
// description explicitly rejects ("emails som null på fremmede
// invitations").
//
// workspaceID is passed in by the caller because the InvitationResponse
// shape carries it as a string and we'd rather not parse-and-uuid-roundtrip
// per row. The handler already has the workspace UUID resolved.
func (h *Handler) maskInvitationsListForCaller(r *http.Request, workspaceID string, resp []InvitationResponse) []InvitationResponse {
	if h == nil || h.PersonaMask == nil || len(resp) == 0 {
		return resp
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	fieldClasses := invitationFieldClasses()
	for i := range resp {
		// ListMyInvitations crosses workspace boundaries — each row may
		// live in a different workspace. Audit under the row's own
		// workspace_id so the ledger reflects where the redaction
		// happened, not where the request landed.
		auditWorkspaceID := resp[i].WorkspaceID
		if auditWorkspaceID == "" {
			auditWorkspaceID = workspaceID
		}
		dec := h.maskDecide(r.Context(),
			actorID, personaActionRead, personaKindInvitation, resp[i].ID,
			"", fieldClasses)
		if !dec.Allowed {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				auditWorkspaceID, actorType, actorID, personaActionRead,
				personaKindInvitation, resp[i].ID, "")
			resp[i].InviteeEmail = ""
			resp[i].InviterEmail = ""
			continue
		}
		if len(dec.MaskedFields) > 0 {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				auditWorkspaceID, actorType, actorID, personaActionRead,
				personaKindInvitation, resp[i].ID, "")
			applyInvitationMask(&resp[i], dec.MaskedFields)
		}
	}
	return resp
}
