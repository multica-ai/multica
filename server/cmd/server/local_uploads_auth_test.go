package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// TestLocalUploadsRouteRequiresAuth replays the MUL-8859 report against the
// real router on local storage: every request below returned 200 without
// credentials, the directory ones as HTML listings of workspace and
// attachment ids.
func TestLocalUploadsRouteRequiresAuth(t *testing.T) {
	t.Setenv("S3_BUCKET", "")
	dir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", dir)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")

	key := "workspaces/" + testWorkspaceID + "/" + uuid.NewString() + ".png"
	full := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("PNGBYTES"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	srv := httptest.NewServer(NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil))
	defer srv.Close()

	get := func(path string, authed bool) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if authed {
			req.Header.Set("Authorization", "Bearer "+testToken)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	for _, path := range []string{
		"/uploads/",
		"/uploads/workspaces/",
		"/uploads/workspaces/" + testWorkspaceID + "/",
		"/uploads/" + key,
	} {
		if code, body := get(path, false); code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d, want 401; body=%q", path, code, body)
		}
	}

	if code, body := get("/uploads/workspaces/"+testWorkspaceID+"/", true); code != http.StatusNotFound {
		t.Errorf("member GET of the workspace directory = %d, want 404; body=%q", code, body)
	}
	if code, body := get("/uploads/"+key, true); code != http.StatusOK || body != "PNGBYTES" {
		t.Errorf("member GET %s = %d %q, want 200 PNGBYTES", key, code, body)
	}
}
