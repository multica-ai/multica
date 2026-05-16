package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	feishuProjectBaseURL = "https://project.feishu.cn"
	feishuProjectMCPURL  = "https://project.feishu.cn/mcp_server/v1"

	feishuProjectSyncPageSize      = 100
	feishuProjectInitialLookback   = 24 * time.Hour
	feishuProjectIncrementalReplay = 10 * time.Minute
	feishuProjectSyncMaxPages      = 1000
)

var ErrFeishuProjectSyncScopeRequired = errors.New("Feishu Project sync requires a bounded sync scope before searching work items")

type FeishuProjectTxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type FeishuProjectSyncService struct {
	Queries *db.Queries
	Tx      FeishuProjectTxStarter
	Client  *FeishuProjectClient
}

type FeishuProjectSyncSummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

type FeishuProjectClient struct {
	HTTPClient *http.Client
	BaseURL    string
	MCPURL     string
}

type FeishuProjectWorkItem struct {
	ID          string
	Type        string
	Title       string
	Description string
	Status      string
	OwnerEmail  string
	UpdatedAt   time.Time
	URL         string
}

type FeishuProjectStatusOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func NewFeishuProjectClient() *FeishuProjectClient {
	return &FeishuProjectClient{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		BaseURL:    feishuProjectBaseURL,
		MCPURL:     feishuProjectMCPURL,
	}
}

func (s *FeishuProjectSyncService) Sync(ctx context.Context, cfg db.FeishuProjectIntegration, trigger string) (FeishuProjectSyncSummary, error) {
	if s.Client == nil {
		s.Client = NewFeishuProjectClient()
	}
	run, _ := s.Queries.CreateFeishuProjectSyncRun(ctx, db.CreateFeishuProjectSyncRunParams{
		IntegrationID: cfg.ID,
		WorkspaceID:   cfg.WorkspaceID,
		Status:        "running",
		Trigger:       trigger,
	})

	summary := FeishuProjectSyncSummary{}
	var syncErr error
	for _, typ := range enabledFeishuProjectTypes(cfg) {
		items, err := s.Client.QueryWorkItems(ctx, cfg, typ)
		if err != nil {
			summary.Errors++
			syncErr = err
			continue
		}
		for _, item := range items {
			item.Type = typ
			result, err := s.syncWorkItem(ctx, cfg, item)
			if err != nil {
				summary.Errors++
				syncErr = err
				continue
			}
			switch result {
			case "created":
				summary.Created++
			case "updated":
				summary.Updated++
			default:
				summary.Skipped++
			}
		}
	}

	status := "succeeded"
	var errText pgtype.Text
	if syncErr != nil {
		status = "failed"
		errText = pgtype.Text{String: syncErr.Error(), Valid: true}
		_ = s.Queries.MarkFeishuProjectIntegrationError(ctx, db.MarkFeishuProjectIntegrationErrorParams{
			ID:        cfg.ID,
			LastError: pgtype.Text{String: syncErr.Error(), Valid: true},
		})
	} else {
		_ = s.Queries.MarkFeishuProjectIntegrationSynced(ctx, cfg.ID)
	}
	if run.ID.Valid {
		_ = s.Queries.FinishFeishuProjectSyncRun(ctx, db.FinishFeishuProjectSyncRunParams{
			ID:           run.ID,
			Status:       status,
			CreatedCount: int32(summary.Created),
			UpdatedCount: int32(summary.Updated),
			SkippedCount: int32(summary.Skipped),
			ErrorCount:   int32(summary.Errors),
			Error:        errText,
		})
	}
	return summary, syncErr
}

func enabledFeishuProjectTypes(cfg db.FeishuProjectIntegration) []string {
	var out []string
	if cfg.SyncIssue {
		out = append(out, "issue")
	}
	return out
}

