package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Environment commands let a member stop, restart, start or read the log of
// an issue's environment from the sidebar. The server cannot reach a daemon
// directly, so a command is a row the daemon claims after a pending-work hint
// and answers with a result. Every replica sees the same rows, and control
// rows are kept as the record of who did what.
const (
	// A command the daemon has not claimed in this window is reported to the
	// member as unanswered and is never carried out afterwards.
	environmentCommandPendingWindow = 30 * time.Second
	// A claimed command covers a stop's grace period plus a start; past this
	// the machine went away mid-command.
	environmentCommandClaimedWindow = 2 * time.Minute
	// Log reads carry output, so they are deleted soon after.
	environmentLogCommandRetention = 10 * time.Minute

	maxEnvironmentLogBytes        = 256 * 1024
	maxEnvironmentCommandError    = 500
	maxDeclaredEnvironments       = 50
	maxDeclaredEnvironmentStart   = 500
	maxDeclaredEnvironmentRepoLen = 200
)

var declaredEnvironmentNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// DeclaredEnvironment is one environment a repository declares.
type DeclaredEnvironment struct {
	// Repo is the repository's directory name inside the task's checkout.
	Repo  string `json:"repo"`
	Name  string `json:"name"`
	Label string `json:"label"`
	// Start is the command, shown so a member knows what will run.
	Start string `json:"start"`
}

type IssueEnvironmentConfigResponse struct {
	RuntimeID    string                `json:"runtime_id"`
	TaskID       string                `json:"task_id"`
	ReportedAt   string                `json:"reported_at"`
	CanControl   bool                  `json:"can_control"`
	Environments []DeclaredEnvironment `json:"environments"`
}

type IssueEnvironmentActionResponse struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Status is pending, succeeded, failed or expired (never answered).
	Status        string  `json:"status"`
	Error         string  `json:"error"`
	EnvironmentID *string `json:"environment_id"`
	RuntimeID     string  `json:"runtime_id"`
	Repo          string  `json:"repo"`
	Name          string  `json:"name"`
	ActorType     string  `json:"actor_type"`
	ActorID       *string `json:"actor_id"`
	CreatedAt     string  `json:"created_at"`
	CompletedAt   *string `json:"completed_at"`
}

type IssueEnvironmentCommandResponse struct {
	IssueEnvironmentActionResponse
	// Output is the log a logs command read.
	Output *string `json:"output"`
}

type StartIssueEnvironmentRequest struct {
	TaskID string `json:"task_id"`
	Repo   string `json:"repo"`
	Name   string `json:"name"`
}

func environmentCommandStatus(status string, createdAt, claimedAt pgtype.Timestamptz, now time.Time) string {
	switch status {
	case "pending":
		if now.Sub(createdAt.Time) > environmentCommandPendingWindow {
			return "expired"
		}
		return "pending"
	case "claimed":
		if claimedAt.Valid && now.Sub(claimedAt.Time) > environmentCommandClaimedWindow {
			return "expired"
		}
		return "pending"
	}
	return status
}

func issueEnvironmentActionToResponse(c db.ListIssueEnvironmentControlsRow, now time.Time) IssueEnvironmentActionResponse {
	return IssueEnvironmentActionResponse{
		ID:            uuidToString(c.ID),
		Action:        c.Action,
		Status:        environmentCommandStatus(c.Status, c.CreatedAt, c.ClaimedAt, now),
		Error:         c.Error,
		EnvironmentID: uuidToPtr(c.EnvironmentID),
		RuntimeID:     uuidToString(c.RuntimeID),
		Repo:          c.Repo,
		Name:          c.Name,
		ActorType:     c.ActorType,
		ActorID:       uuidToPtr(c.ActorID),
		CreatedAt:     timestampToString(c.CreatedAt),
		CompletedAt:   timestampToPtr(c.CompletedAt),
	}
}

