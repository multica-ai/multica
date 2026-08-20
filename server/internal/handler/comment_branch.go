package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type commentBranchSnapshot struct {
	Version              int                    `json:"version"`
	CapturedAt           string                 `json:"captured_at"`
	Issue                commentBranchIssue     `json:"issue"`
	Comments             []commentBranchComment `json:"comments"`
	BranchPointCommentID string                 `json:"branch_point_comment_id"`
	RequestedAgentID     *string                `json:"requested_agent_id"`
}

type commentBranchIssue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Revision    int64  `json:"revision"`
}

type commentBranchComment struct {
	ID           string  `json:"id"`
	ParentID     *string `json:"parent_id"`
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	AuthorName   string  `json:"author_name"`
	Content      string  `json:"content"`
	CreatedAt    string  `json:"created_at"`
	SourceTaskID *string `json:"source_task_id"`
}

const (
	maxCommentBranchAncestryDepth = 128
	maxCommentBranchSnapshotBytes = 512 << 10
)

func commentBranchRequestLockID(requestID pgtype.UUID) string {
	// Request UUIDs are supplied by clients and can equal a real agent UUID.
	// Keep request-dedup locks in a separate namespace from issue/agent queue
	// locks so two cross-agent requests cannot acquire the same pair in reverse.
	return "request:" + uuidToString(requestID)
}

