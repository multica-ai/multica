package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	qoderCloudToolMaxHTTPResponseBytes = 1 << 20
	qoderCloudToolMaxOutputBytes       = 48 << 10
	qoderCloudToolMaxTitleRunes        = 500
	qoderCloudToolMaxTextRunes         = 20_000
	qoderCloudToolDefaultListLimit     = 20
	qoderCloudToolMaxListLimit         = 50
)

var qoderCloudToolIssueStatuses = map[string]struct{}{
	"backlog": {}, "todo": {}, "in_progress": {}, "in_review": {},
	"done": {}, "blocked": {}, "cancelled": {},
}

var qoderCloudToolIssuePriorities = map[string]struct{}{
	"urgent": {}, "high": {}, "medium": {}, "low": {}, "none": {},
}

type qoderCloudToolDispatcher struct {
	client *Client
	task   Task
	token  string
}

func newQoderCloudCustomToolHandler(baseURL, daemonVersion string, task Task) (agent.CustomToolHandler, error) {
	token, err := taskScopedAuthToken(task)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(task.WorkspaceID) == "" {
		return nil, errors.New("qoder cloud custom tools require a workspace")
	}
	client := NewClient(baseURL)
	client.SetToken(token)
	client.SetVersion(daemonVersion)
	dispatcher := &qoderCloudToolDispatcher{client: client, task: task, token: token}
	return dispatcher.handle, nil
}

func (d *qoderCloudToolDispatcher) handle(ctx context.Context, call agent.CustomToolCall) (agent.CustomToolResult, error) {
	var (
		output any
		err    error
	)
	switch call.Name {
	case "multica_list_issues":
		output, err = d.listIssues(ctx, call.Input)
	case "multica_get_issue":
		output, err = d.getIssue(ctx, call.Input)
	case "multica_list_issue_comments":
		output, err = d.listIssueComments(ctx, call.Input)
	case "multica_create_issue":
		output, err = d.createIssue(ctx, call.Input)
	case "multica_update_issue":
		output, err = d.updateIssue(ctx, call.Input)
	case "multica_add_issue_comment":
		output, err = d.addIssueComment(ctx, call.Input)
	default:
		err = errors.New("custom tool is not allowlisted")
	}
	if err != nil {
		return agent.CustomToolResult{}, d.safeError(err)
	}
	content, err := d.outputJSON(output)
	if err != nil {
		return agent.CustomToolResult{}, err
	}
	return agent.CustomToolResult{Content: content}, nil
}

func (d *qoderCloudToolDispatcher) listIssues(ctx context.Context, input map[string]any) (any, error) {
	if err := requireQoderCloudToolKeys(input, "status", "limit"); err != nil {
		return nil, err
	}
	status, _, err := optionalQoderCloudToolString(input, "status", 32, false)
	if err != nil {
		return nil, err
	}
	if status != "" {
		if _, ok := qoderCloudToolIssueStatuses[status]; !ok {
			return nil, errors.New("status is invalid")
		}
	}
	limit, err := qoderCloudToolLimit(input, qoderCloudToolDefaultListLimit)
	if err != nil {
		return nil, err
	}
	if d.task.IssueID != "" {
		issue, err := d.fetchIssue(ctx, d.task.IssueID)
		if err != nil {
			return nil, fmt.Errorf("get assigned issue: %w", err)
		}
		if status != "" && issue["status"] != status {
			return map[string]any{"issues": []map[string]any{}, "total": 0}, nil
		}
		return map[string]any{
			"issues": []map[string]any{compactQoderCloudIssue(issue)},
			"total":  1,
		}, nil
	}
	params := url.Values{}
	params.Set("workspace_id", d.task.WorkspaceID)
	params.Set("limit", strconv.Itoa(limit))
	if status != "" {
		params.Set("status", status)
	}
	var response struct {
		Issues []map[string]any `json:"issues"`
		Total  int              `json:"total"`
	}
	if err := d.request(ctx, http.MethodGet, "/api/issues?"+params.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	issues := make([]map[string]any, 0, len(response.Issues))
	for _, issue := range response.Issues {
		if err := d.requireIssueWorkspace(issue); err != nil {
			return nil, err
		}
		issues = append(issues, compactQoderCloudIssue(issue))
	}
	return map[string]any{"issues": issues, "total": response.Total}, nil
}

func (d *qoderCloudToolDispatcher) getIssue(ctx context.Context, input map[string]any) (any, error) {
	issueID, err := d.issueIDInput(input)
	if err != nil {
		return nil, err
	}
	issue, err := d.fetchIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	return compactQoderCloudIssue(issue), nil
}

func (d *qoderCloudToolDispatcher) listIssueComments(ctx context.Context, input map[string]any) (any, error) {
	if err := requireQoderCloudToolKeys(input, "issue_id", "limit"); err != nil {
		return nil, err
	}
	issueID, err := d.issueIDValue(input)
	if err != nil {
		return nil, err
	}
	limit, err := qoderCloudToolLimit(input, qoderCloudToolDefaultListLimit)
	if err != nil {
		return nil, err
	}
	if _, err := d.fetchIssue(ctx, issueID); err != nil {
		return nil, fmt.Errorf("get issue before listing comments: %w", err)
	}
	params := url.Values{}
	params.Set("recent", strconv.Itoa(limit))
	var comments []map[string]any
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments?" + params.Encode()
	if err := d.request(ctx, http.MethodGet, path, nil, &comments); err != nil {
		return nil, fmt.Errorf("list issue comments: %w", err)
	}
	result := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		result = append(result, compactQoderCloudComment(comment))
	}
	return map[string]any{"comments": result, "count": len(result)}, nil
}

