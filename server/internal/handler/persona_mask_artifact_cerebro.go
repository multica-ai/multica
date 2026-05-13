// JEH-1173: persona-mask application for the artifact read paths.
//
// Sibling of artifact.go (upstream). The artifact table has no
// classification column in migration 9020 — JEH-1079 stopped at
// issue/comment/attachment per Sara's scope precision. v1 here treats
// every artifact as 'internal' for the purpose of the persona decision,
// which still exercises the actor × kind × action policy path. Per-row
// classification on artifacts is a follow-up migration when the artifact
// authoring UI grows a label picker.

package handler

import (
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func applyArtifactMask(resp *ArtifactResponse, masked []string) {
	for _, f := range masked {
		switch strings.ToLower(f) {
		case personaFieldTitle:
			resp.Title = ""
		case personaFieldBody:
			resp.Body = ""
		case personaFieldFileURL:
			resp.FileURL = nil
		}
	}
}

func (h *Handler) maskArtifactForCaller(w http.ResponseWriter, r *http.Request, artifact db.Artifact, resp *ArtifactResponse) bool {
	if h == nil || h.PersonaMask == nil {
		return true
	}
	workspaceID := uuidToString(artifact.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorID == "" {
		actorID = "anonymous"
	}
	// Artifacts inherit 'internal' until the migration adds a
	// classification column. The decision still goes through persona so
	// an actor without read-artifact grant gets a deny; this future-
	// proofs the redaction path against a per-row column landing later.
	classification := "internal"
	dec := h.maskDecide(r.Context(),
		actorID, personaActionRead, personaKindArtifact, uuidToString(artifact.ID),
		classification, nil)
	if !dec.Allowed {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindArtifact, uuidToString(artifact.ID), classification)
		writeError(w, http.StatusNotFound, "artifact not found")
		return false
	}
	if len(dec.MaskedFields) > 0 {
		h.recordPersonaMaskRedaction(r.Context(), dec,
			workspaceID, actorType, actorID, personaActionRead,
			personaKindArtifact, uuidToString(artifact.ID), classification)
		applyArtifactMask(resp, dec.MaskedFields)
	}
	return true
}
