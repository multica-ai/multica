package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── list_issues ──────────────────────────────────────────────────────────────

// FirtalListIssuesTool implements Tool for listing issues in the workspace.
type FirtalListIssuesTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalListIssuesTool) Name() string { return "list_issues" }
func (t *FirtalListIssuesTool) Description() string {
	return "List issues in the current workspace. Optionally filter by status. Returns up to 20 issues."
}
func (t *FirtalListIssuesTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{},
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status: todo, in_progress, in_review, done, blocked, backlog, cancelled",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max issues to return (default 20, max 50)",
			},
		},
	}
}
func (t *FirtalListIssuesTool) Call(ctx context.Context, args map[string]any) (string, error) {
	params := db.ListIssuesParams{
		WorkspaceID: t.tctx.WorkspaceID,
		IsAdmin:     true, // agent operates as admin within its workspace
		Limit:       20,
		Offset:      0,
	}
	if s, ok := args["status"].(string); ok && s != "" {
		params.Status = pgtype.Text{String: s, Valid: true}
	}
	if lv, ok := args["limit"]; ok {
		switch v := lv.(type) {
		case float64:
			params.Limit = int32(v)
		case int:
			params.Limit = int32(v)
		}
		if params.Limit <= 0 || params.Limit > 50 {
			params.Limit = 20
		}
	}

	issues, err := t.queries.ListIssues(ctx, params)
	if err != nil {
		return "", fmt.Errorf("list issues: %w", err)
	}

	out := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		out = append(out, map[string]any{
			"id":       util.UUIDToString(issue.ID),
			"number":   issue.Number,
			"title":    issue.Title,
			"status":   issue.Status,
			"priority": issue.Priority,
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshal list_issues result: %w", err)
	}
	return string(raw), nil
}

// ── create_issue ─────────────────────────────────────────────────────────────

// FirtalCreateIssueTool implements Tool for creating a new issue.
type FirtalCreateIssueTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalCreateIssueTool) Name() string { return "create_issue" }
func (t *FirtalCreateIssueTool) Description() string {
	return "Create a new issue in the current workspace. Returns the created issue's ID and number."
}
func (t *FirtalCreateIssueTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"title"},
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Issue title",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Issue description (markdown)",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Initial status: todo, in_progress, backlog (default: todo)",
			},
			"priority": map[string]any{
				"type":        "string",
				"description": "Priority: urgent, high, medium, low (default: medium)",
			},
			"parent_id": map[string]any{
				"type":        "string",
				"description": "UUID of the parent issue (optional)",
			},
		},
	}
}
func (t *FirtalCreateIssueTool) Call(ctx context.Context, args map[string]any) (string, error) {
	title, ok := args["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("create_issue: title is required")
	}

	if !t.tctx.AgentID.Valid {
		return "", fmt.Errorf("create_issue: missing agent context")
	}

	// Increment workspace issue counter to get the next number.
	number, err := t.queries.IncrementIssueCounter(ctx, t.tctx.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("create_issue: increment counter: %w", err)
	}

	status := "todo"
	if s, ok := args["status"].(string); ok && s != "" {
		status = s
	}
	priority := "medium"
	if p, ok := args["priority"].(string); ok && p != "" {
		priority = p
	}

	params := db.CreateIssueParams{
		WorkspaceID: t.tctx.WorkspaceID,
		Title:       strings.TrimSpace(title),
		Status:      status,
		Priority:    priority,
		CreatorType: "agent",
		CreatorID:   t.tctx.AgentID,
		Number:      number,
		Position:    0,
	}
	if desc, ok := args["description"].(string); ok && desc != "" {
		params.Description = pgtype.Text{String: desc, Valid: true}
	}
	if parentRef, ok := args["parent_id"].(string); ok && parentRef != "" {
		parentID, err := util.ParseUUID(parentRef)
		if err == nil {
			params.ParentIssueID = parentID
		}
	}

	issue, err := t.queries.CreateIssue(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create_issue: %w", err)
	}

	raw, err := json.Marshal(map[string]any{
		"id":     util.UUIDToString(issue.ID),
		"number": issue.Number,
		"title":  issue.Title,
		"status": issue.Status,
	})
	if err != nil {
		return "", fmt.Errorf("create_issue: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── update_issue ─────────────────────────────────────────────────────────────

// FirtalUpdateIssueTool implements Tool for updating an existing issue's
// status, title, or priority.
type FirtalUpdateIssueTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalUpdateIssueTool) Name() string { return "update_issue" }
func (t *FirtalUpdateIssueTool) Description() string {
	return "Update a Multica issue's status, title, or priority. issue_id accepts a UUID or identifier like \"JEH-123\"."
}
func (t *FirtalUpdateIssueTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. JEH-123)",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "New status: todo, in_progress, in_review, done, blocked, backlog, cancelled",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "New title",
			},
			"priority": map[string]any{
				"type":        "string",
				"description": "New priority: urgent, high, medium, low",
			},
		},
	}
}
func (t *FirtalUpdateIssueTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, ok := args["issue_id"].(string)
	if !ok || strings.TrimSpace(issueRef) == "" {
		return "", fmt.Errorf("update_issue: issue_id is required")
	}

	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}

	params := db.UpdateIssueParams{
		ID:          issue.ID,
		Title:       pgtype.Text{String: issue.Title, Valid: true},
		Description: issue.Description,
		Status:      pgtype.Text{String: issue.Status, Valid: true},
		Priority:    pgtype.Text{String: issue.Priority, Valid: true},
		AssigneeType: issue.AssigneeType,
		AssigneeID:  issue.AssigneeID,
	}

	if s, ok := args["status"].(string); ok && s != "" {
		params.Status = pgtype.Text{String: s, Valid: true}
	}
	if ttl, ok := args["title"].(string); ok && ttl != "" {
		params.Title = pgtype.Text{String: ttl, Valid: true}
	}
	if p, ok := args["priority"].(string); ok && p != "" {
		params.Priority = pgtype.Text{String: p, Valid: true}
	}

	updated, err := t.queries.UpdateIssue(ctx, params)
	if err != nil {
		return "", fmt.Errorf("update_issue: %w", err)
	}

	raw, err := json.Marshal(map[string]any{
		"id":     util.UUIDToString(updated.ID),
		"title":  updated.Title,
		"status": updated.Status,
	})
	if err != nil {
		return "", fmt.Errorf("update_issue: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── assign_issue ─────────────────────────────────────────────────────────────

// FirtalAssignIssueTool implements Tool for assigning an issue to an agent or
// member by name or UUID.
type FirtalAssignIssueTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalAssignIssueTool) Name() string { return "assign_issue" }
func (t *FirtalAssignIssueTool) Description() string {
	return "Assign a Multica issue to an agent or workspace member. Provide either assignee_id (UUID) or assignee_name (fuzzy match)."
}
func (t *FirtalAssignIssueTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. JEH-123)",
			},
			"assignee_id": map[string]any{
				"type":        "string",
				"description": "UUID of the agent or member to assign to",
			},
			"assignee_type": map[string]any{
				"type":        "string",
				"description": "\"agent\" or \"member\" (required when providing assignee_id)",
			},
		},
	}
}
func (t *FirtalAssignIssueTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, ok := args["issue_id"].(string)
	if !ok || strings.TrimSpace(issueRef) == "" {
		return "", fmt.Errorf("assign_issue: issue_id is required")
	}

	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}

	assigneeIDStr, _ := args["assignee_id"].(string)
	assigneeType, _ := args["assignee_type"].(string)

	if strings.TrimSpace(assigneeIDStr) == "" {
		return "", fmt.Errorf("assign_issue: assignee_id is required")
	}
	assigneeID, err := util.ParseUUID(strings.TrimSpace(assigneeIDStr))
	if err != nil {
		return "", fmt.Errorf("assign_issue: invalid assignee_id %q: %w", assigneeIDStr, err)
	}
	if assigneeType == "" {
		assigneeType = "member"
	}

	updated, err := t.queries.UpdateIssueAssignee(ctx, db.UpdateIssueAssigneeParams{
		ID:           issue.ID,
		AssigneeType: pgtype.Text{String: assigneeType, Valid: true},
		AssigneeID:   assigneeID,
	})
	if err != nil {
		return "", fmt.Errorf("assign_issue: %w", err)
	}

	raw, err := json.Marshal(map[string]any{
		"id":            util.UUIDToString(updated.ID),
		"assignee_id":   util.UUIDToString(updated.AssigneeID),
		"assignee_type": updated.AssigneeType.String,
	})
	if err != nil {
		return "", fmt.Errorf("assign_issue: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── firtal_bq_query ──────────────────────────────────────────────────────────

// FirtalBQQueryTool implements Tool for running BigQuery SQL via the Firtal
// Data Registry query endpoint.
type FirtalBQQueryTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalBQQueryTool) Name() string { return "firtal_bq_query" }
func (t *FirtalBQQueryTool) Description() string {
	return "Run a BigQuery SQL query via Firtal Data Registry. Returns results as JSON array."
}
func (t *FirtalBQQueryTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"query"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "SQL query to execute",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max rows to return (default 100)",
			},
		},
	}
}

