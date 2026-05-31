package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type FeishuProjectIntegrationResponse struct {
	ID                          string                               `json:"id,omitempty"`
	WorkspaceID                 string                               `json:"workspace_id,omitempty"`
	ProjectName                 string                               `json:"project_name"`
	ProjectKey                  string                               `json:"project_key"`
	PluginID                    string                               `json:"plugin_id"`
	HasPluginSecret             bool                                 `json:"has_plugin_secret"`
	ActorUserKey                *string                              `json:"actor_user_key"`
	Enabled                     bool                                 `json:"enabled"`
	SyncStory                   bool                                 `json:"sync_story"`
	SyncIssue                   bool                                 `json:"sync_issue"`
	MQLFilter                   string                               `json:"mql_filter"`
	StatusMapping               map[string]string                    `json:"status_mapping"`
	ReverseStatusMapping        map[string]string                    `json:"reverse_status_mapping"`
	AssignOpenItemsToOwnerAgent bool                                 `json:"assign_open_items_to_owner_agent"`
	BusinessLineFieldKey        string                               `json:"business_line_field_key"`
	BusinessLineFieldName       string                               `json:"business_line_field_name"`
	LabelSyncRules              []service.FeishuProjectLabelSyncRule `json:"label_sync_rules"`
	LastSyncedAt                *string                              `json:"last_synced_at"`
	LastError                   *string                              `json:"last_error"`
	CreatedAt                   string                               `json:"created_at,omitempty"`
	UpdatedAt                   string                               `json:"updated_at,omitempty"`
}

type UpdateFeishuProjectIntegrationRequest struct {
	ProjectName                 string                                `json:"project_name"`
	ProjectKey                  string                                `json:"project_key"`
	PluginID                    string                                `json:"plugin_id"`
	PluginSecret                *string                               `json:"plugin_secret"`
	ActorUserKey                *string                               `json:"actor_user_key"`
	Enabled                     bool                                  `json:"enabled"`
	SyncStory                   bool                                  `json:"sync_story"`
	SyncIssue                   bool                                  `json:"sync_issue"`
	MQLFilter                   string                                `json:"mql_filter"`
	StatusMapping               map[string]string                     `json:"status_mapping"`
	ReverseStatusMapping        map[string]string                     `json:"reverse_status_mapping"`
	AssignOpenItemsToOwnerAgent bool                                  `json:"assign_open_items_to_owner_agent"`
	BusinessLineFieldKey        string                                `json:"business_line_field_key"`
	BusinessLineFieldName       string                                `json:"business_line_field_name"`
	LabelSyncRules              *[]service.FeishuProjectLabelSyncRule `json:"label_sync_rules"`
}

