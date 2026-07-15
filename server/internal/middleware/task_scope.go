package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const taskScopeMaxBodyBytes = 64 << 10

var taskScopeCanonicalUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var taskScopeIssueStatuses = map[string]struct{}{
	"backlog": {}, "todo": {}, "in_progress": {}, "in_review": {},
	"done": {}, "blocked": {},
}

var taskScopeCommentsQuery = map[string]struct{}{
	"since": {}, "thread": {}, "recent": {}, "tail": {}, "roots_only": {},
	"summary": {}, "fold": {}, "before": {}, "before_id": {},
}

var taskScopeMessagesQuery = map[string]struct{}{"since": {}}

// taskScopeQuerier is the narrow database surface needed to bind a task token
// request to the single issue that owns the running task.
type taskScopeQuerier interface {
	GetAgent(context.Context, pgtype.UUID) (db.Agent, error)
	GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error)
	GetComment(context.Context, pgtype.UUID) (db.Comment, error)
	GetIssue(context.Context, pgtype.UUID) (db.Issue, error)
	GetIssueByNumber(context.Context, db.GetIssueByNumberParams) (db.Issue, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
}

// TaskTokenScopeGuard is the final authorization boundary for mat_ task
// tokens. Human JWT/PAT requests keep their existing behavior. A task token is
// default-denied and may access only an explicit set of operations on the issue
// bound to its currently executing task.
//
// This guard intentionally lives server-side. CLI checks improve diagnostics,
// but a copied binary or a direct HTTP client must receive the same denial.
func TaskTokenScopeGuard(queries taskScopeQuerier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Actor-Source") != "task_token" {
				next.ServeHTTP(w, r)
				return
			}
			if queries == nil || taskScopeAttemptsWorkspaceOverride(r) {
				writeTaskScopeDenied(w)
				return
			}

			taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
			if err != nil {
				writeTaskScopeDenied(w)
				return
			}
			task, err := queries.GetAgentTask(r.Context(), taskID)
			if err != nil || task.ID != taskID || !task.IssueID.Valid || !taskScopeTaskIsActive(task.Status) {
				writeTaskScopeDenied(w)
				return
			}

			agentID, err := util.ParseUUID(r.Header.Get("X-Agent-ID"))
			if err != nil || task.AgentID != agentID {
				writeTaskScopeDenied(w)
				return
			}
			workspaceID, err := util.ParseUUID(r.Header.Get("X-Workspace-ID"))
			if err != nil {
				writeTaskScopeDenied(w)
				return
			}
			agent, err := queries.GetAgent(r.Context(), agentID)
			if err != nil || agent.ID != agentID || agent.WorkspaceID != workspaceID || agent.ArchivedAt.Valid {
				writeTaskScopeDenied(w)
				return
			}
			boundIssue, err := queries.GetIssue(r.Context(), task.IssueID)
			if err != nil || boundIssue.ID != task.IssueID || boundIssue.WorkspaceID != workspaceID {
				writeTaskScopeDenied(w)
				return
			}

			allowed, err := taskScopeAllowsRequest(r, queries, task, boundIssue, workspaceID)
			if err != nil || !allowed {
				writeTaskScopeDenied(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func taskScopeTaskIsActive(status string) bool {
	switch status {
	case "dispatched", "running":
		return true
	default:
		return false
	}
}

func taskScopeAttemptsWorkspaceOverride(r *http.Request) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return true
	}
	return r.Header.Get("X-Workspace-Slug") != "" || query.Has("workspace_id") || query.Has("workspace_slug")
}

func taskScopeAllowsRequest(
	r *http.Request,
	queries taskScopeQuerier,
	boundTask db.AgentTaskQueue,
	boundIssue db.Issue,
	workspaceID pgtype.UUID,
) (bool, error) {
	parts, ok := taskScopeStrictPathParts(r)
	if !ok {
		return false, nil
	}

	if len(parts) == 4 && parts[0] == "api" && parts[1] == "tasks" && parts[3] == "messages" {
		if r.Method != http.MethodGet || !taskScopeQueryAllowed(r, taskScopeMessagesQuery) {
			return false, nil
		}
		messageTaskID, err := taskScopeParseCanonicalUUID(parts[2])
		if err != nil {
			return false, nil
		}
		return messageTaskID == boundTask.ID, nil
	}

	if len(parts) < 3 || len(parts) > 4 || parts[0] != "api" || parts[1] != "issues" {
		return false, nil
	}
	requestIssue, err := taskScopeResolveIssue(r.Context(), queries, parts[2], workspaceID)
	if err != nil || requestIssue.ID != boundIssue.ID {
		return false, err
	}

	if len(parts) == 3 {
		switch r.Method {
		case http.MethodGet:
			return taskScopeQueryAllowed(r, nil), nil
		case http.MethodPut:
			if !taskScopeQueryAllowed(r, nil) {
				return false, nil
			}
			return taskScopeAllowIssueUpdateBody(r)
		default:
			return false, nil
		}
	}

	switch parts[3] {
	case "comments":
		switch r.Method {
		case http.MethodGet:
			return taskScopeQueryAllowed(r, taskScopeCommentsQuery), nil
		case http.MethodPost:
			if !taskScopeQueryAllowed(r, nil) {
				return false, nil
			}
			return taskScopeAllowCommentBody(r, queries, boundIssue, workspaceID)
		default:
			return false, nil
		}
	case "rerun":
		return false, nil
	case "task-runs":
		return false, nil
	default:
		return false, nil
	}
}