type fdrWorkspaceSettings struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}
type fdrSettingsEnvelope struct {
	DataRegistry *fdrWorkspaceSettings `json:"data_registry"`
	// Firtal gateway settings are also checked as a fallback source
	FirtalGateway *WorkspaceFirtalGatewaySettings `json:"firtal_gateway"`
}

func (t *FirtalBQQueryTool) Call(ctx context.Context, args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("firtal_bq_query: query is required")
	}

	limit := 100
	if lv, ok := args["limit"]; ok {
		switch v := lv.(type) {
		case float64:
			limit = int(v)
		case int:
			limit = v
		}
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
	}

	// Load workspace settings to get FDR URL + key.
	ws, err := t.queries.GetWorkspace(ctx, t.tctx.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("firtal_bq_query: load workspace: %w", err)
	}

	var envelope fdrSettingsEnvelope
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &envelope)
	}

	var baseURL, apiKey string
	if envelope.DataRegistry != nil {
		baseURL = strings.TrimRight(envelope.DataRegistry.BaseURL, "/")
		apiKey = envelope.DataRegistry.APIKey
	}
	// Fallback to firtal_gateway URL/key if data_registry not set
	if baseURL == "" && envelope.FirtalGateway != nil {
		baseURL = strings.TrimRight(envelope.FirtalGateway.GatewayURL, "/")
		apiKey = envelope.FirtalGateway.APIKey
	}
	if baseURL == "" {
		return "", fmt.Errorf("firtal_bq_query: data registry URL not configured in workspace settings")
	}

	reqBody, err := json.Marshal(map[string]any{
		"sql":      query,
		"max_rows": limit,
	})
	if err != nil {
		return "", fmt.Errorf("firtal_bq_query: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/data/query", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("firtal_bq_query: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("firtal_bq_query: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("firtal_bq_query: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("firtal_bq_query: HTTP %d: %s", resp.StatusCode, truncateGatewayError(string(body), 512))
	}
	return string(body), nil
}

// ── web_fetch ─────────────────────────────────────────────────────────────────

var (
	htmlTagRE  = regexp.MustCompile(`<[^>]+>`)
	htmlSpaceRE = regexp.MustCompile(`\s{2,}`)
)

const webFetchMaxBytes = 50 * 1024 // 50 KB

// webFetchAllowlist is the default URL allowlist when no config is set.
var webFetchAllowlist = []string{
	".firtal.com",
	"docs.anthropic.com",
}

// WebFetchTool implements Tool for fetching web page text content.
type WebFetchTool struct {
	configAllowlist []string // from agent_tool_grant.config_json, nil = use default
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch the text content of a URL. Only fetches URLs that match the configured allowlist."
}
func (t *WebFetchTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"url"},
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch",
			},
		},
	}
}
func (t *WebFetchTool) Call(ctx context.Context, args map[string]any) (string, error) {
	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", fmt.Errorf("web_fetch: url is required")
	}
	rawURL = strings.TrimSpace(rawURL)

	// Validate and check against allowlist.
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("web_fetch: invalid URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())

	allowlist := t.configAllowlist
	if len(allowlist) == 0 {
		allowlist = webFetchAllowlist
	}

	allowed := false
	for _, pattern := range allowlist {
		if host == strings.TrimPrefix(pattern, ".") || strings.HasSuffix(host, pattern) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("web_fetch: URL host %q is not in the allowlist", host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Multica-Agent/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(webFetchMaxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("web_fetch: read body: %w", err)
	}

	text := string(body)
	// Strip HTML tags with a simple regex fallback (no x/net/html available).
	text = htmlTagRE.ReplaceAllString(text, " ")
	text = htmlSpaceRE.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	if len(text) > webFetchMaxBytes {
		text = text[:webFetchMaxBytes] + "\n[truncated]"
	}
	return text, nil
}

// ── gogcli_sheets_write ──────────────────────────────────────────────────────

// SheetsWriteTool implements Tool for writing data to Google Sheets.
type SheetsWriteTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *SheetsWriteTool) Name() string { return "gogcli_sheets_write" }
func (t *SheetsWriteTool) Description() string {
	return "Write data to a Google Sheets spreadsheet range."
}
func (t *SheetsWriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"spreadsheet_id", "range", "values"},
		"properties": map[string]any{
			"spreadsheet_id": map[string]any{
				"type":        "string",
				"description": "Google Sheets spreadsheet ID",
			},
			"range": map[string]any{
				"type":        "string",
				"description": "A1 notation range, e.g. Sheet1!A1",
			},
			"values": map[string]any{
				"type":        "array",
				"description": "2D array of values to write (rows × columns)",
				"items": map[string]any{
					"type": "array",
				},
			},
		},
	}
}

