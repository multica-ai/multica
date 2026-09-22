package handler

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func historyTaskResponse(row db.ListAgentTaskHistoryPageRow, workspaceID string) AgentTaskResponse {
	task := db.AgentTaskQueue{
		ID:                   row.ID,
		AgentID:              row.AgentID,
		IssueID:              row.IssueID,
		Status:               row.Status,
		Priority:             row.Priority,
		DispatchedAt:         row.DispatchedAt,
		StartedAt:            row.StartedAt,
		CompletedAt:          row.CompletedAt,
		Error:                row.Error,
		CreatedAt:            row.CreatedAt,
		RuntimeID:            row.RuntimeID,
		WorkDir:              row.WorkDir,
		TriggerCommentID:     row.TriggerCommentID,
		ChatSessionID:        row.ChatSessionID,
		AutopilotRunID:       row.AutopilotRunID,
		Attempt:              row.Attempt,
		MaxAttempts:          row.MaxAttempts,
		ParentTaskID:         row.ParentTaskID,
		FailureReason:        row.FailureReason,
		TriggerSummary:       row.TriggerSummary,
		IsLeaderTask:         row.IsLeaderTask,
		HandoffNote:          row.HandoffNote,
		OriginatorUserID:     row.OriginatorUserID,
		CoalescedCommentIds:  row.CoalescedCommentIds,
		DeliveredCommentIds:  row.DeliveredCommentIds,
		OriginatorSource:     row.OriginatorSource,
		DelegatedFromTaskID:  row.DelegatedFromTaskID,
		RetryOfTaskID:        row.RetryOfTaskID,
		RerunOfTaskID:        row.RerunOfTaskID,
		RuleVersionID:        row.RuleVersionID,
		TriggerEvidenceKind:  row.TriggerEvidenceKind,
		TriggerEvidenceRefID: row.TriggerEvidenceRefID,
		AccountableUserID:    row.AccountableUserID,
		BranchName:           row.BranchName,
		DurableWorkDir:       row.DurableWorkDir,
		CancelledByType:      row.CancelledByType,
		CancelledByID:        row.CancelledByID,
		CancelledByName:      row.CancelledByName,
		Context:              row.Context,
	}
	response := taskToResponse(task, workspaceID)
	// JSONB is already validated. Preserve its wire value without expanding
	// large results into a second graph of Go maps/slices/numbers.
	if len(row.Result) != 0 {
		response.Result = json.RawMessage(row.Result)
	}
	return response
}

func (h *Handler) writeAgentTaskHistory(w http.ResponseWriter, r *http.Request, agent db.Agent, includeUsage bool) {
	params := db.ListAgentTaskHistoryPageParams{OwnerID: agent.ID, PageSize: historyBatchSize}
	failure := "failed to list agent tasks"
	if includeUsage {
		failure = "failed to list agent task usage"
	}
	writeHistory(w, r, failure, func() ([]AgentTaskResponse, bool, error) {
		rows, err := h.Queries.ListAgentTaskHistoryPage(r.Context(), params)
		if err != nil {
			return nil, false, err
		}
		response := make([]AgentTaskResponse, len(rows))
		ids := make([]pgtype.UUID, len(rows))
		for i, row := range rows {
			response[i] = historyTaskResponse(row, uuidToString(agent.WorkspaceID))
			ids[i] = row.ID
		}
		if len(rows) > 0 {
			last := rows[len(rows)-1]
			params.BeforeCreatedAt, params.BeforeID = last.CreatedAt, last.ID
		}
		h.hydrateTaskAttributions(r.Context(), attributionsOf(response))
		if includeUsage {
			if err := h.hydrateAgentTaskUsage(r.Context(), agent.ID, ids, response); err != nil {
				return nil, false, err
			}
		}
		return response, len(rows) == int(historyBatchSize), nil
	})
}

func (h *Handler) writeIssueTaskHistory(w http.ResponseWriter, r *http.Request, issue db.Issue, activeOnly bool) {
	params := db.ListIssueTaskHistoryPageParams{OwnerID: issue.ID, ActiveOnly: activeOnly, PageSize: historyBatchSize}
	writeHistory(w, r, "failed to list tasks", func() ([]AgentTaskResponse, bool, error) {
		rows, err := h.Queries.ListIssueTaskHistoryPage(r.Context(), params)
		if err != nil {
			return nil, false, err
		}
		response := make([]AgentTaskResponse, len(rows))
		for i, row := range rows {
			response[i] = historyTaskResponse(db.ListAgentTaskHistoryPageRow(row), uuidToString(issue.WorkspaceID))
		}
		if len(rows) > 0 {
			last := rows[len(rows)-1]
			params.BeforeCreatedAt, params.BeforeID = last.CreatedAt, last.ID
		}
		h.hydrateTaskAttributions(r.Context(), attributionsOf(response))
		if !activeOnly {
			h.hydrateTaskUsage(r.Context(), issue.ID, response)
		}
		return response, len(rows) == int(historyBatchSize), nil
	})
}

func (h *Handler) writeTaskMessageHistory(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue) {
	// An int64 cursor starts below every stored int32 sequence, including
	// legacy negative/zero sequences. A non-null seek key also keeps generic
	// prepared plans from rescanning the prefix on every page.
	params := db.ListTaskMessagesPageParams{TaskID: task.ID, PageSize: historyBatchSize,
		AfterSeq: math.MinInt32 - 1, AfterID: pgtype.UUID{Valid: true}}
	if raw := r.URL.Query().Get("since"); raw != "" {
		seq, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		params.AfterSeq = seq
		params.AfterID = parseUUID("ffffffff-ffff-ffff-ffff-ffffffffffff")
	}
	top, err := h.Queries.GetTaskMessageHighWatermark(r.Context(), task.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, []protocol.TaskMessagePayload{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}
	params.ThroughSeq, params.ThroughID = top.Seq, top.ID
	writeHistory(w, r, "failed to list task messages", func() ([]protocol.TaskMessagePayload, bool, error) {
		rows, err := h.Queries.ListTaskMessagesPage(r.Context(), params)
		if err != nil {
			return nil, false, err
		}
		response := make([]protocol.TaskMessagePayload, len(rows))
		for i, row := range rows {
			response[i] = taskMessageToPayload(row, uuidToString(task.ID), uuidToString(task.IssueID))
		}
		if len(rows) > 0 {
			last := rows[len(rows)-1]
			params.AfterSeq = int64(last.Seq)
			params.AfterID = last.ID
		}
		return response, len(rows) == int(historyBatchSize), nil
	})
}