func issuePriorityValue(priority string) int32 {
	switch priority {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func commentBranchReplayMatches(task db.AgentTaskQueue, userID, commentID, requestedAgentID pgtype.UUID, hasExplicitAgent bool, contentBase string) bool {
	if !(task.OriginatorUserID.Valid &&
		task.OriginatorUserID == userID &&
		task.BranchPointCommentID.Valid &&
		task.BranchPointCommentID == commentID) {
		return false
	}
	if hasExplicitAgent && task.AgentID != requestedAgentID {
		return false
	}
	var snapshot commentBranchSnapshot
	if err := json.Unmarshal(task.BranchContext, &snapshot); err != nil ||
		len(snapshot.Comments) == 0 ||
		snapshot.Comments[len(snapshot.Comments)-1].Content != contentBase {
		return false
	}
	if hasExplicitAgent {
		return snapshot.RequestedAgentID != nil &&
			*snapshot.RequestedAgentID == uuidToString(requestedAgentID)
	}
	if snapshot.RequestedAgentID != nil {
		return false
	}
	return true
}

func validCommentBranchSnapshot(raw []byte, task db.AgentTaskQueue) bool {
	var snapshot commentBranchSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Version != 1 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.CapturedAt); err != nil {
		return false
	}
	if snapshot.Issue.ID != uuidToString(task.IssueID) ||
		strings.TrimSpace(snapshot.Issue.Identifier) == "" ||
		strings.TrimSpace(snapshot.Issue.Title) == "" ||
		snapshot.Issue.Revision <= 0 ||
		!task.BranchPointCommentID.Valid ||
		snapshot.BranchPointCommentID != uuidToString(task.BranchPointCommentID) ||
		len(snapshot.Comments) == 0 {
		return false
	}
	if snapshot.RequestedAgentID != nil {
		requestedAgentID, err := util.ParseUUID(*snapshot.RequestedAgentID)
		if err != nil || requestedAgentID != task.AgentID {
			return false
		}
	}

	seen := make(map[string]struct{}, len(snapshot.Comments))
	for i, comment := range snapshot.Comments {
		if strings.TrimSpace(comment.ID) == "" ||
			strings.TrimSpace(comment.AuthorType) == "" ||
			strings.TrimSpace(comment.AuthorID) == "" {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, comment.CreatedAt); err != nil {
			return false
		}
		if _, exists := seen[comment.ID]; exists {
			return false
		}
		seen[comment.ID] = struct{}{}
		if i == 0 {
			if comment.ParentID != nil {
				return false
			}
		} else if comment.ParentID == nil || *comment.ParentID != snapshot.Comments[i-1].ID {
			return false
		}
	}

	last := snapshot.Comments[len(snapshot.Comments)-1]
	if last.ID != snapshot.BranchPointCommentID {
		return false
	}
	if task.TriggerCommentID.Valid && task.TriggerCommentID != task.BranchPointCommentID {
		return false
	}
	if task.BranchSourceTaskID.Valid {
		return last.SourceTaskID != nil && *last.SourceTaskID == uuidToString(task.BranchSourceTaskID)
	}
	return last.SourceTaskID == nil
}

func (h *Handler) commentBranchAuthorName(ctx context.Context, q *db.Queries, comment db.Comment) string {
	switch comment.AuthorType {
	case "member":
		if user, err := q.GetUser(ctx, comment.AuthorID); err == nil {
			return user.Name
		}
	case "agent":
		if agent, err := q.GetAgent(ctx, comment.AuthorID); err == nil {
			return agent.Name
		}
	case "system":
		return "System"
	}
	return ""
}

func (h *Handler) CreateCommentBranch(w http.ResponseWriter, r *http.Request) {
	// Independent runs are explicit user actions and are attributed as
	// direct_human below. Keep a handler-level backstop in addition to the
	// router middleware so alternate wiring cannot let a task token or cloud
	// node credential mint work under the owning member's identity.
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment id")
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(workspaceID)); !ok {
		return
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: comment.IssueID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	requestIDText := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	requestID, err := util.ParseUUID(requestIDText)
	if requestIDText == "" || err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be a UUID")
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user identity")
		return
	}
	var req struct {
		AgentID     string  `json:"agent_id,omitempty"`
		ContentBase *string `json:"content_base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ContentBase == nil {
		writeError(w, http.StatusBadRequest, "content_base is required")
		return
	}
	var requestedAgentID pgtype.UUID
	hasExplicitAgent := req.AgentID != ""
	if hasExplicitAgent {
		requestedAgentID, ok = parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
	}
	if existing, err := h.Queries.GetCommentBranchTaskByRequest(r.Context(), db.GetCommentBranchTaskByRequestParams{IssueID: issue.ID, BranchRequestID: requestID}); err == nil {
		if !commentBranchReplayMatches(existing, userUUID, comment.ID, requestedAgentID, hasExplicitAgent, *req.ContentBase) {
			writeErrorCode(w, http.StatusConflict, "idempotency_key_conflict", "Idempotency-Key was already used with another comment branch request")
			return
		}
		h.writeCommentBranchResponse(w, http.StatusOK, existing, workspaceID)
		return
	}
	agentID := requestedAgentID
	candidateSquadID := pgtype.UUID{}
	if !hasExplicitAgent {
		if comment.SourceTaskID.Valid {
			if task, taskErr := h.Queries.GetAgentTask(r.Context(), comment.SourceTaskID); taskErr == nil && task.IssueID == issue.ID {
				agentID = task.AgentID
			}
		}
		if !agentID.Valid && issue.AssigneeType.Valid && issue.AssigneeID.Valid {
			switch issue.AssigneeType.String {
			case "agent":
				agentID = issue.AssigneeID
			case "squad":
				if squad, squadErr := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: workspaceID}); squadErr == nil && !squad.ArchivedAt.Valid {
					agentID = squad.LeaderID
					candidateSquadID = squad.ID
				}
			}
		}
	}
	if !agentID.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, "branch_target_unavailable", "no runnable agent is available for this comment")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCommentBranchWorkspace(r.Context(), workspaceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	// Serialize retries even if a caller changes agent_id between attempts.
	if err := qtx.LockCommentBranchQueue(r.Context(), db.LockCommentBranchQueueParams{IssueID: uuidToString(issue.ID), AgentID: commentBranchRequestLockID(requestID)}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	if existing, err := qtx.GetCommentBranchTaskByRequest(r.Context(), db.GetCommentBranchTaskByRequestParams{IssueID: issue.ID, BranchRequestID: requestID}); err == nil {
		if !commentBranchReplayMatches(existing, userUUID, comment.ID, requestedAgentID, hasExplicitAgent, *req.ContentBase) {
			writeErrorCode(w, http.StatusConflict, "idempotency_key_conflict", "Idempotency-Key was already used with another comment branch request")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create comment branch")
			return
		}
		h.writeCommentBranchResponse(w, http.StatusOK, existing, workspaceID)
		return
	}
	var lockedSquad db.Squad
	if candidateSquadID.Valid {
		lockedSquad, err = qtx.LockSquadForAutopilotAssignment(r.Context(), db.LockSquadForAutopilotAssignmentParams{
			ID: candidateSquadID, WorkspaceID: workspaceID,
		})
		if err != nil || lockedSquad.ArchivedAt.Valid || lockedSquad.LeaderID != agentID {
			writeErrorCode(w, http.StatusConflict, "branch_target_changed", "the automatic branch target changed; refresh and try again")
			return
		}
	}
	// Do not pre-lock the agent row here. Runtime merge deliberately locks
	// runtime -> agent, while task ownership writes use compatible KEY SHARE
	// locks inside lock_task_owner_rows. Taking FOR UPDATE on the agent before
	// that fence would invert the runtime-merge order and can deadlock.
	lockedIssue, err := qtx.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	path, err := qtx.ListCommentBranchPathForUpdate(r.Context(), db.ListCommentBranchPathForUpdateParams{
		CommentID: comment.ID, IssueID: issue.ID, WorkspaceID: workspaceID,
		MaxDepth: maxCommentBranchAncestryDepth,
	})
	if err != nil || len(path) == 0 {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if len(path) > maxCommentBranchAncestryDepth {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "branch_context_too_large", "comment ancestry is too deep to branch")
		return
	}
	if path[0].ParentID.Valid {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	lockedComment := path[len(path)-1]
	if lockedComment.Content != *req.ContentBase {
		writeEditConflict(w, "comment", lockedComment.ID)
		return
	}
	// Automatic target selection is repeated from the locked source rows. This
	// prevents a concurrent comment edit or assignee change from creating the
	// branch for a stale target. The selected comment uses an exact content
	// baseline; unrelated issue mutations do not create false conflicts.
	branchSourceTaskID := pgtype.UUID{}
	resolvedAgentID := pgtype.UUID{}
	if lockedComment.SourceTaskID.Valid {
		if task, taskErr := qtx.GetAgentTask(r.Context(), lockedComment.SourceTaskID); taskErr == nil && task.IssueID == lockedIssue.ID {
			branchSourceTaskID = lockedComment.SourceTaskID
			resolvedAgentID = task.AgentID
		}
	}
	if !hasExplicitAgent && !resolvedAgentID.Valid && lockedIssue.AssigneeType.Valid && lockedIssue.AssigneeID.Valid {
		switch lockedIssue.AssigneeType.String {
		case "agent":
			resolvedAgentID = lockedIssue.AssigneeID
		case "squad":
			if candidateSquadID.Valid && lockedIssue.AssigneeID == candidateSquadID {
				resolvedAgentID = lockedSquad.LeaderID
			}
		}
	}
	if !hasExplicitAgent {
		if !resolvedAgentID.Valid || resolvedAgentID != agentID {
			writeErrorCode(w, http.StatusConflict, "branch_target_changed", "the automatic branch target changed; refresh and try again")
			return
		}
	}
	lockedAgent, err := qtx.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil || lockedAgent.Kind != "user" || lockedAgent.ArchivedAt.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, "branch_target_unavailable", "selected agent is unavailable")
		return
	}
	if !h.canInvokeAgent(r.Context(), lockedAgent, "member", userID, userID, uuidToString(workspaceID)) {
		writeErrorCode(w, http.StatusForbidden, "invoke_not_allowed", "selected agent cannot be invoked by this member")
		return
	}
	verdict, err := service.AgentReadiness(r.Context(), qtx, lockedAgent)
	if err != nil || verdict.Blocked() {
		writeErrorCode(w, http.StatusUnprocessableEntity, "branch_target_unavailable", "selected agent runtime is unavailable")
		return
	}
	runtime, err := qtx.GetAgentRuntime(r.Context(), lockedAgent.RuntimeID)
	if err != nil || !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityCommentBranchV1) {
		writeErrorCode(w, http.StatusUnprocessableEntity, "daemon_capability_unsupported", "selected runtime does not support comment branches")
		return
	}
	agent := lockedAgent
	description := ""
	if lockedIssue.Description.Valid {
		description = lockedIssue.Description.String
	}
	var requestedAgentIDText *string
	if hasExplicitAgent {
		value := uuidToString(requestedAgentID)
		requestedAgentIDText = &value
	}
	snapshot := commentBranchSnapshot{
		Version: 1, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Issue:    commentBranchIssue{ID: uuidToString(lockedIssue.ID), Identifier: fmt.Sprintf("%s-%d", h.getIssuePrefix(r.Context(), workspaceID), lockedIssue.Number), Title: lockedIssue.Title, Description: description, Revision: lockedIssue.Revision},
		Comments: make([]commentBranchComment, 0, len(path)), BranchPointCommentID: uuidToString(comment.ID), RequestedAgentID: requestedAgentIDText,
	}
	for _, item := range path {
		sourceTaskID := item.SourceTaskID
		if item.ID == lockedComment.ID {
			sourceTaskID = branchSourceTaskID
		}
		snapshot.Comments = append(snapshot.Comments, commentBranchComment{
			ID: uuidToString(item.ID), ParentID: uuidToPtr(item.ParentID), AuthorType: item.AuthorType,
			AuthorID: uuidToString(item.AuthorID), AuthorName: h.commentBranchAuthorName(r.Context(), qtx, item),
			Content: item.Content, CreatedAt: timestampToString(item.CreatedAt), SourceTaskID: uuidToPtr(sourceTaskID),
		})
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	if len(snapshotJSON) > maxCommentBranchSnapshotBytes {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "branch_context_too_large", "comment branch snapshot is too large")
		return
	}
	if err := qtx.LockCommentBranchQueue(r.Context(), db.LockCommentBranchQueueParams{IssueID: uuidToString(issue.ID), AgentID: uuidToString(agent.ID)}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	created, err := qtx.CreateDeferredCommentBranchTask(r.Context(), db.CreateDeferredCommentBranchTaskParams{
		AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: lockedIssue.ID, Priority: issuePriorityValue(lockedIssue.Priority),
		TriggerCommentID: comment.ID, TriggerSummary: h.TaskService.BuildCommentTriggerSummary(r.Context(), workspaceID, lockedComment.ID),
		OriginatorUserID: userUUID, AccountableUserID: userUUID, OriginatorSource: pgtype.Text{String: "direct_human", Valid: true},
		TriggerEvidenceKind: pgtype.Text{String: "comment_branch", Valid: true}, TriggerEvidenceRefID: comment.ID,
		BranchPointCommentID: lockedComment.ID, BranchSourceTaskID: branchSourceTaskID,
		BranchContext: snapshotJSON, BranchRequestID: requestID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErrorCode(w, http.StatusConflict, "branch_target_changed", "the selected branch target changed; refresh and try again")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	promotionTx, err := tx.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue comment branch")
		return
	}
	promoted, promoteErr := h.Queries.WithTx(promotionTx).PromoteNextDeferredCommentBranch(r.Context(), db.PromoteNextDeferredCommentBranchParams{IssueID: issue.ID, AgentID: agent.ID})
	if promoteErr == nil {
		promoteErr = promotionTx.Commit(r.Context())
	} else {
		_ = promotionTx.Rollback(r.Context())
		var pgErr *pgconn.PgError
		if errors.Is(promoteErr, pgx.ErrNoRows) || (errors.As(promoteErr, &pgErr) && pgErr.Code == "23505") {
			promoteErr = pgx.ErrNoRows
		}
	}
	if promoteErr != nil && !errors.Is(promoteErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to queue comment branch")
		return
	}
	if promoted.ID == created.ID {
		created = promoted
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment branch")
		return
	}
	if promoteErr == nil {
		h.TaskService.AnnounceQueuedTask(r.Context(), promoted)
	}
	h.writeCommentBranchResponse(w, http.StatusCreated, created, workspaceID)
}

func (h *Handler) writeCommentBranchResponse(w http.ResponseWriter, status int, task db.AgentTaskQueue, workspaceID pgtype.UUID) {
	writeJSON(w, status, map[string]any{
		"task":                    taskToResponse(task, uuidToString(workspaceID)),
		"branch_point_comment_id": uuidToString(task.BranchPointCommentID),
		"source_task_id":          uuidToPtr(task.BranchSourceTaskID),
	})
}