type googleSettings struct {
	AccessToken string `json:"google_access_token"`
}

func (t *SheetsWriteTool) Call(ctx context.Context, args map[string]any) (string, error) {
	spreadsheetID, ok := args["spreadsheet_id"].(string)
	if !ok || strings.TrimSpace(spreadsheetID) == "" {
		return "", fmt.Errorf("gogcli_sheets_write: spreadsheet_id is required")
	}
	rangeStr, ok := args["range"].(string)
	if !ok || strings.TrimSpace(rangeStr) == "" {
		return "", fmt.Errorf("gogcli_sheets_write: range is required")
	}
	valuesRaw, ok := args["values"]
	if !ok {
		return "", fmt.Errorf("gogcli_sheets_write: values is required")
	}

	// Load Google access token from workspace settings.
	ws, err := t.queries.GetWorkspace(ctx, t.tctx.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("gogcli_sheets_write: load workspace: %w", err)
	}
	var gSettings googleSettings
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &gSettings)
	}
	if strings.TrimSpace(gSettings.AccessToken) == "" {
		return "", fmt.Errorf("gogcli_sheets_write: google_access_token not configured in workspace settings")
	}

	reqBody, err := json.Marshal(map[string]any{
		"range":          rangeStr,
		"majorDimension": "ROWS",
		"values":         valuesRaw,
	})
	if err != nil {
		return "", fmt.Errorf("gogcli_sheets_write: marshal request: %w", err)
	}

	sheetsURL := fmt.Sprintf(
		"https://sheets.googleapis.com/v4/spreadsheets/%s/values/%s?valueInputOption=RAW",
		url.PathEscape(spreadsheetID),
		url.PathEscape(rangeStr),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, sheetsURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("gogcli_sheets_write: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(gSettings.AccessToken))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gogcli_sheets_write: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("gogcli_sheets_write: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gogcli_sheets_write: HTTP %d: %s", resp.StatusCode, truncateGatewayError(string(body), 512))
	}

	var sheetsResp map[string]any
	if err := json.Unmarshal(body, &sheetsResp); err != nil {
		return string(body), nil
	}
	updatedCells, _ := sheetsResp["updatedCells"].(float64)
	raw, _ := json.Marshal(map[string]any{
		"cells_updated": int(updatedCells),
		"range":         sheetsResp["updatedRange"],
	})
	return string(raw), nil
}

