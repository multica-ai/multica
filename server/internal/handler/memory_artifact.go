package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// memory_artifact handlers — workspace-scoped, kind-discriminated
// markdown artifacts that humans curate and agents append to. See
// migrations/068_memory_artifact.up.sql for the design rationale.
//
// Routing convention:
//   GET    /api/memory                          ListMemoryArtifacts
//   POST   /api/memory                          CreateMemoryArtifact
//   GET    /api/memory/search?q=...             SearchMemoryArtifacts
//   GET    /api/memory/by-anchor/:type/:id      ListByAnchor
//   GET    /api/memory/:id                      GetMemoryArtifact
//   PUT    /api/memory/:id                      UpdateMemoryArtifact
//   POST   /api/memory/:id/archive              ArchiveMemoryArtifact
//   POST   /api/memory/:id/restore              RestoreMemoryArtifact
//   DELETE /api/memory/:id                      DeleteMemoryArtifact

// ---------------------------------------------------------------------------
// Constants & validation
// ---------------------------------------------------------------------------

const (
	maxMemoryTitleLen   = 500
	maxMemoryContentLen = 100 * 1024 // 100 KB — same cap PR #2084 used for wiki_page; survives this PR's reframing.
	maxMemoryListLimit  = 200
	defaultMemoryLimit  = 50
)

// allowedMemoryKinds enumerates the kinds the API accepts on create. The
// SQL column is open-string (no CHECK constraint) so adding a new kind
// is a single line here, no migration. Listed kinds:
//
//   - "wiki_page" — workspace-curated knowledge page (parity with PR #2084)
//   - "agent_note" — agent-authored finding / decision / dead-end
//   - "runbook" — operational procedure
//   - "decision" — architectural decision record
var allowedMemoryKinds = map[string]bool{
	"wiki_page":  true,
	"agent_note": true,
	"runbook":    true,
	"decision":   true,
}

// allowedAnchorTypes enumerates the entity types a memory artifact can
// be anchored to. The set mirrors what the daemon's runtime context
// injection would actually fetch — there's no point allowing an anchor
// the runtime can't resolve.
var allowedAnchorTypes = map[string]bool{
	"issue":   true,
	"project": true,
	"agent":   true,
	"channel": true,
}