func (s *FeishuProjectSyncService) syncWorkItem(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem) (string, error) {
	if item.ID == "" || item.Title == "" {
		return "skipped", nil
	}
	status := mapFeishuStatus(cfg.StatusMapping, item.Type, item.Status)
	if status == "" {
		status = "todo"
	}
	assigneeType, assigneeID := s.resolveOwner(ctx, cfg.WorkspaceID, item.OwnerEmail)

	binding, err := s.Queries.GetFeishuProjectIssueBindingByExternal(ctx, db.GetFeishuProjectIssueBindingByExternalParams{
		IntegrationID: cfg.ID,
		WorkItemType:  item.Type,
		WorkItemID:    item.ID,
	})
	if err == nil {
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: binding.IssueID, WorkspaceID: cfg.WorkspaceID})
		if err != nil {
			return "skipped", nil
		}
		nextDesc := externalDescription(item)
		if issue.Title == item.Title && issue.Description.String == nextDesc && issue.Status == status {
			return "skipped", nil
		}
		_, err = s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:            issue.ID,
			Title:         pgtype.Text{String: item.Title, Valid: true},
			Description:   pgtype.Text{String: nextDesc, Valid: true},
			Status:        pgtype.Text{String: status, Valid: true},
			Priority:      pgtype.Text{String: issue.Priority, Valid: true},
			AssigneeType:  assigneeType,
			AssigneeID:    assigneeID,
			DueDate:       issue.DueDate,
			ParentIssueID: issue.ParentIssueID,
			ProjectID:     issue.ProjectID,
		})
		if err != nil {
			return "skipped", err
		}
		_, _ = s.Queries.UpsertFeishuProjectIssueBinding(ctx, bindingParams(cfg, issue.ID, item))
		return "updated", nil
	}

	if !cfg.CreatedByID.Valid {
		return "skipped", fmt.Errorf("feishu project integration has no creator")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return "skipped", err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	number, err := qtx.IncrementIssueCounter(ctx, cfg.WorkspaceID)
	if err != nil {
		return "skipped", err
	}
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:  cfg.WorkspaceID,
		Title:        item.Title,
		Description:  pgtype.Text{String: externalDescription(item), Valid: true},
		Status:       status,
		Priority:     "none",
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  "member",
		CreatorID:    cfg.CreatedByID,
		Position:     0,
		Number:       number,
	})
	if err != nil {
		return "skipped", err
	}
	if _, err := qtx.UpsertFeishuProjectIssueBinding(ctx, bindingParams(cfg, issue.ID, item)); err != nil {
		return "skipped", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "skipped", err
	}
	return "created", nil
}

func (s *FeishuProjectSyncService) resolveOwner(ctx context.Context, workspaceID pgtype.UUID, email string) (pgtype.Text, pgtype.UUID) {
	if strings.TrimSpace(email) == "" {
		return pgtype.Text{}, pgtype.UUID{}
	}
	user, err := s.Queries.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: workspaceID}); err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	return pgtype.Text{String: "member", Valid: true}, user.ID
}

func bindingParams(cfg db.FeishuProjectIntegration, issueID pgtype.UUID, item FeishuProjectWorkItem) db.UpsertFeishuProjectIssueBindingParams {
	return db.UpsertFeishuProjectIssueBindingParams{
		WorkspaceID:        cfg.WorkspaceID,
		IntegrationID:      cfg.ID,
		IssueID:            issueID,
		ProjectKey:         cfg.ProjectKey,
		WorkItemType:       item.Type,
		WorkItemID:         item.ID,
		ExternalIdentifier: "MEEGO_" + item.ID,
		ExternalUrl:        pgtype.Text{String: item.URL, Valid: item.URL != ""},
		ExternalStatusLabel: pgtype.Text{
			String: item.Status,
			Valid:  item.Status != "",
		},
		LastExternalUpdatedAt: pgtype.Timestamptz{Time: item.UpdatedAt, Valid: !item.UpdatedAt.IsZero()},
	}
}

