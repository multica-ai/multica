package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Custom issue status management (MUL-4809, plan §5). A per-workspace catalog of
// statuses, each pinned to one of the 5 immutable Categories (the only machine
// semantics). The 7 built-ins are seeded per workspace; admins add custom
// statuses and rename / recolor / reorder any of them, but Category and
// system_key are immutable and system statuses cannot be archived.
//
// Contract highlights (plan §5):
//   - Definitions are managed by human owner/admin members only — agents change
//     an issue's status but never the catalog (mirrors custom properties).
//   - Category and system_key are immutable: PATCHing them returns 400
//     immutable_field rather than silently ignoring. To change a status's
//     Category you create a new status and migrate issues explicitly.
//   - The DB guarantees at most one default per Category (partial unique index);
//     this layer maintains at least one by clearing-then-setting in one tx.
//   - Archive (soft delete) requires a same-Category migration target when the
//     status is still in use, and refuses to strand a Category with no default.
const (
	maxIssueStatusNameLen                    = 32
	maxIssueStatusDescriptionLen             = 500
	maxActiveCustomIssueStatusesPerWorkspace = 24
)

// validIssueStatusColors is the allowlist of semantic color tokens a status may
// carry. These match the tokens the built-in statuses use and that the client
// theme already renders (STATUS_CONFIG in packages/core); keeping the allowlist
// at the API boundary stops arbitrary values from leaking into every surface
// that colors a status.
var validIssueStatusColors = map[string]struct{}{
	"muted-foreground": {}, "warning": {}, "success": {}, "info": {}, "destructive": {},
}

// validIssueStatusIcons is the allowlist of icon keys a status may carry. They
// are the built-in status-shape glyphs the client renders (StatusIcon in
// packages/views); a custom status reuses whichever shape best fits its
// Category. icon is human-facing only and never affects machine semantics.
var validIssueStatusIcons = map[string]struct{}{
	"backlog": {}, "todo": {}, "in_progress": {}, "in_review": {},
	"blocked": {}, "done": {}, "cancelled": {},
}

func issueStatusColorList() string {
	return "muted-foreground, warning, success, info, destructive"
}