// memorySlugRE matches valid slugs: lowercase alphanumeric and hyphens,
// no leading/trailing hyphen, no consecutive hyphens. Same shape as
// workspace and channel slugs elsewhere in the codebase.
var memorySlugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// validateMemorySlug trims and validates. Empty input → ("", nil) which
// the handler maps to "no slug." Non-empty input must match memorySlugRE.
func validateMemorySlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if !memorySlugRE.MatchString(s) {
		return "", errors.New("slug must contain only lowercase letters, numbers, and hyphens")
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type MemoryArtifactResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Kind        string          `json:"kind"`
	ParentID    *string         `json:"parent_id"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	Slug        *string         `json:"slug"`
	AnchorType  *string         `json:"anchor_type"`
	AnchorID    *string         `json:"anchor_id"`
	AuthorType  string          `json:"author_type"`
	AuthorID    string          `json:"author_id"`
	Tags        []string        `json:"tags"`
	Metadata    json.RawMessage `json:"metadata"`
	ArchivedAt  *string         `json:"archived_at"`
	ArchivedBy  *string         `json:"archived_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func memoryArtifactToResponse(a db.MemoryArtifact) MemoryArtifactResponse {
	tags := a.Tags
	if tags == nil {
		tags = []string{}
	}
	metadata := json.RawMessage(a.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	return MemoryArtifactResponse{
		ID:          uuidToString(a.ID),
		WorkspaceID: uuidToString(a.WorkspaceID),
		Kind:        a.Kind,
		ParentID:    uuidToPtr(a.ParentID),
		Title:       a.Title,
		Content:     a.Content,
		Slug:        textToPtr(a.Slug),
		AnchorType:  textToPtr(a.AnchorType),
		AnchorID:    uuidToPtr(a.AnchorID),
		AuthorType:  a.AuthorType,
		AuthorID:    uuidToString(a.AuthorID),
		Tags:        tags,
		Metadata:    metadata,
		ArchivedAt:  timestampToPtr(a.ArchivedAt),
		ArchivedBy:  uuidToPtr(a.ArchivedBy),
		CreatedAt:   timestampToString(a.CreatedAt),
		UpdatedAt:   timestampToString(a.UpdatedAt),
	}
}

type CreateMemoryArtifactRequest struct {
	Kind       string          `json:"kind"`
	ParentID   *string         `json:"parent_id"`
	Title      string          `json:"title"`
	Content    string          `json:"content"`
	Slug       *string         `json:"slug"`
	AnchorType *string         `json:"anchor_type"`
	AnchorID   *string         `json:"anchor_id"`
	Tags       []string        `json:"tags"`
	Metadata   json.RawMessage `json:"metadata"`
}

type UpdateMemoryArtifactRequest struct {
	// All fields nullable so PATCH-style partial updates work without
	// callers re-sending the full document.
	Title      *string         `json:"title"`
	Content    *string         `json:"content"`
	Slug       *string         `json:"slug"`
	ParentID   *string         `json:"parent_id"`
	AnchorType *string         `json:"anchor_type"`
	AnchorID   *string         `json:"anchor_id"`
	Tags       *[]string       `json:"tags"`
	Metadata   json.RawMessage `json:"metadata"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseListPagination(r *http.Request) (limit, offset int32) {
	limit = defaultMemoryLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxMemoryListLimit {
				n = maxMemoryListLimit
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return
}

// validateAnchor checks the type/id pair. Either both must be non-nil
// (real anchor) or both nil (free-floating). Mismatched is a 400.
func validateAnchor(w http.ResponseWriter, anchorType, anchorID *string) (pgtype.Text, pgtype.UUID, bool) {
	if anchorType == nil && anchorID == nil {
		return pgtype.Text{}, pgtype.UUID{}, true
	}
	if (anchorType == nil) != (anchorID == nil) {
		writeError(w, http.StatusBadRequest, "anchor_type and anchor_id must be provided together")
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	t := strings.TrimSpace(*anchorType)
	if !allowedAnchorTypes[t] {
		writeError(w, http.StatusBadRequest, "anchor_type must be one of: issue, project, agent, channel")
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, *anchorID, "anchor_id")
	if !ok {
		return pgtype.Text{}, pgtype.UUID{}, false
	}
	return pgtype.Text{String: t, Valid: true}, id, true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *Handler) ListMemoryArtifacts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var kindFilter pgtype.Text
	if k := r.URL.Query().Get("kind"); k != "" {
		kindFilter = pgtype.Text{String: k, Valid: true}
	}
	var parentFilter pgtype.UUID
	if p := r.URL.Query().Get("parent_id"); p != "" {
		id, ok := parseUUIDOrBadRequest(w, p, "parent_id")
		if !ok {
			return
		}
		parentFilter = id
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	limit, offset := parseListPagination(r)

	rows, err := h.Queries.ListMemoryArtifacts(r.Context(), db.ListMemoryArtifactsParams{
		WorkspaceID:     wsUUID,
		Kind:            kindFilter,
		ParentID:        parentFilter,
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		slog.Warn("ListMemoryArtifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list memory artifacts")
		return
	}
	total, err := h.Queries.CountMemoryArtifacts(r.Context(), db.CountMemoryArtifactsParams{
		WorkspaceID:     wsUUID,
		Kind:            kindFilter,
		ParentID:        parentFilter,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		// Best-effort — still return the page even if the count query
		// errors. The page is the truth; total is convenience.
		total = int64(len(rows))
	}

	out := make([]MemoryArtifactResponse, len(rows))
	for i, p := range rows {
		out[i] = memoryArtifactToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_artifacts": out,
		"total":            total,
	})
}

func (h *Handler) GetMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "memory artifact id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	row, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory artifact not found")
		return
	}
	writeJSON(w, http.StatusOK, memoryArtifactToResponse(row))
}

// ListMemoryArtifactsByAnchor powers "show me everything anchored to
// issue X" lookups — used by the daemon's runtime context injection
// (a follow-up PR will hydrate anchored notes into CLAUDE.md when an
// agent claims a task).
func (h *Handler) ListMemoryArtifactsByAnchor(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	anchorType := strings.TrimSpace(chi.URLParam(r, "anchorType"))
	if !allowedAnchorTypes[anchorType] {
		writeError(w, http.StatusBadRequest, "anchor type must be one of: issue, project, agent, channel")
		return
	}
	anchorID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "anchorId"), "anchor id")
	if !ok {
		return
	}
	// Default 50, cap 200 — same as ListMemoryArtifacts. Anchor lookup
	// is the daemon's hot path, so worth a generous default.
	limit := int32(defaultMemoryLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxMemoryListLimit {
				n = maxMemoryListLimit
			}
			limit = int32(n)
		}
	}
	rows, err := h.Queries.ListMemoryArtifactsByAnchor(r.Context(), db.ListMemoryArtifactsByAnchorParams{
		WorkspaceID: wsUUID,
		AnchorType:  pgtype.Text{String: anchorType, Valid: true},
		AnchorID:    anchorID,
		Limit:       limit,
	})
	if err != nil {
		slog.Warn("ListMemoryArtifactsByAnchor failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list memory artifacts")
		return
	}
	out := make([]MemoryArtifactResponse, len(rows))
	for i, p := range rows {
		out[i] = memoryArtifactToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_artifacts": out,
		"total":            len(out),
	})
}

func (h *Handler) SearchMemoryArtifacts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "query parameter q is required")
		return
	}
	var kindFilter pgtype.Text
	if k := r.URL.Query().Get("kind"); k != "" {
		kindFilter = pgtype.Text{String: k, Valid: true}
	}
	limit, offset := parseListPagination(r)
	rows, err := h.Queries.SearchMemoryArtifacts(r.Context(), db.SearchMemoryArtifactsParams{
		WorkspaceID:         wsUUID,
		WebsearchToTsquery:  q,
		Kind:                kindFilter,
		Limit:               limit,
		Offset:              offset,
	})
	if err != nil {
		slog.Warn("SearchMemoryArtifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	// SearchMemoryArtifactsRow includes a `Rank` field; the API surface
	// keeps the basic artifact response and tucks rank into a parallel
	// array if/when the UI wants it. For now just collapse to artifacts.
	out := make([]MemoryArtifactResponse, len(rows))
	for i, p := range rows {
		// Convert SearchMemoryArtifactsRow → MemoryArtifact by copying
		// the embedded fields. sqlc generates a row struct flattened.
		out[i] = memoryArtifactToResponse(db.MemoryArtifact{
			ID:          p.ID,
			WorkspaceID: p.WorkspaceID,
			Kind:        p.Kind,
			ParentID:    p.ParentID,
			Title:       p.Title,
			Content:     p.Content,
			Slug:        p.Slug,
			AnchorType:  p.AnchorType,
			AnchorID:    p.AnchorID,
			AuthorType:  p.AuthorType,
			AuthorID:    p.AuthorID,
			Tags:        p.Tags,
			Metadata:    p.Metadata,
			ContentTsv:  p.ContentTsv,
			ArchivedAt:  p.ArchivedAt,
			ArchivedBy:  p.ArchivedBy,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_artifacts": out,
		"total":            len(out),
	})
}

func (h *Handler) CreateMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateMemoryArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Kind: required, enumerated.
	req.Kind = strings.TrimSpace(req.Kind)
	if !allowedMemoryKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "kind must be one of: wiki_page, agent_note, runbook, decision")
		return
	}

	// Title: required, max length.
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.Title) > maxMemoryTitleLen {
		writeError(w, http.StatusBadRequest, "title is too long")
		return
	}
	if len(req.Content) > maxMemoryContentLen {
		writeError(w, http.StatusBadRequest, "content is too long (max 100 KB)")
		return
	}

	// Slug: optional, must be URL-safe when present.
	var slugParam pgtype.Text
	if req.Slug != nil {
		s, err := validateMemorySlug(*req.Slug)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if s != "" {
			slugParam = pgtype.Text{String: s, Valid: true}
		}
	}

	// Parent: optional, must already exist in the same workspace.
	var parentParam pgtype.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		parentUUID, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
			ID: parentUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "parent_id must reference a memory artifact in this workspace")
			return
		}
		parentParam = parentUUID
	}

	// Anchor: type + id together or both nil.
	anchorTypeParam, anchorIDParam, ok := validateAnchor(w, req.AnchorType, req.AnchorID)
	if !ok {
		return
	}

	// Author: agent (via X-Agent-ID header) or member.
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	authorUUID := parseUUID(authorID)

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	metadata := req.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	created, err := h.Queries.CreateMemoryArtifact(r.Context(), db.CreateMemoryArtifactParams{
		WorkspaceID: wsUUID,
		Kind:        req.Kind,
		ParentID:    parentParam,
		Title:       req.Title,
		Content:     req.Content,
		Slug:        slugParam,
		AnchorType:  anchorTypeParam,
		AnchorID:    anchorIDParam,
		AuthorType:  authorType,
		AuthorID:    authorUUID,
		Tags:        tags,
		Metadata:    metadata,
	})
	if err != nil {
		// Slug uniqueness collision.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug already in use for this kind in this workspace")
			return
		}
		slog.Warn("CreateMemoryArtifact failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create memory artifact")
		return
	}

	resp := memoryArtifactToResponse(created)
	h.publish(protocol.EventMemoryArtifactCreated, workspaceID, authorType, authorID, map[string]any{
		"memory_artifact": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "memory artifact id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify the artifact exists in this workspace before any edits.
	existing, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory artifact not found")
		return
	}
	_ = existing

	var req UpdateMemoryArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateMemoryArtifactParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}

	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		if len(t) > maxMemoryTitleLen {
			writeError(w, http.StatusBadRequest, "title is too long")
			return
		}
		params.Title = pgtype.Text{String: t, Valid: true}
	}
	if req.Content != nil {
		if len(*req.Content) > maxMemoryContentLen {
			writeError(w, http.StatusBadRequest, "content is too long (max 100 KB)")
			return
		}
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
	}
	if req.Slug != nil {
		s, err := validateMemorySlug(*req.Slug)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if s != "" {
			params.Slug = pgtype.Text{String: s, Valid: true}
		}
	}
	if req.ParentID != nil {
		if *req.ParentID != "" {
			parentUUID, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
			if !ok {
				return
			}
			if uuidToString(parentUUID) == id {
				writeError(w, http.StatusBadRequest, "parent_id cannot reference self")
				return
			}
			params.ParentID = parentUUID
		}
	}
	if req.AnchorType != nil || req.AnchorID != nil {
		at, aid, ok := validateAnchor(w, req.AnchorType, req.AnchorID)
		if !ok {
			return
		}
		params.AnchorType = at
		params.AnchorID = aid
	}
	if req.Tags != nil {
		params.Tags = *req.Tags
	}
	if len(req.Metadata) > 0 {
		params.Metadata = req.Metadata
	}

	updated, err := h.Queries.UpdateMemoryArtifact(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug already in use for this kind in this workspace")
			return
		}
		slog.Warn("UpdateMemoryArtifact failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update memory artifact")
		return
	}

	resp := memoryArtifactToResponse(updated)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventMemoryArtifactUpdated, workspaceID, authorType, authorID, map[string]any{
		"memory_artifact": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "memory artifact id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	existing, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory artifact not found")
		return
	}
	if existing.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "memory artifact is already archived")
		return
	}
	archived, err := h.Queries.ArchiveMemoryArtifact(r.Context(), db.ArchiveMemoryArtifactParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
		ArchivedBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive memory artifact")
		return
	}
	resp := memoryArtifactToResponse(archived)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventMemoryArtifactUpdated, workspaceID, authorType, authorID, map[string]any{
		"memory_artifact": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "memory artifact id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	existing, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory artifact not found")
		return
	}
	if !existing.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "memory artifact is not archived")
		return
	}
	restored, err := h.Queries.RestoreMemoryArtifact(r.Context(), db.RestoreMemoryArtifactParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore memory artifact")
		return
	}
	resp := memoryArtifactToResponse(restored)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventMemoryArtifactUpdated, workspaceID, authorType, authorID, map[string]any{
		"memory_artifact": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteMemoryArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "memory artifact id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	// Existence check so we 404 cleanly and the next two-phase write
	// (delete + publish) doesn't run for nonexistent rows.
	if _, err := h.Queries.GetMemoryArtifact(r.Context(), db.GetMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "memory artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load memory artifact")
		return
	}
	if err := h.Queries.DeleteMemoryArtifact(r.Context(), db.DeleteMemoryArtifactParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete memory artifact")
		return
	}
	userID := requestUserID(r)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventMemoryArtifactDeleted, workspaceID, authorType, authorID, map[string]any{
		"memory_artifact_id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}