func (d *qoderCloudToolDispatcher) createIssue(ctx context.Context, input map[string]any) (any, error) {
	if err := requireQoderCloudToolKeys(input, "title", "description", "status", "priority"); err != nil {
		return nil, err
	}
	title, err := requiredQoderCloudToolString(input, "title", qoderCloudToolMaxTitleRunes)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"title": title}
	if description, present, err := optionalQoderCloudToolString(input, "description", qoderCloudToolMaxTextRunes, true); err != nil {
		return nil, err
	} else if present {
		body["description"] = description
	}
	if status, present, err := optionalQoderCloudToolString(input, "status", 32, false); err != nil {
		return nil, err
	} else if present {
		if _, ok := qoderCloudToolIssueStatuses[status]; !ok {
			return nil, errors.New("status is invalid")
		}
		body["status"] = status
	}
	if priority, present, err := optionalQoderCloudToolString(input, "priority", 32, false); err != nil {
		return nil, err
	} else if present {
		if _, ok := qoderCloudToolIssuePriorities[priority]; !ok {
			return nil, errors.New("priority is invalid")
		}
		body["priority"] = priority
	}
	var issue map[string]any
	path := "/api/issues?workspace_id=" + url.QueryEscape(d.task.WorkspaceID)
	if err := d.request(ctx, http.MethodPost, path, body, &issue); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	if err := d.requireIssueWorkspace(issue); err != nil {
		return nil, err
	}
	return compactQoderCloudIssue(issue), nil
}

func (d *qoderCloudToolDispatcher) updateIssue(ctx context.Context, input map[string]any) (any, error) {
	if err := requireQoderCloudToolKeys(input, "issue_id", "title", "description", "status", "priority"); err != nil {
		return nil, err
	}
	issueID, err := d.issueIDValue(input)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"suppress_run": true}
	changed := false
	if title, present, err := optionalQoderCloudToolString(input, "title", qoderCloudToolMaxTitleRunes, false); err != nil {
		return nil, err
	} else if present {
		body["title"] = title
		changed = true
	}
	if description, present, err := optionalQoderCloudToolString(input, "description", qoderCloudToolMaxTextRunes, true); err != nil {
		return nil, err
	} else if present {
		body["description"] = description
		changed = true
	}
	if status, present, err := optionalQoderCloudToolString(input, "status", 32, false); err != nil {
		return nil, err
	} else if present {
		if _, ok := qoderCloudToolIssueStatuses[status]; !ok {
			return nil, errors.New("status is invalid")
		}
		body["status"] = status
		changed = true
	}
	if priority, present, err := optionalQoderCloudToolString(input, "priority", 32, false); err != nil {
		return nil, err
	} else if present {
		if _, ok := qoderCloudToolIssuePriorities[priority]; !ok {
			return nil, errors.New("priority is invalid")
		}
		body["priority"] = priority
		changed = true
	}
	if !changed {
		return nil, errors.New("at least one update field is required")
	}
	var issue map[string]any
	if err := d.request(ctx, http.MethodPut, "/api/issues/"+url.PathEscape(issueID), body, &issue); err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}
	if err := d.requireIssueWorkspace(issue); err != nil {
		return nil, err
	}
	return compactQoderCloudIssue(issue), nil
}

