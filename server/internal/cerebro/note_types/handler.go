package notetypes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler hosts the REST surface for note types. Wired into the router under
// /api/cerebro/note-types by the cerebro-note-types-routes CEREBRO-PATCH in
// server/cmd/server/router.go.
//
// Every endpoint is workspace-scoped via the X-Workspace-ID header (read
// through middleware.WorkspaceIDFromContext) and authenticated via the
// X-User-ID header (requireUserID). Pool is used by RunNow to materialise a
// type inside a transaction.
type Handler struct {
	Cerebro *cerebrodb.Queries
	Pool    *pgxpool.Pool
}

func NewHandler(cerebro *cerebrodb.Queries, pool *pgxpool.Pool) *Handler {
	return &Handler{Cerebro: cerebro, Pool: pool}
}

// ---------------------------------------------------------------------------
// Request / response shapes
// ---------------------------------------------------------------------------

type noteTypeRequest struct {
	Name           string  `json:"name"`
	Icon           string  `json:"icon"`
	TemplateBody   string  `json:"template_body"`
	RecurrenceMode string  `json:"recurrence_mode"`
	CadenceUnit    string  `json:"cadence_unit"`
	CadenceCount   *int32  `json:"cadence_count"`
	TargetFolderID *string `json:"target_folder_id"`
	Enabled        *bool   `json:"enabled"`
}

