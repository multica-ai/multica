package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) hydrateTaskSupplementMetadata(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, tasks []db.AgentTaskQueue, resp []AgentTaskResponse) {
	if len(tasks) == 0 || len(tasks) != len(resp) {
		return
	}
	taskIDs := make([]pgtype.UUID, 0, len(tasks))
	for i := range tasks {
		if tasks[i].IssueID.Valid {
			taskIDs = append(taskIDs, tasks[i].ID)
		}
	}
	if len(taskIDs) == 0 {
		return
	}
	rows, err := h.Queries.ListTaskSupplementMetadata(ctx, db.ListTaskSupplementMetadataParams{
		WorkspaceID: workspaceID,
		TaskIds:     taskIDs,
	})
	if err != nil {
		return // metadata is optional and must fail closed
	}
	byTask := make(map[string]db.ListTaskSupplementMetadataRow, len(rows))
	for _, row := range rows {
		byTask[uuidToString(row.TaskID)] = row
	}

	userID := requestUserID(r)
	// can_supplement answers for the caller: an agent is judged as its
	// steering comment would be, never as the member it runs for.
	actorType, actorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	originatorUserID := ""
	if actorType == "agent" {
		originatorUserID = h.invokeOriginatorFromRequest(r, actorType, actorID)
	}
	permission := make(map[string]bool)
	for i, task := range tasks {
		row, ok := byTask[uuidToString(task.ID)]
		if !ok {
			continue
		}
		resp[i].SupplementCapability = row.Capability
		resp[i].SupplementCommentIDs = uuidsToStrings(row.CommentIds)
		if task.Status != "running" || row.Capability != protocol.DaemonCapabilityTaskSupplementV1 || userID == "" {
			continue
		}
		agentID := uuidToString(task.AgentID)
		allowed, checked := permission[agentID]
		if !checked {
			agent, loadErr := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
				ID: task.AgentID, WorkspaceID: workspaceID,
			})
			switch {
			case loadErr != nil:
				allowed = false
			case actorType == "agent":
				allowed = h.agentMaySteer(ctx, agent, actorID, originatorUserID, uuidToString(workspaceID))
			default:
				allowed = h.canInvokeAgent(ctx, agent, "member", userID, userID, uuidToString(workspaceID))
			}
			permission[agentID] = allowed
		}
		resp[i].CanSupplement = allowed
	}
}

type taskSupplementReceiptResponse struct {
	TaskID        string  `json:"supplement_task_id"`
	Status        string  `json:"supplement_status"`
	FailureReason *string `json:"supplement_failure_reason,omitempty"`
	DeliveredAt   *string `json:"supplement_delivered_at,omitempty"`
}

func supplementReceipt(s db.TaskSupplement) taskSupplementReceiptResponse {
	return taskSupplementReceiptResponse{
		TaskID:        uuidToString(s.TaskID),
		Status:        s.Status,
		FailureReason: textToPtr(s.FailureReason),
		DeliveredAt:   timestampToPtr(s.DeliveredAt),
	}
}

// CommentSupplementResponse is one delivery receipt of a comment that steered
// a running turn. A comment can steer several agents' turns at once.
type CommentSupplementResponse struct {
	TaskID        string  `json:"task_id"`
	AgentID       string  `json:"agent_id,omitempty"`
	Status        string  `json:"status"`
	FailureReason *string `json:"failure_reason,omitempty"`
	DeliveredAt   *string `json:"delivered_at,omitempty"`
}

// listCommentSupplements groups every receipt by comment id, oldest first.
// Receipts are optional metadata: a failed read leaves comments without them.
func (h *Handler) listCommentSupplements(ctx context.Context, workspaceID pgtype.UUID, commentIDs []pgtype.UUID) map[string][]CommentSupplementResponse {
	out := make(map[string][]CommentSupplementResponse)
	if len(commentIDs) == 0 {
		return out
	}
	rows, err := h.Queries.ListTaskSupplementsByCommentIDs(ctx, db.ListTaskSupplementsByCommentIDsParams{
		WorkspaceID: workspaceID, CommentIds: commentIDs,
	})
	if err != nil {
		return out
	}
	for _, row := range rows {
		id := uuidToString(row.CommentID)
		out[id] = append(out[id], CommentSupplementResponse{
			TaskID:        uuidToString(row.TaskID),
			AgentID:       uuidToString(row.AgentID),
			Status:        row.Status,
			FailureReason: textToPtr(row.FailureReason),
			DeliveredAt:   timestampToPtr(row.DeliveredAt),
		})
	}
	return out
}

