package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Structured issue relations (blocks / related).
//
// One canonical directed row per edge; "blocked by" is derived by the reader
// from rows where the issue is the target and never stored. See migration 196
// and queries/issue_relation.sql. Parent/child ("sub-issues") is a separate
// hierarchy and is intentionally not modeled here.
// ---------------------------------------------------------------------------

const (
	relationTypeBlocks  = "blocks"
	relationTypeRelated = "related"

	// Cap batched relation lookups to match the sub-issue equivalent
	// (listChildrenByParentsLimit).
	listRelationsByIssuesLimit = 200
)

// isStorableRelationType reports whether t is a type that is persisted. Note
// "blocked_by" is NOT storable — it is the derived inverse of "blocks".
func isStorableRelationType(t string) bool {
	return t == relationTypeBlocks || t == relationTypeRelated
}

type AddIssueRelationRequest struct {
	Type          string `json:"type"`
	TargetIssueID string `json:"target_issue_id"`
}

type IssueRelationResponse struct {
	ID            string  `json:"id"`
	SourceIssueID string  `json:"source_issue_id"`
	TargetIssueID string  `json:"target_issue_id"`
	Type          string  `json:"type"`
	CreatedByType *string `json:"created_by_type"`
	CreatedByID   *string `json:"created_by_id"`
	CreatedAt     string  `json:"created_at"`
}

func issueRelationToResponse(rel db.IssueRelation) IssueRelationResponse {
	return IssueRelationResponse{
		ID:            uuidToString(rel.ID),
		SourceIssueID: uuidToString(rel.SourceIssueID),
		TargetIssueID: uuidToString(rel.TargetIssueID),
		Type:          rel.Type,
		CreatedByType: textToPtr(rel.CreatedByType),
		CreatedByID:   uuidToPtr(rel.CreatedByID),
		CreatedAt:     timestampToString(rel.CreatedAt),
	}
}

// ListIssueRelations returns every relation touching one issue, in either
// direction. The client derives the per-issue blocks/blocked_by label from
// source/target.
func (h *Handler) ListIssueRelations(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	rels, err := h.Queries.ListIssueRelations(r.Context(), db.ListIssueRelationsParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		slog.Warn("ListIssueRelations failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list relations")
		return
	}
	resp := make([]IssueRelationResponse, len(rels))
	for i, rel := range rels {
		resp[i] = issueRelationToResponse(rel)
	}
	writeJSON(w, http.StatusOK, map[string]any{"relations": resp})
}

// ListRelationsForIssues is the bulk variant: every relation touching any issue
// in the ?issue_ids= set, so list/board views can fold relations into rows
// without an N+1 (mirrors ListChildrenByParents).
func (h *Handler) ListRelationsForIssues(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	raw := r.URL.Query().Get("issue_ids")
	if raw == "" {
		writeJSON(w, http.StatusOK, map[string]any{"relations": []IssueRelationResponse{}})
		return
	}
	parts := strings.Split(raw, ",")
	if len(parts) > listRelationsByIssuesLimit {
		writeError(w, http.StatusBadRequest, "too many issue_ids")
		return
	}
	issueIDs := make([]pgtype.UUID, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, ok := parseUUIDOrBadRequest(w, s, "issue_ids")
		if !ok {
			return
		}
		issueIDs = append(issueIDs, id)
	}
	if len(issueIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"relations": []IssueRelationResponse{}})
		return
	}

	rels, err := h.Queries.ListIssueRelationsForIssues(r.Context(), db.ListIssueRelationsForIssuesParams{
		WorkspaceID: wsUUID,
		IssueIds:    issueIDs,
	})
	if err != nil {
		slog.Warn("ListRelationsForIssues failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list relations")
		return
	}
	resp := make([]IssueRelationResponse, len(rels))
	for i, rel := range rels {
		resp[i] = issueRelationToResponse(rel)
	}
	writeJSON(w, http.StatusOK, map[string]any{"relations": resp})
}

