package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	qoderCloudToolTestWorkspace = "11111111-1111-4111-8111-111111111111"
	qoderCloudToolTestIssue     = "22222222-2222-4222-8222-222222222222"
	qoderCloudToolTestOther     = "33333333-3333-4333-8333-333333333333"
	qoderCloudToolTestComment   = "44444444-4444-4444-8444-444444444444"
	qoderCloudToolTestToken     = "mat_qoder-cloud-tool-test-secret"
)

func TestQoderCloudCustomToolDispatcherUsesTaskTokenForAllowlistedOperations(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+qoderCloudToolTestToken {
			t.Errorf("Authorization = %q, want task token", got)
		}
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues":
			_, _ = io.WriteString(w, `{"issues":[{"id":"`+qoderCloudToolTestIssue+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-1","title":"One","status":"todo","priority":"high"}],"total":1}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+qoderCloudToolTestIssue:
			_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestIssue+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-1","title":"One","description":"`+qoderCloudToolTestToken+`","status":"todo","priority":"high","metadata":{"must_not":"cross"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+qoderCloudToolTestIssue+"/comments":
			_, _ = io.WriteString(w, `[{"id":"`+qoderCloudToolTestComment+`","issue_id":"`+qoderCloudToolTestIssue+`","content":"hello","type":"comment"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["title"] != "Created by cloud" || body["priority"] != "medium" {
				t.Errorf("create body = %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestOther+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-2","title":"Created by cloud","status":"todo","priority":"medium"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+qoderCloudToolTestIssue:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["status"] != "done" || body["suppress_run"] != true {
				t.Errorf("update body = %#v, want status and suppress_run", body)
			}
			_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestIssue+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-1","title":"One","status":"done","priority":"high"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+qoderCloudToolTestIssue+"/comments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["content"] != "Cloud note" {
				t.Errorf("comment body = %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestComment+`","issue_id":"`+qoderCloudToolTestIssue+`","content":"Cloud note","type":"comment"}`)
		default:
			http.Error(w, `{"error":"unexpected route"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
		WorkspaceID:   qoderCloudToolTestWorkspace,
		ChatSessionID: "chat-test",
		AuthToken:     qoderCloudToolTestToken,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	tests := []agent.CustomToolCall{
		{Name: "multica_list_issues", Input: map[string]any{"status": "todo", "limit": json.Number("10")}},
		{Name: "multica_get_issue", Input: map[string]any{"issue_id": qoderCloudToolTestIssue}},
		{Name: "multica_list_issue_comments", Input: map[string]any{"issue_id": qoderCloudToolTestIssue, "limit": json.Number("5")}},
		{Name: "multica_create_issue", Input: map[string]any{"title": "Created by cloud", "priority": "medium"}},
		{Name: "multica_update_issue", Input: map[string]any{"issue_id": qoderCloudToolTestIssue, "status": "done"}},
		{Name: "multica_add_issue_comment", Input: map[string]any{"issue_id": qoderCloudToolTestIssue, "content": "Cloud note"}},
	}
	for _, call := range tests {
		result, callErr := handler(context.Background(), call)
		if callErr != nil || result.IsError || result.Content == "" {
			t.Fatalf("%s result=%#v err=%v", call.Name, result, callErr)
		}
		if !json.Valid([]byte(result.Content)) {
			t.Fatalf("%s returned malformed JSON after redaction: %s", call.Name, result.Content)
		}
		if strings.Contains(result.Content, qoderCloudToolTestToken) || strings.Contains(result.Content, "must_not") {
			t.Fatalf("%s returned secret or unselected fields: %s", call.Name, result.Content)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	wantFragments := []string{
		"GET /api/issues?",
		"GET /api/issues/" + qoderCloudToolTestIssue,
		"GET /api/issues/" + qoderCloudToolTestIssue + "/comments?recent=5",
		"POST /api/issues?",
		"PUT /api/issues/" + qoderCloudToolTestIssue,
		"POST /api/issues/" + qoderCloudToolTestIssue + "/comments",
	}
	joined := strings.Join(requests, "\n")
	for _, want := range wantFragments {
		if !strings.Contains(joined, want) {
			t.Errorf("request log missing %q:\n%s", want, joined)
		}
	}
}

func TestQoderCloudCustomToolDispatcherListIssuesRespectsTaskScope(t *testing.T) {
	t.Run("issue task reads only assigned issue", func(t *testing.T) {
		var (
			mu       sync.Mutex
			requests []string
		)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, r.Method+" "+r.URL.RequestURI())
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/issues/" + qoderCloudToolTestIssue:
				_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestIssue+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-1","title":"Assigned","status":"todo","priority":"high"}`)
			case "/api/issues":
				_, _ = io.WriteString(w, `{"issues":[{"id":"`+qoderCloudToolTestOther+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-2","title":"Must not be disclosed","status":"todo","priority":"urgent"}],"total":1}`)
			default:
				http.Error(w, `{"error":"unexpected route"}`, http.StatusNotFound)
			}
		}))
		defer server.Close()

		handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
			WorkspaceID: qoderCloudToolTestWorkspace,
			IssueID:     qoderCloudToolTestIssue,
			AuthToken:   qoderCloudToolTestToken,
		})
		if err != nil {
			t.Fatal(err)
		}

		result, err := handler(context.Background(), agent.CustomToolCall{
			Name:  "multica_list_issues",
			Input: map[string]any{"status": "todo", "limit": json.Number("1")},
		})
		if err != nil {
			t.Fatalf("list assigned issue: %v", err)
		}
		var output struct {
			Issues []map[string]any `json:"issues"`
			Total  int              `json:"total"`
		}
		if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		if output.Total != 1 || len(output.Issues) != 1 || output.Issues[0]["id"] != qoderCloudToolTestIssue {
			t.Fatalf("result = %#v, want only assigned issue", output)
		}
		if strings.Contains(result.Content, qoderCloudToolTestOther) || strings.Contains(result.Content, "Must not be disclosed") {
			t.Fatalf("result disclosed workspace issue: %s", result.Content)
		}

		mismatch, err := handler(context.Background(), agent.CustomToolCall{
			Name:  "multica_list_issues",
			Input: map[string]any{"status": "done", "limit": json.Number("1")},
		})
		if err != nil {
			t.Fatalf("list assigned issue with mismatched status: %v", err)
		}
		if err := json.Unmarshal([]byte(mismatch.Content), &output); err != nil {
			t.Fatalf("decode mismatched result: %v", err)
		}
		if output.Total != 0 || len(output.Issues) != 0 {
			t.Fatalf("mismatched status result = %#v, want empty", output)
		}

		mu.Lock()
		requestCount := len(requests)
		mu.Unlock()
		_, err = handler(context.Background(), agent.CustomToolCall{
			Name:  "multica_list_issues",
			Input: map[string]any{"limit": json.Number("0")},
		})
		if err == nil || !strings.Contains(err.Error(), "limit must be between 1") {
			t.Fatalf("invalid limit error = %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if len(requests) != requestCount {
			t.Fatalf("invalid limit made a request: before=%d after=%d", requestCount, len(requests))
		}
		if len(requests) != 2 {
			t.Fatalf("requests = %#v, want two assigned-issue reads", requests)
		}
		for _, request := range requests {
			if request != "GET /api/issues/"+qoderCloudToolTestIssue {
				t.Fatalf("request = %q, workspace-wide list must never be requested", request)
			}
		}
	})

	t.Run("chat task keeps workspace list behavior", func(t *testing.T) {
		var (
			mu         sync.Mutex
			requestURI string
		)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requestURI = r.URL.RequestURI()
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"issues":[{"id":"`+qoderCloudToolTestOther+`","workspace_id":"`+qoderCloudToolTestWorkspace+`","identifier":"MUL-2","title":"Workspace issue","status":"todo","priority":"medium"}],"total":1}`)
		}))
		defer server.Close()

		handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
			WorkspaceID:   qoderCloudToolTestWorkspace,
			ChatSessionID: "chat-test",
			AuthToken:     qoderCloudToolTestToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := handler(context.Background(), agent.CustomToolCall{
			Name:  "multica_list_issues",
			Input: map[string]any{"status": "todo", "limit": json.Number("7")},
		})
		if err != nil {
			t.Fatalf("list workspace issues: %v", err)
		}
		var output struct {
			Issues []map[string]any `json:"issues"`
			Total  int              `json:"total"`
		}
		if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		if output.Total != 1 || len(output.Issues) != 1 || output.Issues[0]["id"] != qoderCloudToolTestOther {
			t.Fatalf("result = %#v, want workspace list result", output)
		}
		mu.Lock()
		defer mu.Unlock()
		parsedURI, err := url.ParseRequestURI(requestURI)
		if err != nil {
			t.Fatalf("parse request URI %q: %v", requestURI, err)
		}
		query := parsedURI.Query()
		if parsedURI.Path != "/api/issues" || query.Get("workspace_id") != qoderCloudToolTestWorkspace || query.Get("status") != "todo" || query.Get("limit") != "7" {
			t.Fatalf("request URI = %q, want unchanged workspace list query", requestURI)
		}
	})
}

func TestQoderCloudCustomToolDispatcherEnforcesTaskAndWorkspaceBoundaries(t *testing.T) {
	t.Run("task token required", func(t *testing.T) {
		_, err := newQoderCloudCustomToolHandler("http://example.invalid", "test", Task{
			WorkspaceID: qoderCloudToolTestWorkspace,
			AuthToken:   "mul_owner-token",
		})
		if err == nil || !strings.Contains(err.Error(), "non-task-scoped") {
			t.Fatalf("error = %v, want task-token refusal", err)
		}
	})

	t.Run("issue task cannot target another issue", func(t *testing.T) {
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
		defer server.Close()
		handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
			WorkspaceID: qoderCloudToolTestWorkspace,
			IssueID:     qoderCloudToolTestIssue,
			AuthToken:   qoderCloudToolTestToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = handler(context.Background(), agent.CustomToolCall{
			Name: "multica_update_issue",
			Input: map[string]any{
				"issue_id": qoderCloudToolTestOther,
				"status":   "done",
			},
		})
		if err == nil || !strings.Contains(err.Error(), "outside the issue assigned") || requests != 0 {
			t.Fatalf("boundary error=%v requests=%d", err, requests)
		}
	})

	t.Run("comment task restricts reply parent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("invalid parent reached server: %s", r.URL.Path)
		}))
		defer server.Close()
		handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
			WorkspaceID:      qoderCloudToolTestWorkspace,
			IssueID:          qoderCloudToolTestIssue,
			TriggerCommentID: qoderCloudToolTestComment,
			AuthToken:        qoderCloudToolTestToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = handler(context.Background(), agent.CustomToolCall{
			Name: "multica_add_issue_comment",
			Input: map[string]any{
				"issue_id":  qoderCloudToolTestIssue,
				"content":   "wrong thread",
				"parent_id": qoderCloudToolTestOther,
			},
		})
		if err == nil || !strings.Contains(err.Error(), "outside the comments assigned") {
			t.Fatalf("parent boundary error = %v", err)
		}
	})

	t.Run("server response cannot cross workspace", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"id":"`+qoderCloudToolTestIssue+`","workspace_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","title":"foreign"}`)
		}))
		defer server.Close()
		handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
			WorkspaceID:   qoderCloudToolTestWorkspace,
			ChatSessionID: "chat-test",
			AuthToken:     qoderCloudToolTestToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = handler(context.Background(), agent.CustomToolCall{
			Name:  "multica_get_issue",
			Input: map[string]any{"issue_id": qoderCloudToolTestIssue},
		})
		if err == nil || !strings.Contains(err.Error(), "workspace boundary") {
			t.Fatalf("workspace boundary error = %v", err)
		}
	})
}