func externalDescription(item FeishuProjectWorkItem) string {
	var b strings.Builder
	if strings.TrimSpace(item.Description) != "" {
		b.WriteString(strings.TrimSpace(item.Description))
		b.WriteString("\n\n")
	}
	b.WriteString("External-Id: MEEGO_")
	b.WriteString(item.ID)
	if item.URL != "" {
		b.WriteString("\nExternal-Url: ")
		b.WriteString(item.URL)
	}
	return b.String()
}

func mapFeishuStatus(raw []byte, typ, external string) string {
	var mapping map[string]map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		if byType := mapping[typ]; byType != nil {
			if v := byType[external]; v != "" {
				return v
			}
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat[external]
	}
	return ""
}

func MapMulticaStatusToFeishu(raw []byte, typ, status string) string {
	var mapping map[string]map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		if byType := mapping[typ]; byType != nil {
			if v := byType[status]; v != "" {
				return v
			}
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat[status]
	}
	return ""
}

func mappedFeishuProjectStatuses(raw []byte, typ string) []string {
	add := func(out []string, seen map[string]bool, status string) []string {
		status = strings.TrimSpace(status)
		if status == "" || seen[status] {
			return out
		}
		seen[status] = true
		return append(out, status)
	}
	seen := map[string]bool{}
	var out []string
	var nested map[string]map[string]string
	if err := json.Unmarshal(raw, &nested); err == nil {
		if byType := nested[typ]; byType != nil {
			for external, local := range byType {
				if strings.TrimSpace(local) != "" {
					out = add(out, seen, external)
				}
			}
			sort.Strings(out)
			return out
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		for external, local := range flat {
			if strings.TrimSpace(local) != "" {
				out = add(out, seen, external)
			}
		}
	}
	sort.Strings(out)
	return out
}

func feishuProjectSyncSinceDate(cfg db.FeishuProjectIntegration, now time.Time) string {
	return feishuProjectSyncSince(cfg, now).UTC().Format("2006-01-02")
}

func feishuProjectSyncSinceUnixMilli(cfg db.FeishuProjectIntegration, now time.Time) int64 {
	return feishuProjectSyncSince(cfg, now).UnixMilli()
}

func feishuProjectSyncSince(cfg db.FeishuProjectIntegration, now time.Time) time.Time {
	since := now.Add(-feishuProjectInitialLookback)
	if cfg.LastSyncedAt.Valid {
		since = cfg.LastSyncedAt.Time.Add(-feishuProjectIncrementalReplay)
	}
	return since
}

func buildFeishuProjectSyncMQL(projectKey, workItemType string, statuses []string, sinceDate, extraFilter string, offset, limit int) string {
	conditions := []string{
		fmt.Sprintf("`work_item_status` IN (%s)", quoteMQLStrings(statuses)),
		fmt.Sprintf("`updated_at` >= %s", quoteMQLString(sinceDate)),
	}
	if filter := normalizeFeishuProjectMQLFilter(extraFilter); filter != "" {
		conditions = append(conditions, "("+filter+")")
	}
	return fmt.Sprintf(
		"SELECT `work_item_id`, `name`, `description`, `work_item_status`, `current_status_operator`, `updated_at` FROM `%s`.`%s` WHERE %s ORDER BY `updated_at` DESC LIMIT %d, %d",
		escapeMQLIdent(projectKey),
		escapeMQLIdent(workItemType),
		strings.Join(conditions, " AND "),
		offset,
		limit,
	)
}

func normalizeFeishuProjectMQLFilter(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(filter), "WHERE ") {
		return strings.TrimSpace(filter[len("WHERE "):])
	}
	return filter
}

func quoteMQLStrings(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			quoted = append(quoted, quoteMQLString(value))
		}
	}
	return strings.Join(quoted, ", ")
}

func quoteMQLString(value string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(value), "'", "''") + "'"
}

