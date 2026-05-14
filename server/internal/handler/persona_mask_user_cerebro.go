// CEREBRO-PATCH(persona-mask-user-cerebro): JEH-1186 — net-new cerebro
// file. Sibling of auth.go / invitation.go (upstream). Wires
// mask.KindUser ("multica.user") into the auth/me + login response
// paths so callers without a pii grant see email/phone as null on the
// UserResponse shape.
//
// Self-reads (GetMe on own ID, just-minted session in login flows) are
// expected to receive a pii-grant via persona policy — see
// persona/policies — so this helper never gates the response, it only
// applies the mask. A deny outcome would mean persona is mis-configured
// for self-reads; in that case we still return the row with PII zeroed
// rather than 404 the user out of their own session.

package handler

import (
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const personaKindUser = "multica.user"

// applyUserMask zeros every field in `masked` on resp. Unknown field
// names are no-ops so the persona contract can grow without forcing a
// handler edit. Phone is not on the response shape today; the case is
// staged for the day it joins UserResponse.
func applyUserMask(resp *UserResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldEmail:
			resp.Email = ""
		case personaFieldPhone:
			// UserResponse has no phone field yet — placeholder.
		}
	}
}

// maskUserForCaller runs the persona decision for a single user read
// and applies any returned MaskedFields in place. Unlike the
// issue/comment/attachment masks, this helper does NOT gate the response
// on a deny outcome — login + GetMe handlers can't sensibly 404 the
// caller out of their own session. A deny instead zeroes every pii
// field on the response so the policy stays defensive even when persona
// is misconfigured for self-reads.
//
// No-op when persona is not configured — preserves the pre-JEH-1186
// baseline (full payload) for workspaces running without persona.
func (h *Handler) maskUserForCaller(r *http.Request, user db.User, resp *UserResponse) {
	if h == nil || h.PersonaMask == nil {
		return
	}
	userID := uuidToString(user.ID)
	actorID := requestUserID(r)
	if actorID == "" {
		actorID = "anonymous"
	}
	// User-shape reads cross workspace boundaries (login mints a token
	// before any workspace is selected), so the actor identity is the
	// user themselves — no resolveActor agent override applies here.
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindUser, userID,
		"", userFieldClasses())
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			"", "member", actorID, personaActionRead,
			personaKindUser, userID, "")
		// Defensive zero: persona denied even self-read. Strip every
		// pii field rather than leak through a mis-configured policy.
		resp.Email = ""
		return
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			"", "member", actorID, personaActionRead,
			personaKindUser, userID, "")
		applyUserMask(resp, dec.MaskedFields)
	}
}

// userFieldClasses mirrors mask.FieldClassifications(mask.KindUser) but
// inlined here so the upstream handler package doesn't import the
// cerebro tree. Router-side startup keeps the two in sync (see
// router cerebro wiring).
func userFieldClasses() map[string]string {
	return map[string]string{
		personaFieldEmail: "pii",
		personaFieldPhone: "pii",
	}
}