type FeishuProjectSyncRunResponse struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	Trigger     string  `json:"trigger"`
	Created     int32   `json:"created"`
	Updated     int32   `json:"updated"`
	Skipped     int32   `json:"skipped"`
	Errors      int32   `json:"errors"`
	Processed   int32   `json:"processed"`
	Total       int32   `json:"total"`
	CurrentPage int32   `json:"current_page"`
	CurrentType string  `json:"current_type"`
	Error       *string `json:"error"`
	StartedAt   *string `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
}

type FeishuProjectSyncResponse struct {
	Status  string                           `json:"status"`
	Run     *FeishuProjectSyncRunResponse    `json:"run,omitempty"`
	Summary service.FeishuProjectSyncSummary `json:"summary"`
	Error   string                           `json:"error,omitempty"`
}

type SyncFeishuProjectIntegrationRequest struct {
	WorkItemID string `json:"work_item_id"`
}

func (h *Handler) GetFeishuProjectIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, FeishuProjectIntegrationResponse{
				Enabled:                     false,
				SyncStory:                   false,
				SyncIssue:                   true,
				MQLFilter:                   "",
				StatusMapping:               defaultFeishuProjectStatusMapping(),
				ReverseStatusMapping:        defaultFeishuProjectReverseStatusMapping(),
				AssignOpenItemsToOwnerAgent: false,
				LabelSyncRules:              []service.FeishuProjectLabelSyncRule{},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load Feishu Project integration")
		return
	}
	writeJSON(w, http.StatusOK, feishuProjectIntegrationToResponse(cfg))
}

func (h *Handler) UpdateFeishuProjectIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	var req UpdateFeishuProjectIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectKey := feishuProjectNameFromRequest(req)
	pluginID := strings.TrimSpace(req.PluginID)
	if projectKey == "" || pluginID == "" {
		writeError(w, http.StatusBadRequest, "project_name and plugin_id are required")
		return
	}
	pluginSecret := ""
	if req.PluginSecret != nil {
		pluginSecret = strings.TrimSpace(*req.PluginSecret)
	}
	wsUUID := parseUUID(workspaceID)
	existing, existingErr := h.Queries.GetFeishuProjectIntegration(r.Context(), wsUUID)
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load existing Feishu Project integration")
		return
	}
	if pluginSecret == "" {
		if existingErr == nil && existing.PluginID == pluginID {
			pluginSecret = existing.PluginSecret
		}
	}
	if pluginSecret == "" {
		writeError(w, http.StatusBadRequest, "plugin_secret is required")
		return
	}
	statusMapping := req.StatusMapping
	if statusMapping == nil {
		statusMapping = defaultFeishuProjectStatusMapping()
	}
	reverseMapping := req.ReverseStatusMapping
	if reverseMapping == nil {
		reverseMapping = defaultFeishuProjectReverseStatusMapping()
	}
	statusJSON, _ := json.Marshal(statusMapping)
	reverseJSON, _ := json.Marshal(reverseMapping)
	labelSyncRules := []service.FeishuProjectLabelSyncRule{}
	if req.LabelSyncRules != nil {
		var ok bool
		labelSyncRules, ok = normalizeFeishuProjectLabelSyncRules(w, *req.LabelSyncRules)
		if !ok {
			return
		}
	} else if existingErr == nil {
		labelSyncRules = decodeFeishuProjectLabelSyncRules(existing.LabelSyncRules)
	}
	labelSyncRulesJSON, _ := json.Marshal(labelSyncRules)
	mqlFilter := strings.TrimSpace(req.MQLFilter)
	var actor pgtype.Text
	if req.ActorUserKey != nil && strings.TrimSpace(*req.ActorUserKey) != "" {
		actor = pgtype.Text{String: strings.TrimSpace(*req.ActorUserKey), Valid: true}
	}
	var cfg db.FeishuProjectIntegration
	var err error
	bizLineKey := strings.TrimSpace(req.BusinessLineFieldKey)
	bizLineName := strings.TrimSpace(req.BusinessLineFieldName)
	if existingErr == nil {
		cfg, err = h.Queries.UpdateFeishuProjectIntegrationByID(r.Context(), db.UpdateFeishuProjectIntegrationByIDParams{
			ID:                          existing.ID,
			WorkspaceID:                 wsUUID,
			ProjectKey:                  projectKey,
			PluginID:                    pluginID,
			PluginSecret:                pluginSecret,
			ActorUserKey:                actor,
			Enabled:                     req.Enabled,
			SyncStory:                   req.SyncStory,
			SyncIssue:                   req.SyncIssue,
			MqlFilter:                   mqlFilter,
			StatusMapping:               statusJSON,
			ReverseStatusMapping:        reverseJSON,
			AssignOpenItemsToOwnerAgent: req.AssignOpenItemsToOwnerAgent,
			BusinessLineFieldKey:        bizLineKey,
			BusinessLineFieldName:       bizLineName,
			LabelSyncRules:              labelSyncRulesJSON,
		})
	} else {
		cfg, err = h.Queries.UpsertFeishuProjectIntegration(r.Context(), db.UpsertFeishuProjectIntegrationParams{
			WorkspaceID:                 wsUUID,
			ProjectKey:                  projectKey,
			PluginID:                    pluginID,
			PluginSecret:                pluginSecret,
			ActorUserKey:                actor,
			Enabled:                     req.Enabled,
			SyncStory:                   req.SyncStory,
			SyncIssue:                   req.SyncIssue,
			MqlFilter:                   mqlFilter,
			StatusMapping:               statusJSON,
			ReverseStatusMapping:        reverseJSON,
			AssignOpenItemsToOwnerAgent: req.AssignOpenItemsToOwnerAgent,
			CreatedByID:                 member.UserID,
			BusinessLineFieldKey:        bizLineKey,
			BusinessLineFieldName:       bizLineName,
			LabelSyncRules:              labelSyncRulesJSON,
		})
	}
	if err != nil {
		slog.Warn("update Feishu Project integration failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update Feishu Project integration")
		return
	}
	writeJSON(w, http.StatusOK, feishuProjectIntegrationToResponse(cfg))
}

func normalizeFeishuProjectLabelSyncRules(w http.ResponseWriter, in []service.FeishuProjectLabelSyncRule) ([]service.FeishuProjectLabelSyncRule, bool) {
	out := make([]service.FeishuProjectLabelSyncRule, 0, len(in))
	seen := map[string]bool{}
	for i, rule := range in {
		id := strings.TrimSpace(rule.ID)
		if id == "" {
			writeError(w, http.StatusBadRequest, "label_sync_rules["+strconv.Itoa(i)+"].id is required")
			return nil, false
		}
		if seen[id] {
			writeError(w, http.StatusBadRequest, "label_sync_rules["+strconv.Itoa(i)+"].id must be unique")
			return nil, false
		}
		seen[id] = true
		fieldKey := strings.TrimSpace(rule.FieldKey)
		match := strings.TrimSpace(rule.Match)
		labelName, err := validateLabelName(rule.LabelName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "label_sync_rules["+strconv.Itoa(i)+"].label_name is invalid")
			return nil, false
		}
		if fieldKey == "" || match == "" {
			writeError(w, http.StatusBadRequest, "label_sync_rules["+strconv.Itoa(i)+"].field_key and match are required")
			return nil, false
		}
		out = append(out, service.FeishuProjectLabelSyncRule{
			ID:        id,
			Enabled:   rule.Enabled,
			FieldKey:  fieldKey,
			FieldName: strings.TrimSpace(rule.FieldName),
			Match:     match,
			LabelName: labelName,
		})
	}
	return out, true
}

func (h *Handler) SyncFeishuProjectIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "Feishu Project integration not found")
		return
	}
	var req SyncFeishuProjectIntegrationRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	req.WorkItemID = strings.TrimSpace(req.WorkItemID)
	if latest, err := h.Queries.GetLatestFeishuProjectManualSyncRun(r.Context(), cfg.ID); err == nil && latest.Status == "running" {
		if latest.StartedAt.Valid && time.Since(latest.StartedAt.Time) > 2*time.Hour {
			_ = h.Queries.FinishFeishuProjectSyncRun(r.Context(), db.FinishFeishuProjectSyncRunParams{
				ID:           latest.ID,
				Status:       "failed",
				CreatedCount: latest.CreatedCount,
				UpdatedCount: latest.UpdatedCount,
				SkippedCount: latest.SkippedCount,
				ErrorCount:   latest.ErrorCount + 1,
				Error:        pgtype.Text{String: "previous manual sync timed out", Valid: true},
			})
		} else {
			writeJSON(w, http.StatusAccepted, FeishuProjectSyncResponse{Status: "running", Run: feishuProjectSyncRunToResponse(latest)})
			return
		}
	}
	// Manual sync must take the same advisory lock as the scheduled worker.
	// Without it, manual+scheduled can run concurrently against the same
	// integration, which races on create-issue for new work items and
	// double-inserts attachments. h.TxStarter is wired with the pgxpool.Pool
	// at server startup; the type assertion documents that contract.
	locker, ok := h.TxStarter.(*pgxpool.Pool)
	if !ok {
		writeError(w, http.StatusInternalServerError, "sync locker unavailable")
		return
	}
	locked, unlock, err := service.TryAcquireFeishuProjectSyncLock(r.Context(), locker, cfg.ID)
	if err != nil {
		slog.Warn("Feishu Project sync lock acquire failed", "workspace_id", workspaceID, "integration_id", uuidToString(cfg.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to acquire sync lock")
		return
	}
	if !locked {
		// A scheduled run or another manual run holds the lock. Surface the
		// latest run so the UI keeps polling progress instead of starting a
		// duplicate.
		latest, _ := h.Queries.GetLatestFeishuProjectSyncRun(r.Context(), cfg.ID)
		writeJSON(w, http.StatusAccepted, FeishuProjectSyncResponse{Status: "running", Run: feishuProjectSyncRunToResponse(latest)})
		return
	}
	run, err := h.Queries.CreateFeishuProjectSyncRun(r.Context(), db.CreateFeishuProjectSyncRunParams{
		IntegrationID: cfg.ID,
		WorkspaceID:   cfg.WorkspaceID,
		Status:        "running",
		Trigger:       "manual",
	})
	if err != nil {
		unlock()
		writeError(w, http.StatusInternalServerError, "failed to start Feishu Project sync")
		return
	}
	go func() {
		defer unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		svc := &service.FeishuProjectSyncService{Queries: h.Queries, Tx: h.TxStarter, Client: service.NewFeishuProjectClient(), Storage: h.Storage, TaskService: h.TaskService}
		if _, err := svc.SyncWithRunAndOptions(ctx, cfg, "manual", run, service.FeishuProjectSyncOptions{WorkItemID: req.WorkItemID}); err != nil {
			slog.Warn("Feishu Project manual sync failed", "workspace_id", workspaceID, "integration_id", uuidToString(cfg.ID), "run_id", uuidToString(run.ID), "error", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, FeishuProjectSyncResponse{Status: "running", Run: feishuProjectSyncRunToResponse(run)})
}

func (h *Handler) GetFeishuProjectSyncRun(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "Feishu Project integration not found")
		return
	}
	run, err := h.Queries.GetLatestFeishuProjectSyncRun(r.Context(), cfg.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, FeishuProjectSyncResponse{Status: "idle"})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get Feishu Project sync")
		return
	}
	writeJSON(w, http.StatusOK, FeishuProjectSyncResponse{Status: run.Status, Run: feishuProjectSyncRunToResponse(run), Summary: service.FeishuProjectSyncSummary{
		Created: int(run.CreatedCount),
		Updated: int(run.UpdatedCount),
		Skipped: int(run.SkippedCount),
		Errors:  int(run.ErrorCount),
	}})
}

func (h *Handler) GetFeishuProjectIssueStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "Feishu Project integration not found")
		return
	}
	statuses, err := service.NewFeishuProjectClient().IssueStatusOptions(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"statuses": statuses})
}

func feishuProjectIntegrationToResponse(cfg db.FeishuProjectIntegration) FeishuProjectIntegrationResponse {
	return FeishuProjectIntegrationResponse{
		ID:                          uuidToString(cfg.ID),
		WorkspaceID:                 uuidToString(cfg.WorkspaceID),
		ProjectName:                 cfg.ProjectKey,
		ProjectKey:                  cfg.ProjectKey,
		PluginID:                    cfg.PluginID,
		HasPluginSecret:             cfg.PluginSecret != "",
		ActorUserKey:                textToPtr(cfg.ActorUserKey),
		Enabled:                     cfg.Enabled,
		SyncStory:                   cfg.SyncStory,
		SyncIssue:                   cfg.SyncIssue,
		MQLFilter:                   cfg.MqlFilter,
		StatusMapping:               decodeFlatStringMap(cfg.StatusMapping),
		ReverseStatusMapping:        decodeFlatStringMap(cfg.ReverseStatusMapping),
		AssignOpenItemsToOwnerAgent: cfg.AssignOpenItemsToOwnerAgent,
		BusinessLineFieldKey:        cfg.BusinessLineFieldKey,
		BusinessLineFieldName:       cfg.BusinessLineFieldName,
		LabelSyncRules:              decodeFeishuProjectLabelSyncRules(cfg.LabelSyncRules),
		LastSyncedAt:                timestampToPtr(cfg.LastSyncedAt),
		LastError:                   textToPtr(cfg.LastError),
		CreatedAt:                   timestampToString(cfg.CreatedAt),
		UpdatedAt:                   timestampToString(cfg.UpdatedAt),
	}
}

func decodeFeishuProjectLabelSyncRules(raw []byte) []service.FeishuProjectLabelSyncRule {
	if len(raw) == 0 {
		return []service.FeishuProjectLabelSyncRule{}
	}
	var out []service.FeishuProjectLabelSyncRule
	if err := json.Unmarshal(raw, &out); err != nil {
		return []service.FeishuProjectLabelSyncRule{}
	}
	return out
}

func feishuProjectNameFromRequest(req UpdateFeishuProjectIntegrationRequest) string {
	projectName := strings.TrimSpace(req.ProjectName)
	if projectName != "" {
		return projectName
	}
	return strings.TrimSpace(req.ProjectKey)
}

func feishuProjectSyncRunToResponse(run db.FeishuProjectSyncRun) *FeishuProjectSyncRunResponse {
	return &FeishuProjectSyncRunResponse{
		ID:          uuidToString(run.ID),
		Status:      run.Status,
		Trigger:     run.Trigger,
		Created:     run.CreatedCount,
		Updated:     run.UpdatedCount,
		Skipped:     run.SkippedCount,
		Errors:      run.ErrorCount,
		Processed:   run.ProcessedCount,
		Total:       run.TotalCount,
		CurrentPage: run.CurrentPage,
		CurrentType: run.CurrentType,
		Error:       textToPtr(run.Error),
		StartedAt:   timestampToPtr(run.StartedAt),
		FinishedAt:  timestampToPtr(run.FinishedAt),
	}
}

func decodeFlatStringMap(raw []byte) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func defaultFeishuProjectStatusMapping() map[string]string {
	return map[string]string{
		"OPEN":        "todo",
		"CLOSED":      "done",
		"REOPENED":    "todo",
		"IN PROGRESS": "in_progress",
		"RESOLVED":    "in_review",
		"新建":          "todo",
		"未开始":         "todo",
		"重新打开":        "todo",
		"处理中":         "in_progress",
		"待测试":         "in_review",
		"待验证":         "in_review",
		"测试通过":        "done",
		"已关闭":         "done",
		"Closed":      "done",
		"挂起":          "blocked",
		"外部原因":        "blocked",
		"暂不处理":        "backlog",
		"无法复现":        "cancelled",
		"设计如此":        "cancelled",
		"放弃":          "cancelled",
		"重复bug":       "cancelled",
		"已终止":         "cancelled",
	}
}

func defaultFeishuProjectReverseStatusMapping() map[string]string {
	return map[string]string{
		"todo":        "OPEN",
		"in_progress": "IN PROGRESS",
		"in_review":   "RESOLVED",
		"done":        "CLOSED",
		"blocked":     "挂起",
		"backlog":     "暂不处理",
		"cancelled":   "已终止",
	}
}

func (h *Handler) DeleteFeishuProjectIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if err := h.Queries.DeleteFeishuProjectIntegration(r.Context(), db.DeleteFeishuProjectIntegrationParams{ID: cfg.ID, WorkspaceID: cfg.WorkspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete Feishu Project integration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
