// handler.go exposes the workspace-copy API (TECH-3582).
//
// Route (mounted under /api/workspaces/{id}/cerebro/copy in router.go, owner/admin only):
//
//	POST / — copy one entity from this workspace into a target workspace.
//	         body: {target_workspace_id, entity_type, source_id, run_id?}
//	         entity_type ∈ {issue, channel, dm, agent, project, chat, autopilot}
//	         entity_type = "relink" runs the issue->parent / issue->project
//	         healing post-pass on the target (source_id not required).
//	         entity_type = "workspace_artifacts" bulk-copies the source
//	         workspace's folder tree + workspace-scoped artifacts (no source_id).
//	         Issue- and project-scoped artifacts travel with their parent copy.
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
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes the copy engine over HTTP.
type Handler struct {
	Store *Store
}

// NewHandler builds a Handler bound to the given pool and blob store. The blob
// store lets the relink post-pass materialize copied file blobs into the target
// workspace (TECH-3766).
func NewHandler(pool *pgxpool.Pool, st storage.Storage) *Handler {
	return &Handler{Store: New(pool).WithStorage(st)}
}

type copyRequest struct {
	TargetWorkspaceID string `json:"target_workspace_id"`
	EntityType        string `json:"entity_type"`
	SourceID          string `json:"source_id"`
	RunID             string `json:"run_id,omitempty"`
	// Cascade copies everything underneath the picked root in one call: for an
	// issue, all open descendant sub-issues; for a project, all its open issues.
	Cascade bool `json:"cascade,omitempty"`
	// Statuses optionally restricts a project cascade to issues with these
	// statuses (and their matching-status sub-issues). Empty = every open issue.
	Statuses []string `json:"statuses,omitempty"`
	// Conflict is the name-clash policy for uniquely-named entities (agent,
	// skill): "skip" (default — reuse the existing target), "overwrite" (update
	// it from the source) or "keep_both" (create a suffixed copy).
	Conflict string `json:"conflict,omitempty"`
}

// relinkResponse is the combined result of the target-only post-pass: structural
// link healing, internal-reference rewriting, and file-blob materialization.
type relinkResponse struct {
	RelinkResult
	RewriteResult
	FilesMaterializedResult
}

// Copy copies one entity from the URL workspace into the target workspace.
func (h *Handler) Copy(w http.ResponseWriter, r *http.Request) {
	sourceWS, ok := h.workspaceID(w, r)
	if !ok {
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

	// workspace_artifacts is a bulk pass: copy the source workspace's folder tree
	// and its workspace-scoped artifacts (documents/notes) into the target. No
	// per-entity source_id — the source is the URL workspace.
	if req.EntityType == "workspace_artifacts" {
		runID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		res, err := h.Store.CopyWorkspaceArtifacts(r.Context(), runID, sourceWS, target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "copy failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	// foundation is the one-time bulk pass that must run FIRST: labels, skills,
	// connections + credentials, the role/group/permission graph, GitHub links
	// and workspace settings. No per-entity source_id.
	if req.EntityType == "foundation" {
		runID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		res, err := h.Store.CopyFoundation(r.Context(), runID, sourceWS, target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "copy failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	// group_access heals group→agent access grants after agents are copied.
	if req.EntityType == "group_access" {
		n, err := h.Store.RelinkGroupAccess(r.Context(), target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "relink failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"group_agent_access_relinked": n})
		return
	}

	// relink is a target-only post-pass (no source): it heals issue->parent /
	// issue->project links AND rewrites internal text references (mention-link
	// UUIDs + identifier tokens) to the copies, regardless of copy order.
	if req.EntityType == "relink" {
		rel, err := h.Store.RelinkIssueRelations(r.Context(), target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "relink failed: "+err.Error())
			return
		}
		rew, err := h.Store.RewriteInternalReferences(r.Context(), target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reference rewrite failed: "+err.Error())
			return
		}
		// Give every shared file blob its own physical copy in the target so the
		// source workspace can be deleted without the copies losing their files.
		mat, err := h.Store.MaterializeCopiedFiles(r.Context(), target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "file materialization failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, relinkResponse{RelinkResult: rel, RewriteResult: rew, FilesMaterializedResult: mat})
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
	policy := parseConflictPolicy(req.Conflict)
	switch req.EntityType {
	case "issue", "channel", "dm":
		if req.Cascade {
			res, err = h.Store.CopyIssueCascade(r.Context(), runID, target, source)
		} else {
			res, err = h.Store.CopyIssue(r.Context(), runID, target, source)
		}
	case "agent":
		res, err = h.Store.CopyAgent(r.Context(), runID, target, source, policy)
	case "skill":
		res, err = h.Store.CopySkill(r.Context(), runID, target, source, policy)
	case "project":
		if req.Cascade {
			res, err = h.Store.CopyProjectCascadeFiltered(r.Context(), runID, target, source, req.Statuses)
		} else {
			res, err = h.Store.CopyProject(r.Context(), runID, target, source)
		}
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