func TestQoderCloudCustomToolDispatcherRejectsMalformedInputsAndUnknownTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("invalid tool call reached server: %s", r.URL.Path)
	}))
	defer server.Close()
	handler, err := newQoderCloudCustomToolHandler(server.URL, "test", Task{
		WorkspaceID:   qoderCloudToolTestWorkspace,
		ChatSessionID: "chat-test",
		AuthToken:     qoderCloudToolTestToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		call agent.CustomToolCall
		want string
	}{
		{name: "unknown tool", call: agent.CustomToolCall{Name: "http_request", Input: map[string]any{}}, want: "not allowlisted"},
		{name: "unknown field", call: agent.CustomToolCall{Name: "multica_list_issues", Input: map[string]any{"url": "https://example.invalid"}}, want: "unexpected input field"},
		{name: "invalid enum", call: agent.CustomToolCall{Name: "multica_create_issue", Input: map[string]any{"title": "x", "status": "shipped"}}, want: "status is invalid"},
		{name: "fractional limit", call: agent.CustomToolCall{Name: "multica_list_issues", Input: map[string]any{"limit": 1.5}}, want: "limit must be an integer"},
		{name: "empty update", call: agent.CustomToolCall{Name: "multica_update_issue", Input: map[string]any{"issue_id": qoderCloudToolTestIssue}}, want: "at least one update field"},
		{name: "non uuid", call: agent.CustomToolCall{Name: "multica_get_issue", Input: map[string]any{"issue_id": "../secrets"}}, want: "must be a UUID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, callErr := handler(context.Background(), tc.call)
			if callErr == nil || !strings.Contains(callErr.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", callErr, tc.want)
			}
		})
	}
}
