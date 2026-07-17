// CEREBRO-PATCH(embedded-chat-artifacts): FIR-2835 typed report cards in streams.
package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/cerebro/chatstream"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) chatStreamArtifactParts(r *http.Request, content, workspaceID string) []any {
	ids := parseArtifactMentions(content)
	if len(ids) == 0 {
		return nil
	}
	workspaceSlug := ""
	if workspace, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID)); err == nil {
		workspaceSlug = workspace.Slug
	}
	parts := make([]any, 0, len(ids))
	for _, id := range ids {
		artifact, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
			ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil || !h.folderVisibleToCaller(r, artifact.FolderID) {
			continue
		}
		data := map[string]string{
			"id": id, "title": artifact.Title, "kind": artifact.Kind, "body": artifact.Body,
		}
		if documentURL := chatstream.ArtifactDocumentURL(h.cfg.PublicURL, workspaceSlug, id); documentURL != "" {
			data["url"] = documentURL
		}
		parts = append(parts, chatstream.DataChunk{
			Type: "data-artifact",
			ID:   "artifact-" + id,
			Data: data,
		})
	}
	return parts
}
