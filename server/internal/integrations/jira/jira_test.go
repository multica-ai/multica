package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySecret(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/webhooks/jira/x", nil)
	req.Header.Set(SecretHeader, "s3cr3t")
	if !VerifySecret("s3cr3t", req) {
		t.Error("matching header secret rejected")
	}
	if VerifySecret("other", req) {
		t.Error("mismatched secret accepted")
	}
	if VerifySecret("", req) {
		t.Error("empty stored secret must never validate")
	}

	// Query-param fallback for webhooks that cannot set custom headers.
	qreq := httptest.NewRequest("POST", "/api/webhooks/jira/x?secret=s3cr3t", nil)
	if !VerifySecret("s3cr3t", qreq) {
		t.Error("matching query secret rejected")
	}

	bare := httptest.NewRequest("POST", "/api/webhooks/jira/x", nil)
	if VerifySecret("s3cr3t", bare) {
		t.Error("request with no secret accepted")
	}
}

func TestParseIssueEvent_Created(t *testing.T) {
	body := []byte(`{
		"webhookEvent": "jira:issue_created",
		"issue": {
			"id": "10042", "key": "PROJ-7",
			"fields": {
				"summary": "Fix the login flow",
				"description": "plain text description",
				"status": {"name": "To Do"},
				"priority": {"name": "High"},
				"assignee": {"accountId": "abc123", "displayName": "Ada", "emailAddress": "ada@acme.test"}
			}
		}
	}`)
	ev, err := ParseIssueEvent(body)
	if err != nil {
		t.Fatalf("ParseIssueEvent: %v", err)
	}
	if ev.Kind != EventIssueCreated {
		t.Errorf("Kind = %v, want EventIssueCreated", ev.Kind)
	}
	if ev.IssueID != "10042" || ev.IssueKey != "PROJ-7" {
		t.Errorf("bad identity: %+v", ev)
	}
	if ev.Summary != "Fix the login flow" || ev.Description != "plain text description" {
		t.Errorf("bad content: %+v", ev)
	}
	if ev.Status != "To Do" || ev.Priority != "High" {
		t.Errorf("bad status/priority: %+v", ev)
	}
	if ev.AssigneeAccountID != "abc123" || ev.AssigneeDisplayName != "Ada" {
		t.Errorf("bad assignee: %+v", ev)
	}
	if ev.AssigneeChanged {
		t.Error("created event without changelog must not report AssigneeChanged")
	}
}

func TestParseIssueEvent_UpdatedWithAssigneeChange(t *testing.T) {
	body := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"issue": {
			"id": "10042", "key": "PROJ-7",
			"fields": {"summary": "Fix the login flow", "status": {"name": "In Progress"}}
		},
		"changelog": {"items": [
			{"field": "status"},
			{"field": "assignee"}
		]}
	}`)
	ev, err := ParseIssueEvent(body)
	if err != nil {
		t.Fatalf("ParseIssueEvent: %v", err)
	}
	if ev.Kind != EventIssueUpdated {
		t.Errorf("Kind = %v, want EventIssueUpdated", ev.Kind)
	}
	if !ev.AssigneeChanged {
		t.Error("assignee changelog item not detected")
	}
	if ev.AssigneeAccountID != "" {
		t.Errorf("unassigned issue must have empty assignee, got %q", ev.AssigneeAccountID)
	}
}

func TestParseIssueEvent_ADFDescription(t *testing.T) {
	body := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"issue": {
			"id": "1", "key": "PROJ-1",
			"fields": {
				"summary": "s",
				"description": {
					"type": "doc", "version": 1,
					"content": [
						{"type": "paragraph", "content": [{"type": "text", "text": "First line"}]},
						{"type": "paragraph", "content": [{"type": "text", "text": "Second "}, {"type": "text", "text": "line"}]}
					]
				}
			}
		}
	}`)
	ev, err := ParseIssueEvent(body)
	if err != nil {
		t.Fatalf("ParseIssueEvent: %v", err)
	}
	if ev.Description != "First line\nSecond line" {
		t.Errorf("Description = %q, want %q", ev.Description, "First line\nSecond line")
	}
}

func TestParseIssueEvent_OtherEventIgnored(t *testing.T) {
	ev, err := ParseIssueEvent([]byte(`{"webhookEvent":"comment_created","issue":{"key":"PROJ-1"}}`))
	if err != nil {
		t.Fatalf("ParseIssueEvent: %v", err)
	}
	if ev.Kind != EventOther {
		t.Errorf("Kind = %v, want EventOther", ev.Kind)
	}
}

