// CEREBRO-PATCH(issue-dependencies): HTTP handlers for the blocks / blocked-by /
// related issue relations (FIR-823). The issue_dependency table ships in
// 001_init.up.sql; queries live in pkg/db/queries/issue_dependency.sql.
//
// All relations are stored as canonical rows (see the SQL file): a directed
// 'blocks' edge, or a single LEAST/GREATEST-ordered 'related' row. "blocked_by"
// is never persisted on its own — it is a 'blocks' edge read from the other end.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueDependencyRef is a lightweight issue reference returned in dependency
// lists — just enough for the frontend to render a clickable chip.
type IssueDependencyRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

// IssueDependenciesResponse groups an issue's relations by direction/type.
type IssueDependenciesResponse struct {
	Blocks    []IssueDependencyRef `json:"blocks"`
	BlockedBy []IssueDependencyRef `json:"blocked_by"`
	Related   []IssueDependencyRef `json:"related"`
}

type addDependencyRequest struct {
	IssueID string `json:"issue_id"`
}

func depRef(id pgtype.UUID, number int32, title, status, prefix string) IssueDependencyRef {
	return IssueDependencyRef{
		ID:         uuidToString(id),
		Identifier: prefix + "-" + strconv.Itoa(int(number)),
		Title:      title,
		Status:     status,
	}
}