func issueStatusIconList() string {
	return "backlog, todo, in_progress, in_review, blocked, done, cancelled"
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type IssueStatusResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Color       string  `json:"color"`
	Category    string  `json:"category"`
	SystemKey   *string `json:"system_key"`
	IsSystem    bool    `json:"is_system"`
	IsDefault   bool    `json:"is_default"`
	Position    float64 `json:"position"`
	Archived    bool    `json:"archived"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// IssueStatusCatalogResponse is what the catalog endpoint returns: the ordered
// statuses plus the alias resolution table an agent/CLI reads before calling
// `issue status` (plan §3.2). category_defaults maps each Category to its
// current default status id; aliases maps every alias token (5 Category + 2
// legacy) to the status id it resolves to today, so a rename never leaves a
// caller guessing.
type IssueStatusCatalogResponse struct {
	Statuses         []IssueStatusResponse `json:"statuses"`
	CategoryDefaults map[string]string     `json:"category_defaults"`
	Aliases          map[string]string     `json:"aliases"`
	Total            int                   `json:"total"`
}

type CreateIssueStatusRequest struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	IsDefault   bool   `json:"is_default"`
}

type UpdateIssueStatusRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Icon        *string  `json:"icon"`
	Color       *string  `json:"color"`
	Position    *float64 `json:"position"`
	IsDefault   *bool    `json:"is_default"`
	// Immutable fields. They are decoded only to reject the request loudly if a
	// caller sends them (plan §5.3), never applied.
	Category    *string `json:"category"`
	SystemKey   *string `json:"system_key"`
	WorkspaceID *string `json:"workspace_id"`
}

func issueStatusToResponse(s db.IssueStatus) IssueStatusResponse {
	resp := IssueStatusResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Name:        s.Name,
		Description: s.Description,
		Icon:        s.Icon,
		Color:       s.Color,
		Category:    s.Category,
		IsSystem:    s.SystemKey.Valid,
		IsDefault:   s.IsDefault,
		Position:    s.Position,
		Archived:    s.ArchivedAt.Valid,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
	if s.SystemKey.Valid {
		key := s.SystemKey.String
		resp.SystemKey = &key
	}
	if s.ArchivedAt.Valid {
		at := timestampToString(s.ArchivedAt)
		resp.ArchivedAt = &at
	}
	return resp
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// validateIssueStatusName trims and validates a display name. Beyond length and
// control-char rules it rejects the 7 reserved alias tokens: the alias resolver
// claims those first, so a status named "todo" or "in_review" could never be
// targeted by its own name (plan §3.1).
func validateIssueStatusName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain tabs, newlines, or control characters")
		}
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxIssueStatusNameLen {
		return "", fmt.Errorf("name must be %d characters or fewer", maxIssueStatusNameLen)
	}
	if issuestatus.IsReservedStatusToken(name) {
		return "", fmt.Errorf("%q is a reserved status alias and cannot be a status name", name)
	}
	return name, nil
}

func validateIssueStatusCategory(raw string) (string, error) {
	c := strings.TrimSpace(raw)
	for _, valid := range issuestatus.Categories {
		if c == valid {
			return c, nil
		}
	}
	return "", fmt.Errorf("category must be one of: %s", strings.Join(issuestatus.Categories, ", "))
}

func validateIssueStatusColor(raw string) (string, error) {
	c := strings.TrimSpace(raw)
	if _, ok := validIssueStatusColors[c]; !ok {
		return "", fmt.Errorf("color must be one of: %s", issueStatusColorList())
	}
	return c, nil
}

func validateIssueStatusIcon(raw string) (string, error) {
	icon := strings.TrimSpace(raw)
	if _, ok := validIssueStatusIcons[icon]; !ok {
		return "", fmt.Errorf("icon must be one of: %s", issueStatusIconList())
	}
	return icon, nil
}

func validateIssueStatusDescription(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > maxIssueStatusDescriptionLen {
		return "", fmt.Errorf("description must be %d characters or fewer", maxIssueStatusDescriptionLen)
	}
	return sanitizeNullBytes(strings.TrimSpace(raw)), nil
}

// ---------------------------------------------------------------------------
// Admin gate
// ---------------------------------------------------------------------------

// requireIssueStatusAdmin gates catalog writes: human owner/admin members only.
// Agent actors are rejected before the role check (mirror of
// requirePropertyAdmin) — an agent inherits its runtime owner's credentials, so
// without this an admin's agent could reshape the status catalog. Agents change
// an issue's status; they do not manage the catalog.
func (h *Handler) requireIssueStatusAdmin(w http.ResponseWriter, r *http.Request) (workspaceID, userID string, ok bool) {
	workspaceID = h.resolveWorkspaceID(r)
	userID, ok = requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage issue statuses")
		return "", "", false
	}
	if _, roleOK := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !roleOK {
		return "", "", false
	}
	return workspaceID, userID, true
}

// withIssueStatusLock runs fn inside a transaction holding a workspace-scoped
// advisory lock, serializing catalog writes for the workspace: the active-count
// cap, name-uniqueness, and the clear-then-set default swap are all
// read-then-write and must not interleave. The lock is transaction-scoped.
func (h *Handler) withIssueStatusLock(r *http.Request, workspaceID string, fn func(q *db.Queries) error) error {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "issuestatus:"+workspaceID); err != nil {
		return err
	}
	if err := fn(h.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(r.Context())
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ListIssueStatuses (GET /api/issue-statuses) returns the workspace catalog plus
// the alias resolution table. Readable by any workspace member and by agents —
// the alias table is exactly what an agent reads before calling `issue status`.
func (h *Handler) ListIssueStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	statuses, err := h.Queries.ListWorkspaceIssueStatuses(r.Context(), db.ListWorkspaceIssueStatusesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		slog.Warn("ListWorkspaceIssueStatuses failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue statuses")
		return
	}

	resp := IssueStatusCatalogResponse{
		Statuses:         make([]IssueStatusResponse, len(statuses)),
		CategoryDefaults: make(map[string]string),
		Aliases:          make(map[string]string),
		Total:            len(statuses),
	}
	for i, s := range statuses {
		resp.Statuses[i] = issueStatusToResponse(s)
		if s.ArchivedAt.Valid {
			continue
		}
		id := uuidToString(s.ID)
		// Category alias -> current default; also the per-Category default map.
		if s.IsDefault {
			resp.CategoryDefaults[s.Category] = id
			resp.Aliases[s.Category] = id
		}
		// Legacy aliases key on the immutable system_key, so they survive renames.
		if s.SystemKey.Valid {
			switch s.SystemKey.String {
			case "in_review", "blocked":
				resp.Aliases[s.SystemKey.String] = id
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireIssueStatusAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req CreateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := validateIssueStatusName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	category, err := validateIssueStatusCategory(req.Category)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	icon, err := validateIssueStatusIcon(req.Icon)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	color, err := validateIssueStatusColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	description, err := validateIssueStatusDescription(req.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var created db.IssueStatus
	var httpStatus int
	var httpMsg string
	fail := func(status int, msg string) error {
		httpStatus, httpMsg = status, msg
		return errClientRejected
	}
	err = h.withIssueStatusLock(r, workspaceID, func(q *db.Queries) error {
		active, err := q.CountActiveCustomIssueStatuses(r.Context(), wsUUID)
		if err != nil {
			return err
		}
		if active >= maxActiveCustomIssueStatusesPerWorkspace {
			return fail(http.StatusBadRequest, fmt.Sprintf("a workspace cannot have more than %d custom statuses; archive unused ones first", maxActiveCustomIssueStatusesPerWorkspace))
		}
		// Promoting to default must clear the Category's current default first,
		// or the (workspace_id, category) partial unique index rejects the insert.
		if req.IsDefault {
			if err := q.ClearCategoryDefault(r.Context(), db.ClearCategoryDefaultParams{WorkspaceID: wsUUID, Category: category}); err != nil {
				return err
			}
		}
		created, err = q.CreateCustomIssueStatus(r.Context(), db.CreateCustomIssueStatusParams{
			WorkspaceID: wsUUID,
			Name:        name,
			Description: description,
			Icon:        icon,
			Color:       color,
			Category:    category,
			IsDefault:   req.IsDefault,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, errClientRejected) {
			writeError(w, httpStatus, httpMsg)
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a status with that name already exists")
			return
		}
		slog.Warn("CreateCustomIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue status")
		return
	}
	resp := issueStatusToResponse(created)
	h.publish(protocol.EventIssueStatusCreated, workspaceID, "member", userID, map[string]any{"status": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireIssueStatusAdmin(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "status id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req UpdateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Immutable fields are rejected loudly, not silently ignored (plan §5.3).
	if req.Category != nil || req.SystemKey != nil || req.WorkspaceID != nil {
		writeError(w, http.StatusBadRequest, "immutable_field: category, system_key, and workspace_id cannot be changed; create a new status and migrate issues instead")
		return
	}

	var updated db.IssueStatus
	var httpStatus int
	var httpMsg string
	fail := func(status int, msg string) error {
		httpStatus, httpMsg = status, msg
		return errClientRejected
	}
	err := h.withIssueStatusLock(r, workspaceID, func(q *db.Queries) error {
		existing, err := q.GetWorkspaceIssueStatus(r.Context(), db.GetWorkspaceIssueStatusParams{ID: idUUID, WorkspaceID: wsUUID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "status not found")
			}
			return err
		}

		params := db.UpdateIssueStatusFieldsParams{ID: idUUID, WorkspaceID: wsUUID}
		if req.Name != nil {
			name, err := validateIssueStatusName(*req.Name)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Name = pgtype.Text{String: name, Valid: true}
		}
		if req.Description != nil {
			description, err := validateIssueStatusDescription(*req.Description)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Description = pgtype.Text{String: description, Valid: true}
		}
		if req.Icon != nil {
			icon, err := validateIssueStatusIcon(*req.Icon)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Icon = pgtype.Text{String: icon, Valid: true}
		}
		if req.Color != nil {
			color, err := validateIssueStatusColor(*req.Color)
			if err != nil {
				return fail(http.StatusBadRequest, err.Error())
			}
			params.Color = pgtype.Text{String: color, Valid: true}
		}
		if req.Position != nil {
			if math.IsNaN(*req.Position) || math.IsInf(*req.Position, 0) {
				return fail(http.StatusBadRequest, "position must be a finite number")
			}
			params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
		}

		updated, err = q.UpdateIssueStatusFields(r.Context(), params)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "status not found")
			}
			if isUniqueViolation(err) {
				return fail(http.StatusConflict, "a status with that name already exists")
			}
			return err
		}

		// Default swap, if requested, runs after the field update but in the
		// same tx. Only promotion (true) is meaningful: the DB holds "at most
		// one" default per Category, and this layer refuses to strand a
		// Category with zero defaults, so demoting the current default is done
		// by promoting another status, not by clearing this one.
		if req.IsDefault != nil {
			switch {
			case *req.IsDefault:
				if existing.ArchivedAt.Valid {
					return fail(http.StatusBadRequest, "an archived status cannot be made the default")
				}
				if !updated.IsDefault {
					if err := q.ClearCategoryDefault(r.Context(), db.ClearCategoryDefaultParams{WorkspaceID: wsUUID, Category: existing.Category}); err != nil {
						return err
					}
					updated, err = q.SetIssueStatusDefault(r.Context(), db.SetIssueStatusDefaultParams{ID: idUUID, WorkspaceID: wsUUID, IsDefault: true})
					if err != nil {
						return err
					}
				}
			default:
				if updated.IsDefault {
					return fail(http.StatusBadRequest, "cannot unset the default of a category; promote another status to default instead")
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errClientRejected) {
			writeError(w, httpStatus, httpMsg)
			return
		}
		slog.Warn("UpdateIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue status")
		return
	}
	resp := issueStatusToResponse(updated)
	h.publish(protocol.EventIssueStatusUpdated, workspaceID, "member", userID, map[string]any{"status": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteIssueStatus archives (soft-deletes) a custom status. System statuses
// cannot be archived. If issues still point at it, the caller must pass a
// same-Category migrate_to_status_id and the issues move over in the same tx.
// A default status cannot be archived until another status is promoted.
func (h *Handler) DeleteIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireIssueStatusAdmin(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "status id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	migrateTo := strings.TrimSpace(r.URL.Query().Get("migrate_to_status_id"))

	var httpStatus int
	var httpMsg string
	fail := func(status int, msg string) error {
		httpStatus, httpMsg = status, msg
		return errClientRejected
	}
	err := h.withIssueStatusLock(r, workspaceID, func(q *db.Queries) error {
		existing, err := q.GetWorkspaceIssueStatus(r.Context(), db.GetWorkspaceIssueStatusParams{ID: idUUID, WorkspaceID: wsUUID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(http.StatusNotFound, "status not found")
			}
			return err
		}
		if existing.ArchivedAt.Valid {
			return fail(http.StatusBadRequest, "status is already archived")
		}
		if existing.SystemKey.Valid {
			return fail(http.StatusBadRequest, "built-in statuses cannot be archived")
		}
		if existing.IsDefault {
			return fail(http.StatusBadRequest, "promote another status to this category's default before archiving it")
		}

		inUse, err := q.CountIssuesUsingStatus(r.Context(), db.CountIssuesUsingStatusParams{WorkspaceID: wsUUID, StatusID: idUUID})
		if err != nil {
			return err
		}
		if inUse > 0 {
			if migrateTo == "" {
				return fail(http.StatusConflict, fmt.Sprintf("status is used by %d issue(s); pass migrate_to_status_id to move them to another status in the same category first", inUse))
			}
			targetUUID, perr := util.ParseUUID(migrateTo)
			if perr != nil {
				return fail(http.StatusBadRequest, "migrate_to_status_id must be a status id")
			}
			if targetUUID == idUUID {
				return fail(http.StatusBadRequest, "migrate_to_status_id cannot be the status being archived")
			}
			target, err := q.GetWorkspaceIssueStatus(r.Context(), db.GetWorkspaceIssueStatusParams{ID: targetUUID, WorkspaceID: wsUUID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fail(http.StatusBadRequest, "migrate_to_status_id does not name a status in this workspace")
				}
				return err
			}
			if target.ArchivedAt.Valid {
				return fail(http.StatusBadRequest, "migrate_to_status_id cannot be an archived status")
			}
			if target.Category != existing.Category {
				return fail(http.StatusBadRequest, "migrate_to_status_id must be in the same category; changing category must be an explicit per-issue transition")
			}
			if err := q.ReassignIssuesStatus(r.Context(), db.ReassignIssuesStatusParams{
				WorkspaceID:  wsUUID,
				FromStatusID: idUUID,
				ToStatusID:   targetUUID,
			}); err != nil {
				return err
			}
		}

		_, err = q.ArchiveIssueStatus(r.Context(), db.ArchiveIssueStatusParams{ID: idUUID, WorkspaceID: wsUUID})
		return err
	})
	if err != nil {
		if errors.Is(err, errClientRejected) {
			writeError(w, httpStatus, httpMsg)
			return
		}
		slog.Warn("DeleteIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue status")
		return
	}
	h.publish(protocol.EventIssueStatusUpdated, workspaceID, "member", userID, map[string]any{"status_id": uuidToString(idUUID), "archived": true})
	writeJSON(w, http.StatusOK, map[string]any{"archived": true})
}
