package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type localCLIRun struct {
	ID           pgtype.UUID
	WorkspaceID  pgtype.UUID
	IssueID      pgtype.UUID
	OwnerID      pgtype.UUID
	CLIName      string
	Status       string
	StartedAt    pgtype.Timestamptz
	CompletedAt  pgtype.Timestamptz
	ExitCode     pgtype.Int4
	WorkDir      pgtype.Text
	ContextDir   pgtype.Text
	CommentsMode string
	TopCommentID pgtype.UUID
	Error        pgtype.Text
	Source       pgtype.Text
	SourceKey    pgtype.Text
	CreatedAt    pgtype.Timestamptz
	UpdatedAt    pgtype.Timestamptz
}

type createLocalCLIRunRequest struct {
	CLIName        string `json:"cli_name"`
	WorkDir        string `json:"work_dir"`
	ContextDir     string `json:"context_dir"`
	CommentsMode   string `json:"comments_mode"`
	NoStatusUpdate bool   `json:"no_status_update"`
	Source         string `json:"source"`
	SourceKey      string `json:"source_key"`
}

type updateLocalCLIRunRequest struct {
	Status     string `json:"status"`
	ExitCode   *int32 `json:"exit_code"`
	Error      string `json:"error"`
	ContextDir string `json:"context_dir"`
}

type createLocalCLIMessageRequest struct {
	Type      string         `json:"type"`
	Tool      string         `json:"tool"`
	Content   string         `json:"content"`
	Input     map[string]any `json:"input"`
	Output    string         `json:"output"`
	Source    string         `json:"source"`
	SourceKey string         `json:"source_key"`
}

type updateLocalCLIUsageRequest struct {
	Usage []localCLIUsagePayload `json:"usage"`
}

type localCLIUsagePayload struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

type localCLICommentDisplayRow struct {
	CommentID   pgtype.UUID
	DisplayName string
}

