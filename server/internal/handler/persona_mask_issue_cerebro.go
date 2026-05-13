// JEH-1173: persona-mask application for the issue read paths.
//
// Lives alongside issue.go (upstream) as a cerebro-prefixed sibling so
// the upstream call site stays a single CEREBRO-PATCH line. Each masked
// or denied read writes one row to cerebro_persona_mask_audit via
// h.PersonaMaskAudit — persona's logbog only captures /v1/log calls
// pushed by the SDK, so the cerebro side is the canonical redaction
// ledger.

package handler

import (
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	personaKindIssue      = "multica.issue"
	personaKindComment    = "multica.comment"
	personaKindAttachment = "multica.attachment"
	personaKindArtifact   = "multica.artifact"

	personaFieldTitle       = "title"
	personaFieldDescription = "description"
	personaFieldContent     = "content"
	personaFieldURL         = "url"
	personaFieldDownloadURL = "download_url"
	personaFieldFilename    = "filename"
	personaFieldBody        = "body"
	personaFieldFileURL     = "file_url"
)

// applyIssueMask zeros every field in `masked` on resp. Unknown field
// names are no-ops so persona's contract can grow without forcing a
// handler edit. Title/description are the only content fields the issue
// response shape exposes today.
func applyIssueMask(resp *IssueResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldTitle:
			resp.Title = ""
		case personaFieldDescription:
			resp.Description = nil
		}
	}
}

// maskIssueForCaller runs the persona decision for a single issue read.
// Returns true when the caller may render `resp` (possibly mutated with
// masked fields zeroed). Returns false after writing a 404 — the caller
// MUST exit immediately.
//
// A deny / mask outcome writes one row to cerebro_persona_mask_audit
// before returning. Allow-without-mask writes nothing — the audit table
// is a redaction ledger, not a general access log.
func (h *Handler) maskIssueForCaller(w http.ResponseWriter, r *http.Request, issue db.Issue, resp *IssueResponse) bool {
	if h == nil || h.PersonaMask == nil {
		return true
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	classification := issue.Classification
	if classification == "" {
		classification = "internal"
	}
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindIssue, uuidToString(issue.ID),
		classification, issueFieldClasses(classification))
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindIssue, uuidToString(issue.ID), classification)
		writeError(w, http.StatusNotFound, "issue not found")
		return false
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindIssue, uuidToString(issue.ID), classification)
		applyIssueMask(resp, dec.MaskedFields)
	}
	return true
}

// issueFieldClasses tags the sensitive content fields with the row's
// classification. Persona will only add a field to MaskedFields when
// its classification exceeds the firing grant's MaxClassification — for
// an internal-classified row read by an internal-grant actor, this map
// produces zero masking, identical to the pre-JEH-1173 baseline.
func issueFieldClasses(classification string) map[string]string {
	if classification == "" || classification == "public" || classification == "internal" {
		return nil
	}
	return map[string]string{
		personaFieldTitle:       classification,
		personaFieldDescription: classification,
	}
}