func (c *FeishuProjectClient) QueryWorkItems(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string) ([]FeishuProjectWorkItem, error) {
	statuses := mappedFeishuProjectStatuses(cfg.StatusMapping, workItemType)
	if len(statuses) == 0 {
		return nil, ErrFeishuProjectSyncScopeRequired
	}
	var out []FeishuProjectWorkItem
	pageNum := 1
	for page := 0; page < feishuProjectSyncMaxPages; page++ {
		payload, err := c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/work_item/filter", cfg.ProjectKey), map[string]any{
			"work_item_type_keys": []string{workItemType},
			"work_item_status":    feishuProjectWorkItemStatusFilter(statuses),
			"updated_at": map[string]any{
				"start": feishuProjectSyncSinceUnixMilli(cfg, time.Now()),
			},
			"page_num":  pageNum,
			"page_size": feishuProjectSyncPageSize,
			"expand": map[string]any{
				"need_multi_text":  true,
				"need_user_detail": true,
			},
		})
		if err != nil {
			return nil, err
		}
		items := parseFeishuProjectSearch(payload, workItemType, cfg.ProjectKey)
		out = append(out, items...)
		total, hasTotal := feishuProjectOpenAPITotal(payload)
		if hasTotal {
			if pageNum*feishuProjectSyncPageSize >= total {
				return out, nil
			}
			pageNum++
			continue
		}
		if len(items) < feishuProjectSyncPageSize {
			return out, nil
		}
		pageNum++
	}
	return out, fmt.Errorf("Feishu Project sync stopped after %d pages; narrow the sync scope", feishuProjectSyncMaxPages)
}

func feishuProjectWorkItemStatusFilter(statuses []string) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		status = strings.TrimSpace(status)
		if status != "" {
			out = append(out, map[string]any{"state_key": status})
		}
	}
	return out
}

func (c *FeishuProjectClient) projectMQLTableName(ctx context.Context, cfg db.FeishuProjectIntegration) (string, error) {
	body := map[string]any{
		"project_keys": []string{cfg.ProjectKey},
	}
	if cfg.ActorUserKey.Valid && strings.TrimSpace(cfg.ActorUserKey.String) != "" {
		body["user_key"] = strings.TrimSpace(cfg.ActorUserKey.String)
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodPost, "/open_api/projects/detail", body)
	if err != nil {
		return "", err
	}
	data, _ := payload["data"].(map[string]any)
	for _, projectAny := range data {
		project, _ := projectAny.(map[string]any)
		if simple := firstNonEmpty(fmt.Sprint(project["simple_name"])); simple != "" {
			return simple, nil
		}
	}
	return cfg.ProjectKey, nil
}

func (c *FeishuProjectClient) mappedStatusLabels(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string) ([]string, error) {
	mapped := mappedFeishuProjectStatuses(cfg.StatusMapping, workItemType)
	if len(mapped) == 0 {
		return nil, nil
	}
	options, err := c.IssueStatusOptions(ctx, cfg)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(options))
	byName := make(map[string]string, len(options))
	for _, option := range options {
		byKey[option.Key] = option.Name
		byName[option.Name] = option.Name
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(mapped))
	for _, status := range mapped {
		label := firstNonEmpty(byKey[status], byName[status], status)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return out, nil
}