func issueEnvironmentCommandToResponse(c db.IssueEnvironmentCommand, now time.Time) IssueEnvironmentCommandResponse {
	resp := IssueEnvironmentCommandResponse{IssueEnvironmentActionResponse: issueEnvironmentActionToResponse(db.ListIssueEnvironmentControlsRow{
		ID: c.ID, WorkspaceID: c.WorkspaceID, IssueID: c.IssueID, RuntimeID: c.RuntimeID,
		EnvironmentID: c.EnvironmentID, TaskID: c.TaskID, Action: c.Action, LocalKey: c.LocalKey,
		Repo: c.Repo, Name: c.Name, ActorType: c.ActorType, ActorID: c.ActorID, Status: c.Status,
		Error: c.Error, ResultKey: c.ResultKey, CreatedAt: c.CreatedAt, ClaimedAt: c.ClaimedAt,
		CompletedAt: c.CompletedAt,
	}, now)}
	if c.Action == "logs" && c.Status == "succeeded" && c.Output.Valid {
		output := c.Output.String
		resp.Output = &output
	}
	return resp
}

// environmentControlChecker reports, per runtime, whether the caller may stop,
// restart or start environments on it: a human member who may use the
// machine. Private machines stay their owner's, as for running agents on them.
func (h *Handler) environmentControlChecker(r *http.Request, issue db.Issue) func(pgtype.UUID) bool {
	if isMachineCredentialActor(r) {
		return func(pgtype.UUID) bool { return false }
	}
	member, err := h.getWorkspaceMember(r.Context(), requestUserID(r), uuidToString(issue.WorkspaceID))
	if err != nil {
		return func(pgtype.UUID) bool { return false }
	}
	memo := make(map[string]bool)
	return func(runtimeID pgtype.UUID) bool {
		if !runtimeID.Valid {
			return false
		}
		key := uuidToString(runtimeID)
		if allowed, ok := memo[key]; ok {
			return allowed
		}
		rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceIssue, runtimeID)
		allowed := err == nil && rt.WorkspaceID == issue.WorkspaceID && canUseRuntimeForAgent(member, rt)
		memo[key] = allowed
		return allowed
	}
}

// environmentRuntime loads the machine a command targets and checks it can
// take one now. control adds the permission to act on the machine; reading a
// log only needs the issue.
func (h *Handler) environmentRuntime(w http.ResponseWriter, r *http.Request, issue db.Issue, runtimeID pgtype.UUID, control bool) (db.AgentRuntime, bool) {
	if !runtimeID.Valid {
		writeError(w, http.StatusConflict, "the machine this environment ran on is no longer registered")
		return db.AgentRuntime{}, false
	}
	rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceIssue, runtimeID)
	if err != nil || rt.WorkspaceID != issue.WorkspaceID {
		writeError(w, http.StatusConflict, "the machine this environment ran on is no longer registered")
		return db.AgentRuntime{}, false
	}
	if control {
		member, ok := h.requireWorkspaceMember(w, r, uuidToString(issue.WorkspaceID), "issue not found")
		if !ok {
			return db.AgentRuntime{}, false
		}
		if !canUseRuntimeForAgent(member, rt) {
			writeError(w, http.StatusForbidden, "you cannot use this machine")
			return db.AgentRuntime{}, false
		}
	}
	if rt.Status != "online" {
		writeError(w, http.StatusConflict, "the machine is offline")
		return db.AgentRuntime{}, false
	}
	return rt, true
}

func (h *Handler) StopIssueEnvironment(w http.ResponseWriter, r *http.Request) {
	h.commandIssueEnvironment(w, r, "stop")
}

func (h *Handler) RestartIssueEnvironment(w http.ResponseWriter, r *http.Request) {
	h.commandIssueEnvironment(w, r, "restart")
}

func (h *Handler) ReadIssueEnvironmentLogs(w http.ResponseWriter, r *http.Request) {
	h.commandIssueEnvironment(w, r, "logs")
}