func (h *Handler) CreateLocalCLIRun(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req createLocalCLIRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.CLIName = strings.TrimSpace(req.CLIName)
	if req.CLIName == "" {
		writeError(w, http.StatusBadRequest, "cli_name is required")
		return
	}
	if req.CommentsMode == "" {
		req.CommentsMode = "thread"
	}
	if req.CommentsMode != "thread" && req.CommentsMode != "off" {
		writeError(w, http.StatusBadRequest, "invalid comments_mode")
		return
	}
	req.Source = strings.TrimSpace(req.Source)
	req.SourceKey = strings.TrimSpace(req.SourceKey)

	userID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user_id")
	if !ok {
		return
	}

	if req.Source != "" && req.SourceKey != "" {
		run, found, err := h.loadLocalCLIRunBySource(r.Context(), issue.WorkspaceID, req.Source, req.SourceKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check local run source")
			return
		}
		if found {
			if uuidToString(run.IssueID) != uuidToString(issue.ID) {
				writeError(w, http.StatusConflict, "source_key is already bound to a different issue")
				return
			}
			writeJSON(w, http.StatusOK, localCLIRunToResponse(run))
			return
		}
	}

	var topCommentID pgtype.UUID
	if req.CommentsMode == "thread" {
		content := fmt.Sprintf("Started local `%s` run from `%s`.", req.CLIName, req.WorkDir)
		comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			AuthorType:  "member",
			AuthorID:    userID,
			Content:     content,
			Type:        "system",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create run comment")
			return
		}
		topCommentID = comment.ID
		resp := commentToResponse(comment, nil, nil)
		h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "member", uuidToString(userID), map[string]any{
			"comment":             resp,
			"issue_title":         issue.Title,
			"issue_assignee_type": textToPtr(issue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
			"issue_status":        issue.Status,
			"app_origin":          requestAppOrigin(r),
		})
	}

	if !req.NoStatusUpdate && (issue.Status == "backlog" || issue.Status == "todo") {
		prevStatus := issue.Status
		updatedIssue, err := h.Queries.UpdateIssueStatus(r.Context(), db.UpdateIssueStatusParams{
			ID:          issue.ID,
			Status:      "in_progress",
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update issue status")
			return
		}
		issue = updatedIssue
		h.publishIssueStatusChangedFromComment(r, issue, prevStatus, "member", uuidToString(userID))
	}

	row := h.DB.QueryRow(r.Context(), `
		INSERT INTO local_cli_run (
			workspace_id, issue_id, owner_id, cli_name, status,
			work_dir, context_dir, comments_mode, top_comment_id,
			source, source_key
		) VALUES ($1, $2, $3, $4, 'running', $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''))
		ON CONFLICT (workspace_id, source, source_key)
			WHERE source IS NOT NULL AND source_key IS NOT NULL
			DO NOTHING
		RETURNING id, workspace_id, issue_id, owner_id, cli_name, status,
			started_at, completed_at, exit_code, work_dir, context_dir,
			comments_mode, top_comment_id, error, source, source_key, created_at, updated_at
	`, issue.WorkspaceID, issue.ID, userID, req.CLIName, textOrNil(req.WorkDir), textOrNil(req.ContextDir), req.CommentsMode, uuidOrNil(topCommentID), req.Source, req.SourceKey)
	run, err := scanLocalCLIRun(row)
	if err != nil {
		if req.Source != "" && req.SourceKey != "" && err == pgx.ErrNoRows {
			existing, found, loadErr := h.loadLocalCLIRunBySource(r.Context(), issue.WorkspaceID, req.Source, req.SourceKey)
			if loadErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to load local run source")
				return
			}
			if found {
				if uuidToString(existing.IssueID) != uuidToString(issue.ID) {
					writeError(w, http.StatusConflict, "source_key is already bound to a different issue")
					return
				}
				writeJSON(w, http.StatusOK, localCLIRunToResponse(existing))
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to create local run")
		return
	}

	h.publishLocalCLIRunEvent(protocol.EventTaskDispatch, run)
	writeJSON(w, http.StatusCreated, localCLIRunToResponse(run))
}

func (h *Handler) UpdateLocalCLIRun(w http.ResponseWriter, r *http.Request) {
	run, ok := h.loadLocalCLIRunForUser(w, r, chi.URLParam(r, "runId"))
	if !ok {
		return
	}

	var req updateLocalCLIRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "running" && req.Status != "completed" && req.Status != "failed" && req.Status != "cancelled" {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if isTerminalLocalCLIStatus(run.Status) {
		writeJSON(w, http.StatusOK, localCLIRunToResponse(run))
		return
	}

	completedExpr := "completed_at"
	if req.Status == "completed" || req.Status == "failed" || req.Status == "cancelled" {
		completedExpr = "COALESCE(completed_at, now())"
	}

	var exitCode any
	if req.ExitCode != nil {
		exitCode = *req.ExitCode
	}
	row := h.DB.QueryRow(r.Context(), fmt.Sprintf(`
		UPDATE local_cli_run
		SET status = $2,
		    completed_at = %s,
		    exit_code = COALESCE($3, exit_code),
		    error = NULLIF($4, ''),
		    context_dir = COALESCE(NULLIF($5, ''), context_dir),
		    updated_at = now()
		WHERE id = $1 AND workspace_id = $6
		RETURNING id, workspace_id, issue_id, owner_id, cli_name, status,
			started_at, completed_at, exit_code, work_dir, context_dir,
			comments_mode, top_comment_id, error, source, source_key, created_at, updated_at
	`, completedExpr), run.ID, req.Status, exitCode, req.Error, req.ContextDir, run.WorkspaceID)
	updated, err := scanLocalCLIRun(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update local run")
		return
	}

	if isTerminalLocalCLIStatus(updated.Status) && updated.Status != run.Status {
		h.publishLocalCLIRunEvent(localCLITerminalEvent(updated.Status), updated)
	}
	writeJSON(w, http.StatusOK, localCLIRunToResponse(updated))
}

func (h *Handler) publishLocalCLIRunEvent(eventType string, run localCLIRun) {
	payload := map[string]any{
		"task_id":    uuidToString(run.ID),
		"agent_id":   "",
		"issue_id":   uuidToString(run.IssueID),
		"runtime_id": "",
		"status":     run.Status,
	}
	h.publishTask(eventType, uuidToString(run.WorkspaceID), "member", uuidToString(run.OwnerID), uuidToString(run.ID), payload)
}

func isTerminalLocalCLIStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func localCLITerminalEvent(status string) string {
	switch status {
	case "completed":
		return protocol.EventTaskCompleted
	case "cancelled":
		return protocol.EventTaskCancelled
	default:
		return protocol.EventTaskFailed
	}
}

func (h *Handler) CreateLocalCLIMessage(w http.ResponseWriter, r *http.Request) {
	run, ok := h.loadLocalCLIRunForUser(w, r, chi.URLParam(r, "runId"))
	if !ok {
		return
	}

	var req createLocalCLIMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		req.Type = "raw"
	}
	req.Content = redact.Text(req.Content)
	req.Output = redact.Text(req.Output)
	req.Input = redact.InputMap(req.Input)
	req.Source = strings.TrimSpace(req.Source)
	req.SourceKey = strings.TrimSpace(req.SourceKey)

	if req.Source != "" && req.SourceKey != "" {
		msg, found, err := h.loadLocalCLIMessageBySource(r.Context(), run, req.Source, req.SourceKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check local run message")
			return
		}
		if found {
			writeJSON(w, http.StatusOK, msg)
			return
		}
	}

	input, _ := json.Marshal(req.Input)
	if req.Input == nil {
		input = nil
	}

	row := h.DB.QueryRow(r.Context(), `
		INSERT INTO local_cli_message (run_id, seq, type, tool, content, input, output, source, source_key)
		VALUES (
			$1,
			COALESCE((SELECT MAX(seq) + 1 FROM local_cli_message WHERE run_id = $1), 1),
			$2, NULLIF($3, ''), NULLIF($4, ''), $5::jsonb, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, '')
		)
		ON CONFLICT (run_id, source, source_key)
			WHERE source IS NOT NULL AND source_key IS NOT NULL
			DO NOTHING
		RETURNING id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
	`, run.ID, req.Type, req.Tool, req.Content, input, req.Output, req.Source, req.SourceKey)
	msg, err := scanLocalCLIMessage(row, uuidToString(run.IssueID))
	if err != nil {
		if req.Source != "" && req.SourceKey != "" && err == pgx.ErrNoRows {
			existing, found, loadErr := h.loadLocalCLIMessageBySource(r.Context(), run, req.Source, req.SourceKey)
			if loadErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to load local run message")
				return
			}
			if found {
				writeJSON(w, http.StatusOK, existing)
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to create local run message")
		return
	}

	var commentID pgtype.UUID
	createsThreadReply := (req.Type == "final" || (req.Type == "user_input" && !localCLIMessageIsNonCommentableCommand(req)) || localCLIMessageIsCodexProposedPlan(req)) &&
		run.CommentsMode == "thread" &&
		run.TopCommentID.Valid
	createsReply := createsThreadReply && strings.TrimSpace(req.Content) != ""
	if createsReply {
		parentID := run.TopCommentID
		comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     run.IssueID,
			WorkspaceID: run.WorkspaceID,
			AuthorType:  "member",
			AuthorID:    run.OwnerID,
			Content:     req.Content,
			Type:        "comment",
			ParentID:    parentID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create final reply")
			return
		}
		commentID = comment.ID
		var displayName *string
		if req.Type == "final" || localCLIMessageIsCodexProposedPlan(req) {
			name := h.localCLIDisplayName(r.Context(), run)
			displayName = &name
		}
		resp := commentToResponseWithDisplay(comment, nil, nil, displayName)
		h.publish(protocol.EventCommentCreated, uuidToString(run.WorkspaceID), "member", uuidToString(run.OwnerID), map[string]any{
			"comment":    resp,
			"app_origin": requestAppOrigin(r),
		})
		if _, err := h.DB.Exec(r.Context(), `
			UPDATE local_cli_message
			SET comment_id = $2
			WHERE run_id = $1 AND seq = $3
		`, run.ID, uuidOrNil(commentID), msg.Seq); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to attach local run comment")
			return
		}
	}
	h.publishTask(protocol.EventTaskMessage, uuidToString(run.WorkspaceID), "member", uuidToString(run.OwnerID), uuidToString(run.ID), msg)
	writeJSON(w, http.StatusCreated, msg)
}