// ── get_issue ─────────────────────────────────────────────────────────────────

// FirtalGetIssueTool implements Tool for reading a single issue with comments.
type FirtalGetIssueTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalGetIssueTool) Name() string { return "get_issue" }
func (t *FirtalGetIssueTool) Description() string {
	return "Get full details of a Multica issue: title, description, status, priority, and comments. issue_id accepts a UUID or identifier like \"JEH-123\"."
}
func (t *FirtalGetIssueTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. JEH-123)",
			},
		},
	}
}
func (t *FirtalGetIssueTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, ok := args["issue_id"].(string)
	if !ok || strings.TrimSpace(issueRef) == "" {
		return "", fmt.Errorf("get_issue: issue_id is required")
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	comments, err := t.queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: t.tctx.WorkspaceID,
		Limit:       firtalGatewayIssueCommentCap,
	})
	if err != nil {
		return "", fmt.Errorf("get_issue: list comments: %w", err)
	}
	result := map[string]any{
		"id":       util.UUIDToString(issue.ID),
		"number":   issue.Number,
		"title":    issue.Title,
		"status":   issue.Status,
		"priority": issue.Priority,
		"comments": formatComments(comments),
	}
	if desc := strings.TrimSpace(issue.Description.String); desc != "" {
		result["description"] = desc
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("get_issue: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── list_comments ─────────────────────────────────────────────────────────────

// FirtalListCommentsTool implements Tool for listing comments on an issue.
type FirtalListCommentsTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalListCommentsTool) Name() string { return "list_comments" }
func (t *FirtalListCommentsTool) Description() string {
	return "List all comments on a Multica issue in chronological order. issue_id accepts a UUID or identifier like \"JEH-123\"."
}
func (t *FirtalListCommentsTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. JEH-123)",
			},
		},
	}
}
func (t *FirtalListCommentsTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, ok := args["issue_id"].(string)
	if !ok || strings.TrimSpace(issueRef) == "" {
		return "", fmt.Errorf("list_comments: issue_id is required")
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	comments, err := t.queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: t.tctx.WorkspaceID,
		Limit:       firtalGatewayIssueCommentCap,
	})
	if err != nil {
		return "", fmt.Errorf("list_comments: %w", err)
	}
	raw, err := json.Marshal(map[string]any{
		"issue_id": util.UUIDToString(issue.ID),
		"comments": formatComments(comments),
	})
	if err != nil {
		return "", fmt.Errorf("list_comments: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── add_comment ───────────────────────────────────────────────────────────────

// FirtalAddCommentTool implements Tool for posting a comment on an issue.
type FirtalAddCommentTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalAddCommentTool) Name() string { return "add_comment" }
func (t *FirtalAddCommentTool) Description() string {
	return "Post a comment on a Multica issue as the calling agent. issue_id accepts a UUID or identifier like \"JEH-123\"."
}
func (t *FirtalAddCommentTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "content"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. JEH-123)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Markdown body of the comment",
			},
		},
	}
}
func (t *FirtalAddCommentTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, ok := args["issue_id"].(string)
	if !ok || strings.TrimSpace(issueRef) == "" {
		return "", fmt.Errorf("add_comment: issue_id is required")
	}
	content, ok := args["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("add_comment: content is required")
	}
	if !t.tctx.AgentID.Valid {
		return "", fmt.Errorf("add_comment: missing agent context")
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	comment, err := t.queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: t.tctx.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    t.tctx.AgentID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return "", fmt.Errorf("add_comment: %w", err)
	}
	raw, err := json.Marshal(map[string]any{
		"id":         util.UUIDToString(comment.ID),
		"issue_id":   util.UUIDToString(comment.IssueID),
		"created_at": comment.CreatedAt.Time.Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("add_comment: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── list_projects ─────────────────────────────────────────────────────────────

// FirtalListProjectsTool implements Tool for listing projects in the workspace.
type FirtalListProjectsTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalListProjectsTool) Name() string { return "list_projects" }
func (t *FirtalListProjectsTool) Description() string {
	return "List all projects in the current workspace with their status and priority."
}
func (t *FirtalListProjectsTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   []string{},
		"properties": map[string]any{},
	}
}
func (t *FirtalListProjectsTool) Call(ctx context.Context, args map[string]any) (string, error) {
	projects, err := t.queries.ListProjects(ctx, db.ListProjectsParams{
		WorkspaceID: t.tctx.WorkspaceID,
	})
	if err != nil {
		return "", fmt.Errorf("list_projects: %w", err)
	}
	out := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		out = append(out, map[string]any{
			"id":     util.UUIDToString(p.ID),
			"title":  p.Title,
			"status": p.Status,
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("list_projects: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── get_me ────────────────────────────────────────────────────────────────────

// FirtalGetMeTool implements Tool for returning the calling agent's identity.
type FirtalGetMeTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalGetMeTool) Name() string { return "get_me" }
func (t *FirtalGetMeTool) Description() string {
	return "Return the calling agent's ID, name, and workspace context. Use this to confirm your own identity before taking workspace actions."
}
func (t *FirtalGetMeTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   []string{},
		"properties": map[string]any{},
	}
}
func (t *FirtalGetMeTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if !t.tctx.AgentID.Valid {
		return "", fmt.Errorf("get_me: missing agent context")
	}
	agent, err := t.queries.GetAgent(ctx, t.tctx.AgentID)
	if err != nil {
		return "", fmt.Errorf("get_me: %w", err)
	}
	raw, err := json.Marshal(map[string]any{
		"agent_id":     util.UUIDToString(agent.ID),
		"name":         agent.Name,
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
	})
	if err != nil {
		return "", fmt.Errorf("get_me: marshal result: %w", err)
	}
	return string(raw), nil
}

// ── Registration helpers ──────────────────────────────────────────────────────

// NewDefaultRegistry creates a Registry, registers all built-in tools, and
// returns it ready for wiring into the executor.
func NewDefaultRegistry(pool interface {
	// We accept a pgxpool.Pool but avoid importing pgxpool at call sites by
	// letting callers pass nil to get a stub. The real pool arrives via
	// NewRegistry.
}, queries *db.Queries, tctx ToolContext) *Registry {
	// For compatibility, the registry only needs a *pgxpool.Pool for DB-backed
	// lookups. Callers that wire the full stack pass it via NewRegistry.
	// This helper focuses on tool registration.
	r := &Registry{tools: make(map[string]Tool)}
	registerBuiltinTools(r, queries, tctx)
	return r
}

// registerBuiltinTools registers all built-in tools with the given registry.
// Called at executor startup with the per-task ToolContext.
func registerBuiltinTools(r *Registry, queries *db.Queries, tctx ToolContext) {
	r.Register(&FirtalGetIssueTool{queries: queries, tctx: tctx})
	r.Register(&FirtalListIssuesTool{queries: queries, tctx: tctx})
	r.Register(&FirtalCreateIssueTool{queries: queries, tctx: tctx})
	r.Register(&FirtalUpdateIssueTool{queries: queries, tctx: tctx})
	r.Register(&FirtalAssignIssueTool{queries: queries, tctx: tctx})
	r.Register(&FirtalListCommentsTool{queries: queries, tctx: tctx})
	r.Register(&FirtalAddCommentTool{queries: queries, tctx: tctx})
	r.Register(&FirtalListProjectsTool{queries: queries, tctx: tctx})
	r.Register(&FirtalGetMeTool{queries: queries, tctx: tctx})
	r.Register(&FirtalBQQueryTool{queries: queries, tctx: tctx})
	r.Register(&WebFetchTool{})
	r.Register(&SheetsWriteTool{queries: queries, tctx: tctx})
}