func (c *FeishuProjectClient) IssueStatusOptions(ctx context.Context, cfg db.FeishuProjectIntegration) ([]FeishuProjectStatusOption, error) {
	var statuses []FeishuProjectStatusOption
	templates, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/template_list/%s", cfg.ProjectKey, "issue"), nil)
	if err == nil {
		for _, templateID := range parseFeishuProjectTemplateIDs(templates) {
			detail, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/template_detail/%s", cfg.ProjectKey, templateID), nil)
			if err != nil {
				return nil, err
			}
			statuses = appendFeishuProjectStatuses(statuses, parseFeishuProjectStateFlowStatuses(detail)...)
		}
	}
	if len(statuses) > 0 {
		return statuses, nil
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/work_item/%s/meta", cfg.ProjectKey, "issue"), nil)
	if err != nil {
		return nil, err
	}
	statuses = parseFeishuProjectStatusOptions(payload)
	if len(statuses) == 0 {
		return nil, fmt.Errorf("Feishu Project issue status metadata is empty")
	}
	return statuses, nil
}

func (c *FeishuProjectClient) TransitionStatus(ctx context.Context, cfg db.FeishuProjectIntegration, workItemID, workItemType, targetStatus string) error {
	if targetStatus == "" {
		return nil
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/work_item/%s/%s/workflow/query", cfg.ProjectKey, workItemType, workItemID), map[string]any{
		"flow_type": 1,
	})
	if err != nil {
		return err
	}
	transitionID := findTransitionID(payload, targetStatus)
	if transitionID == "" {
		return fmt.Errorf("no Feishu Project transition to %q for work item %s", targetStatus, workItemID)
	}
	transitionIDValue, err := strconv.ParseInt(transitionID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Feishu Project transition id %q: %w", transitionID, err)
	}
	_, err = c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/workflow/%s/%s/node/state_change", cfg.ProjectKey, workItemType, workItemID), map[string]any{
		"transition_id": transitionIDValue,
	})
	return err
}

func (c *FeishuProjectClient) openAPI(ctx context.Context, cfg db.FeishuProjectIntegration, method, path string, body any) (map[string]any, error) {
	token, err := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PLUGIN-TOKEN", token)
	if cfg.ActorUserKey.Valid {
		req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Feishu Project API %s %s http %d: %s", method, path, resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if msg := feishuProjectAPIError(out); msg != "" {
		return nil, fmt.Errorf("Feishu Project API %s %s failed: %s", method, path, msg)
	}
	return out, nil
}

