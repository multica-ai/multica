package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type agentRunDashboardFilters struct {
	WorkspaceID pgtype.UUID
	Since       pgtype.Timestamptz
	AgentIDs    []pgtype.UUID
	OwnerIDs    []pgtype.UUID
	StartHour   int32
	EndHour     int32
	Timezone    string
	Limit       int32
}

type AgentRunDashboardSummaryResponse struct {
	TotalRuns              int32   `json:"total_runs"`
	SuccessfulRuns         int32   `json:"successful_runs"`
	FailedRuns             int32   `json:"failed_runs"`
	SuccessRate            float64 `json:"success_rate"`
	ActiveAgentCount       int32   `json:"active_agent_count"`
	AverageDurationSeconds float64 `json:"average_duration_seconds"`
}

type AgentRunDashboardDailyResponse struct {
	Date           string  `json:"date"`
	TotalRuns      int32   `json:"total_runs"`
	SuccessfulRuns int32   `json:"successful_runs"`
	FailedRuns     int32   `json:"failed_runs"`
	SuccessRate    float64 `json:"success_rate"`
}

type AgentRunDashboardHeatmapResponse struct {
	Weekday  int32 `json:"weekday"`
	Hour     int32 `json:"hour"`
	RunCount int32 `json:"run_count"`
}

type AgentRunDashboardFailureReasonResponse struct {
	Reason string `json:"reason"`
	Count  int32  `json:"count"`
}

type AgentRunDashboardAgentResponse struct {
	AgentID                string   `json:"agent_id"`
	AgentName              string   `json:"agent_name"`
	AgentStatus            string   `json:"agent_status"`
	TotalRuns              int32    `json:"total_runs"`
	SuccessfulRuns         int32    `json:"successful_runs"`
	FailedRuns             int32    `json:"failed_runs"`
	SuccessRate            float64  `json:"success_rate"`
	AverageDurationSeconds float64  `json:"average_duration_seconds"`
	LastRunAt              *string  `json:"last_run_at"`
	LastTaskID             *string  `json:"last_task_id"`
	LastStatus             *string  `json:"last_status"`
	ProjectID              *string  `json:"project_id"`
	ProjectTitle           *string  `json:"project_title"`
	ProjectCount           int32    `json:"project_count"`
	AvailableActions       []string `json:"available_actions"`
}

type AgentRunDashboardRunResponse struct {
	ID              string  `json:"id"`
	AgentID         string  `json:"agent_id"`
	AgentName       string  `json:"agent_name"`
	IssueID         *string `json:"issue_id"`
	IssueTitle      *string `json:"issue_title"`
	IssueNumber     *int32  `json:"issue_number"`
	ProjectID       *string `json:"project_id"`
	ProjectTitle    *string `json:"project_title"`
	Status          string  `json:"status"`
	RunAt           string  `json:"run_at"`
	StartedAt       *string `json:"started_at"`
	CompletedAt     *string `json:"completed_at"`
	DurationSeconds float64 `json:"duration_seconds"`
	FailureReason   string  `json:"failure_reason"`
	Error           *string `json:"error"`
	Attempt         int32   `json:"attempt"`
	MaxAttempts     int32   `json:"max_attempts"`
}

type AgentRunDashboardRetryDistributionResponse struct {
	Attempt int32 `json:"attempt"`
	Count   int32 `json:"count"`
}

type AgentRunTimelineEventResponse struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Timestamp string `json:"timestamp"`
}

type AgentRunDurationBreakdownResponse struct {
	TotalSeconds       float64 `json:"total_seconds"`
	LLMSeconds         float64 `json:"llm_seconds"`
	ToolCallSeconds    float64 `json:"tool_call_seconds"`
	NetworkWaitSeconds float64 `json:"network_wait_seconds"`
}

