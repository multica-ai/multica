package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// GetOfficeDigest returns the Secretary's digest: per-squad facts computed by
// SQL and bounded in size. Any workspace member may read it.
//
//	GET /api/office/digest?since=<RFC 3339 timestamp>   (default: the last 24h)
func (h *Handler) GetOfficeDigest(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	svc := service.NewDigestService(h.Queries)
	since, err := service.ParseDigestSince(r.URL.Query().Get("since"), time.Now())
	if err != nil {
		var windowErr *service.ErrDigestWindow
		if errors.As(err, &windowErr) {
			writeError(w, http.StatusBadRequest, windowErr.Message)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid since")
		return
	}

	digest, err := svc.Compute(r.Context(), wsUUID, since, h.getIssuePrefix(r.Context(), wsUUID))
	if err != nil {
		slog.Warn("compute digest failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to compute digest")
		return
	}
	writeJSON(w, http.StatusOK, digest)
}