// loadDependencies reads all three relation lists for an issue. On a read error
// it logs and returns empty slices — a missing dependency list should not fail
// the whole issue fetch.
func (h *Handler) loadDependencies(r *http.Request, issue db.Issue, prefix string) IssueDependenciesResponse {
	ctx := r.Context()
	out := IssueDependenciesResponse{
		Blocks:    []IssueDependencyRef{},
		BlockedBy: []IssueDependencyRef{},
		Related:   []IssueDependencyRef{},
	}
	if blocks, err := h.Queries.ListBlocks(ctx, db.ListBlocksParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err == nil {
		for _, b := range blocks {
			out.Blocks = append(out.Blocks, depRef(b.ID, b.Number, b.Title, b.Status, prefix))
		}
	} else {
		slog.Warn("ListBlocks failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	if bb, err := h.Queries.ListBlockedBy(ctx, db.ListBlockedByParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err == nil {
		for _, b := range bb {
			out.BlockedBy = append(out.BlockedBy, depRef(b.ID, b.Number, b.Title, b.Status, prefix))
		}
	} else {
		slog.Warn("ListBlockedBy failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	if rel, err := h.Queries.ListRelated(ctx, db.ListRelatedParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err == nil {
		for _, b := range rel {
			out.Related = append(out.Related, depRef(b.ID, b.Number, b.Title, b.Status, prefix))
		}
	} else {
		slog.Warn("ListRelated failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	return out
}

// attachDependencyRefs bulk-loads blocks/blocked_by for a page of issues and
// folds them into each IssueResponse, avoiding an N+1 from the list endpoint.
// Related is intentionally skipped on the list path — it's only needed on the
// detail view, and the morning-overview dedup keys on blocked_by.
func (h *Handler) attachDependencyRefs(r *http.Request, wsUUID pgtype.UUID, prefix string, resp []IssueResponse) {
	if len(resp) == 0 {
		return
	}
	ids := make([]pgtype.UUID, len(resp))
	for i := range resp {
		ids[i] = parseUUID(resp[i].ID)
	}
	ctx := r.Context()

	blockedBy := map[string][]IssueDependencyRef{}
	if rows, err := h.Queries.ListBlockedByForIssues(ctx, db.ListBlockedByForIssuesParams{IssueIds: ids, WorkspaceID: wsUUID}); err == nil {
		for _, row := range rows {
			key := uuidToString(row.IssueID)
			blockedBy[key] = append(blockedBy[key], depRef(row.ID, row.Number, row.Title, row.Status, prefix))
		}
	} else {
		slog.Warn("ListBlockedByForIssues failed", append(logger.RequestAttrs(r), "error", err)...)
	}

	blocks := map[string][]IssueDependencyRef{}
	if rows, err := h.Queries.ListBlocksForIssues(ctx, db.ListBlocksForIssuesParams{IssueIds: ids, WorkspaceID: wsUUID}); err == nil {
		for _, row := range rows {
			key := uuidToString(row.IssueID)
			blocks[key] = append(blocks[key], depRef(row.ID, row.Number, row.Title, row.Status, prefix))
		}
	} else {
		slog.Warn("ListBlocksForIssues failed", append(logger.RequestAttrs(r), "error", err)...)
	}

	for i := range resp {
		bb := blockedBy[resp[i].ID]
		if bb == nil {
			bb = []IssueDependencyRef{}
		}
		bl := blocks[resp[i].ID]
		if bl == nil {
			bl = []IssueDependencyRef{}
		}
		resp[i].BlockedBy = &bb
		resp[i].Blocks = &bl
	}
}

// ListIssueDependencies returns all relations for an issue grouped by type.
func (h *Handler) ListIssueDependencies(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	writeJSON(w, http.StatusOK, h.loadDependencies(r, issue, prefix))
}

// resolveDependencyTarget loads the acting issue, parses + validates the target
// issue id from the body, and confirms the target is in the same workspace.
// Returns both resolved issues.
func (h *Handler) resolveDependencyTarget(w http.ResponseWriter, r *http.Request, bodyTarget string) (db.Issue, db.Issue, bool) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return db.Issue{}, db.Issue{}, false
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, bodyTarget, "issue_id")
	if !ok {
		return db.Issue{}, db.Issue{}, false
	}
	if uuidToString(targetUUID) == uuidToString(issue.ID) {
		writeError(w, http.StatusBadRequest, "an issue cannot depend on itself")
		return db.Issue{}, db.Issue{}, false
	}
	target, err := h.Queries.GetIssue(r.Context(), targetUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(issue.WorkspaceID) {
		writeError(w, http.StatusNotFound, "target issue not found")
		return db.Issue{}, db.Issue{}, false
	}
	return issue, target, true
}

// createBlocksEdgeChecked creates a 'blocks' edge (blocker → blocked) after a
// cycle check, then writes the refreshed dependency set for the acting issue.
func (h *Handler) createBlocksEdgeChecked(w http.ResponseWriter, r *http.Request, blocker, blocked, acting db.Issue) {
	// Adding "blocker blocks blocked" closes a cycle iff blocked already reaches
	// blocker through the blocks graph.
	cycle, err := h.Queries.BlocksPathExists(r.Context(), db.BlocksPathExistsParams{
		FromIssueID: blocked.ID,
		ToIssueID:   blocker.ID,
	})
	if err != nil {
		slog.Warn("BlocksPathExists failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add dependency")
		return
	}
	if cycle {
		writeError(w, http.StatusConflict, "that link would create a circular dependency")
		return
	}
	if err := h.Queries.CreateBlocksEdge(r.Context(), db.CreateBlocksEdgeParams{
		IssueID:          blocker.ID,
		DependsOnIssueID: blocked.ID,
		WorkspaceID:      acting.WorkspaceID,
	}); err != nil {
		slog.Warn("CreateBlocksEdge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add dependency")
		return
	}
	h.writeDependencies(w, r, acting)
}

// AddBlocks: the acting issue blocks the body issue. POST /issues/{id}/blocks
func (h *Handler) AddBlocks(w http.ResponseWriter, r *http.Request) {
	var req addDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, target, ok := h.resolveDependencyTarget(w, r, req.IssueID)
	if !ok {
		return
	}
	h.createBlocksEdgeChecked(w, r, issue, target, issue)
}

// AddBlockedBy: the acting issue is blocked by the body issue.
// POST /issues/{id}/blocked-by — stored as a 'blocks' edge (target → acting).
func (h *Handler) AddBlockedBy(w http.ResponseWriter, r *http.Request) {
	var req addDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, target, ok := h.resolveDependencyTarget(w, r, req.IssueID)
	if !ok {
		return
	}
	h.createBlocksEdgeChecked(w, r, target, issue, issue)
}

// AddRelated: symmetric relation between the acting issue and the body issue.
func (h *Handler) AddRelated(w http.ResponseWriter, r *http.Request) {
	var req addDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, target, ok := h.resolveDependencyTarget(w, r, req.IssueID)
	if !ok {
		return
	}
	if err := h.Queries.CreateRelatedEdge(r.Context(), db.CreateRelatedEdgeParams{
		IssueA:      issue.ID,
		IssueB:      target.ID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("CreateRelatedEdge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}
	h.writeDependencies(w, r, issue)
}

// DeleteBlocks removes a 'blocks' edge (acting → otherId).
func (h *Handler) DeleteBlocks(w http.ResponseWriter, r *http.Request) {
	issue, other, ok := h.resolveDependencyParam(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteBlocksEdge(r.Context(), db.DeleteBlocksEdgeParams{
		IssueID:          issue.ID,
		DependsOnIssueID: other,
		WorkspaceID:      issue.WorkspaceID,
	}); err != nil {
		slog.Warn("DeleteBlocksEdge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove dependency")
		return
	}
	h.writeDependencies(w, r, issue)
}

// DeleteBlockedBy removes the 'blocks' edge (otherId → acting).
func (h *Handler) DeleteBlockedBy(w http.ResponseWriter, r *http.Request) {
	issue, other, ok := h.resolveDependencyParam(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteBlocksEdge(r.Context(), db.DeleteBlocksEdgeParams{
		IssueID:          other,
		DependsOnIssueID: issue.ID,
		WorkspaceID:      issue.WorkspaceID,
	}); err != nil {
		slog.Warn("DeleteBlocksEdge (blocked-by) failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove dependency")
		return
	}
	h.writeDependencies(w, r, issue)
}

// DeleteRelated removes the symmetric relation between acting and otherId.
func (h *Handler) DeleteRelated(w http.ResponseWriter, r *http.Request) {
	issue, other, ok := h.resolveDependencyParam(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteRelatedEdge(r.Context(), db.DeleteRelatedEdgeParams{
		IssueA:      issue.ID,
		IssueB:      other,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("DeleteRelatedEdge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove relation")
		return
	}
	h.writeDependencies(w, r, issue)
}

// resolveDependencyParam loads the acting issue and parses the {otherId} path
// param. The workspace guard lives in the DELETE queries themselves; here we
// only need a syntactically valid UUID.
func (h *Handler) resolveDependencyParam(w http.ResponseWriter, r *http.Request) (db.Issue, pgtype.UUID, bool) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	other, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "otherId"), "otherId")
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	return issue, other, true
}

// writeDependencies reloads and returns the acting issue's dependency set after
// a mutation, so the client can update its cache from the response.
func (h *Handler) writeDependencies(w http.ResponseWriter, r *http.Request, issue db.Issue) {
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	writeJSON(w, http.StatusOK, h.loadDependencies(r, issue, prefix))
}
