package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	Icon        string  `json:"icon"`
	IsSystem    bool    `json:"is_system"`
	Position    float64 `json:"position"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// terminalIssueStatusKeys resolves the concrete status keys whose categories
// carry terminal behavior. Callers pass the result into indexed status
// predicates instead of resolving the category once per issue row.
func (h *Handler) terminalIssueStatusKeys(ctx context.Context, workspaceID pgtype.UUID) ([]string, error) {
	return issuestatus.ExpandCategories(ctx, h.Queries, workspaceID, []string{
		issuestatus.CategoryDone,
		issuestatus.CategoryClosed,
	})
}

func issueStatusToResponse(s db.IssueStatus) IssueStatusResponse {
	category := issuestatus.WireCategory(s.Key, s.Category)
	return IssueStatusResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Key:         s.Key,
		Name:        s.Name,
		Description: s.Description,
		Category:    category,
		Color:       s.Color,
		Icon:        s.Icon,
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
	Icon        string `json:"icon"`
}

// UpdateIssueStatusRequest deliberately has no Key or Category field. Both are
// immutable: changing a category would silently rewrite the machine semantics
// of every issue already on that status, and changing a key would strand them.
type UpdateIssueStatusRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Color       *string  `json:"color"`
	Icon        *string  `json:"icon"`
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
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
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
	category, validCategory := issuestatus.ParseCategory(req.Category)
	if !validCategory {
		writeError(w, http.StatusBadRequest, "category must be one of: "+strings.Join(issuestatus.Categories(), ", "))
		return
	}
	req.Category = category
	if !validIssueStatusIcon(req.Icon) {
		writeError(w, http.StatusBadRequest, "invalid status icon")
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// An explicit key wins and is checked here, against the reserved built-in
	// names and the storage pattern. Without one the key is DERIVED from the
	// display name, which needs the catalog and so happens under the lock in
	// createIssueStatusEntry.
	var explicitKey string
	if strings.TrimSpace(req.Key) != "" {
		explicitKey, err = issuestatus.ValidateKey(req.Key)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	entry, badRequest, err := h.createIssueStatusEntry(r.Context(), wsUUID, req.Category, db.CreateIssueStatusEntryParams{
		WorkspaceID: wsUUID,
		Key:         explicitKey,
		Name:        name,
		Description: req.Description,
		Category:    category,
		Color:       strings.ToLower(color),
		Icon:        req.Icon,
	})
	if badRequest != "" {
		writeError(w, http.StatusBadRequest, badRequest)
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a status with this key or name already exists")
			return
		}
		slog.Warn("CreateIssueStatus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue status")
		return
	}
	h.publishIssueStatusChanged(workspaceID, member, "created")
	writeJSON(w, http.StatusCreated, issueStatusToResponse(entry))
}

// createIssueStatusEntry writes one catalog row, deriving arg.Key from the
// display name when it arrives empty.
//
// Derivation READS the catalog to choose a key nothing already owns, so the
// read and the insert have to be a single atomic step: two admins creating a
// Chinese-named Started status at the same instant would otherwise both
// compute `started_2`, and the loser would be told a key they never typed was
// already taken. The EXCLUSIVE catalog lock — the same one archive takes —
// serializes them.
//
// EVERY create takes that lock, including one that supplies its own key.
// Excluding those would leave the race half-closed: an explicit-key insert of
// `started_2` could still land between a derive's catalog read and its
// insert, and the derive — a UI request with no key field to blame — would come
// back 409. The lock is only contended by catalog writes, which are rare admin
// actions, so serializing them costs nothing worth keeping the hole for.
//
// A non-empty second return is a caller error the handler reports as 400,
// distinct from a nil-error success and from an infrastructure failure.
func (h *Handler) createIssueStatusEntry(ctx context.Context, workspaceID pgtype.UUID, publicCategory string, arg db.CreateIssueStatusEntryParams) (db.IssueStatus, string, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.IssueStatus{}, "", err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockIssueStatusCatalog(ctx, workspaceID); err != nil {
		return db.IssueStatus{}, "", err
	}

	if arg.Key == "" {
		// IncludeArchived, because idx_issue_status_workspace_key is NOT a
		// partial index: a retired status still owns its key, so reusing it
		// would fail on insert instead of producing a second candidate.
		entries, err := qtx.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
			WorkspaceID:     workspaceID,
			IncludeArchived: true,
		})
		if err != nil {
			return db.IssueStatus{}, "", err
		}
		taken := make(map[string]bool, len(entries))
		for _, e := range entries {
			taken[e.Key] = true
		}
		key, err := issuestatus.DeriveKey(arg.Name, publicCategory, taken)
		if err != nil {
			return db.IssueStatus{}, err.Error(), nil
		}
		arg.Key = key
	}

	entry, err := qtx.CreateIssueStatusEntry(ctx, arg)
	if err != nil {
		return db.IssueStatus{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.IssueStatus{}, "", err
	}
	return entry, "", nil
}

// UpdateIssueStatus edits a custom status's presentation. Built-in statuses are
// immutable in v1 — name and color included — so the default workspace looks
// and behaves identically for everyone who never opens this settings page.
func (h *Handler) UpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, member, ok := h.loadIssueStatusForAdmin(w, r)
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
	var icon pgtype.Text
	if req.Icon != nil {
		if !validIssueStatusIcon(*req.Icon) {
			writeError(w, http.StatusBadRequest, "invalid status icon")
			return
		}
		icon = pgtype.Text{String: *req.Icon, Valid: true}
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
		Icon:        icon,
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
	h.publishIssueStatusChanged(uuidToString(wsUUID), member, "updated")
	writeJSON(w, http.StatusOK, issueStatusToResponse(updated))
}

// These identifiers describe geometry, not workflow semantics. Empty selects
// the category default. Unknown values are rejected on writes, not on reads.
func validIssueStatusIcon(icon string) bool {
	switch icon {
	case "", "dotted", "circle", "half", "three_quarters", "check", "slash", "cross":
		return true
	default:
		return false
	}
}

// ArchiveIssueStatus retires an empty custom status from future assignment.
func (h *Handler) ArchiveIssueStatus(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, member, ok := h.loadIssueStatusForAdmin(w, r)
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

	// Count and archive under the EXCLUSIVE catalog lock. Custom-status writers
	// re-resolve under its SHARED side, so no assignment can enter between the
	// empty check and the archive commit. Migration is a separate user action.
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
	count, err := qtx.CountIssuesUsingStatusKey(r.Context(), db.CountIssuesUsingStatusKeyParams{
		WorkspaceID: wsUUID,
		Key:         entry.Key,
	})
	if err != nil {
		slog.Warn("ArchiveIssueStatus count failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to check issues using status")
		return
	}
	if count > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       fmt.Sprintf("cannot archive status: %d issues still use it; move them to another status first", count),
			"code":        "issue_status_in_use",
			"issue_count": count,
		})
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
	// After the commit, never before: an event that announced a change the
	// transaction then rolled back would have every other tab re-read the
	// catalog and cache the pre-archive row as the new truth.
	h.publishIssueStatusChanged(uuidToString(wsUUID), member, "archived")
	writeJSON(w, http.StatusOK, issueStatusToResponse(archived))
}

// loadIssueStatusForAdmin resolves the {id} path param inside the caller's
// workspace and enforces the owner/admin gate. The member it resolved is
// returned so the write can name its actor on the realtime event.
func (h *Handler) loadIssueStatusForAdmin(w http.ResponseWriter, r *http.Request) (db.IssueStatus, pgtype.UUID, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.IssueStatus{}, pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return db.IssueStatus{}, pgtype.UUID{}, db.Member{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue status id")
	if !ok {
		return db.IssueStatus{}, pgtype.UUID{}, db.Member{}, false
	}
	entry, err := h.Queries.GetIssueStatusEntryByID(r.Context(), db.GetIssueStatusEntryByIDParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue status not found")
			return db.IssueStatus{}, pgtype.UUID{}, db.Member{}, false
		}
		slog.Warn("load issue status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load issue status")
		return db.IssueStatus{}, pgtype.UUID{}, db.Member{}, false
	}
	return entry, wsUUID, member, true
}

// publishIssueStatusChanged announces that the workspace catalog moved.
//
// One event for every write (see protocol.EventIssueStatusChanged): clients
// re-read the catalog, they do not merge the payload. Nothing about the
// changed row travels here on purpose — a client that merged an entry out of
// an event would have to reconcile it against a concurrent write it cannot
// see, and the catalog is a handful of rows to re-read.
func (h *Handler) publishIssueStatusChanged(workspaceID string, actor db.Member, action string) {
	h.publish(protocol.EventIssueStatusChanged, workspaceID, "member", uuidToString(actor.UserID), map[string]any{
		"action": action,
	})
}

// ReorderIssueStatusesRequest orders a category's active statuses. IncludeSystem
// opts into the complete set; omitted/false preserves the custom-only API used
// by installed clients. Partial sets are rejected in either mode.
type ReorderIssueStatusesRequest struct {
	Category      string   `json:"category"`
	IDs           []string `json:"ids"`
	IncludeSystem bool     `json:"include_system"`
}

// ReorderIssueStatuses rewrites the intra-category order of a category's
// statuses, atomically.
//
// Everything happens inside ONE transaction holding the catalog's EXCLUSIVE lock,
// which is the archive path's counterpart. That is not decoration:
//
//   - Validating outside the transaction leaves a window. "Validate A and B →
//     another request archives B → reorder runs" would write A's new position
//     and silently skip B (the UPDATE excludes archived rows), then report 200
//     on a half-applied order.
//   - The affected-row count is checked against the payload, so any row the
//     UPDATE declines to touch fails the whole request instead of committing a
//     prefix.
func (h *Handler) ReorderIssueStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req ReorderIssueStatusesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	category, validCategory := issuestatus.ParseCategory(req.Category)
	if !validCategory {
		writeError(w, http.StatusBadRequest, "category must be one of: "+strings.Join(issuestatus.Categories(), ", "))
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.IDs))
	seen := make(map[string]struct{}, len(req.IDs))
	for _, raw := range req.IDs {
		if _, duplicate := seen[raw]; duplicate {
			writeError(w, http.StatusBadRequest, "duplicate ids")
			return
		}
		seen[raw] = struct{}{}
		idUUID, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue status id")
			return
		}
		ids = append(ids, idUUID)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("ReorderIssueStatuses begin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Serialize reorders as well as create/archive, including legacy requests
	// that reuse custom rows' existing slots among movable built-ins.
	if err := qtx.LockIssueStatusCatalog(r.Context(), wsUUID); err != nil {
		slog.Warn("ReorderIssueStatuses lock failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		return
	}

	// Read the authoritative set under the lock, including built-ins only when
	// the caller opts in. No schema or response change is needed.
	catalog, err := qtx.ListIssueStatusEntries(r.Context(), db.ListIssueStatusEntriesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: false,
	})
	if err != nil {
		slog.Warn("ReorderIssueStatuses list failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		return
	}
	active := make([]db.IssueStatus, 0, len(catalog))
	for _, entry := range catalog {
		if entry.Category == category && (req.IncludeSystem || !entry.IsSystem) {
			active = append(active, entry)
		}
	}
	activeIDs := make(map[string]struct{}, len(active))
	for _, entry := range active {
		activeIDs[util.UUIDToString(entry.ID)] = struct{}{}
	}
	for i, raw := range req.IDs {
		if _, isActive := activeIDs[raw]; isActive {
			continue
		}
		// Still inside the lock, so this diagnosis cannot go stale. Reported
		// per-reason rather than as one opaque conflict, because "you sent a
		// built-in" and "someone archived it while you dragged" are different
		// problems for the caller.
		entry, err := qtx.GetIssueStatusEntryByID(r.Context(), db.GetIssueStatusEntryByIDParams{
			ID:          ids[i],
			WorkspaceID: wsUUID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "issue status not found")
		case err != nil:
			slog.Warn("load issue status for reorder failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		case entry.IsSystem && !req.IncludeSystem:
			writeError(w, http.StatusForbidden, "include_system is required to reorder built-in statuses")
		case entry.ArchivedAt.Valid:
			writeError(w, http.StatusConflict, "archived statuses cannot be reordered")
		case entry.Category != category:
			writeError(w, http.StatusBadRequest, "ids must all belong to the requested category")
		default:
			writeError(w, http.StatusConflict, "issue status catalog changed during reorder")
		}
		return
	}
	// Every id is active — so a length mismatch means the payload LEFT ONE OUT.
	// Applying it would assign positions from the array index and collide with
	// the omitted row, so the whole request is refused.
	if len(active) != len(ids) {
		writeError(w, http.StatusConflict, "ids must name every active status in the requested reorder scope")
		return
	}

	positions := make([]float64, len(ids))
	for i := range ids {
		if req.IncludeSystem {
			positions[i] = float64(i + 1)
		} else {
			// Old clients reorder only the custom slots, without moving built-ins
			// or colliding with their newly configurable positions.
			positions[i] = active[i].Position
		}
	}
	affected, err := qtx.ReorderIssueStatusEntries(r.Context(), db.ReorderIssueStatusEntriesParams{
		Ids:           ids,
		WorkspaceID:   wsUUID,
		Positions:     positions,
		IncludeSystem: req.IncludeSystem,
	})
	if err != nil {
		slog.Warn("ReorderIssueStatuses failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		return
	}
	if affected != int64(len(ids)) {
		// Belt and braces behind the lock: if the UPDATE declined a row anyway,
		// roll the whole order back rather than commit a prefix.
		slog.Warn("ReorderIssueStatuses touched an unexpected row count",
			append(logger.RequestAttrs(r), "affected", affected, "expected", len(ids))...)
		writeError(w, http.StatusConflict, "issue status catalog changed during reorder")
		return
	}

	entries, err := qtx.ListIssueStatusEntries(r.Context(), db.ListIssueStatusEntriesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: true,
	})
	if err != nil {
		slog.Warn("list issue statuses after reorder failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue statuses")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ReorderIssueStatuses commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue statuses")
		return
	}
	h.publishIssueStatusChanged(workspaceID, member, "reordered")

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