type AgentRunMessageResponse struct {
	Seq       int            `json:"seq"`
	Type      string         `json:"type"`
	Tool      string         `json:"tool,omitempty"`
	Content   string         `json:"content,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Output    string         `json:"output,omitempty"`
	CreatedAt string         `json:"created_at"`
}

type AgentRunDashboardRunDetailResponse struct {
	Run               AgentRunDashboardRunResponse      `json:"run"`
	Timeline          []AgentRunTimelineEventResponse   `json:"timeline"`
	DurationBreakdown AgentRunDurationBreakdownResponse `json:"duration_breakdown"`
	Messages          []AgentRunMessageResponse         `json:"messages"`
	Result            any                               `json:"result,omitempty"`
}

type AgentRunDashboardResponse struct {
	Summary           AgentRunDashboardSummaryResponse             `json:"summary"`
	Daily             []AgentRunDashboardDailyResponse             `json:"daily"`
	Heatmap           []AgentRunDashboardHeatmapResponse           `json:"heatmap"`
	FailureReasons    []AgentRunDashboardFailureReasonResponse     `json:"failure_reasons"`
	Agents            []AgentRunDashboardAgentResponse             `json:"agents"`
	RecentRuns        []AgentRunDashboardRunResponse               `json:"recent_runs"`
	RecentFailures    []AgentRunDashboardRunResponse               `json:"recent_failures"`
	RetryDistribution []AgentRunDashboardRetryDistributionResponse `json:"retry_distribution"`
}

func (h *Handler) GetAgentRunDashboard(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	filters, ok := parseAgentRunDashboardFilters(w, r, workspaceID, member.UserID)
	if !ok {
		return
	}
	params := db.GetAgentRunDashboardSummaryParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	}
	summary, err := h.Queries.GetAgentRunDashboardSummary(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent run summary")
		return
	}
	dailyRows, err := h.Queries.ListAgentRunDashboardDaily(r.Context(), db.ListAgentRunDashboardDailyParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list daily agent runs")
		return
	}
	heatmapRows, err := h.Queries.ListAgentRunDashboardHeatmap(r.Context(), db.ListAgentRunDashboardHeatmapParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent run heatmap")
		return
	}
	failureRows, err := h.Queries.ListAgentRunDashboardFailureReasons(r.Context(), db.ListAgentRunDashboardFailureReasonsParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failure reasons")
		return
	}
	agentRows, err := h.Queries.ListAgentRunDashboardAgents(r.Context(), db.ListAgentRunDashboardAgentsParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent run rows")
		return
	}
	recentRows, err := h.Queries.ListAgentRunDashboardRecentRuns(r.Context(), db.ListAgentRunDashboardRecentRunsParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
		FailedOnly:  false,
		LimitCount:  filters.Limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent runs")
		return
	}
	recentFailureRows, err := h.Queries.ListAgentRunDashboardRecentRuns(r.Context(), db.ListAgentRunDashboardRecentRunsParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
		FailedOnly:  true,
		LimitCount:  20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent failures")
		return
	}
	retryRows, err := h.Queries.ListAgentRunDashboardRetryDistribution(r.Context(), db.ListAgentRunDashboardRetryDistributionParams{
		WorkspaceID: filters.WorkspaceID,
		Since:       filters.Since,
		AgentIds:    filters.AgentIDs,
		OwnerIds:    filters.OwnerIDs,
		StartHour:   filters.StartHour,
		EndHour:     filters.EndHour,
		Tz:          filters.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list retry distribution")
		return
	}

	writeJSON(w, http.StatusOK, AgentRunDashboardResponse{
		Summary:           agentRunSummaryResponse(summary),
		Daily:             agentRunDailyResponses(dailyRows),
		Heatmap:           agentRunHeatmapResponses(heatmapRows),
		FailureReasons:    agentRunFailureReasonResponses(failureRows),
		Agents:            agentRunAgentResponses(agentRows),
		RecentRuns:        agentRunRecentResponses(recentRows),
		RecentFailures:    agentRunRecentResponses(recentFailureRows),
		RetryDistribution: agentRunRetryResponses(retryRows),
	})
}

func (h *Handler) GetAgentRunDashboardRun(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return
	}
	row, err := h.Queries.GetAgentRunDashboardRun(r.Context(), db.GetAgentRunDashboardRunParams{
		TaskID:      taskID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get run detail")
		return
	}
	messages, err := h.Queries.ListTaskMessages(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run messages")
		return
	}
	respMessages := agentRunMessageResponses(messages)
	detail := AgentRunDashboardRunDetailResponse{
		Run:               agentRunDetailResponse(row),
		Timeline:          agentRunTimeline(row),
		DurationBreakdown: agentRunDurationBreakdown(row, messages),
		Messages:          respMessages,
	}
	if len(row.Result) > 0 {
		var result any
		if err := json.Unmarshal(row.Result, &result); err == nil {
			detail.Result = result
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

func parseAgentRunDashboardFilters(w http.ResponseWriter, r *http.Request, workspaceID string, currentUserID pgtype.UUID) (agentRunDashboardFilters, bool) {
	tz := strings.TrimSpace(r.URL.Query().Get("tz"))
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tz")
		return agentRunDashboardFilters{}, false
	}
	startHour, ok := parseHourQueryParam(w, r, "start_hour", 0)
	if !ok {
		return agentRunDashboardFilters{}, false
	}
	endHour, ok := parseHourQueryParam(w, r, "end_hour", 23)
	if !ok {
		return agentRunDashboardFilters{}, false
	}
	agentIDs, ok := parseAgentIDQueryParam(w, r)
	if !ok {
		return agentRunDashboardFilters{}, false
	}
	ownerIDs, ok := parseOwnerIDQueryParam(w, r, currentUserID)
	if !ok {
		return agentRunDashboardFilters{}, false
	}
	limit := int32(50)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return agentRunDashboardFilters{}, false
		}
		if parsed > 100 {
			parsed = 100
		}
		limit = int32(parsed)
	}
	return agentRunDashboardFilters{
		WorkspaceID: parseUUID(workspaceID),
		Since:       parseSinceParamInTZ(r, 30, tz),
		AgentIDs:    agentIDs,
		OwnerIDs:    ownerIDs,
		StartHour:   int32(startHour),
		EndHour:     int32(endHour),
		Timezone:    tz,
		Limit:       limit,
	}, true
}

func parseHourQueryParam(w http.ResponseWriter, r *http.Request, name string, defaultValue int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 || parsed > 23 {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return parsed, true
}

func parseAgentIDQueryParam(w http.ResponseWriter, r *http.Request) ([]pgtype.UUID, bool) {
	rawValues := collectUUIDQueryValues(r, "agent_id", "agent_ids")
	return parseUUIDQueryValues(w, rawValues, "agent_id")
}

func parseOwnerIDQueryParam(w http.ResponseWriter, r *http.Request, currentUserID pgtype.UUID) ([]pgtype.UUID, bool) {
	rawValues := []string{uuidToString(currentUserID)}
	if raw := strings.TrimSpace(r.URL.Query().Get("owner")); raw != "" {
		if raw != "me" && raw != "all" && raw != uuidToString(currentUserID) {
			writeError(w, http.StatusForbidden, "owner_id must match current user")
			return nil, false
		}
	}
	for _, raw := range collectUUIDQueryValues(r, "owner_id", "owner_ids") {
		if strings.TrimSpace(raw) != uuidToString(currentUserID) {
			writeError(w, http.StatusForbidden, "owner_id must match current user")
			return nil, false
		}
	}
	return parseUUIDQueryValues(w, rawValues, "owner_id")
}

func collectUUIDQueryValues(r *http.Request, singleKey string, listKey string) []string {
	rawValues := append([]string{}, r.URL.Query()[singleKey]...)
	if raw := r.URL.Query().Get(listKey); raw != "" {
		rawValues = append(rawValues, strings.Split(raw, ",")...)
	}
	return rawValues
}

func parseUUIDQueryValues(w http.ResponseWriter, rawValues []string, fieldName string) ([]pgtype.UUID, bool) {
	seen := map[string]struct{}{}
	out := make([]pgtype.UUID, 0, len(rawValues))
	for _, raw := range rawValues {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		u, err := util.ParseUUID(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+fieldName)
			return nil, false
		}
		seen[value] = struct{}{}
		out = append(out, u)
	}
	return out, true
}

func agentRunSummaryResponse(row db.GetAgentRunDashboardSummaryRow) AgentRunDashboardSummaryResponse {
	return AgentRunDashboardSummaryResponse{
		TotalRuns:              row.TotalRuns,
		SuccessfulRuns:         row.SuccessfulRuns,
		FailedRuns:             row.FailedRuns,
		SuccessRate:            successRate(row.SuccessfulRuns, row.FailedRuns),
		ActiveAgentCount:       row.ActiveAgentCount,
		AverageDurationSeconds: roundSeconds(row.AverageDurationSeconds),
	}
}

func agentRunDailyResponses(rows []db.ListAgentRunDashboardDailyRow) []AgentRunDashboardDailyResponse {
	resp := make([]AgentRunDashboardDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = AgentRunDashboardDailyResponse{
			Date:           row.Date.Time.Format("2006-01-02"),
			TotalRuns:      row.TotalRuns,
			SuccessfulRuns: row.SuccessfulRuns,
			FailedRuns:     row.FailedRuns,
			SuccessRate:    successRate(row.SuccessfulRuns, row.FailedRuns),
		}
	}
	return resp
}

func agentRunHeatmapResponses(rows []db.ListAgentRunDashboardHeatmapRow) []AgentRunDashboardHeatmapResponse {
	resp := make([]AgentRunDashboardHeatmapResponse, len(rows))
	for i, row := range rows {
		resp[i] = AgentRunDashboardHeatmapResponse{
			Weekday:  row.Weekday,
			Hour:     row.Hour,
			RunCount: row.RunCount,
		}
	}
	return resp
}

func agentRunFailureReasonResponses(rows []db.ListAgentRunDashboardFailureReasonsRow) []AgentRunDashboardFailureReasonResponse {
	resp := make([]AgentRunDashboardFailureReasonResponse, len(rows))
	for i, row := range rows {
		resp[i] = AgentRunDashboardFailureReasonResponse{
			Reason: row.Reason,
			Count:  row.Count,
		}
	}
	return resp
}

func agentRunAgentResponses(rows []db.ListAgentRunDashboardAgentsRow) []AgentRunDashboardAgentResponse {
	resp := make([]AgentRunDashboardAgentResponse, len(rows))
	for i, row := range rows {
		actions := []string{}
		if row.LastTaskID.Valid {
			actions = append(actions, "view_run")
		}
		actions = append(actions, "open_agent")
		resp[i] = AgentRunDashboardAgentResponse{
			AgentID:                uuidToString(row.AgentID),
			AgentName:              row.AgentName,
			AgentStatus:            deriveAgentDashboardStatus(row.AgentStatus, row.LastStatus),
			TotalRuns:              row.TotalRuns,
			SuccessfulRuns:         row.SuccessfulRuns,
			FailedRuns:             row.FailedRuns,
			SuccessRate:            successRate(row.SuccessfulRuns, row.FailedRuns),
			AverageDurationSeconds: roundSeconds(row.AverageDurationSeconds),
			LastRunAt:              timestampToPtr(row.LastRunAt),
			LastTaskID:             uuidToPtr(row.LastTaskID),
			LastStatus:             textToPtr(row.LastStatus),
			ProjectID:              uuidToPtr(row.ProjectID),
			ProjectTitle:           textToPtr(row.ProjectTitle),
			ProjectCount:           row.ProjectCount,
			AvailableActions:       actions,
		}
	}
	return resp
}

func agentRunRecentResponses(rows []db.ListAgentRunDashboardRecentRunsRow) []AgentRunDashboardRunResponse {
	resp := make([]AgentRunDashboardRunResponse, len(rows))
	for i, row := range rows {
		resp[i] = AgentRunDashboardRunResponse{
			ID:              uuidToString(row.ID),
			AgentID:         uuidToString(row.AgentID),
			AgentName:       row.AgentName,
			IssueID:         uuidToPtr(row.IssueID),
			IssueTitle:      textToPtr(row.IssueTitle),
			IssueNumber:     int4ToPtr(row.IssueNumber),
			ProjectID:       uuidToPtr(row.ProjectID),
			ProjectTitle:    textToPtr(row.ProjectTitle),
			Status:          row.Status,
			RunAt:           timestampToString(row.RunAt),
			StartedAt:       timestampToPtr(row.StartedAt),
			CompletedAt:     timestampToPtr(row.CompletedAt),
			DurationSeconds: roundSeconds(durationSeconds(row.StartedAt, row.CompletedAt)),
			FailureReason:   row.FailureReason,
			Error:           textToPtr(row.Error),
			Attempt:         row.Attempt,
			MaxAttempts:     row.MaxAttempts,
		}
	}
	return resp
}

func agentRunDetailResponse(row db.GetAgentRunDashboardRunRow) AgentRunDashboardRunResponse {
	return AgentRunDashboardRunResponse{
		ID:              uuidToString(row.ID),
		AgentID:         uuidToString(row.AgentID),
		AgentName:       row.AgentName,
		IssueID:         uuidToPtr(row.IssueID),
		IssueTitle:      textToPtr(row.IssueTitle),
		IssueNumber:     int4ToPtr(row.IssueNumber),
		ProjectID:       uuidToPtr(row.ProjectID),
		ProjectTitle:    textToPtr(row.ProjectTitle),
		Status:          row.Status,
		RunAt:           timestampToString(row.RunAt),
		StartedAt:       timestampToPtr(row.StartedAt),
		CompletedAt:     timestampToPtr(row.CompletedAt),
		DurationSeconds: roundSeconds(durationSeconds(row.StartedAt, row.CompletedAt)),
		FailureReason:   classifyFailureReason(row.Status, row.FailureReason, row.Error),
		Error:           textToPtr(row.Error),
		Attempt:         row.Attempt,
		MaxAttempts:     row.MaxAttempts,
	}
}

func agentRunRetryResponses(rows []db.ListAgentRunDashboardRetryDistributionRow) []AgentRunDashboardRetryDistributionResponse {
	resp := make([]AgentRunDashboardRetryDistributionResponse, len(rows))
	for i, row := range rows {
		resp[i] = AgentRunDashboardRetryDistributionResponse{
			Attempt: row.Attempt,
			Count:   row.Count,
		}
	}
	return resp
}

func agentRunTimeline(row db.GetAgentRunDashboardRunRow) []AgentRunTimelineEventResponse {
	events := []AgentRunTimelineEventResponse{
		{Key: "created", Label: "Queued", Timestamp: timestampToString(row.CreatedAt)},
	}
	add := func(key, label string, ts pgtype.Timestamptz) {
		if ts.Valid {
			events = append(events, AgentRunTimelineEventResponse{
				Key:       key,
				Label:     label,
				Timestamp: timestampToString(ts),
			})
		}
	}
	add("dispatched", "Dispatched", row.DispatchedAt)
	add("started", "Started", row.StartedAt)
	if row.CompletedAt.Valid {
		label := "Completed"
		if row.Status == "failed" {
			label = "Failed"
		} else if row.Status == "cancelled" {
			label = "Cancelled"
		}
		add(row.Status, label, row.CompletedAt)
	}
	return events
}

func agentRunMessageResponses(messages []db.TaskMessage) []AgentRunMessageResponse {
	resp := make([]AgentRunMessageResponse, len(messages))
	for i, m := range messages {
		var input map[string]any
		if len(m.Input) > 0 {
			_ = json.Unmarshal(m.Input, &input)
		}
		resp[i] = AgentRunMessageResponse{
			Seq:       int(m.Seq),
			Type:      m.Type,
			Tool:      m.Tool.String,
			Content:   m.Content.String,
			Input:     input,
			Output:    m.Output.String,
			CreatedAt: timestampToString(m.CreatedAt),
		}
	}
	return resp
}

func agentRunDurationBreakdown(row db.GetAgentRunDashboardRunRow, messages []db.TaskMessage) AgentRunDurationBreakdownResponse {
	total := durationSeconds(row.StartedAt, row.CompletedAt)
	var toolSeconds float64
	var currentToolStart *time.Time
	for _, m := range messages {
		if !m.CreatedAt.Valid {
			continue
		}
		switch m.Type {
		case "tool_use":
			t := m.CreatedAt.Time
			currentToolStart = &t
		case "tool_result":
			if currentToolStart != nil && m.CreatedAt.Time.After(*currentToolStart) {
				toolSeconds += m.CreatedAt.Time.Sub(*currentToolStart).Seconds()
			}
			currentToolStart = nil
		}
	}
	if toolSeconds > total {
		toolSeconds = total
	}
	llmSeconds := total - toolSeconds
	return AgentRunDurationBreakdownResponse{
		TotalSeconds:       roundSeconds(total),
		LLMSeconds:         roundSeconds(llmSeconds),
		ToolCallSeconds:    roundSeconds(toolSeconds),
		NetworkWaitSeconds: 0,
	}
}

func deriveAgentDashboardStatus(agentStatus string, lastStatus pgtype.Text) string {
	if lastStatus.Valid {
		switch lastStatus.String {
		case "queued", "dispatched", "running":
			return lastStatus.String
		}
	}
	return agentStatus
}

func successRate(successful, failed int32) float64 {
	denominator := successful + failed
	if denominator <= 0 {
		return 0
	}
	return math.Round((float64(successful)/float64(denominator))*1000) / 1000
}

func durationSeconds(startedAt, completedAt pgtype.Timestamptz) float64 {
	if !startedAt.Valid {
		return 0
	}
	end := time.Now()
	if completedAt.Valid {
		end = completedAt.Time
	}
	if end.Before(startedAt.Time) {
		return 0
	}
	return end.Sub(startedAt.Time).Seconds()
}

func roundSeconds(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*10) / 10
}

func int4ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func classifyFailureReason(status string, reason pgtype.Text, errText pgtype.Text) string {
	if status != "failed" {
		return ""
	}
	if reason.Valid && strings.TrimSpace(reason.String) != "" {
		return reason.String
	}
	err := strings.ToLower(errText.String)
	switch {
	case strings.Contains(err, "503") || strings.Contains(err, "service unavailable"):
		return "http_503"
	case strings.Contains(err, "timeout") || strings.Contains(err, "deadline"):
		return "timeout"
	case strings.Contains(err, "permission") || strings.Contains(err, "forbidden") || strings.Contains(err, "unauthorized"):
		return "permission"
	case strings.Contains(err, "invalid") || strings.Contains(err, "400") || strings.Contains(err, "parameter"):
		return "invalid_request"
	default:
		return "agent_error"
	}
}
