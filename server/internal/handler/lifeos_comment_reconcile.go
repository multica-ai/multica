package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultLifeOSCommentLookbackHours  = 72
	maxLifeOSCommentLookbackHours      = 168
	defaultLifeOSCommentReconcileLimit = 100
	maxLifeOSCommentReconcileLimit     = 100
)

type LifeOSCommentReconcileRequest struct {
	LookbackHours int `json:"lookback_hours,omitempty"`
	Limit         int `json:"limit,omitempty"`
}

type LifeOSCommentReconcileResponse struct {
	Scanned   int `json:"scanned"`
	Queued    int `json:"queued"`
	Coalesced int `json:"coalesced"`
	Deferred  int `json:"deferred"`
	Blocked   int `json:"blocked"`
	Skipped   int `json:"skipped"`
}

// ReconcileLifeOSComments repairs recent chairman comments that were persisted
// before AI 星耀 received a task. It is local-mode only, bounded, content-free
// in its response, and idempotent because comments already referenced by an
// AI 星耀 task or threaded reply are excluded by the query.
func (h *Handler) ReconcileLifeOSComments(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.LocalMode {
		http.NotFound(w, r)
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceIDText := h.resolveWorkspaceID(r)
	workspaceID, err := util.ParseUUID(workspaceIDText)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceIDText)
	if actorType != "member" || actorID != userID {
		writeError(w, http.StatusForbidden, "chairman session required")
		return
	}

	req := LifeOSCommentReconcileRequest{
		LookbackHours: defaultLifeOSCommentLookbackHours,
		Limit:         defaultLifeOSCommentReconcileLimit,
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, localAuthBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LookbackHours == 0 {
		req.LookbackHours = defaultLifeOSCommentLookbackHours
	}
	if req.Limit == 0 {
		req.Limit = defaultLifeOSCommentReconcileLimit
	}
	if req.LookbackHours < 1 || req.LookbackHours > maxLifeOSCommentLookbackHours ||
		req.Limit < 1 || req.Limit > maxLifeOSCommentReconcileLimit {
		writeError(w, http.StatusBadRequest, "reconciliation bounds exceeded")
		return
	}

	ceo, ok := h.localLifeOSCEOForChairman(r.Context(), workspaceID, actorID)
	if !ok {
		writeError(w, http.StatusConflict, "AI 星耀 is not available")
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT c.id
		FROM comment c
		JOIN issue i
		  ON i.id = c.issue_id
		 AND i.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1
		  AND c.author_type = 'member'
		  AND c.author_id = $2
		  AND c.created_at >= now() - make_interval(hours => $4)
		  AND ltrim(c.content, E' \t\r\n') !~* '^/note([[:space:]]|$)'
		  AND ltrim(c.content, E' \t\r\n') !~ '^仅记录，无需回复([[:space:]:：,，。;；!！]|$)'
		  AND NOT EXISTS (
			  SELECT 1
			  FROM agent_task_queue task
			  WHERE task.agent_id = $3
			    AND (
				    task.trigger_comment_id = c.id
				    OR c.id = ANY(task.coalesced_comment_ids)
				    OR c.id = ANY(task.delivered_comment_ids)
			    )
		  )
		  AND NOT EXISTS (
			  SELECT 1
			  FROM comment reply
			  WHERE reply.parent_id = c.id
			    AND reply.author_type = 'agent'
			    AND reply.author_id = $3
		  )
		ORDER BY c.created_at ASC, c.id ASC
		LIMIT $5
	`, workspaceID, util.MustParseUUID(actorID), ceo.ID, req.LookbackHours, req.Limit)
	if err != nil {
		slog.Error("LifeOS comment reconciliation query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reconcile comments")
		return
	}
	defer rows.Close()

	commentIDs := make([]pgtype.UUID, 0, req.Limit)
	for rows.Next() {
		var commentID pgtype.UUID
		if err := rows.Scan(&commentID); err != nil {
			slog.Error("LifeOS comment reconciliation scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to reconcile comments")
			return
		}
		commentIDs = append(commentIDs, commentID)
	}
	if err := rows.Err(); err != nil {
		slog.Error("LifeOS comment reconciliation rows failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reconcile comments")
		return
	}

	response := LifeOSCommentReconcileResponse{}
	for _, commentID := range commentIDs {
		response.Scanned++
		comment, err := h.Queries.GetComment(r.Context(), commentID)
		if err != nil || isNoteComment(comment.Content) {
			response.Skipped++
			continue
		}
		issue, err := h.Queries.GetIssue(r.Context(), comment.IssueID)
		if err != nil || uuidToString(issue.WorkspaceID) != workspaceIDText {
			response.Blocked++
			continue
		}
		trigger, ok := h.routeLifeOSChairmanToCEO(
			r.Context(),
			issue,
			"member",
			actorID,
			commentTriggerComputeOptions{
				ExcludeTriggerCommentID: comment.ID,
				OriginatorUserID:        actorID,
			},
		)
		if !ok {
			response.Blocked++
			continue
		}
		result, ok := h.enqueueCommentAgentTriggers(
			r.Context(),
			issue,
			comment.ID,
			[]commentAgentTrigger{trigger},
		)[uuidToString(ceo.ID)]
		if !ok {
			if h.lifeOSCommentCovered(r.Context(), ceo.ID, comment.ID) {
				response.Coalesced++
				continue
			}
			response.Blocked++
			continue
		}
		switch result.status {
		case DispatchQueued:
			response.Queued++
		case DispatchCoalesced:
			response.Coalesced++
		case DispatchDeferred:
			response.Deferred++
		default:
			if h.lifeOSCommentCovered(r.Context(), ceo.ID, comment.ID) {
				response.Coalesced++
			} else {
				response.Blocked++
			}
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) lifeOSCommentCovered(
	ctx context.Context,
	agentID pgtype.UUID,
	commentID pgtype.UUID,
) bool {
	var covered bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_task_queue task
			WHERE task.agent_id = $1
			  AND (
				  task.trigger_comment_id = $2
				  OR $2 = ANY(task.coalesced_comment_ids)
				  OR $2 = ANY(task.delivered_comment_ids)
			  )
		)
	`, agentID, commentID).Scan(&covered)
	return err == nil && covered
}

func (h *Handler) localLifeOSCEOForChairman(
	ctx context.Context,
	workspaceID pgtype.UUID,
	chairmanID string,
) (db.Agent, bool) {
	agents, err := h.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, false
	}
	for _, agent := range agents {
		if agent.Name == localLifeOSCEOAgentName &&
			agent.RuntimeID.Valid &&
			agent.OwnerID.Valid &&
			uuidToString(agent.OwnerID) == chairmanID {
			return agent, true
		}
	}
	return db.Agent{}, false
}