func taskScopeResolveIssue(ctx context.Context, queries taskScopeQuerier, ref string, workspaceID pgtype.UUID) (db.Issue, error) {
	if issueID, err := taskScopeParseCanonicalUUID(ref); err == nil {
		issue, queryErr := queries.GetIssue(ctx, issueID)
		if queryErr != nil || issue.WorkspaceID != workspaceID {
			return db.Issue{}, queryErr
		}
		return issue, nil
	}

	dash := strings.LastIndex(ref, "-")
	if dash <= 0 || dash == len(ref)-1 {
		return db.Issue{}, nil
	}
	number, err := strconv.ParseInt(ref[dash+1:], 10, 32)
	if err != nil || number <= 0 || strconv.FormatInt(number, 10) != ref[dash+1:] || !taskScopeAlphaNumeric(ref[:dash]) {
		return db.Issue{}, nil
	}
	workspace, err := queries.GetWorkspace(ctx, workspaceID)
	if err != nil || !strings.EqualFold(ref[:dash], workspace.IssuePrefix) {
		return db.Issue{}, err
	}
	return queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      int32(number),
	})
}

func taskScopeAllowIssueUpdateBody(r *http.Request) (bool, error) {
	body, err := taskScopeReadJSONObject(r, false)
	if err != nil {
		return false, err
	}
	if len(body) == 1 {
		rawStatus, statusOnly := body["status"]
		if !statusOnly {
			return false, nil
		}
		var status string
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			return false, nil
		}
		_, allowed := taskScopeIssueStatuses[status]
		return allowed, nil
	}
	return false, nil
}

func taskScopeReadJSONObject(r *http.Request, allowEmpty bool) (map[string]json.RawMessage, error) {
	limited := io.LimitReader(r.Body, taskScopeMaxBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	if len(data) > taskScopeMaxBodyBytes {
		return nil, io.ErrShortBuffer
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if allowEmpty {
			return map[string]json.RawMessage{}, nil
		}
		return nil, io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := opening.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("request body must be one JSON object")
	}
	body := make(map[string]json.RawMessage)
	for decoder.More() {
		rawKey, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := rawKey.(string)
		if !ok {
			return nil, errors.New("invalid JSON object key")
		}
		if _, duplicate := body[key]; duplicate {
			return nil, errors.New("duplicate JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		body[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("invalid JSON object")
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, errors.New("request body must contain exactly one JSON object")
	}
	return body, nil
}

func taskScopeAllowCommentBody(r *http.Request, queries taskScopeQuerier, boundIssue db.Issue, workspaceID pgtype.UUID) (bool, error) {
	body, err := taskScopeReadJSONObject(r, false)
	if err != nil || !taskScopeKeysAllowed(body, map[string]struct{}{"content": {}, "parent_id": {}}) {
		return false, err
	}
	rawContent, present := body["content"]
	if !present {
		return false, nil
	}
	var content string
	if err := json.Unmarshal(rawContent, &content); err != nil || content == "" {
		return false, nil
	}
	rawParent, present := body["parent_id"]
	if !present || bytes.Equal(bytes.TrimSpace(rawParent), []byte("null")) {
		return true, nil
	}
	var parentIDString string
	if err := json.Unmarshal(rawParent, &parentIDString); err != nil {
		return false, nil
	}
	parentID, err := taskScopeParseCanonicalUUID(parentIDString)
	if err != nil {
		return false, nil
	}
	parent, err := queries.GetComment(r.Context(), parentID)
	if err != nil {
		return false, err
	}
	return parent.ID == parentID && parent.IssueID == boundIssue.ID && parent.WorkspaceID == workspaceID, nil
}

func taskScopeParseCanonicalUUID(value string) (pgtype.UUID, error) {
	if !taskScopeCanonicalUUID.MatchString(value) {
		return pgtype.UUID{}, errors.New("UUID is not canonical")
	}
	return util.ParseUUID(value)
}

func taskScopeAlphaNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func taskScopeStrictPathParts(r *http.Request) ([]string, bool) {
	path := r.URL.Path
	if path == "" || path[0] != '/' || path == "/" || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return nil, false
	}
	// No allowlisted route needs an escaped path segment. Reject RawPath and
	// any escaped representation so the guard and Chi can never disagree on
	// segment boundaries or dot/slash normalization.
	if r.URL.RawPath != "" || r.URL.EscapedPath() != path || strings.Contains(path, "/./") || strings.Contains(path, "/../") {
		return nil, false
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/"), true
}

func taskScopeQueryAllowed(r *http.Request, allowed map[string]struct{}) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if len(values) != 1 {
			return false
		}
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func taskScopeKeysAllowed(body map[string]json.RawMessage, allowed map[string]struct{}) bool {
	for key := range body {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func writeTaskScopeDenied(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `{"error":"task_scope_denied"}`)
}