// applyCommentSupplements sets the receipt list and mirrors its first entry
// into the single-receipt fields older clients read.
func applyCommentSupplements(resp *CommentResponse, receipts []CommentSupplementResponse) {
	if len(receipts) == 0 {
		return
	}
	resp.Supplements = receipts
	resp.SupplementTaskID = receipts[0].TaskID
	resp.SupplementStatus = receipts[0].Status
	resp.SupplementFailureReason = receipts[0].FailureReason
	resp.SupplementDeliveredAt = receipts[0].DeliveredAt
}

// Agent-authored steering is capped per running turn: in total, and per
// authoring agent. Two agents mentioning each other mid-run can then only
// add a bounded number of messages to each other's turns; past the cap the
// comment is an ordinary trigger. Member steering is not capped.
const (
	maxAgentSteersPerTurn         = 10
	maxAgentSteersPerAgentPerTurn = 3
)

// commentSteerRequest is how the author of a comment asked for it to reach
// running turns. TaskIDs name exact turns; All asks for the current running
// turn of every recipient that has no named turn.
type commentSteerRequest struct {
	TaskIDs []pgtype.UUID
	All     bool
}

func (s commentSteerRequest) empty() bool { return len(s.TaskIDs) == 0 && !s.All }

// agentMaySteer reports whether an agent author may add its comment to the
// target agent's running turn: exactly when its comment could start that
// agent. Steering only moves the comment into the turn the target is already
// running, so it grants nothing an @mention does not. An agent never steers
// itself.
func (h *Handler) agentMaySteer(ctx context.Context, target db.Agent, actorID, originatorUserID, workspaceID string) bool {
	if actorID == "" || uuidToString(target.ID) == actorID {
		return false
	}
	return h.canInvokeAgent(ctx, target, "agent", actorID, originatorUserID, workspaceID)
}

