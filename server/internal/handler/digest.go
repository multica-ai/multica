package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetOfficeDigest returns the Secretary's digest: per-squad facts computed by
// SQL and bounded in size. Any workspace member may read it.
//
//	GET /api/office/digest?since=<RFC 3339 timestamp>   (default: the last 24h)
//	GET /api/office/digest?floor_id=<floor id>          (one floor's squads)
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
	if !h.applyDigestScope(w, r, wsUUID, svc) {
		return
	}
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

// applyDigestScope sets the digest's scope. A floor_id scopes it to that floor
// (a floor chief's digest). Otherwise the company digest reads squads or floor
// briefs by the workspace's digest mode (spec 4.8).
func (h *Handler) applyDigestScope(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, svc *service.DigestService) bool {
	if raw := r.URL.Query().Get("floor_id"); raw != "" {
		floorID, ok := parseUUIDOrBadRequest(w, raw, "floor_id")
		if !ok {
			return false
		}
		if _, err := h.Queries.GetOfficeFloorByID(r.Context(), db.GetOfficeFloorByIDParams{ID: floorID, WorkspaceID: wsUUID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "floor not found")
				return false
			}
			writeError(w, http.StatusInternalServerError, "failed to read the floor")
			return false
		}
		svc.FloorID = floorID
		return true
	}
	mode, err := h.digestModeOf(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the digest mode")
		return false
	}
	svc.Mode = mode.Resolved
	return true
}
