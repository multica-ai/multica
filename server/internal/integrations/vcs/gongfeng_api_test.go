package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGongfengAPI_MRIdentityAndForkBranch(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Error("missing private token")
		}
		paths = append(paths, r.URL.EscapedPath())
		switch r.URL.Path {
		case "/api/v3/projects/55/merge_requests":
			if r.URL.Query().Get("iid") != "7" {
				t.Error("must filter by display IID")
			}
			if r.URL.Query().Get("state") != "" {
				// Official v3 treats "all" as a literal state and returns no MRs.
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":901,"iid":7}]`))
		case "/api/v3/projects/55/merge_request/901":
			_, _ = w.Write([]byte(`{"id":901,"iid":7,"source_project_id":88,"source_branch":"feature/hello"}`))
		case "/api/v3/projects/88/repository/branches/feature/hello":
			_, _ = w.Write([]byte(`{"commit":{"id":"fork-sha"}}`))
		default:
			t.Errorf("unexpected API path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGongfengClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	mr, err := client.MergeRequest(context.Background(), "55", 7)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := client.HeadSHA(context.Background(), mr)
	if err != nil || sha != "fork-sha" {
		t.Fatalf("head %q, err %v", sha, err)
	}
	if !strings.Contains(paths[2], "feature%2Fhello") {
		t.Fatalf("branch path not escaped: %v", paths)
	}
}

func TestGongfengAPI_MRListIncludesAllStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("order_by") != "updated_at" || r.URL.Query().Get("sort") != "desc" {
			t.Error("historical scans must be ordered by latest update")
		}
		_, _ = w.Write([]byte(`[{"id":1,"iid":1,"state":"opened"},{"id":2,"iid":2,"state":"closed"},{"id":3,"iid":3,"state":"merged"}]`))
	}))
	defer server.Close()
	client, _ := NewGongfengClient(server.URL, "test-token")
	mrs, next, err := client.MergeRequests(context.Background(), "55", 1)
	if err != nil || next != 0 || len(mrs) != 3 {
		t.Fatalf("MRs %+v, next %d, err %v", mrs, next, err)
	}
}

func TestGongfengAPI_ReopenedMRState(t *testing.T) {
	for _, draft := range []bool{false, true} {
		mr := GongfengMR{ID: 901, IID: 7, State: "reopened", Title: "MUL-123", WorkInProgress: draft, CreatedAt: "2026-10-09T08:00:00+0000", UpdatedAt: "2026-10-09T09:00:00+0000"}
		event, err := mr.Event(GongfengProject{ID: 55, Path: "acme/widget", WebURL: "https://git.code.tencent.com/acme/widget"})
		want := "open"
		if draft {
			want = "draft"
		}
		if err != nil || event.State != want {
			t.Fatalf("reopened MR draft %t: state %q, err %v", draft, event.State, err)
		}
	}
}
func TestGongfengAPI_CheckPaginationLatestAndUnknown(t *testing.T) {
	unknown := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unknown {
			_, _ = w.Write([]byte(`[{"id":9,"context":"lint","state":"new-unknown"}]`))
			return
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"id":1,"context":"lint","state":"failure"},{"id":3,"context":"build","state":"pending"}]`))
		} else {
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write([]byte(`[{"id":2,"context":"lint","state":"success"},{"id":4,"context":"unit","state":"error"}]`))
		}
	}))
	defer server.Close()
	client, _ := NewGongfengClient(server.URL, "test-token")
	checks, err := client.Checks(context.Background(), 55, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if checks.Total != 3 || checks.Passed != 1 || checks.Failed != 1 || checks.Pending != 1 || checks.Running != 0 || len(checks.FailedNames) != 1 || checks.FailedNames[0] != "unit" {
		t.Fatalf("checks %+v", checks)
	}
	unknown = true
	if _, err := client.Checks(context.Background(), 55, "abc"); err == nil {
		t.Fatal("unknown checks must not be reported as successful")
	}
}
func TestGongfengAPI_HookRetryPreservesEventsAndDoesNotDuplicate(t *testing.T) {
	var posted int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`[{"id":9,"url":"https://multica.example/api/webhooks/vcs/connection","push_events":true,"note_events":true}]`))
		case "POST":
			posted++
			t.Error("must reuse existing exact-URL hook")
		case "PUT":
			var hook GongfengHook
			if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
				t.Fatal(err)
			}
			if !hook.Push || !hook.Notes || !hook.MergeRequests || !hook.VerifySSL || hook.Token != "new-secret" {
				t.Errorf("updated hook %+v", hook)
			}
			_, _ = w.Write([]byte(`{"id":9}`))
		}
	}))
	defer server.Close()
	client, _ := NewGongfengClient(server.URL, "test-token")
	id, err := client.EnsureHook(context.Background(), 55, "https://multica.example/api/webhooks/vcs/connection", "new-secret")
	if err != nil || id != 9 || posted != 0 {
		t.Fatalf("hook %d, err %v, posted %d", id, err, posted)
	}
}
func TestGongfengAPI_ErrorsNeverEchoCredentialsOrFollowRedirects(t *testing.T) {
	for _, code := range []int{302, 401, 403, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("token forwarded to redirect") }))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(code)
				_, _ = w.Write([]byte("test-token secret-payload"))
			}))
			defer server.Close()
			client, _ := NewGongfengClient(server.URL, "test-token")
			_, _, err := client.Projects(context.Background(), "", 1)
			if err == nil || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "secret-payload") {
				t.Fatalf("unsafe error %v", err)
			}
		})
	}
}

func TestGongfengAPI_RetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name, header     string
		minimum, maximum time.Duration
	}{
		{"seconds", "600", 10 * time.Minute, 10 * time.Minute},
		{"date", time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat), 9 * time.Minute, 10 * time.Minute},
		{"missing", "", time.Minute, time.Minute},
		{"invalid", "not-a-date", time.Minute, time.Minute},
		{"negative", "-1", time.Minute, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", tc.header)
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			client, err := NewGongfengClient(server.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.Projects(context.Background(), "", 1)
			var limit *GongfengAPIError
			if !errors.As(err, &limit) || limit.Status != 429 || limit.RetryAfter < tc.minimum || limit.RetryAfter > tc.maximum {
				t.Fatalf("Retry-After %q: %+v", tc.header, err)
			}
		})
	}
}