func (c *FeishuProjectClient) callTool(ctx context.Context, cfg db.FeishuProjectIntegration, name string, args map[string]any) (map[string]any, error) {
	token, err := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.MCPURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json,text/event-stream")
	req.Header.Set("X-Mcp-Token", token)
	if cfg.ActorUserKey.Valid {
		req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Feishu Project tool %s http %d: %s", name, resp.StatusCode, string(raw))
	}
	var envelope struct {
		Error  any `json:"error"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("Feishu Project tool %s failed: %v", name, envelope.Error)
	}
	texts := make([]string, 0, len(envelope.Result.Content))
	for _, item := range envelope.Result.Content {
		if strings.HasPrefix(item.Text, "log_id:") || strings.HasPrefix(item.Text, "logid:") {
			continue
		}
		if strings.TrimSpace(item.Text) != "" {
			texts = append(texts, strings.TrimSpace(item.Text))
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(item.Text), &out); err == nil {
			if toolErr := feishuProjectToolError(out); toolErr != "" {
				return nil, fmt.Errorf("Feishu Project tool %s failed: %s", name, toolErr)
			}
			return out, nil
		}
	}
	if envelope.Result.IsError || len(texts) > 0 {
		return nil, fmt.Errorf("Feishu Project tool %s failed: %s", name, strings.Join(texts, "; "))
	}
	return map[string]any{}, nil
}

func (c *FeishuProjectClient) pluginToken(ctx context.Context, pluginID, pluginSecret string) (string, error) {
	body, _ := json.Marshal(map[string]string{"plugin_id": pluginID, "plugin_secret": pluginSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/open_api/authen/plugin_token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		ErrCode int    `json:"err_code"`
		ErrMsg  string `json:"err_msg"`
		Data    struct {
			Token       string `json:"token"`
			PluginToken string `json:"plugin_token"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Feishu Project plugin_token http %d: %s", resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	token := parsed.Data.Token
	if token == "" {
		token = parsed.Data.PluginToken
	}
	if parsed.ErrCode != 0 || token == "" {
		return "", fmt.Errorf("Feishu Project plugin_token err_code=%d msg=%q", parsed.ErrCode, parsed.ErrMsg)
	}
	return token, nil
}

func (c *FeishuProjectClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func parseFeishuProjectMQL(payload map[string]any, typ, projectKey string) []FeishuProjectWorkItem {
	var out []FeishuProjectWorkItem
	data, _ := payload["data"].(map[string]any)
	for _, rowsAny := range data {
		rows, _ := rowsAny.([]any)
		for _, rowAny := range rows {
			row, _ := rowAny.(map[string]any)
			fields, _ := row["moql_field_list"].([]any)
			record := map[string]string{}
			for _, fieldAny := range fields {
				field, _ := fieldAny.(map[string]any)
				key, _ := field["key"].(string)
				if key == "" {
					continue
				}
				if value := feishuProjectFieldValue(field); value != "" {
					record[key] = value
				}
			}
			id := record["work_item_id"]
			if id == "" {
				continue
			}
			status := record["work_item_status"]
			if status == "" {
				status = record["status"]
			}
			updatedAt := feishuProjectTime(record["updated_at"])
			out = append(out, FeishuProjectWorkItem{
				ID:          id,
				Type:        typ,
				Title:       firstNonEmpty(record["name"], record["title"]),
				Description: record["description"],
				Status:      status,
				OwnerEmail:  extractEmail(firstNonEmpty(record["current_status_operator"], record["owner"], record["operator"])),
				UpdatedAt:   updatedAt,
				URL:         fmt.Sprintf("https://project.feishu.cn/%s/%s/detail/%s", projectKey, typ, id),
			})
		}
	}
	return out
}

func feishuProjectMQLCount(payload map[string]any) (int, bool) {
	rows, _ := payload["list"].([]any)
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		switch v := row["count"].(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		case int64:
			return int(v), true
		case json.Number:
			n, err := strconv.Atoi(v.String())
			return n, err == nil
		case string:
			n, err := strconv.Atoi(v)
			return n, err == nil
		}
	}
	return 0, false
}

func feishuProjectOpenAPITotal(payload map[string]any) (int, bool) {
	pagination, _ := payload["pagination"].(map[string]any)
	switch v := pagination["total"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := strconv.Atoi(v.String())
		return n, err == nil
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseFeishuProjectSearch(payload map[string]any, typ, projectKey string) []FeishuProjectWorkItem {
	var out []FeishuProjectWorkItem
	rows, _ := payload["data"].([]any)
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		id := firstNonEmpty(feishuProjectIDString(row["id"]), feishuProjectIDString(row["work_item_id"]))
		if id == "" {
			continue
		}
		record := map[string]string{
			"name":               fmt.Sprint(row["name"]),
			"sub_stage":          fmt.Sprint(row["sub_stage"]),
			"current_status":     fmt.Sprint(row["current_status"]),
			"work_item_status":   feishuProjectStatusValue(row["work_item_status"]),
			"current_status_key": feishuProjectStatusValue(row["current_status_key"]),
		}
		if ts := feishuProjectTime(row["updated_at"]); !ts.IsZero() {
			record["updated_at"] = ts.Format(time.RFC3339Nano)
		}
		fields, _ := row["fields"].([]any)
		for _, fieldAny := range fields {
			field, _ := fieldAny.(map[string]any)
			key := firstNonEmpty(fmt.Sprint(field["field_key"]), fmt.Sprint(field["field_alias"]))
			if key == "" {
				continue
			}
			if value := feishuProjectOpenAPIFieldValue(field); value != "" {
				record[key] = value
			}
		}
		multiTexts, _ := row["multi_texts"].([]any)
		for _, fieldAny := range multiTexts {
			field, _ := fieldAny.(map[string]any)
			key := fmt.Sprint(field["field_key"])
			if key == "" {
				continue
			}
			if value := feishuProjectOpenAPIFieldValue(field); value != "" {
				record[key] = value
			}
		}
		updatedAt, _ := time.Parse(time.RFC3339Nano, record["updated_at"])
		out = append(out, FeishuProjectWorkItem{
			ID:          id,
			Type:        typ,
			Title:       firstNonEmpty(record["name"], record["title"]),
			Description: record["description"],
			Status:      firstNonEmpty(record["work_item_status"], record["sub_stage"], record["status"]),
			OwnerEmail:  extractEmail(firstNonEmpty(record["current_status_operator"], record["owner"], record["operator"])),
			UpdatedAt:   updatedAt,
			URL:         fmt.Sprintf("https://project.feishu.cn/%s/%s/detail/%s", projectKey, typ, id),
		})
	}
	return out
}

func parseFeishuProjectStatusOptions(payload map[string]any) []FeishuProjectStatusOption {
	var out []FeishuProjectStatusOption
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if fmt.Sprint(x["field_key"]) == "work_item_status" || fmt.Sprint(x["field_type"]) == "_work_item_status" || fmt.Sprint(x["field_type_key"]) == "_work_item_status" {
				options, _ := x["option"].([]any)
				if len(options) == 0 {
					options, _ = x["options"].([]any)
				}
				for _, optionAny := range options {
					option, _ := optionAny.(map[string]any)
					key := firstNonEmpty(fmt.Sprint(option["option_id"]), fmt.Sprint(option["value"]))
					name := firstNonEmpty(fmt.Sprint(option["option_name"]), fmt.Sprint(option["label"]))
					if key == "" || key == "<nil>" {
						continue
					}
					out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(name, key)})
				}
				return true
			}
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	walk(payload)
	return out
}

