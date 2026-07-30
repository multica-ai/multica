package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectFeishuSyncResponse struct {
	State                    string  `json:"state"`
	ProjectBindingID         string  `json:"project_binding_id"`
	InstallationID           string  `json:"installation_id"`
	BotName                  string  `json:"bot_name"`
	AgentID                  string  `json:"agent_id"`
	AgentName                string  `json:"agent_name"`
	ChatID                   *string `json:"chat_id"`
	ChatName                 *string `json:"chat_name"`
	BoundIssueCount          int64   `json:"bound_issue_count"`
	ManualUnboundIssueCount  int64   `json:"manual_unbound_issue_count"`
	TotalIssueCount          int64   `json:"total_issue_count"`
	PendingNotificationCount int64   `json:"pending_notification_count"`
	LastSyncedAt             *string `json:"last_synced_at"`
}

type ChannelIssueTopicBindingResponse struct {
	ID                 string  `json:"id"`
	InstallationID     string  `json:"installation_id"`
	ProjectBindingID   *string `json:"project_binding_id"`
	ProjectID          *string `json:"project_id"`
	IssueID            string  `json:"issue_id"`
	ChatID             string  `json:"chat_id"`
	TopicRootMessageID string  `json:"topic_root_message_id"`
	ThreadID           *string `json:"thread_id"`
	BindingSource      string  `json:"binding_source"`
	State              string  `json:"state"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func projectFeishuSyncToResponse(summary lark.ChannelProjectSyncSummary) ProjectFeishuSyncResponse {
	resp := ProjectFeishuSyncResponse{
		State:                    summary.Binding.State,
		ProjectBindingID:         uuidToString(summary.Binding.ID),
		InstallationID:           uuidToString(summary.Binding.InstallationID),
		BotName:                  summary.BotName,
		AgentID:                  uuidToString(summary.AgentID),
		AgentName:                summary.AgentName,
		BoundIssueCount:          summary.BoundIssueCount,
		ManualUnboundIssueCount:  summary.ManualUnboundIssueCount,
		TotalIssueCount:          summary.TotalIssueCount,
		PendingNotificationCount: summary.PendingNotificationCount,
	}
	if summary.Binding.ChannelChatID.Valid {
		value := summary.Binding.ChannelChatID.String
		resp.ChatID = &value
	}
	if summary.Binding.ChannelChatName.Valid {
		value := summary.Binding.ChannelChatName.String
		resp.ChatName = &value
	}
	if summary.LastSyncedAt.Valid {
		value := timestampToString(summary.LastSyncedAt)
		resp.LastSyncedAt = &value
	}
	return resp
}

func channelIssueTopicToResponse(binding lark.ChannelIssueTopicBinding) ChannelIssueTopicBindingResponse {
	resp := ChannelIssueTopicBindingResponse{
		ID:                 uuidToString(binding.ID),
		InstallationID:     uuidToString(binding.InstallationID),
		IssueID:            uuidToString(binding.IssueID),
		ChatID:             binding.ChannelChatID,
		TopicRootMessageID: binding.TopicRootMessageID,
		BindingSource:      binding.BindingSource,
		State:              binding.State,
		CreatedAt:          timestampToString(binding.CreatedAt),
		UpdatedAt:          timestampToString(binding.UpdatedAt),
	}
	if binding.ProjectBindingID.Valid {
		value := uuidToString(binding.ProjectBindingID)
		resp.ProjectBindingID = &value
	}
	if binding.ProjectID.Valid {
		value := uuidToString(binding.ProjectID)
		resp.ProjectID = &value
	}
	if binding.ChannelThreadID.Valid {
		value := binding.ChannelThreadID.String
		resp.ThreadID = &value
	}
	return resp
}

func (h *Handler) loadProjectFeishuSync(ctx context.Context, workspaceID, projectID pgtype.UUID) *ProjectFeishuSyncResponse {
	if h.LarkProjectSync == nil {
		return nil
	}
	summary, err := h.LarkProjectSync.ProjectSummary(ctx, workspaceID, projectID)
	if err != nil {
		return nil
	}
	resp := projectFeishuSyncToResponse(summary)
	return &resp
}

type beginProjectFeishuBindingRequest struct {
	InstallationID string `json:"installation_id"`
}

func (h *Handler) BeginProjectFeishuBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	project, workspaceID, userID, ok := h.projectSyncRequestContext(w, r)
	if !ok {
		return
	}
	var req beginProjectFeishuBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstallationID == "" {
		writeError(w, http.StatusBadRequest, "installation_id is required")
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, req.InstallationID, "installation_id")
	if !ok {
		return
	}
	binding, code, err := h.LarkProjectSync.BeginProjectBinding(
		r.Context(), workspaceID, project.ID, installationID, userID,
	)
	if h.writeProjectSyncError(w, err) {
		return
	}
	status := http.StatusCreated
	confirmationCommand := ""
	expiresInSeconds := 0
	if code != "" {
		confirmationCommand = "/project bind " + code
		expiresInSeconds = 600
	} else {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"binding":              projectFeishuBindingMap(binding),
		"confirmation_code":    code,
		"confirmation_command": confirmationCommand,
		"expires_in_seconds":   expiresInSeconds,
	})
}

func (h *Handler) GetProjectFeishuBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	project, workspaceID, _, ok := h.projectSyncRequestContext(w, r)
	if !ok {
		return
	}
	summary, err := h.LarkProjectSync.ProjectSummary(r.Context(), workspaceID, project.ID)
	if errors.Is(err, lark.ErrProjectSyncNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"feishu_sync": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project synchronization")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feishu_sync": projectFeishuSyncToResponse(summary)})
}

func (h *Handler) DeleteProjectFeishuBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	project, workspaceID, userID, ok := h.projectSyncRequestContext(w, r)
	if !ok {
		return
	}
	summary, err := h.LarkProjectSync.ProjectSummary(r.Context(), workspaceID, project.ID)
	if h.writeProjectSyncError(w, err) {
		return
	}
	if err := h.LarkProjectSync.UnbindProject(r.Context(), workspaceID, summary.Binding.ID, userID); h.writeProjectSyncError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ConfirmProjectFeishuBinding(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusBadRequest, "confirm the binding from the target Feishu group with /project bind <confirmation_code>")
}

func (h *Handler) RetryProjectFeishuTopics(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	project, workspaceID, userID, ok := h.projectSyncRequestContext(w, r)
	if !ok {
		return
	}
	retried, err := h.LarkProjectSync.RetryProjectTopics(r.Context(), workspaceID, project.ID, userID)
	if h.writeProjectSyncError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retried_dead_notifications": retried})
}

func (h *Handler) ListInstallationProjectBindings(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(workspaceID)); !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation_id")
	if !ok {
		return
	}
	items, err := h.LarkProjectSync.ListInstallationProjects(r.Context(), workspaceID, installationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project bindings")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		value := projectFeishuBindingMap(item.Binding)
		value["project_title"] = item.ProjectTitle
		value["agent_name"] = item.AgentName
		value["bot_name"] = item.BotName
		out = append(out, value)
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_bindings": out})
}

func (h *Handler) GetIssueChannelTopicBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	binding, err := h.LarkProjectSync.IssueTopic(r.Context(), issue.WorkspaceID, issue.ID)
	if errors.Is(err, lark.ErrProjectSyncNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"channel_topic_binding": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue topic binding")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel_topic_binding": channelIssueTopicToResponse(binding)})
}

func (h *Handler) DeleteIssueChannelTopicBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.LarkProjectSync.UnbindIssueTopic(r.Context(), issue.WorkspaceID, issue.ID, userUUID); h.writeProjectSyncError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) EnableIssueChannelTopicBinding(w http.ResponseWriter, r *http.Request) {
	if h.LarkProjectSync == nil {
		writeError(w, http.StatusServiceUnavailable, "lark project sync not configured")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	binding, err := h.LarkProjectSync.EnableIssueTopic(r.Context(), issue.WorkspaceID, issue.ID, userUUID)
	if h.writeProjectSyncError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel_topic_binding": channelIssueTopicToResponse(binding)})
}

func (h *Handler) projectSyncRequestContext(w http.ResponseWriter, r *http.Request) (db.Project, pgtype.UUID, pgtype.UUID, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.Project{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return db.Project{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Project{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return db.Project{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	return project, workspaceID, userUUID, true
}

func (h *Handler) writeProjectSyncError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, lark.ErrProjectSyncNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, lark.ErrProjectSyncForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, lark.ErrProjectSyncConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, lark.ErrProjectSyncInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "project synchronization failed")
	}
	return true
}

func projectFeishuBindingMap(binding lark.ChannelProjectBinding) map[string]any {
	out := map[string]any{
		"id":              uuidToString(binding.ID),
		"workspace_id":    uuidToString(binding.WorkspaceID),
		"project_id":      uuidToString(binding.ProjectID),
		"installation_id": uuidToString(binding.InstallationID),
		"state":           binding.State,
		"created_at":      timestampToString(binding.CreatedAt),
		"updated_at":      timestampToString(binding.UpdatedAt),
	}
	if binding.ChannelChatID.Valid {
		out["chat_id"] = binding.ChannelChatID.String
	}
	if binding.ChannelChatName.Valid {
		out["chat_name"] = binding.ChannelChatName.String
	}
	return out
}