func (h *Handler) loadLocalCLIMessageBySource(ctx context.Context, run localCLIRun, source, sourceKey string) (protocol.TaskMessagePayload, bool, error) {
	row := h.DB.QueryRow(ctx, `
		SELECT id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
		FROM local_cli_message
		WHERE run_id = $1 AND source = $2 AND source_key = $3
	`, run.ID, source, sourceKey)
	msg, err := scanLocalCLIMessage(row, uuidToString(run.IssueID))
	if err == nil {
		return msg, true, nil
	}
	if err == pgx.ErrNoRows {
		return protocol.TaskMessagePayload{}, false, nil
	}
	return protocol.TaskMessagePayload{}, false, err
}

func localCLIMessageIsNonCommentableCommand(req createLocalCLIMessageRequest) bool {
	if req.Input == nil {
		return false
	}
	command, _ := req.Input["command"].(bool)
	commentable, _ := req.Input["commentable"].(bool)
	return command && !commentable
}

func localCLIMessageIsCodexProposedPlan(req createLocalCLIMessageRequest) bool {
	if req.Type != "text" || req.Input == nil {
		return false
	}
	kind, _ := req.Input["kind"].(string)
	return kind == "codex_proposed_plan"
}

func (h *Handler) UpdateLocalCLIUsage(w http.ResponseWriter, r *http.Request) {
	run, ok := h.loadLocalCLIRunForUser(w, r, chi.URLParam(r, "runId"))
	if !ok {
		return
	}

	var req updateLocalCLIUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, u := range req.Usage {
		provider := strings.ToLower(strings.TrimSpace(u.Provider))
		if provider != "codex" && provider != "claude" {
			writeError(w, http.StatusBadRequest, "invalid provider")
			return
		}
		model := strings.TrimSpace(u.Model)
		if model == "" {
			model = "unknown"
		}
		if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
			writeError(w, http.StatusBadRequest, "usage tokens must be non-negative")
			return
		}
		if err := h.Queries.UpsertLocalCLIUsage(r.Context(), db.UpsertLocalCLIUsageParams{
			RunID:            run.ID,
			Provider:         provider,
			Model:            model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update local run usage")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) localCLIDisplayName(ctx context.Context, run localCLIRun) string {
	if user, err := h.Queries.GetUser(ctx, run.OwnerID); err == nil {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			name = strings.TrimSpace(user.Email)
		}
		if name != "" {
			return name + "-local-" + run.CLIName
		}
	}
	return "local-" + run.CLIName
}

func (h *Handler) localCLICommentDisplayNames(ctx context.Context, commentIDs []pgtype.UUID) map[string]*string {
	if len(commentIDs) == 0 || h.DB == nil {
		return nil
	}
	rows, err := h.DB.Query(ctx, `
		SELECT lcm.comment_id, COALESCE(NULLIF(u.name, ''), u.email) || '-local-' || lcr.cli_name AS display_name
		FROM local_cli_message lcm
		JOIN local_cli_run lcr ON lcr.id = lcm.run_id
		JOIN "user" u ON u.id = lcr.owner_id
		WHERE lcm.comment_id = ANY($1)
			AND (
				lcm.type = 'final'
				OR (lcm.type = 'text' AND lcm.input->>'kind' = 'codex_proposed_plan')
			)
	`, commentIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]*string)
	for rows.Next() {
		var row localCLICommentDisplayRow
		if err := rows.Scan(&row.CommentID, &row.DisplayName); err != nil {
			continue
		}
		display := row.DisplayName
		result[uuidToString(row.CommentID)] = &display
	}
	return result
}