func parseFeishuProjectTemplateIDs(payload map[string]any) []string {
	rows, _ := payload["data"].([]any)
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		id := firstNonEmpty(fmt.Sprint(row["template_id"]), fmt.Sprint(row["id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func parseFeishuProjectStateFlowStatuses(payload map[string]any) []FeishuProjectStatusOption {
	var out []FeishuProjectStatusOption
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if rows, _ := x["state_flow_confs"].([]any); len(rows) > 0 {
				for _, rowAny := range rows {
					row, _ := rowAny.(map[string]any)
					key := firstNonEmpty(fmt.Sprint(row["state_key"]), fmt.Sprint(row["key"]))
					name := firstNonEmpty(fmt.Sprint(row["name"]), fmt.Sprint(row["state_name"]), key)
					if key != "" {
						out = append(out, FeishuProjectStatusOption{Key: key, Name: name})
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(payload)
	return appendFeishuProjectStatuses(nil, out...)
}

func appendFeishuProjectStatuses(base []FeishuProjectStatusOption, items ...FeishuProjectStatusOption) []FeishuProjectStatusOption {
	seen := make(map[string]bool, len(base)+len(items))
	out := make([]FeishuProjectStatusOption, 0, len(base)+len(items))
	for _, item := range append(base, items...) {
		key := strings.TrimSpace(item.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(item.Name, key)})
	}
	return out
}

func parseFeishuProjectWorkflowStatuses(payload map[string]any) []FeishuProjectStatusOption {
	data, _ := payload["data"].(map[string]any)
	seen := map[string]bool{}
	var out []FeishuProjectStatusOption
	add := func(key, name string) {
		key = firstNonEmpty(key)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(name, key)})
	}
	nodes, _ := data["state_flow_nodes"].([]any)
	for _, nodeAny := range nodes {
		node, _ := nodeAny.(map[string]any)
		add(fmt.Sprint(node["id"]), fmt.Sprint(node["name"]))
	}
	connections, _ := data["connections"].([]any)
	for _, connAny := range connections {
		conn, _ := connAny.(map[string]any)
		add(fmt.Sprint(conn["source_state_key"]), "")
		add(fmt.Sprint(conn["target_state_key"]), "")
	}
	return out
}

func feishuProjectFieldValue(field map[string]any) string {
	value, _ := field["value"].(map[string]any)
	for _, key := range []string{"long_value", "string_value"} {
		if v, ok := value[key]; ok {
			return fmt.Sprint(v)
		}
	}
	if values, _ := value["key_label_value_list"].([]any); len(values) > 0 {
		first, _ := values[0].(map[string]any)
		return firstNonEmpty(fmt.Sprint(first["key"]), fmt.Sprint(first["label"]))
	}
	if values, _ := value["string_value_list"].([]any); len(values) > 0 {
		return fmt.Sprint(values[0])
	}
	return ""
}

func feishuProjectOpenAPIFieldValue(field map[string]any) string {
	value := field["field_value"]
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return fmt.Sprint(int64(v))
	case map[string]any:
		if text, _ := v["doc_text"].(string); text != "" {
			return text
		}
		return firstNonEmpty(fmt.Sprint(v["label"]), fmt.Sprint(v["name"]), fmt.Sprint(v["email"]), fmt.Sprint(v["value"]))
	case []any:
		values := make([]string, 0, len(v))
		for _, itemAny := range v {
			switch item := itemAny.(type) {
			case string:
				values = append(values, item)
			case map[string]any:
				values = append(values, firstNonEmpty(fmt.Sprint(item["email"]), fmt.Sprint(item["name_cn"]), fmt.Sprint(item["name_en"]), fmt.Sprint(item["name"]), fmt.Sprint(item["label"]), fmt.Sprint(item["value"])))
			default:
				values = append(values, fmt.Sprint(item))
			}
		}
		return strings.Join(values, ", ")
	default:
		return fmt.Sprint(v)
	}
}

func feishuProjectStatusValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		return firstNonEmpty(fmt.Sprint(x["state_key"]), fmt.Sprint(x["name"]), fmt.Sprint(x["label"]))
	default:
		return ""
	}
}