func (d *qoderCloudToolDispatcher) addIssueComment(ctx context.Context, input map[string]any) (any, error) {
	if err := requireQoderCloudToolKeys(input, "issue_id", "content", "parent_id"); err != nil {
		return nil, err
	}
	issueID, err := d.issueIDValue(input)
	if err != nil {
		return nil, err
	}
	content, err := requiredQoderCloudToolString(input, "content", qoderCloudToolMaxTextRunes)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"content": content}
	parentID, parentPresent, err := optionalQoderCloudToolString(input, "parent_id", 64, false)
	if err != nil {
		return nil, err
	}
	if parentPresent {
		if _, err := uuid.Parse(parentID); err != nil {
			return nil, errors.New("parent_id must be a UUID")
		}
		body["parent_id"] = parentID
	}
	if d.task.TriggerCommentID != "" {
		if !parentPresent {
			return nil, errors.New("parent_id is required for a comment-triggered task")
		}
		if !d.allowedReplyParent(parentID) {
			return nil, errors.New("parent_id is outside the comments assigned to this task")
		}
	}
	if _, err := d.fetchIssue(ctx, issueID); err != nil {
		return nil, fmt.Errorf("get issue before adding comment: %w", err)
	}
	var comment map[string]any
	if err := d.request(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(issueID)+"/comments", body, &comment); err != nil {
		return nil, fmt.Errorf("add issue comment: %w", err)
	}
	return compactQoderCloudComment(comment), nil
}

func (d *qoderCloudToolDispatcher) issueIDInput(input map[string]any) (string, error) {
	if err := requireQoderCloudToolKeys(input, "issue_id"); err != nil {
		return "", err
	}
	return d.issueIDValue(input)
}

func (d *qoderCloudToolDispatcher) issueIDValue(input map[string]any) (string, error) {
	issueID, err := requiredQoderCloudToolString(input, "issue_id", 64)
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(issueID); err != nil {
		return "", errors.New("issue_id must be a UUID")
	}
	if d.task.IssueID != "" && issueID != d.task.IssueID {
		return "", errors.New("issue_id is outside the issue assigned to this task")
	}
	return issueID, nil
}

func (d *qoderCloudToolDispatcher) fetchIssue(ctx context.Context, issueID string) (map[string]any, error) {
	var issue map[string]any
	if err := d.request(ctx, http.MethodGet, "/api/issues/"+url.PathEscape(issueID), nil, &issue); err != nil {
		return nil, err
	}
	if err := d.requireIssueWorkspace(issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func (d *qoderCloudToolDispatcher) requireIssueWorkspace(issue map[string]any) error {
	workspaceID, ok := issue["workspace_id"].(string)
	if !ok || workspaceID == "" {
		return errors.New("Multica response omitted issue workspace_id")
	}
	if workspaceID != d.task.WorkspaceID {
		return errors.New("Multica response crossed the task workspace boundary")
	}
	return nil
}

func (d *qoderCloudToolDispatcher) allowedReplyParent(parentID string) bool {
	if parentID == d.task.TriggerCommentID {
		return true
	}
	for _, commentID := range d.task.CoalescedCommentIDs {
		if parentID == commentID {
			return true
		}
	}
	return false
}

func (d *qoderCloudToolDispatcher) request(ctx context.Context, method, path string, requestBody, responseBody any) error {
	err := d.client.requestJSONBounded(ctx, method, path, requestBody, responseBody, qoderCloudToolMaxHTTPResponseBytes)
	if err != nil {
		return d.safeError(err)
	}
	return nil
}

func (d *qoderCloudToolDispatcher) safeError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), d.token, "[REDACTED MULTICA TASK TOKEN]")
	return errors.New(redact.Text(message))
}