func (h *Handler) commandIssueEnvironment(w http.ResponseWriter, r *http.Request, action string) {
	control := action != "logs"
	if control && isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	envID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "environmentId"), "environment_id")
	if !ok {
		return
	}
	env, err := h.Queries.GetIssueEnvironment(r.Context(), db.GetIssueEnvironmentParams{
		ID: envID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "environment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load environment")
		return
	}
	switch {
	case action == "stop" && env.EndedAt.Valid:
		writeError(w, http.StatusConflict, "the environment is not running")
		return
	case action != "stop" && env.Source != "config":
		writeError(w, http.StatusConflict, "only environments the repository declares can be restarted or have a log")
		return
	case !env.Controllable:
		writeError(w, http.StatusConflict, "update Multica on this machine to control its environments from the sidebar")
		return
	}
	rt, ok := h.environmentRuntime(w, r, issue, env.RuntimeID, control)
	if !ok {
		return
	}
	now := time.Now()
	if control {
		open, err := h.Queries.HasOpenIssueEnvironmentControl(r.Context(), db.HasOpenIssueEnvironmentControlParams{
			IssueID:       issue.ID,
			EnvironmentID: env.ID,
			PendingAfter:  pgtype.Timestamptz{Time: now.Add(-environmentCommandPendingWindow), Valid: true},
			ClaimedAfter:  pgtype.Timestamptz{Time: now.Add(-environmentCommandClaimedWindow), Valid: true},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to queue the command")
			return
		}
		if open {
			writeError(w, http.StatusConflict, "the environment is already being stopped or restarted")
			return
		}
	}
	h.queueEnvironmentCommand(w, r, issue, rt, db.CreateIssueEnvironmentCommandParams{
		EnvironmentID: env.ID,
		TaskID:        env.TaskID,
		Action:        action,
		LocalKey:      env.LocalKey,
		Name:          env.Name,
	})
}

// StartIssueEnvironment starts an environment the latest run on a machine
// declared, in that run's directory.
func (h *Handler) StartIssueEnvironment(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req StartIssueEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, req.TaskID, "task_id")
	if !ok {
		return
	}
	cfg, err := h.Queries.GetIssueEnvironmentConfig(r.Context(), db.GetIssueEnvironmentConfigParams{
		TaskID: taskID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no declared environments for this run")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load declared environments")
		return
	}
	var declared []DeclaredEnvironment
	_ = json.Unmarshal(cfg.Environments, &declared)
	found := false
	for _, d := range declared {
		if d.Repo == req.Repo && d.Name == req.Name {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "the run does not declare this environment")
		return
	}
	rt, ok := h.environmentRuntime(w, r, issue, cfg.RuntimeID, true)
	if !ok {
		return
	}
	h.queueEnvironmentCommand(w, r, issue, rt, db.CreateIssueEnvironmentCommandParams{
		TaskID: cfg.TaskID,
		Action: "start",
		Repo:   req.Repo,
		Name:   req.Name,
	})
}

func (h *Handler) queueEnvironmentCommand(w http.ResponseWriter, r *http.Request, issue db.Issue, rt db.AgentRuntime, params db.CreateIssueEnvironmentCommandParams) {
	params.WorkspaceID = issue.WorkspaceID
	params.IssueID = issue.ID
	params.RuntimeID = rt.ID
	params.ActorType = "member"
	params.ActorID = optionalActorUUID(requestUserID(r))
	cmd, err := h.Queries.CreateIssueEnvironmentCommand(r.Context(), params)
	if err != nil {
		slog.Warn("queue environment command failed", append(logger.RequestAttrs(r), "error", err, "action", params.Action)...)
		writeError(w, http.StatusInternalServerError, "failed to queue the command")
		return
	}
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindEnvironmentCommand)
	if params.Action != "logs" {
		h.publishEnvironmentChanged(uuidToString(issue.WorkspaceID), issue.ID)
	}
	writeJSON(w, http.StatusAccepted, issueEnvironmentCommandToResponse(cmd, time.Now()))
}

