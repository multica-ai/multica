package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGetIssue(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/issues/issue-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "issue-1",
			"identifier": "OPE-251",
			"title":      "新增通知渠道 系统通知栏",
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	client.SetToken("test-token")

	issue, err := client.GetIssue(context.Background(), "issue-1")
	if err != nil {
		t.Fatalf("GetIssue returned error: %v", err)
	}
	if issue == nil {
		t.Fatal("GetIssue returned nil issue")
	}
	if issue.ID != "issue-1" || issue.Identifier != "OPE-251" || issue.Title != "新增通知渠道 系统通知栏" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
}
