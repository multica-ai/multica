package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue status catalog API (MUL-6243).
//
// Reading the catalog is open to any workspace member — every client needs it
// to render a status. Mutating it is owner/admin only: a status is workflow
// configuration shared by the whole workspace, and creating one changes what
// agents can be told to do.

type IssueStatusResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Color       string  `json:"color"`
	IsSystem    bool    `json:"is_system"`
	Position    float64 `json:"position"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func issueStatusToResponse(s db.IssueStatus) IssueStatusResponse {
	return IssueStatusResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Key:         s.Key,
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Color:       s.Color,
		IsSystem:    s.IsSystem,
		Position:    s.Position,
		ArchivedAt:  timestampToPtr(s.ArchivedAt),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

type CreateIssueStatusRequest struct {
	// Key is optional; it is derived from Name when omitted. Immutable once
	// created, because it is the value stored in issue.status.
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Color       string `json:"color"`
}

// UpdateIssueStatusRequest deliberately has no Key or Category field. Both are
// immutable: changing a category would silently rewrite the machine semantics
// of every issue already on that status, and changing a key would strand them.
type UpdateIssueStatusRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Color       *string  `json:"color"`
	Position    *float64 `json:"position"`
}

// ListIssueStatuses returns the workspace's status catalog in display order.
// Any member may read it.
func (h *Handler) ListIssueStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	// Self-heal: a workspace created by a pod that predates this feature has no
	// catalog rows. Seeding on read keeps the endpoint correct during a rolling
	// deploy without a second backfill pass. Idempotent, so this is a no-op
	// once the rows exist.
	if err := issuestatus.Ensure(r.Context(), h.Queries, wsUUID); err != nil {
		slog.Warn("failed to ensure issue status catalog", append(logger.RequestAttrs(r), "error", err)...)
	}

	includeArchived := strings.EqualFold(r.URL.Query().Get("include_archived"), "true")
	entries, err := h.Queries.ListIssueStatusEntries(r.Context(), db.ListIssueStatusEntriesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		slog.Warn("ListIssueStatuses failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue statuses")
		return
	}

	resp := make([]IssueStatusResponse, len(entries))
	for i, e := range entries {
		resp[i] = issueStatusToResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"statuses":   resp,
		"categories": issuestatus.Canonical(),
		"total":      len(resp),
	})
}

// CreateIssueStatus adds a custom status to the workspace catalog.
func (h *Handler) CreateIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	// Rollout gate (see featureflags.CustomIssueStatuses). Creating the first
	// custom status mints a value older pods cannot interpret, so it stays
	// closed until the whole fleet is running code that resolves categories.
	// Only creation is gated — reading and resolving are safe unconditionally.
	if !featureflags.CustomIssueStatusesEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusForbidden, "custom issue statuses are not enabled for this deployment")
		return
	}

	var req CreateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}
	if len([]rune(req.Description)) > 256 {
		writeError(w, http.StatusBadRequest, "description must be at most 256 characters")
		return
	}
	if !issuestatus.IsCategory(req.Category) {
		writeError(w, http.StatusBadRequest, "category must be one of: "+strings.Join(issuestatus.Canonical(), ", "))
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// An explicit key wins; otherwise derive one from the name so the common
	// case is a single field. Either way it is validated against the reserved
	// built-in keys and the storage pattern.
	var key string
	if strings.TrimSpace(req.Key) != "" {
		key, err = issuestatus.ValidateKey(req.Key)
	} else {
		key, err = issuestatus.SlugifyKey(name)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entry, err := h.Queries.CreateIssueStatusEntry(r.Context(), db.CreateIssueStatusEntryParams{
		WorkspaceID: wsUUID,
		Key:         key,
		Name:        name,
		Description: req.Description,
		Category:    req.Category,
		Color:       strings.ToLower(color),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a status with this key or name already exists")
			return
		}
		slog.Warn("CreateIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue status")
		return
	}
	writeJSON(w, http.StatusCreated, issueStatusToResponse(entry))
}

// UpdateIssueStatus edits a custom status's presentation. Built-in statuses are
// immutable in v1 — name and color included — so the default workspace looks
// and behaves identically for everyone who never opens this settings page.
func (h *Handler) UpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, ok := h.loadIssueStatusForAdmin(w, r)
	if !ok {
		return
	}

	var req UpdateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if entry.IsSystem {
		writeError(w, http.StatusForbidden, "built-in statuses cannot be modified")
		return
	}
	if entry.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived statuses cannot be modified")
		return
	}

	var name pgtype.Text
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len([]rune(trimmed)) > 64 {
			writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
			return
		}
		name = pgtype.Text{String: trimmed, Valid: true}
	}
	var description pgtype.Text
	if req.Description != nil {
		if len([]rune(*req.Description)) > 256 {
			writeError(w, http.StatusBadRequest, "description must be at most 256 characters")
			return
		}
		description = pgtype.Text{String: *req.Description, Valid: true}
	}
	var color pgtype.Text
	if req.Color != nil {
		normalized, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		color = pgtype.Text{String: strings.ToLower(normalized), Valid: true}
	}
	var position pgtype.Float8
	if req.Position != nil {
		position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}

	updated, err := h.Queries.UpdateIssueStatusEntry(r.Context(), db.UpdateIssueStatusEntryParams{
		ID:          entry.ID,
		WorkspaceID: wsUUID,
		Name:        name,
		Description: description,
		Color:       color,
		Position:    position,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row moved out from under us (archived concurrently, or the
			// is_system guard in the statement rejected it).
			writeError(w, http.StatusConflict, "status is no longer editable")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a status with this name already exists")
			return
		}
		slog.Warn("UpdateIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue status")
		return
	}
	writeJSON(w, http.StatusOK, issueStatusToResponse(updated))
}

// ArchiveIssueStatus retires a custom status. It refuses while any issue still
// sits on the status: silently rewriting those issues would change their
// machine semantics with no audit trail, so the caller migrates them first.
func (h *Handler) ArchiveIssueStatus(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, ok := h.loadIssueStatusForAdmin(w, r)
	if !ok {
		return
	}

	if entry.IsSystem {
		writeError(w, http.StatusForbidden, "built-in statuses cannot be archived")
		return
	}
	if entry.ArchivedAt.Valid {
		writeJSON(w, http.StatusOK, issueStatusToResponse(entry))
		return
	}

	// The census and the archive run in ONE transaction under the EXCLUSIVE
	// catalog lock. Without it the two steps race: a concurrent issue write can
	// pass its active-status check, this handler can then observe zero usage and
	// archive, and the write lands afterwards — stranding an issue on an
	// archived status. Issue writes that target a custom status take the shared
	// side of this lock (runWithIssueStatusGuard), so they cannot interleave
	// with the census. (MUL-6243)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("ArchiveIssueStatus begin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockIssueStatusCatalog(r.Context(), wsUUID); err != nil {
		slog.Warn("ArchiveIssueStatus lock failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}

	inUse, err := qtx.CountIssuesUsingStatusKey(r.Context(), db.CountIssuesUsingStatusKeyParams{
		WorkspaceID: wsUUID,
		Key:         entry.Key,
	})
	if err != nil {
		slog.Warn("ArchiveIssueStatus count failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}
	if inUse > 0 {
		writeError(w, http.StatusConflict,
			"move the issues still using this status to another status before archiving it")
		return
	}

	archived, err := qtx.ArchiveIssueStatusEntry(r.Context(), db.ArchiveIssueStatusEntryParams{
		ID:          entry.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "status is no longer archivable")
			return
		}
		slog.Warn("ArchiveIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ArchiveIssueStatus commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}
	writeJSON(w, http.StatusOK, issueStatusToResponse(archived))
}

// loadIssueStatusForAdmin resolves the {id} path param inside the caller's
// workspace and enforces the owner/admin gate.
func (h *Handler) loadIssueStatusForAdmin(w http.ResponseWriter, r *http.Request) (db.IssueStatus, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.IssueStatus{}, pgtype.UUID{}, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return db.IssueStatus{}, pgtype.UUID{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue status id")
	if !ok {
		return db.IssueStatus{}, pgtype.UUID{}, false
	}
	entry, err := h.Queries.GetIssueStatusEntryByID(r.Context(), db.GetIssueStatusEntryByIDParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue status not found")
			return db.IssueStatus{}, pgtype.UUID{}, false
		}
		slog.Warn("load issue status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load issue status")
		return db.IssueStatus{}, pgtype.UUID{}, false
	}
	return entry, wsUUID, true
}