// GetIssueEnvironmentCommand reports how a command went; a log read carries
// its output.
func (h *Handler) GetIssueEnvironmentCommand(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	cmdID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commandId"), "command_id")
	if !ok {
		return
	}
	cmd, err := h.Queries.GetIssueEnvironmentCommand(r.Context(), db.GetIssueEnvironmentCommandParams{
		ID: cmdID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load command")
		return
	}
	writeJSON(w, http.StatusOK, issueEnvironmentCommandToResponse(cmd, time.Now()))
}

func (h *Handler) publishEnvironmentChanged(workspaceID string, issueID pgtype.UUID) {
	h.publish(protocol.EventEnvironmentChanged, workspaceID, "system", "", map[string]string{
		"issue_id": uuidToString(issueID),
	})
}

// Daemon side.

type DaemonEnvironmentCommand struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	TaskID      string `json:"task_id"`
	LocalKey    string `json:"local_key"`
	Repo        string `json:"repo"`
	Name        string `json:"name"`
}

type ClaimEnvironmentCommandsResponse struct {
	Commands []DaemonEnvironmentCommand `json:"commands"`
}

type EnvironmentCommandResultRequest struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	// Output is the log a logs command read.
	Output *string `json:"output"`
	// EnvironmentKey names the environment a start or restart produced.
	EnvironmentKey string `json:"environment_key"`
}

type ReportTaskEnvironmentConfigsRequest struct {
	Environments []DeclaredEnvironment `json:"environments"`
}

// ClaimEnvironmentCommands hands a daemon the commands queued for one of its
// runtimes.
func (h *Handler) ClaimEnvironmentCommands(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	rows, err := h.Queries.ClaimIssueEnvironmentCommands(r.Context(), db.ClaimIssueEnvironmentCommandsParams{
		RuntimeID:    rt.ID,
		PendingAfter: pgtype.Timestamptz{Time: time.Now().Add(-environmentCommandPendingWindow), Valid: true},
	})
	if err != nil {
		slog.Warn("claim environment commands failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to claim environment commands")
		return
	}
	out := make([]DaemonEnvironmentCommand, 0, len(rows))
	for _, c := range rows {
		out = append(out, DaemonEnvironmentCommand{
			ID:          uuidToString(c.ID),
			Action:      c.Action,
			WorkspaceID: uuidToString(c.WorkspaceID),
			IssueID:     uuidToString(c.IssueID),
			TaskID:      uuidToString(c.TaskID),
			LocalKey:    c.LocalKey,
			Repo:        c.Repo,
			Name:        c.Name,
		})
	}
	writeJSON(w, http.StatusOK, ClaimEnvironmentCommandsResponse{Commands: out})
}