func (h *Handler) ListLocalCLIMessages(w http.ResponseWriter, r *http.Request) {
	run, ok := h.loadLocalCLIRunForUser(w, r, chi.URLParam(r, "runId"))
	if !ok {
		return
	}
	h.writeLocalCLIMessages(w, r, run)
}

func (h *Handler) loadLocalCLIRunForUser(w http.ResponseWriter, r *http.Request, runID string) (localCLIRun, bool) {
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run_id")
	if !ok {
		return localCLIRun{}, false
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return localCLIRun{}, false
	}

	row := h.DB.QueryRow(r.Context(), `
		SELECT id, workspace_id, issue_id, owner_id, cli_name, status,
			started_at, completed_at, exit_code, work_dir, context_dir,
			comments_mode, top_comment_id, error, source, source_key, created_at, updated_at
		FROM local_cli_run
		WHERE id = $1 AND workspace_id = $2
	`, runUUID, wsUUID)
	run, err := scanLocalCLIRun(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "local run not found")
		return localCLIRun{}, false
	}
	return run, true
}

func (h *Handler) loadLocalCLIRunBySource(ctx context.Context, workspaceID pgtype.UUID, source, sourceKey string) (localCLIRun, bool, error) {
	row := h.DB.QueryRow(ctx, `
		SELECT id, workspace_id, issue_id, owner_id, cli_name, status,
			started_at, completed_at, exit_code, work_dir, context_dir,
			comments_mode, top_comment_id, error, source, source_key, created_at, updated_at
		FROM local_cli_run
		WHERE workspace_id = $1 AND source = $2 AND source_key = $3
	`, workspaceID, source, sourceKey)
	run, err := scanLocalCLIRun(row)
	if err == nil {
		return run, true, nil
	}
	if err == pgx.ErrNoRows {
		return localCLIRun{}, false, nil
	}
	return localCLIRun{}, false, err
}