// steerCommentAgentTriggers binds a member or agent comment to a running turn
// of each recipient: the turn its author named, or with steer.All the
// recipient's current running turn. A bound agent leaves the enqueue list:
// that turn receives the comment instead of a follow-up run. A chosen turn
// that has ended, has not started, or cannot take additional input is never
// swapped for another turn of the same agent; that recipient keeps its normal
// queued / coalesced / deferred handling, so losing a race never drops or
// misdirects the comment. An agent author steers only agents it may invoke,
// within the per-turn caps.
func (h *Handler) steerCommentAgentTriggers(ctx context.Context, issue db.Issue, comment db.Comment, actorType, actorID, originatorUserID string, triggers []commentAgentTrigger, steer commentSteerRequest) ([]commentAgentTrigger, map[string]commentEnqueueResult) {
	if steer.empty() || len(triggers) == 0 || (actorType != "member" && actorType != "agent") {
		return triggers, nil
	}
	// Each chosen turn names its agent; only a turn of this issue counts.
	chosen := make(map[string]pgtype.UUID, len(steer.TaskIDs))
	for _, taskID := range steer.TaskIDs {
		task, err := h.Queries.GetAgentTask(ctx, taskID)
		if err != nil || task.IssueID != issue.ID {
			continue
		}
		chosen[uuidToString(task.AgentID)] = task.ID
	}
	kept := make([]commentAgentTrigger, 0, len(triggers))
	steered := make(map[string]commentEnqueueResult)
	for _, trigger := range triggers {
		agentID := uuidToString(trigger.Agent.ID)
		taskID, ok := chosen[agentID]
		if !ok && steer.All {
			running, err := h.Queries.GetRunningTaskForAgentOnIssue(ctx, db.GetRunningTaskForAgentOnIssueParams{
				IssueID: issue.ID, AgentID: trigger.Agent.ID,
			})
			taskID, ok = running, err == nil
		}
		if !ok {
			kept = append(kept, trigger)
			continue
		}
		// Some trigger routes (a worker's reply reaching its assigned leader)
		// do not pass the invoke gate, so an agent author is re-checked here.
		if actorType == "agent" && !h.agentMaySteer(ctx, trigger.Agent, actorID, originatorUserID, uuidToString(issue.WorkspaceID)) {
			kept = append(kept, trigger)
			continue
		}
		// The comment is its own request id: it binds to a turn at most once.
		bound, err := h.bindCommentSteer(ctx, db.BindCommentTaskSupplementParams{
			TaskID: taskID, IssueID: issue.ID, AgentID: trigger.Agent.ID, WorkspaceID: issue.WorkspaceID,
			AuthorType: comment.AuthorType, MaxAgentBindings: maxAgentSteersPerTurn, MaxBindingsPerAgent: maxAgentSteersPerAgentPerTurn,
			CommentID: comment.ID, AuthorID: comment.AuthorID, ClientRequestID: comment.ID,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("steer comment into running turn failed",
					"issue_id", uuidToString(issue.ID), "comment_id", uuidToString(comment.ID), "task_id", uuidToString(taskID), "error", err)
			}
			kept = append(kept, trigger)
			continue
		}
		steered[agentID] = commentEnqueueResult{status: DispatchSteered, reason: ReasonSteered}
		if h.DaemonTaskSupplement != nil && bound.RuntimeID.Valid {
			h.DaemonTaskSupplement.NotifyTaskSupplementAvailable(uuidToString(bound.RuntimeID), uuidToString(bound.TaskID))
		}
	}
	if len(steered) > 0 {
		h.publishCommentSupplementUpdate(ctx, issue.WorkspaceID, comment.ID)
	}
	return kept, steered
}