// ReportEnvironmentCommandResult records how a claimed command went.
func (h *Handler) ReportEnvironmentCommandResult(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	cmdID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commandId"), "command_id")
	if !ok {
		return
	}
	var req EnvironmentCommandResultRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEnvironmentLogBytes+64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.CompleteIssueEnvironmentCommandParams{
		ID:        cmdID,
		RuntimeID: rt.ID,
		Status:    "succeeded",
		ResultKey: strings.TrimSpace(req.EnvironmentKey),
	}
	if len(params.ResultKey) > 128 {
		params.ResultKey = ""
	}
	if !req.OK {
		params.Status = "failed"
		params.Error = truncateRunes(redact.Text(strings.TrimSpace(req.Error)), maxEnvironmentCommandError)
		if params.Error == "" {
			params.Error = "the machine could not carry out the command"
		}
	}
	if req.OK && req.Output != nil {
		output := redact.Text(strings.ToValidUTF8(*req.Output, "\uFFFD"))
		if len(output) > maxEnvironmentLogBytes {
			output = strings.ToValidUTF8(output[len(output)-maxEnvironmentLogBytes:], "")
		}
		params.Output = pgtype.Text{String: output, Valid: true}
	}
	cmd, err := h.Queries.CompleteIssueEnvironmentCommand(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		slog.Warn("complete environment command failed", "command_id", uuidToString(cmdID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record the result")
		return
	}
	// A log read is collected by the member polling its command.
	if cmd.Action != "logs" {
		if cmd.Status == "succeeded" {
			h.recordEnvironmentActivity(r.Context(), cmd)
		}
		h.publishEnvironmentChanged(uuidToString(cmd.WorkspaceID), cmd.IssueID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) recordEnvironmentActivity(ctx context.Context, cmd db.IssueEnvironmentCommand) {
	details := map[string]string{"action": cmd.Action, "name": cmd.Name}
	if cmd.EnvironmentID.Valid {
		if env, err := h.Queries.GetIssueEnvironment(ctx, db.GetIssueEnvironmentParams{
			ID: cmd.EnvironmentID, IssueID: cmd.IssueID, WorkspaceID: cmd.WorkspaceID,
		}); err == nil {
			details["label"] = env.Label
			details["url"] = env.Url
		}
	}
	raw, _ := json.Marshal(details)
	actorID := uuidToString(cmd.ActorID)
	activity, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: cmd.WorkspaceID,
		IssueID:     cmd.IssueID,
		ActorType:   strToText(cmd.ActorType),
		ActorID:     cmd.ActorID,
		Action:      "environment_controlled",
		Details:     raw,
	})
	if err != nil {
		slog.Warn("record environment activity failed", "command_id", uuidToString(cmd.ID), "error", err)
		return
	}
	h.publish(protocol.EventActivityCreated, uuidToString(cmd.WorkspaceID), cmd.ActorType, actorID, map[string]any{
		"issue_id": uuidToString(cmd.IssueID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": cmd.ActorType,
			"actor_id":   actorID,
			"action":     activity.Action,
			"details":    json.RawMessage(raw),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})
}

// ReportTaskEnvironmentConfigs records the environments a finished task's
// repositories declare. The latest report per machine is what the sidebar
// offers to start, so an empty list clears what an earlier run declared.
func (h *Handler) ReportTaskEnvironmentConfigs(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var req ReportTaskEnvironmentConfigsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Environments) > maxDeclaredEnvironments {
		writeError(w, http.StatusBadRequest, "too many environments")
		return
	}
	seen := make(map[string]bool, len(req.Environments))
	envs := make([]DeclaredEnvironment, 0, len(req.Environments))
	for _, env := range req.Environments {
		env.Repo = strings.TrimSpace(env.Repo)
		env.Name = strings.TrimSpace(env.Name)
		env.Label = truncateRunes(strings.TrimSpace(env.Label), 120)
		env.Start = truncateRunes(strings.TrimSpace(env.Start), maxDeclaredEnvironmentStart)
		if !declaredEnvironmentNameRE.MatchString(env.Name) {
			writeError(w, http.StatusBadRequest, "invalid environment name "+env.Name)
			return
		}
		if env.Repo == "" || len(env.Repo) > maxDeclaredEnvironmentRepoLen || strings.ContainsAny(env.Repo, `/\`) {
			writeError(w, http.StatusBadRequest, "invalid repository name")
			return
		}
		key := env.Repo + "\x00" + env.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		envs = append(envs, env)
	}
	if !task.IssueID.Valid || !task.RuntimeID.Valid {
		writeJSON(w, http.StatusOK, map[string]int{"accepted": 0})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	raw, _ := json.Marshal(envs)
	if err := h.Queries.UpsertIssueEnvironmentConfig(r.Context(), db.UpsertIssueEnvironmentConfigParams{
		TaskID:       task.ID,
		WorkspaceID:  wsUUID,
		IssueID:      task.IssueID,
		RuntimeID:    task.RuntimeID,
		Environments: raw,
	}); err != nil {
		slog.Warn("record declared environments failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record declared environments")
		return
	}
	h.publishEnvironmentChanged(workspaceID, task.IssueID)
	writeJSON(w, http.StatusOK, map[string]int{"accepted": len(envs)})
}

// SweepEnvironmentLogCommands deletes log reads whose output is no longer
// needed.
func SweepEnvironmentLogCommands(ctx context.Context, queries *db.Queries, now time.Time) (int64, error) {
	return queries.DeleteIssueEnvironmentLogCommandsBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-environmentLogCommandRetention), Valid: true,
	})
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