func (h *Handler) listLocalCLIRunsByIssue(r *http.Request, issue db.Issue, ownerID pgtype.UUID) ([]localCLIRun, error) {
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, workspace_id, issue_id, owner_id, cli_name, status,
			started_at, completed_at, exit_code, work_dir, context_dir,
			comments_mode, top_comment_id, error, source, source_key, created_at, updated_at
		FROM local_cli_run
		WHERE issue_id = $1 AND workspace_id = $2 AND owner_id = $3
		ORDER BY created_at DESC
	`, issue.ID, issue.WorkspaceID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []localCLIRun
	for rows.Next() {
		run, err := scanLocalCLIRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (h *Handler) writeLocalCLIMessages(w http.ResponseWriter, r *http.Request, run localCLIRun) {
	query := `
		SELECT id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
		FROM local_cli_message
		WHERE run_id = $1
		ORDER BY seq ASC
	`
	args := []any{run.ID}
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		query = `
			SELECT id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
			FROM local_cli_message
			WHERE run_id = $1 AND seq > $2
			ORDER BY seq ASC
		`
		var since int
		if _, err := fmt.Sscanf(sinceStr, "%d", &since); err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		args = append(args, since)
	}

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list local run messages")
		return
	}
	defer rows.Close()

	var resp []protocol.TaskMessagePayload
	for rows.Next() {
		msg, err := scanLocalCLIMessage(rows, uuidToString(run.IssueID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read local run messages")
			return
		}
		resp = append(resp, msg)
	}
	writeJSON(w, http.StatusOK, resp)
}

type localCLIRunScanner interface {
	Scan(dest ...any) error
}

func scanLocalCLIRun(row localCLIRunScanner) (localCLIRun, error) {
	var run localCLIRun
	err := row.Scan(
		&run.ID, &run.WorkspaceID, &run.IssueID, &run.OwnerID,
		&run.CLIName, &run.Status, &run.StartedAt, &run.CompletedAt,
		&run.ExitCode, &run.WorkDir, &run.ContextDir, &run.CommentsMode,
		&run.TopCommentID, &run.Error, &run.Source, &run.SourceKey, &run.CreatedAt, &run.UpdatedAt,
	)
	return run, err
}

type localCLIMessageScanner interface {
	Scan(dest ...any) error
}

func scanLocalCLIMessage(row localCLIMessageScanner, issueID string) (protocol.TaskMessagePayload, error) {
	var (
		id, runID  pgtype.UUID
		seq        int32
		msgType    string
		tool       pgtype.Text
		content    pgtype.Text
		inputBytes []byte
		output     pgtype.Text
		createdAt  pgtype.Timestamptz
		source     pgtype.Text
		sourceKey  pgtype.Text
	)
	err := row.Scan(&id, &runID, &seq, &msgType, &tool, &content, &inputBytes, &output, &createdAt, &source, &sourceKey)
	if err != nil {
		return protocol.TaskMessagePayload{}, err
	}
	var input map[string]any
	if len(inputBytes) > 0 {
		_ = json.Unmarshal(inputBytes, &input)
	}
	return protocol.TaskMessagePayload{
		TaskID:  uuidToString(runID),
		IssueID: issueID,
		Seq:     int(seq),
		Type:    msgType,
		Tool:    tool.String,
		Content: content.String,
		Input:   input,
		Output:  output.String,
	}, nil
}

func localCLIRunToResponse(run localCLIRun) map[string]any {
	resp := map[string]any{
		"id":              uuidToString(run.ID),
		"kind":            "local_cli",
		"agent_id":        "",
		"runtime_id":      "",
		"issue_id":        uuidToString(run.IssueID),
		"owner_id":        uuidToString(run.OwnerID),
		"cli_name":        run.CLIName,
		"status":          run.Status,
		"priority":        0,
		"dispatched_at":   nil,
		"started_at":      timestampToPtr(run.StartedAt),
		"completed_at":    timestampToPtr(run.CompletedAt),
		"result":          nil,
		"error":           textToPtr(run.Error),
		"created_at":      timestampToString(run.CreatedAt),
		"trigger_summary": fmt.Sprintf("Local %s", run.CLIName),
		"comments_mode":   run.CommentsMode,
		"work_dir":        run.WorkDir.String,
		"context_dir":     run.ContextDir.String,
	}
	if run.ExitCode.Valid {
		resp["exit_code"] = run.ExitCode.Int32
	}
	if run.TopCommentID.Valid {
		resp["top_comment_id"] = uuidToString(run.TopCommentID)
	}
	if run.Source.Valid {
		resp["source"] = run.Source.String
	}
	if run.SourceKey.Valid {
		resp["source_key"] = run.SourceKey.String
	}
	return resp
}

func textOrNil(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func uuidOrNil(u pgtype.UUID) any {
	if !u.Valid {
		return nil
	}
	return u
}