// bindCommentSteer locks the turn in one statement and binds in the next, in
// one transaction, so the bind's cap count sees every binding committed by an
// earlier lock holder (see LockTaskForSupplementBind). pgx.ErrNoRows means
// nothing was bound.
func (h *Handler) bindCommentSteer(ctx context.Context, params db.BindCommentTaskSupplementParams) (db.BindCommentTaskSupplementRow, error) {
	if h.TxStarter == nil {
		return h.Queries.BindCommentTaskSupplement(ctx, params)
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.BindCommentTaskSupplementRow{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockTaskForSupplementBind(ctx, params.TaskID); err != nil {
		return db.BindCommentTaskSupplementRow{}, err
	}
	bound, err := qtx.BindCommentTaskSupplement(ctx, params)
	if err != nil {
		return db.BindCommentTaskSupplementRow{}, err
	}
	return bound, tx.Commit(ctx)
}

func (h *Handler) loadTaskSupplementTarget(w http.ResponseWriter, r *http.Request) (db.Issue, db.AgentTaskQueue, pgtype.UUID, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	authorID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "task not found")
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID: task.AgentID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, uuidToString(issue.WorkspaceID)) {
		writeErrorCode(w, http.StatusForbidden, "invocation_not_allowed", "you cannot run this agent")
		return db.Issue{}, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	return issue, task, authorID, true
}

type createTaskSupplementRequest struct {
	Content         string `json:"content"`
	ClientRequestID string `json:"client_request_id"`
}

// CreateTaskSupplement adds one ordinary historical comment to one exact run.
// The SQL statement owns the terminal race: when completion wins, no comment,
// binding, or coverage marker is created.
func (h *Handler) CreateTaskSupplement(w http.ResponseWriter, r *http.Request) {
	issue, task, authorID, ok := h.loadTaskSupplementTarget(w, r)
	if !ok {
		return
	}
	var req createTaskSupplementRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*maxCommentContentBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = sanitizeNullBytes(req.Content)
	if len(req.Content) > maxCommentContentBytes {
		writeError(w, http.StatusBadRequest, "content is too long")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, req.ClientRequestID, "client_request_id")
	if !ok {
		return
	}
	lookup := db.GetTaskSupplementByRequestParams{
		TaskID: task.ID, WorkspaceID: issue.WorkspaceID, AuthorID: authorID, ClientRequestID: requestID,
	}
	if existing, err := h.Queries.GetTaskSupplementByRequest(r.Context(), lookup); err == nil {
		h.notifyTaskSupplementAvailable(task)
		h.writeExistingTaskSupplement(w, r, existing)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check additional message")
		return
	}
	if task.Status != "running" {
		writeErrorCode(w, http.StatusConflict, "task_supplement_turn_ended", "this run has ended")
		return
	}
	capability, err := h.Queries.GetTaskSupplementCapability(r.Context(), task.ID)
	if errors.Is(err, pgx.ErrNoRows) || capability.Capability != protocol.DaemonCapabilityTaskSupplementV1 {
		writeErrorCode(w, http.StatusPreconditionFailed, "task_supplement_unsupported", "this run does not support additional messages")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check additional-message support")
		return
	}

	created, err := h.Queries.CreateTaskSupplement(r.Context(), db.CreateTaskSupplementParams{
		TaskID: task.ID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorID: authorID, Content: req.Content, ClientRequestID: requestID,
	})
	if isUniqueViolation(err) {
		if existing, loadErr := h.Queries.GetTaskSupplementByRequest(r.Context(), lookup); loadErr == nil {
			h.notifyTaskSupplementAvailable(task)
			h.writeExistingTaskSupplement(w, r, existing)
			return
		} else if !errors.Is(loadErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to check additional message")
			return
		}
		writeError(w, http.StatusConflict, "client_request_id is already in use")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "task_supplement_turn_ended", "this run has ended")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create additional message")
		return
	}
	comment := db.Comment{
		ID: created.ID, IssueID: created.IssueID, AuthorType: created.AuthorType,
		AuthorID: created.AuthorID, Content: created.Content, Type: created.Type,
		CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt, ParentID: created.ParentID,
		WorkspaceID: created.WorkspaceID, ResolvedAt: created.ResolvedAt,
		ResolvedByType: created.ResolvedByType, ResolvedByID: created.ResolvedByID,
		SourceTaskID: created.SourceTaskID, QuickActionID: created.QuickActionID,
		ViaPluginID: created.ViaPluginID, Revision: created.Revision,
		RecoverySettledAt: created.RecoverySettledAt, DeletedAt: created.DeletedAt,
	}
	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	applyCommentSupplements(&resp, []CommentSupplementResponse{{
		TaskID:        uuidToString(created.SupplementTaskID),
		AgentID:       uuidToString(task.AgentID),
		Status:        created.SupplementStatus,
		FailureReason: textToPtr(created.SupplementFailureReason),
		DeliveredAt:   timestampToPtr(created.SupplementDeliveredAt),
	}})
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "member", uuidToString(authorID), map[string]any{
		"comment": resp, "issue_title": issue.Title, "issue_revision": created.IssueRevision,
	})
	h.notifyTaskSupplementAvailable(task)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) notifyTaskSupplementAvailable(task db.AgentTaskQueue) {
	if h.DaemonTaskSupplement == nil || task.Status != "running" || !task.RuntimeID.Valid {
		return
	}
	h.DaemonTaskSupplement.NotifyTaskSupplementAvailable(uuidToString(task.RuntimeID), uuidToString(task.ID))
}