func feishuProjectTime(v any) time.Time {
	switch x := v.(type) {
	case float64:
		return time.UnixMilli(int64(x))
	case int64:
		return time.UnixMilli(x)
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return time.UnixMilli(n)
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", x); err == nil {
			return t
		}
	}
	return time.Time{}
}

func findTransitionID(payload map[string]any, target string) string {
	raw, _ := json.Marshal(payload)
	var walk func(any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			label := firstNonEmpty(
				fmt.Sprint(x["target_state_key"]),
				fmt.Sprint(x["state_key"]),
				fmt.Sprint(x["id"]),
				fmt.Sprint(x["name"]),
				fmt.Sprint(x["state_name"]),
				fmt.Sprint(x["end_state_key_name"]),
			)
			if label == target {
				for _, key := range []string{"transition_id", "uuid"} {
					if x[key] != nil {
						return feishuProjectIDString(x[key])
					}
				}
			}
			for _, child := range x {
				if got := walk(child); got != "" {
					return got
				}
			}
		case []any:
			for _, child := range x {
				if got := walk(child); got != "" {
					return got
				}
			}
		}
		return ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	return walk(decoded)
}

func feishuProjectIDString(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func escapeMQLIdent(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "")
}

func mqlWhereClause(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return " WHERE " + s
}

func feishuProjectToolError(payload map[string]any) string {
	if payload == nil || payload["error"] == nil {
		return ""
	}
	raw, _ := json.Marshal(payload["error"])
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil {
		return firstNonEmpty(parsed.Message, parsed.Code, string(raw))
	}
	return string(raw)
}

func feishuProjectAPIError(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if code, ok := payload["err_code"]; ok && fmt.Sprint(code) != "0" {
		return firstNonEmpty(fmt.Sprint(payload["err_msg"]), fmt.Sprint(payload["message"]), fmt.Sprint(code))
	}
	if code, ok := payload["code"]; ok && fmt.Sprint(code) != "0" {
		return firstNonEmpty(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), fmt.Sprint(code))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

var feishuProjectEmailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func extractEmail(s string) string {
	return strings.ToLower(feishuProjectEmailRe.FindString(s))
}

func UUIDString(id pgtype.UUID) string {
	return util.UUIDToString(id)
}