func TestParseIssueEvent_Malformed(t *testing.T) {
	if _, err := ParseIssueEvent([]byte(`{"webhookEvent":`)); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestDescriptionText(t *testing.T) {
	cases := map[string]string{
		``:               "",
		`null`:           "",
		`"plain"`:        "plain",
		`{"type":"doc"}`: "",
		`[1,2]`:          "",
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]}`: "hi",
	}
	for in, want := range cases {
		if got := DescriptionText(json.RawMessage(in)); got != want {
			t.Errorf("DescriptionText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://acme.atlassian.net/": "https://acme.atlassian.net",
		"  https://jira.corp.test  ":  "https://jira.corp.test",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── HTTPClient (against a local httptest server; never a real Jira site) ────

func TestHTTPClientValidateCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/myself" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		email, token, ok := r.BasicAuth()
		if !ok || email != "ops@acme.test" || token != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"accountId": "abc", "displayName": "Ops", "emailAddress": "ops@acme.test",
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.Client())
	acct, err := c.ValidateCredentials(context.Background(), srv.URL+"/", "ops@acme.test", "tok")
	if err != nil {
		t.Fatalf("ValidateCredentials: %v", err)
	}
	if acct.AccountID != "abc" || acct.DisplayName != "Ops" {
		t.Errorf("bad account: %+v", acct)
	}

	if _, err := c.ValidateCredentials(context.Background(), srv.URL, "ops@acme.test", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bad token: err = %v, want ErrUnauthorized", err)
	}
}

func TestHTTPClientGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-7":
			json.NewEncoder(w).Encode(map[string]any{
				"id": "10042", "key": "PROJ-7",
				"fields": map[string]any{
					"summary":     "Fix login",
					"description": "the details",
					"status":      map[string]string{"name": "To Do"},
					"priority":    map[string]string{"name": "High"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.Client())
	issue, err := c.GetIssue(context.Background(), srv.URL, "e", "t", "PROJ-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Key != "PROJ-7" || issue.Summary != "Fix login" || issue.Description != "the details" ||
		issue.Status != "To Do" || issue.Priority != "High" {
		t.Errorf("bad issue: %+v", issue)
	}

	if _, err := c.GetIssue(context.Background(), srv.URL, "e", "t", "NOPE-1"); !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("missing issue: err = %v, want ErrIssueNotFound", err)
	}
}

func TestHTTPClientSearchIssues(t *testing.T) {
	// 3 matching issues served in pages, so pagination is exercised.
	total := 3
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("jql"); got != "assignee = currentUser()" {
			t.Errorf("jql = %q", got)
		}
		startAt := 0
		if tok := r.URL.Query().Get("nextPageToken"); tok != "" {
			fmt.Sscanf(tok, "%d", &startAt)
		}
		pageSize := 2 // server-side clamp below the client's requested page size
		issues := []map[string]any{}
		for i := startAt; i < total && len(issues) < pageSize; i++ {
			issues = append(issues, map[string]any{
				"id": fmt.Sprintf("100%d", i), "key": fmt.Sprintf("PROJ-%d", i+1),
				"fields": map[string]any{
					"summary":     fmt.Sprintf("Task %d", i+1),
					"description": "details",
					"status":      map[string]string{"name": "To Do"},
					"priority":    map[string]string{"name": "Medium"},
					"assignee":    map[string]string{"accountId": "abc", "displayName": "Ada"},
				},
			})
		}
		next := ""
		if startAt+len(issues) < total {
			next = fmt.Sprintf("%d", startAt+len(issues))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issues": issues, "nextPageToken": next, "isLast": next == "",
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.Client())
	got, err := c.SearchIssues(context.Background(), srv.URL, "e", "t", "assignee = currentUser()", 100)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != total {
		t.Fatalf("len = %d, want %d", len(got), total)
	}
	if got[0].Key != "PROJ-1" || got[2].Key != "PROJ-3" {
		t.Errorf("bad keys: %+v", got)
	}
	if got[0].Summary != "Task 1" || got[0].Description != "details" ||
		got[0].AssigneeAccountID != "abc" || got[0].AssigneeDisplayName != "Ada" {
		t.Errorf("bad issue: %+v", got[0])
	}
}

func TestHTTPClientSearchIssuesRespectsMaxResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startAt := 0
		if tok := r.URL.Query().Get("nextPageToken"); tok != "" {
			fmt.Sscanf(tok, "%d", &startAt)
		}
		pageSize := 0
		fmt.Sscanf(r.URL.Query().Get("maxResults"), "%d", &pageSize)
		issues := []map[string]any{}
		for i := startAt; len(issues) < pageSize; i++ { // pretend an endless result set
			issues = append(issues, map[string]any{
				"id": fmt.Sprintf("1%04d", i), "key": fmt.Sprintf("BIG-%d", i+1),
				"fields": map[string]any{"summary": "x"},
			})
		}
		json.NewEncoder(w).Encode(map[string]any{
			// Always advertise another page so the client stops only at maxResults.
			"issues": issues, "nextPageToken": fmt.Sprintf("%d", startAt+pageSize),
		})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.Client())
	got, err := c.SearchIssues(context.Background(), srv.URL, "e", "t", "project = BIG", 7)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 7 {
		t.Errorf("len = %d, want 7 (caller maxResults)", len(got))
	}

	// A zero/oversized maxResults is clamped to the hard cap of 100.
	got, err = c.SearchIssues(context.Background(), srv.URL, "e", "t", "project = BIG", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("len = %d, want 100 (hard cap)", len(got))
	}
}

func TestHTTPClientSearchIssuesBadJQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"errorMessages": []string{"bad jql"}})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.Client())
	if _, err := c.SearchIssues(context.Background(), srv.URL, "e", "t", "nonsense ===", 10); !errors.Is(err, ErrBadRequest) {
		t.Errorf("bad jql: err = %v, want ErrBadRequest", err)
	}
}