func (h *Handler) writeExistingTaskSupplement(w http.ResponseWriter, r *http.Request, existing db.TaskSupplement) {
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID: existing.CommentID, WorkspaceID: existing.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load additional message")
		return
	}
	resp := commentToResponse(comment, nil, nil)
	applyCommentSupplements(&resp, h.listCommentSupplements(r.Context(), existing.WorkspaceID, []pgtype.UUID{existing.CommentID})[uuidToString(existing.CommentID)])
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RetryTaskSupplement(w http.ResponseWriter, r *http.Request) {
	issue, task, _, ok := h.loadTaskSupplementTarget(w, r)
	if !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment id")
	if !ok {
		return
	}
	row, err := h.Queries.RetryTaskSupplement(r.Context(), db.RetryTaskSupplementParams{
		CommentID: commentID, WorkspaceID: issue.WorkspaceID, TaskID: task.ID, IssueID: issue.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := h.Queries.GetTaskSupplementForRun(r.Context(), db.GetTaskSupplementForRunParams{
			CommentID: commentID, TaskID: task.ID, WorkspaceID: issue.WorkspaceID,
		})
		if loadErr == nil && existing.Status == "pending" {
			row = existing // duplicate retry is an idempotent success
			err = nil
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "task_supplement_not_retryable", "this message cannot be retried")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry additional message")
		return
	}
	h.publishTaskSupplementUpdate(r, row)
	h.notifyTaskSupplementAvailable(task)
	writeJSON(w, http.StatusOK, supplementReceipt(row))
}

type ackTaskSupplementRequest struct {
	Delivered bool   `json:"delivered"`
	Error     string `json:"error,omitempty"`
}

func (h *Handler) ClaimTaskSupplement(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}
	var req struct {
		Capabilities []string `json:"capabilities"`
	}
	// Older daemons send {} and frame every message as a human's, so they are
	// never handed an agent's message (see ClaimNextTaskSupplement). A
	// malformed body only loses the capability.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req)
	rendersAuthorType := slices.Contains(req.Capabilities, protocol.DaemonCapabilityTaskSupplementAuthorV1)
	row, err := h.Queries.ClaimNextTaskSupplement(r.Context(), db.ClaimNextTaskSupplementParams{
		TaskID: parseUUID(taskID), IncludeAgentAuthored: rendersAuthorType,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim additional message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"comment_id": uuidToString(row.CommentID), "author_type": row.AuthorType, "author_name": row.AuthorName,
		"content": row.Content,
	})
}

func (h *Handler) AckTaskSupplement(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment id")
	if !ok {
		return
	}
	var req ackTaskSupplementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var row db.TaskSupplement
	var err error
	if req.Delivered {
		row, err = h.Queries.AckTaskSupplementDelivered(r.Context(), db.AckTaskSupplementDeliveredParams{
			TaskID: parseUUID(taskID), CommentID: commentID,
		})
	} else {
		reason := stableTaskSupplementFailureReason(req.Error)
		row, err = h.Queries.AckTaskSupplementFailed(r.Context(), db.AckTaskSupplementFailedParams{
			TaskID: parseUUID(taskID), CommentID: commentID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "additional message is no longer deliverable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to acknowledge additional message")
		return
	}
	h.publishTaskSupplementUpdate(r, row)
	writeJSON(w, http.StatusOK, supplementReceipt(row))
}

func stableTaskSupplementFailureReason(reason string) string {
	reason = strings.TrimSpace(sanitizeNullBytes(reason))
	switch reason {
	case protocol.TaskSupplementFailureTurnNotStarted, protocol.TaskSupplementFailureTimeout,
		protocol.TaskSupplementFailureTurnEnded, protocol.TaskSupplementFailureProviderRejected:
		return reason
	default:
		// Provider and Go errors are private daemon diagnostics. Never persist or
		// rebroadcast them to workspace members, and never byte-truncate UTF-8.
		return protocol.TaskSupplementFailureProviderRejected
	}
}

func (h *Handler) publishTaskSupplementUpdate(r *http.Request, row db.TaskSupplement) {
	h.publishCommentSupplementUpdate(r.Context(), row.WorkspaceID, row.CommentID)
}

// publishCommentSupplementUpdate rebroadcasts a comment with every receipt it
// carries, so a change to one run's delivery never hides another run's.
func (h *Handler) publishCommentSupplementUpdate(ctx context.Context, workspaceID, commentID pgtype.UUID) {
	comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: commentID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return
	}
	resp := commentToResponse(comment, nil, nil)
	applyCommentSupplements(&resp, h.listCommentSupplements(ctx, workspaceID, []pgtype.UUID{commentID})[uuidToString(commentID)])
	h.publish(protocol.EventCommentUpdated, uuidToString(workspaceID), "system", "", map[string]any{"comment": resp})
}
