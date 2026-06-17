// handler.go exposes the workspace-copy API (TECH-3582).
//
// Route (mounted under /api/workspaces/{id}/cerebro/copy in router.go, owner/admin only):
//
//	POST / — copy one entity from this workspace into a target workspace.
//	         body: {target_workspace_id, entity_type, source_id, run_id?}
//	         entity_type ∈ {issue, channel, dm, agent, project, chat, autopilot}
//	         entity_type = "relink" runs the issue->parent / issue->project
//	         healing post-pass on the target (source_id not required).
//
// Copy is non-destructive: the source is never modified. See store.go.
//
// CEREBRO-PATCH(workspace-copy): TECH-3582 fork-specific workspace copy handler.
package workspacecopy

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes the copy engine over HTTP.
type Handler struct {
	Store *Store
}

// NewHandler builds a Handler bound to the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Store: New(pool)}
}

type copyRequest struct {
	TargetWorkspaceID string `json:"target_workspace_id"`
	EntityType        string `json:"entity_type"`
	SourceID          string `json:"source_id"`
	RunID             string `json:"run_id,omitempty"`
}

// Copy copies one entity from the URL workspace into the target workspace.
func (h *Handler) Copy(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.workspaceID(w, r); !ok {
		return
	}
	var req copyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, ok := parseUUIDOrBadRequest(w, req.TargetWorkspaceID, "target_workspace_id")
	if !ok {
		return
	}

	// relink is a target-only post-pass (no source): it heals issue->parent /
	// issue->project links once both ends are copied, regardless of copy order.
	if req.EntityType == "relink" {
		rel, err := h.Store.RelinkIssueRelations(r.Context(), target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "relink failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rel)
		return
	}

	source, ok := parseUUIDOrBadRequest(w, req.SourceID, "source_id")
	if !ok {
		return
	}
	runID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if req.RunID != "" {
		if runID, ok = parseUUIDOrBadRequest(w, req.RunID, "run_id"); !ok {
			return
		}
	}

	var (
		res CopyResult
		err error
	)
	switch req.EntityType {
	case "issue", "channel", "dm":
		res, err = h.Store.CopyIssue(r.Context(), runID, target, source)
	case "agent":
		res, err = h.Store.CopyAgent(r.Context(), runID, target, source)
	case "project":
		res, err = h.Store.CopyProject(r.Context(), runID, target, source)
	case "chat":
		res, err = h.Store.CopyChat(r.Context(), runID, target, source)
	case "autopilot":
		res, err = h.Store.CopyAutopilot(r.Context(), runID, target, source)
	default:
		writeError(w, http.StatusBadRequest, "unknown entity_type: "+req.EntityType)
		return
	}

	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, res)
	case errors.Is(err, ErrNameConflict), errors.Is(err, ErrAgentNotCopied), errors.Is(err, ErrAssigneeNotCopied):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "copy failed: "+err.Error())
	}
}

// --- helpers (mirrors the connections handler conventions) ------------------

func (h *Handler) workspaceID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if _, ok := middleware.MemberFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func parseUUIDOrBadRequest(w http.ResponseWriter, s, field string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return pgtype.UUID{}, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