func (d *qoderCloudToolDispatcher) outputJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", d.safeError(fmt.Errorf("encode custom tool result: %w", err))
	}
	// Redact decoded string values, then marshal again. Running a plain-text
	// regexp over serialized JSON can consume closing quotes and braces and
	// leave the model with malformed output.
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return "", errors.New("normalize custom tool result")
	}
	normalized = redactQoderCloudToolValue(normalized, d.token, 0)
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return "", errors.New("encode redacted custom tool result")
	}
	output := string(encoded)
	if len(output) <= qoderCloudToolMaxOutputBytes {
		return output, nil
	}
	previewLimit := qoderCloudToolMaxOutputBytes / 2
	for previewLimit > 0 && !utf8.RuneStart(output[previewLimit]) {
		previewLimit--
	}
	truncated, err := json.Marshal(map[string]any{
		"truncated": true,
		"preview":   output[:previewLimit],
	})
	if err != nil {
		return "", errors.New("encode truncated custom tool result")
	}
	return string(truncated), nil
}

func redactQoderCloudToolValue(value any, taskToken string, depth int) any {
	if depth >= 32 {
		return "[REDACTED DEPTH LIMIT]"
	}
	switch value := value.(type) {
	case string:
		value = strings.ReplaceAll(value, taskToken, "[REDACTED MULTICA TASK TOKEN]")
		return redact.Text(value)
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = redactQoderCloudToolValue(item, taskToken, depth+1)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			result[index] = redactQoderCloudToolValue(item, taskToken, depth+1)
		}
		return result
	default:
		return value
	}
}

func requireQoderCloudToolKeys(input map[string]any, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range input {
		if _, ok := allowedSet[key]; !ok {
			return fmt.Errorf("unexpected input field %q", key)
		}
	}
	return nil
}

func requiredQoderCloudToolString(input map[string]any, key string, maxRunes int) (string, error) {
	value, present, err := optionalQoderCloudToolString(input, key, maxRunes, false)
	if err != nil {
		return "", err
	}
	if !present || value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalQoderCloudToolString(input map[string]any, key string, maxRunes int, allowEmpty bool) (string, bool, error) {
	raw, present := input[key]
	if !present {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", false, fmt.Errorf("%s contains invalid text", key)
	}
	if !allowEmpty {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", false, fmt.Errorf("%s must not be empty", key)
		}
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return "", false, fmt.Errorf("%s exceeds %d characters", key, maxRunes)
	}
	return value, true, nil
}

func qoderCloudToolLimit(input map[string]any, fallback int) (int, error) {
	raw, present := input["limit"]
	if !present {
		return fallback, nil
	}
	var value int64
	switch number := raw.(type) {
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, errors.New("limit must be an integer")
		}
		value = parsed
	case float64:
		value = int64(number)
		if float64(value) != number {
			return 0, errors.New("limit must be an integer")
		}
	case int:
		value = int64(number)
	default:
		return 0, errors.New("limit must be an integer")
	}
	if value < 1 || value > qoderCloudToolMaxListLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", qoderCloudToolMaxListLimit)
	}
	return int(value), nil
}

func compactQoderCloudIssue(issue map[string]any) map[string]any {
	return selectQoderCloudToolFields(issue,
		"id", "workspace_id", "identifier", "title", "description", "status", "priority",
		"assignee_type", "assignee_id", "parent_issue_id", "project_id", "start_date", "due_date",
		"created_at", "updated_at",
	)
}

func compactQoderCloudComment(comment map[string]any) map[string]any {
	return selectQoderCloudToolFields(comment,
		"id", "issue_id", "parent_id", "author_type", "author_id", "type", "content", "created_at", "updated_at",
	)
}

func selectQoderCloudToolFields(source map[string]any, fields ...string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, ok := source[field]; ok {
			result[field] = value
		}
	}
	return result
}
