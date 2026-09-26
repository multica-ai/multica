package ghsnapshot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pullRequestFilesServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_secret","expires_at":"` +
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
			return
		}
		if r.URL.Path != "/repos/acme/my%20repo/pulls/7/files" && r.URL.EscapedPath() != "/repos/acme/my%20repo/pulls/7/files" {
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ghs_secret" {
			t.Errorf("Authorization = %q", got)
		}
		var page int
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		start := (page - 1) * pullRequestFilesPageSize
		var items []string
		for i := start; i < total && i < start+pullRequestFilesPageSize; i++ {
			items = append(items, fmt.Sprintf(`{"filename":"f%d.go","status":"modified","additions":%d,"deletions":1,"patch":"@@ -1 +1 @@\n-a\n+b"}`, i, i))
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
}

func TestPullRequestFilesPaginates(t *testing.T) {
	srv := pullRequestFilesServer(t, 150)
	defer srv.Close()

	files, truncated, err := newTestClient(t, srv.URL).PullRequestFiles(context.Background(), 1, "acme", "my repo", 7, 3000)
	if err != nil {
		t.Fatalf("PullRequestFiles: %v", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(files) != 150 {
		t.Fatalf("len(files) = %d, want 150", len(files))
	}
	if files[149].Filename != "f149.go" || files[149].Additions != 149 || !strings.HasPrefix(files[0].Patch, "@@") {
		t.Fatalf("unexpected file: %+v", files[149])
	}
}

func TestPullRequestFilesStopsAtLimit(t *testing.T) {
	srv := pullRequestFilesServer(t, 250)
	defer srv.Close()

	files, truncated, err := newTestClient(t, srv.URL).PullRequestFiles(context.Background(), 1, "acme", "my repo", 7, 120)
	if err != nil {
		t.Fatalf("PullRequestFiles: %v", err)
	}
	if !truncated || len(files) != 120 {
		t.Fatalf("got %d files, truncated=%v; want 120, true", len(files), truncated)
	}
}

func TestPullRequestFilesDisabled(t *testing.T) {
	var c *Client
	if _, _, err := c.PullRequestFiles(context.Background(), 1, "a", "b", 1, 10); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	var m *Manager
	if _, _, err := m.PullRequestFiles(context.Background(), 1, "a", "b", 1, 10); !errors.Is(err, ErrDisabled) {
		t.Fatalf("manager err = %v, want ErrDisabled", err)
	}
}
