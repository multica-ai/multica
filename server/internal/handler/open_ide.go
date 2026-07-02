package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const openIdeIntelliJ = "intellij_idea"

var errOpenIdeHandled = errors.New("open ide request already handled")

type openIssueInIdeRequest struct {
	IDE    string `json:"ide"`
	TaskID string `json:"task_id"`
}

type openIssueInIdeResponse struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id"`
}

type openIdeCommandStatusResponse struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id"`
	Error     string `json:"error"`
}

type openIdeTask struct {
	TaskID        pgtype.UUID
	IssueID       pgtype.UUID
	ParentIssueID pgtype.UUID
	ProjectID     pgtype.UUID
	WorkDir       pgtype.Text
	AgentName     string
	RuntimeID     pgtype.UUID
	DaemonID      pgtype.Text
	RuntimeStatus string
	OwnerID       pgtype.UUID
}

func (h *Handler) OpenIssueInIde(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	var req openIssueInIdeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IDE != openIdeIntelliJ {
		writeError(w, http.StatusBadRequest, "unsupported IDE")
		return
	}

	task, err := h.openIdeTaskForRequest(w, r, issue, strings.TrimSpace(req.TaskID))
	if err != nil {
		if errors.Is(err, errOpenIdeHandled) {
			return
		}
		if isNotFound(err) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":  "no_eligible_task",
				"error": "current issue has no working directory to open",
			})
			return
		}
		slog.Warn("open IDE: failed to resolve task", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve working directory")
		return
	}

	userID := requestUserID(r)
	if !task.OwnerID.Valid || uuidToString(task.OwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the runtime owner can open this task")
		return
	}
	if !task.DaemonID.Valid || strings.TrimSpace(task.DaemonID.String) == "" || task.RuntimeStatus != "online" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":  "daemon_offline",
			"error": "local runtime is offline",
		})
		return
	}
	if !task.WorkDir.Valid || strings.TrimSpace(task.WorkDir.String) == "" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":  "no_eligible_task",
			"error": "current issue has no working directory to open",
		})
		return
	}

	workDir, hasGithubRepo := h.openIdeWorkDir(r, task)
	commandPayload := map[string]string{
		"ide":      openIdeIntelliJ,
		"work_dir": workDir,
	}
	if hasGithubRepo {
		commandPayload["branch_name"] = openIdeBranchName(task)
	}
	payload, err := json.Marshal(commandPayload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode command")
		return
	}

	command, err := h.Queries.CreateDaemonCommand(r.Context(), db.CreateDaemonCommandParams{
		WorkspaceID:     issue.WorkspaceID,
		DaemonID:        task.DaemonID.String,
		RuntimeID:       task.RuntimeID,
		RequesterUserID: parseUUID(userID),
		IssueID:         task.IssueID,
		TaskID:          task.TaskID,
		CommandType:     "open_intellij",
		Payload:         payload,
	})
	if err != nil {
		slog.Warn("open IDE: failed to create daemon command", "issue_id", uuidToString(issue.ID), "task_id", uuidToString(task.TaskID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create command")
		return
	}

	writeJSON(w, http.StatusAccepted, openIssueInIdeResponse{
		CommandID: uuidToString(command.ID),
		Status:    command.Status,
		TaskID:    uuidToString(command.TaskID),
	})
}

func (h *Handler) GetOpenIdeCommandStatus(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	commandID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commandId"), "command_id")
	if !ok {
		return
	}
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	command, err := h.Queries.GetOpenIdeCommandForRequester(r.Context(), db.GetOpenIdeCommandForRequesterParams{
		ID:              commandID,
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		RequesterUserID: parseUUID(userID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		slog.Warn("open IDE: failed to load command status", "issue_id", uuidToString(issue.ID), "command_id", chi.URLParam(r, "commandId"), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load command status")
		return
	}

	errorText := ""
	if command.Error.Valid {
		errorText = command.Error.String
	}
	writeJSON(w, http.StatusOK, openIdeCommandStatusResponse{
		CommandID: uuidToString(command.ID),
		Status:    command.Status,
		TaskID:    uuidToString(command.TaskID),
		Error:     errorText,
	})
}

func (h *Handler) openIdeTaskForRequest(w http.ResponseWriter, r *http.Request, issue db.Issue, taskID string) (openIdeTask, error) {
	if taskID != "" {
		taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
		if !ok {
			return openIdeTask{}, errOpenIdeHandled
		}
		row, err := h.Queries.GetOpenIdeTaskByID(r.Context(), db.GetOpenIdeTaskByIDParams{
			TaskID:      taskUUID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return openIdeTask{}, err
		}
		task := openIdeTaskFromIDRow(row)
		if !openIdeTaskBelongsToRouteIssue(task, issue) {
			return openIdeTask{}, pgx.ErrNoRows
		}
		return task, nil
	}

	row, err := h.Queries.GetLatestOpenIdeTaskForIssue(r.Context(), db.GetLatestOpenIdeTaskForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return openIdeTask{}, err
	}
	return openIdeTaskFromLatestRow(row), nil
}

func openIdeTaskFromLatestRow(row db.GetLatestOpenIdeTaskForIssueRow) openIdeTask {
	return openIdeTask{
		TaskID:        row.TaskID,
		IssueID:       row.IssueID,
		ParentIssueID: row.ParentIssueID,
		ProjectID:     row.ProjectID,
		WorkDir:       row.WorkDir,
		AgentName:     row.AgentName,
		RuntimeID:     row.RuntimeID,
		DaemonID:      row.DaemonID,
		RuntimeStatus: row.RuntimeStatus,
		OwnerID:       row.OwnerID,
	}
}

func openIdeTaskFromIDRow(row db.GetOpenIdeTaskByIDRow) openIdeTask {
	return openIdeTask{
		TaskID:        row.TaskID,
		IssueID:       row.IssueID,
		ParentIssueID: row.ParentIssueID,
		ProjectID:     row.ProjectID,
		WorkDir:       row.WorkDir,
		AgentName:     row.AgentName,
		RuntimeID:     row.RuntimeID,
		DaemonID:      row.DaemonID,
		RuntimeStatus: row.RuntimeStatus,
		OwnerID:       row.OwnerID,
	}
}

func openIdeTaskBelongsToRouteIssue(task openIdeTask, issue db.Issue) bool {
	if uuidToString(task.IssueID) == uuidToString(issue.ID) {
		return true
	}
	return task.ParentIssueID.Valid && uuidToString(task.ParentIssueID) == uuidToString(issue.ID)
}

func (h *Handler) openIdeWorkDir(r *http.Request, task openIdeTask) (string, bool) {
	workDir := strings.TrimSpace(task.WorkDir.String)
	for _, resource := range h.listProjectResourcesForProject(r.Context(), task.ProjectID) {
		if resource.ResourceType != "github_repo" {
			continue
		}
		var ref struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(resource.ResourceRef, &ref); err != nil {
			continue
		}
		if repoName := repoDirNameFromURL(ref.URL); repoName != "" {
			return joinUserPath(workDir, repoName), true
		}
	}
	return workDir, false
}

var openIdeNonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

func openIdeBranchName(task openIdeTask) string {
	return "agent/" + openIdeSanitizeName(task.AgentName) + "/" + openIdeShortID(uuidToString(task.TaskID))
}

func openIdeSanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = openIdeNonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}

func openIdeShortID(id string) string {
	s := strings.ReplaceAll(id, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func repoDirNameFromURL(raw string) string {
	s := strings.Trim(strings.TrimSpace(raw), `"'`)
	s = strings.TrimRight(s, `/\`)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

func joinUserPath(base, child string) string {
	base = strings.TrimRight(strings.TrimSpace(base), `/\`)
	child = strings.Trim(child, `/\`)
	if base == "" {
		return child
	}
	if strings.Contains(base, `\`) {
		return base + `\` + child
	}
	return base + "/" + child
}

type daemonCommandResponse struct {
	ID          string         `json:"id"`
	CommandType string         `json:"command_type"`
	Payload     map[string]any `json:"payload"`
}

func daemonCommandToResponse(command db.DaemonCommand) daemonCommandResponse {
	var payload map[string]any
	if len(command.Payload) > 0 {
		_ = json.Unmarshal(command.Payload, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return daemonCommandResponse{
		ID:          uuidToString(command.ID),
		CommandType: command.CommandType,
		Payload:     payload,
	}
}

func daemonCommandTokenID(r *http.Request) string {
	if middleware.DaemonAuthPathFromContext(r.Context()) != middleware.DaemonAuthPathDaemonToken {
		return ""
	}
	daemonID := strings.TrimSpace(middleware.DaemonIDFromContext(r.Context()))
	if daemonID == "" {
		return ""
	}
	return daemonID
}

type claimDaemonCommandsRequest struct {
	DaemonID string `json:"daemon_id"`
	Limit    int32  `json:"limit"`
}

func (h *Handler) ClaimDaemonCommands(w http.ResponseWriter, r *http.Request) {
	var req claimDaemonCommandsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.DaemonID) == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 5
	}

	var (
		commands []db.DaemonCommand
		err      error
	)
	if authenticatedDaemonID := daemonCommandTokenID(r); authenticatedDaemonID != "" {
		if authenticatedDaemonID != req.DaemonID {
			writeError(w, http.StatusNotFound, "daemon not found")
			return
		}
		commands, err = h.Queries.ClaimDaemonCommands(r.Context(), db.ClaimDaemonCommandsParams{
			DaemonID:   req.DaemonID,
			LimitCount: req.Limit,
		})
	} else {
		userID := requestUserID(r)
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "daemon authentication required")
			return
		}
		commands, err = h.Queries.ClaimOwnedDaemonCommands(r.Context(), db.ClaimOwnedDaemonCommandsParams{
			DaemonID:   req.DaemonID,
			OwnerID:    parseUUID(userID),
			LimitCount: req.Limit,
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim commands")
		return
	}

	resp := make([]daemonCommandResponse, len(commands))
	for i, command := range commands {
		resp[i] = daemonCommandToResponse(command)
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": resp})
}

type completeDaemonCommandRequest struct {
	DaemonID string `json:"daemon_id"`
	Status   string `json:"status"`
	Error    string `json:"error"`
}

func (h *Handler) CompleteDaemonCommand(w http.ResponseWriter, r *http.Request) {
	commandID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commandId"), "command_id")
	if !ok {
		return
	}

	var req completeDaemonCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "completed" && req.Status != "failed" {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	requestedDaemonID := strings.TrimSpace(req.DaemonID)
	if requestedDaemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}

	errText := pgtype.Text{}
	if req.Error != "" {
		errText = pgtype.Text{String: req.Error, Valid: true}
	}
	var (
		command db.DaemonCommand
		err     error
	)
	if authenticatedDaemonID := daemonCommandTokenID(r); authenticatedDaemonID != "" {
		if requestedDaemonID != authenticatedDaemonID {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		command, err = h.Queries.CompleteDaemonCommand(r.Context(), db.CompleteDaemonCommandParams{
			ID:       commandID,
			DaemonID: authenticatedDaemonID,
			Status:   req.Status,
			Error:    errText,
		})
	} else {
		userID := requestUserID(r)
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "daemon authentication required")
			return
		}
		command, err = h.Queries.CompleteOwnedDaemonCommand(r.Context(), db.CompleteOwnedDaemonCommandParams{
			ID:       commandID,
			DaemonID: requestedDaemonID,
			OwnerID:  parseUUID(userID),
			Status:   req.Status,
			Error:    errText,
		})
	}
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to complete command")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":     uuidToString(command.ID),
		"status": command.Status,
	})
}