// AddIssueRelation links the path issue (source) to a target issue. It rejects
// self-relations and cross-workspace targets, canonicalizes the symmetric
// "related" edge, refuses duplicates, and — for "blocks" — refuses an edge that
// would close a cycle. The cycle check + insert run under a workspace-scoped
// advisory lock so concurrent writers cannot race past each other.
func (h *Handler) AddIssueRelation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req AddIssueRelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !isStorableRelationType(req.Type) {
		writeError(w, http.StatusBadRequest, "type must be one of: blocks, related")
		return
	}
	if req.TargetIssueID == "" {
		writeError(w, http.StatusBadRequest, "target_issue_id is required")
		return
	}

	source, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, req.TargetIssueID, "target_issue_id")
	if !ok {
		return
	}
	if targetUUID == source.ID {
		writeError(w, http.StatusBadRequest, "an issue cannot relate to itself")
		return
	}

	// Target must live in the same workspace as the source.
	target, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          targetUUID,
		WorkspaceID: source.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "target issue not found in this workspace")
			return
		}
		slog.Warn("GetIssueInWorkspace in AddIssueRelation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}

	// Canonicalize the symmetric "related" edge (source < target by UUID text)
	// so A~B and B~A collapse to a single row. "blocks" keeps request order —
	// direction is meaningful.
	srcID, tgtID := source.ID, target.ID
	if req.Type == relationTypeRelated && uuidToString(tgtID) < uuidToString(srcID) {
		srcID, tgtID = tgtID, srcID
	}

	// Derive both attribution columns from a single decision so they always
	// satisfy the paired-nullability CHECK (both set, or both null). resolveActor
	// normally returns a valid actor, but the task-token short-circuit can yield
	// an unparseable id; in that case store neither rather than a half-populated
	// row that the CHECK would reject with a 500.
	actorType, actorID := h.resolveActor(r, userID, uuidToString(source.WorkspaceID))
	createdByType := pgtype.Text{}
	createdByID := pgtype.UUID{}
	if aid, perr := util.ParseUUID(actorID); perr == nil && actorType != "" {
		createdByType = pgtype.Text{String: actorType, Valid: true}
		createdByID = aid
	}

	// Serialize validation + insert per workspace so two concurrent adds cannot
	// both pass the cycle check and form a cycle.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockWorkspaceRelations(r.Context(), uuidToString(source.WorkspaceID)); err != nil {
		slog.Warn("LockWorkspaceRelations failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}

	// Duplicate?
	if _, err := qtx.GetIssueRelationEdge(r.Context(), db.GetIssueRelationEdgeParams{
		WorkspaceID:   source.WorkspaceID,
		SourceIssueID: srcID,
		TargetIssueID: tgtID,
		Type:          req.Type,
	}); err == nil {
		writeError(w, http.StatusConflict, "relation already exists")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("GetIssueRelationEdge failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}

	// Cycle check: adding "src blocks tgt" closes a cycle iff src is already
	// reachable from tgt via blocks edges. So start the walk at tgt and see if
	// it reaches src.
	if req.Type == relationTypeBlocks {
		reachable, err := qtx.IssueBlocksReachable(r.Context(), db.IssueBlocksReachableParams{
			WorkspaceID: source.WorkspaceID,
			FromIssueID: tgtID,
			ToIssueID:   srcID,
		})
		if err != nil {
			slog.Warn("IssueBlocksReachable failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to add relation")
			return
		}
		if reachable {
			writeError(w, http.StatusConflict, "relation would create a blocking cycle")
			return
		}
	}

	rel, err := qtx.CreateIssueRelation(r.Context(), db.CreateIssueRelationParams{
		WorkspaceID:   source.WorkspaceID,
		SourceIssueID: srcID,
		TargetIssueID: tgtID,
		Type:          req.Type,
		CreatedByType: createdByType,
		CreatedByID:   createdByID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The INSERT...SELECT returned no row. A duplicate is impossible here:
			// we hold the workspace advisory lock and already dedup-checked under
			// it, and no concurrent add can insert while we hold the lock — so
			// ON CONFLICT never fires. The only way to reach here is that one of
			// the WHERE EXISTS guards failed, i.e. an endpoint was deleted between
			// the pre-lock load and now. Report which one.
			// Re-check on the transaction connection (not the pool) — we still
			// hold the tx and its advisory lock.
			if _, terr := qtx.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          target.ID,
				WorkspaceID: source.WorkspaceID,
			}); errors.Is(terr, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "target issue not found in this workspace")
				return
			}
			if _, serr := qtx.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          source.ID,
				WorkspaceID: source.WorkspaceID,
			}); errors.Is(serr, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "issue not found")
				return
			}
		}
		slog.Warn("CreateIssueRelation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit in AddIssueRelation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to add relation")
		return
	}

	// Notify both endpoints so each side's relations cache invalidates.
	h.publish(protocol.EventIssueRelationsChanged, uuidToString(source.WorkspaceID), actorType, actorID, map[string]any{
		"issue_ids": []string{uuidToString(srcID), uuidToString(tgtID)},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"relation": issueRelationToResponse(rel)})
}

// RemoveIssueRelation deletes one relation by id. The delete is scoped to the
// workspace and required to touch the path issue, so a mismatched pair 404s.
func (h *Handler) RemoveIssueRelation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	relUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "relationId"), "relation id")
	if !ok {
		return
	}

	deleted, err := h.Queries.DeleteIssueRelation(r.Context(), db.DeleteIssueRelationParams{
		ID:          relUUID,
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "relation not found")
			return
		}
		slog.Warn("DeleteIssueRelation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove relation")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueRelationsChanged, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue_ids": []string{uuidToString(deleted.SourceIssueID), uuidToString(deleted.TargetIssueID)},
	})
	w.WriteHeader(http.StatusNoContent)
}

// deleteIssueWithRelations removes an issue and every relation edge touching it
// in one transaction, and returns the distinct counterpart issue ids whose
// relation caches the caller must invalidate. issue_relation has no FK cascade,
// so the cleanup must be explicit and atomic with the delete — otherwise a
// delete would leave edges pointing at a gone issue. The workspace advisory lock
// (the same one AddIssueRelation takes) serializes this against concurrent adds,
// so a delete can never race an add into an orphan edge. Used by both DeleteIssue
// and BatchDeleteIssues.
func (h *Handler) deleteIssueWithRelations(ctx context.Context, issueID, workspaceID pgtype.UUID) ([]string, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockWorkspaceRelations(ctx, uuidToString(workspaceID)); err != nil {
		return nil, err
	}
	removed, err := qtx.DeleteIssueRelationsForIssue(ctx, db.DeleteIssueRelationsForIssueParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.DeleteIssue(ctx, db.DeleteIssueParams{
		ID:          issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Distinct counterparts (the surviving side of each removed edge), excluding
	// the just-deleted issue itself.
	deletedID := uuidToString(issueID)
	seen := map[string]bool{}
	counterparts := make([]string, 0, len(removed))
	for _, edge := range removed {
		for _, endpoint := range []pgtype.UUID{edge.SourceIssueID, edge.TargetIssueID} {
			id := uuidToString(endpoint)
			if id == deletedID || seen[id] {
				continue
			}
			seen[id] = true
			counterparts = append(counterparts, id)
		}
	}
	return counterparts, nil
}
