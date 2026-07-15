// CEREBRO-PATCH(embedded-chat-artifacts): FIR-2835 typed report cards in streams.
package handler

import (
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/chatstream"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) chatStreamArtifactParts(r *http.Request, content, workspaceID string) []any {
	ids := parseArtifactMentions(content)
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
		if baseURL := strings.TrimSuffix(h.cfg.PublicURL, "/"); baseURL != "" {
			data["url"] = baseURL + "/" + workspaceID + "/documents/" + id
		}
		parts = append(parts, chatstream.DataChunk{
			Type: "data-artifact",
			ID:   "artifact-" + id,
			Data: data,
		})
	}
	return parts
}
