// JEH-1079: field-level redaction for the workspace members endpoints.
//
// Lives alongside workspace.go (upstream) as a cerebro-prefixed sibling
// so the upstream file's modification stays a single CEREBRO-PATCH
// line. The persona-side static map keys members.email + members.phone
// as pii, so callers whose grant is capped at internal see them as null.

package handler

import (
	"net/http"
	"strings"
)

const (
	personaKindMember  = "multica.member"
	personaActionRead  = "read"
	personaFieldEmail  = "email"
	personaFieldPhone  = "phone"
)

// maskMembersListForCaller applies persona's per-field decision to a
// slice of MemberWithUserResponse rows. The actor-id used for the
// decision is the requesting user — anonymous callers and unknown
// principals fail closed (every member is rendered with redacted PII).
//
// The function runs a single persona check per row at the moment. A
// bulk-check variant is sketched in JEH-1079's comment thread; v1 keeps
// the per-row call so the wire format stays minimal. List endpoints are
// already paginated upstream, so the request count stays bounded.
func (h *Handler) maskMembersListForCaller(r *http.Request, workspaceID string, rows []MemberWithUserResponse) []MemberWithUserResponse {
	if h == nil || h.PersonaMask == nil || len(rows) == 0 {
		return rows
	}
	actorID := requestUserID(r)
	if actorID == "" {
		// Anonymous reads of the members list aren't normally allowed,
		// but if they reach here we still strip pii rather than leak it.
		actorID = "anonymous"
	}

	fieldClasses := memberFieldClasses()
	for i := range rows {
		dec := h.maskDecide(r.Context(), actorID, personaActionRead, personaKindMember, rows[i].ID, "", fieldClasses)
		if !dec.Allowed {
			// Deny on a row inside a list: blank everything but the
			// stable identifiers so the UI can still render a "no
			// access" placeholder. The alternative — dropping the row
			// — would mask the existence of the member, which we
			// intentionally do not do here.
			rows[i].Email = ""
			rows[i].Name = ""
			rows[i].AvatarURL = nil
			continue
		}
		applyMemberMask(&rows[i], dec.MaskedFields)
	}
	return rows
}

func memberFieldClasses() map[string]string {
	// Mirrors mask.FieldClassifications(mask.KindMember) but inlined
	// here so the upstream handler package doesn't import the cerebro
	// tree. The router has a startup assertion (see router cerebro
	// wiring) that keeps the two in sync.
	return map[string]string{
		personaFieldEmail: "pii",
		personaFieldPhone: "pii",
	}
}

// applyMemberMask strips the named fields from one row. Fields that
// don't exist on MemberWithUserResponse (e.g. phone — present on the
// User table but not surfaced on this response yet) are no-ops so the
// persona contract can grow without forcing handler edits.
func applyMemberMask(row *MemberWithUserResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldEmail:
			row.Email = ""
		case personaFieldPhone:
			// Not on this response shape yet; placeholder for the day it
			// joins MemberWithUserResponse.
		}
	}
}
