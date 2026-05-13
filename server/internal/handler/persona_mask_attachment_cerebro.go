// JEH-1173: persona-mask application for the attachment read paths.
//
// Sibling of file.go (upstream). On deny the GET returns 404. On
// allow-with-mask the filename / url / download_url fields are blanked
// so the client renders an "access denied" placeholder without leaking
// the original filename or signed URL.

package handler

import (
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func applyAttachmentMask(resp *AttachmentResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldFilename:
			resp.Filename = ""
		case personaFieldURL:
			resp.URL = ""
		case personaFieldDownloadURL:
			resp.DownloadURL = ""
		}
	}
}

// maskAttachmentForCaller runs the persona decision for a single
// attachment GET. Returns true to render, false after writing 404.
func (h *Handler) maskAttachmentForCaller(w http.ResponseWriter, r *http.Request, att db.Attachment, resp *AttachmentResponse) bool {
	if h == nil || h.PersonaMask == nil {
		return true
	}
	workspaceID := uuidToString(att.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	classification := att.Classification
	if classification == "" {
		classification = "internal"
	}
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindAttachment, uuidToString(att.ID),
		classification, attachmentFieldClasses(classification))
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindAttachment, uuidToString(att.ID), classification)
		writeError(w, http.StatusNotFound, "attachment not found")
		return false
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindAttachment, uuidToString(att.ID), classification)
		applyAttachmentMask(resp, dec.MaskedFields)
	}
	return true
}

func attachmentFieldClasses(classification string) map[string]string {
	if classification == "" || classification == "public" || classification == "internal" {
		return nil
	}
	return map[string]string{
		personaFieldFilename:    classification,
		personaFieldURL:         classification,
		personaFieldDownloadURL: classification,
	}
}

// maskAttachmentsForCaller filters a slice of attachment-rows in place:
// rows the caller can't see are dropped; rows with masked fields have
// those fields zeroed. Used for embedded attachments inside the issue /
// comment GET responses so the redaction guarantee holds even when the
// attachment isn't fetched through GetAttachmentByID.
func (h *Handler) maskAttachmentsForCaller(r *http.Request, workspaceID string, atts []db.Attachment, resp []AttachmentResponse) []AttachmentResponse {
	if h == nil || h.PersonaMask == nil || len(atts) == 0 {
		return resp
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	out := resp[:0]
	for i, a := range atts {
		classification := a.Classification
		if classification == "" {
			classification = "internal"
		}
		dec := h.maskDecide(r.Context(),
			actorID, personaActionRead, personaKindAttachment, uuidToString(a.ID),
			classification, attachmentFieldClasses(classification))
		if !dec.Allowed {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindAttachment, uuidToString(a.ID), classification)
			continue
		}
		if len(dec.MaskedFields) > 0 {
			h.recordPersonaMaskRedaction(r.Context(), dec,
				workspaceID, actorType, actorID, personaActionRead,
				personaKindAttachment, uuidToString(a.ID), classification)
			applyAttachmentMask(&resp[i], dec.MaskedFields)
		}
		out = append(out, resp[i])
	}
	return out
}