type noteTypeResponse struct {
	ID                   string  `json:"id"`
	WorkspaceID          string  `json:"workspace_id"`
	Name                 string  `json:"name"`
	Icon                 string  `json:"icon"`
	TemplateBody         string  `json:"template_body"`
	RecurrenceMode       string  `json:"recurrence_mode"`
	CadenceUnit          string  `json:"cadence_unit"`
	CadenceCount         int32   `json:"cadence_count"`
	TargetFolderID       *string `json:"target_folder_id"`
	RunningDocArtifactID *string `json:"running_doc_artifact_id"`
	Enabled              bool    `json:"enabled"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

func toResponse(nt cerebrodb.CerebroNoteType) noteTypeResponse {
	resp := noteTypeResponse{
		ID:             util.UUIDToString(nt.ID),
		WorkspaceID:    util.UUIDToString(nt.WorkspaceID),
		Name:           nt.Name,
		Icon:           nt.Icon,
		TemplateBody:   nt.TemplateBody,
		RecurrenceMode: nt.RecurrenceMode,
		CadenceUnit:    nt.CadenceUnit,
		CadenceCount:   nt.CadenceCount,
		Enabled:        nt.Enabled,
		CreatedAt:      tsString(nt.CreatedAt),
		UpdatedAt:      tsString(nt.UpdatedAt),
	}
	if nt.TargetFolderID.Valid {
		s := util.UUIDToString(nt.TargetFolderID)
		resp.TargetFolderID = &s
	}
	if nt.RunningDocArtifactID.Valid {
		s := util.UUIDToString(nt.RunningDocArtifactID)
		resp.RunningDocArtifactID = &s
	}
	return resp
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// ListNoteTypes returns every note type in the current workspace.
func (h *Handler) ListNoteTypes(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroNoteTypesByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list note types failed")
		return
	}
	out := make([]noteTypeResponse, 0, len(rows))
	for _, nt := range rows {
		out = append(out, toResponse(nt))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetNoteType returns a single note type.
func (h *Handler) GetNoteType(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	nt, err := h.Cerebro.GetCerebroNoteType(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load note type failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, nt.WorkspaceID) {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(nt))
}

// CreateNoteType creates a new note type in the current workspace.
func (h *Handler) CreateNoteType(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	createdBy, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user")
		return
	}

	var req noteTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	mode, cadence, count, folder, problem := h.normalize(req, ModeRunningDoc, CadenceManual, 1, w, r)
	if problem {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	nt, err := h.Cerebro.CreateCerebroNoteType(r.Context(), cerebrodb.CreateCerebroNoteTypeParams{
		WorkspaceID:    wsUUID,
		Name:           strings.TrimSpace(req.Name),
		Icon:           req.Icon,
		TemplateBody:   req.TemplateBody,
		RecurrenceMode: mode,
		CadenceUnit:    cadence,
		CadenceCount:   count,
		Enabled:        enabled,
		CreatedBy:      createdBy,
		TargetFolderID: folder,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create note type failed")
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(nt))
}

// UpdateNoteType updates an existing note type.
func (h *Handler) UpdateNoteType(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroNoteType(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load note type failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}

	var req noteTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	mode, cadence, count, folder, problem := h.normalize(req, existing.RecurrenceMode, existing.CadenceUnit, existing.CadenceCount, w, r)
	if problem {
		return
	}
	name := existing.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	nt, err := h.Cerebro.UpdateCerebroNoteType(r.Context(), cerebrodb.UpdateCerebroNoteTypeParams{
		ID:             id,
		Name:           name,
		Icon:           req.Icon,
		TemplateBody:   req.TemplateBody,
		RecurrenceMode: mode,
		CadenceUnit:    cadence,
		CadenceCount:   count,
		Enabled:        enabled,
		TargetFolderID: folder,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update note type failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(nt))
}

// DeleteNoteType removes a note type. Materialised notes are kept (the FK is
// ON DELETE SET NULL) so history is never lost.
func (h *Handler) DeleteNoteType(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroNoteType(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load note type failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}
	if err := h.Cerebro.DeleteCerebroNoteType(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete note type failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunNow materialises the note type immediately ("Start ny nu"), regardless of
// cadence. Returns the affected artifact id and whether a note was created.
func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	nt, err := h.Cerebro.GetCerebroNoteType(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note type not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load note type failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, nt.WorkspaceID) {
		return
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run failed")
		return
	}
	defer tx.Rollback(r.Context())
	created, artifactID, err := Apply(r.Context(), h.Cerebro.WithTx(tx), nt, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "run failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created":     created,
		"artifact_id": util.UUIDToString(artifactID),
	})
}

// normalize validates + resolves the mutable fields shared by create/update,
// falling back to the supplied defaults when a field is omitted. Returns
// problem=true (after writing the 4xx) when validation fails.
func (h *Handler) normalize(req noteTypeRequest, defMode, defCadence string, defCount int32, w http.ResponseWriter, r *http.Request) (mode, cadence string, count int32, folder pgtype.UUID, problem bool) {
	mode = req.RecurrenceMode
	if mode == "" {
		mode = defMode
	}
	if err := ValidateMode(mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", 0, pgtype.UUID{}, true
	}
	cadence = req.CadenceUnit
	if cadence == "" {
		cadence = defCadence
	}
	if err := ValidateCadence(cadence); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", 0, pgtype.UUID{}, true
	}
	count = defCount
	if req.CadenceCount != nil {
		count = *req.CadenceCount
	}
	if count <= 0 {
		count = 1
	}
	if req.TargetFolderID != nil && strings.TrimSpace(*req.TargetFolderID) != "" {
		f, err := util.ParseUUID(strings.TrimSpace(*req.TargetFolderID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid target_folder_id")
			return "", "", 0, pgtype.UUID{}, true
		}
		folder = f
	}
	return mode, cadence, count, folder, false
}

// ---------------------------------------------------------------------------
// Helpers (mirrors of the sprints package; kept local to avoid cross-package
// coupling on request-plumbing internals).
// ---------------------------------------------------------------------------

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing "+name)
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return uid, true
}

func workspaceFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, false
	}
	return uid, true
}

func ensureWorkspaceMatch(w http.ResponseWriter, r *http.Request, target pgtype.UUID) bool {
	current, ok := workspaceFromContext(w, r)
	if !ok {
		return false
	}
	if !uuidEqual(current, target) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return false
	}
	return true
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func tsString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
