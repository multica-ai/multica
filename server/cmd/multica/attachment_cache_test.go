package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentCacheRoundTripAndVerification(t *testing.T) {
	cache := newAttachmentCache(t.TempDir())
	data := []byte("deliverable v1")
	sum := contentSHA256(data)

	if _, ok := cache.read(sum); ok {
		t.Fatal("empty cache reported a hit")
	}
	if err := cache.write(sum, data); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := cache.read(sum)
	if !ok || string(got) != string(data) {
		t.Fatalf("read = %q, %v", got, ok)
	}

	// A copy changed after it was cached is a miss, never served.
	if err := os.WriteFile(cache.path(sum), []byte("edited later"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.read(sum); ok {
		t.Fatal("modified copy was served")
	}

	if err := cache.write(sum, []byte("other bytes")); err == nil {
		t.Fatal("bytes that do not match the hash were cached")
	}
	for _, bad := range []string{"", "ABC", "../" + sum, strings.ToUpper(sum)} {
		if _, ok := cache.read(bad); ok {
			t.Errorf("malformed hash %q reported a hit", bad)
		}
	}

	var nilCache *attachmentCache
	if _, ok := nilCache.read(sum); ok || nilCache.write(sum, data) != nil {
		t.Fatal("a nil cache must be a silent miss")
	}
}

// useTestWorkspacesRoot points both the task and the standalone workspaces
// root at a temp dir, so the CLI's cache never touches the real one.
func useTestWorkspacesRoot(t *testing.T) *attachmentCache {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MULTICA_TASK_WORKSPACES_ROOT", root)
	t.Setenv("MULTICA_WORKSPACES_ROOT", root)
	return newAttachmentCache(root)
}

type attachmentTestServer struct {
	meta      map[string]any
	metaCode  int
	body      []byte
	downloads int
}

func (s *attachmentTestServer) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/attachments/att-1":
			if s.metaCode != 0 {
				w.WriteHeader(s.metaCode)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "forbidden"})
				return
			}
			_ = json.NewEncoder(w).Encode(s.meta)
		case "/download":
			s.downloads++
			_, _ = w.Write(s.body)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
}

func runDownloadForTest(t *testing.T) (string, string, error) {
	t.Helper()
	outputDir := t.TempDir()
	cmd := newAttachmentDownloadTestCmd()
	_ = cmd.Flags().Set("output-dir", outputDir)
	out, err := captureStdout(t, func() error { return runAttachmentDownload(cmd, []string{"att-1"}) })
	return out, filepath.Join(outputDir, "report.txt"), err
}

func TestRunAttachmentDownloadReadsMatchingLocalCopy(t *testing.T) {
	cache := useTestWorkspacesRoot(t)
	body := []byte("cached deliverable")
	sum := contentSHA256(body)
	if err := cache.write(sum, body); err != nil {
		t.Fatal(err)
	}
	srv := &attachmentTestServer{meta: map[string]any{
		"id": "att-1", "filename": "report.txt", "download_url": "/download", "content_sha256": sum,
	}}
	srv.start(t)

	out, dest, err := runDownloadForTest(t)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if srv.downloads != 0 {
		t.Fatalf("downloaded %d times despite a matching local copy", srv.downloads)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(body) {
		t.Fatalf("written = %q", got)
	}
	if !strings.Contains(out, `"source": "local"`) || !strings.Contains(out, dest) {
		t.Fatalf("stdout = %q, want source local and the path", out)
	}
}

func TestRunAttachmentDownloadFallsBackAndRepairsStaleCopy(t *testing.T) {
	cache := useTestWorkspacesRoot(t)
	body := []byte("server bytes")
	sum := contentSHA256(body)
	if err := os.MkdirAll(filepath.Dir(cache.path(sum)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.path(sum), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &attachmentTestServer{body: body, meta: map[string]any{
		"id": "att-1", "filename": "report.txt", "download_url": "/download", "content_sha256": sum,
	}}
	srv.start(t)

	out, dest, err := runDownloadForTest(t)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if srv.downloads != 1 || !strings.Contains(out, `"source": "remote"`) {
		t.Fatalf("downloads = %d, stdout = %q, want one remote download", srv.downloads, out)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(body) {
		t.Fatalf("written = %q", got)
	}
	if got, ok := cache.read(sum); !ok || string(got) != string(body) {
		t.Fatalf("cache after download = %q, %v, want the server bytes", got, ok)
	}
}

func TestRunAttachmentDownloadWithoutHashNeverCaches(t *testing.T) {
	root := useTestWorkspacesRoot(t)
	srv := &attachmentTestServer{body: []byte("legacy"), meta: map[string]any{
		"id": "att-1", "filename": "report.txt", "download_url": "/download",
	}}
	srv.start(t)

	out, _, err := runDownloadForTest(t)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !strings.Contains(out, `"source": "remote"`) {
		t.Fatalf("stdout = %q", out)
	}
	if _, err := os.Stat(root.dir); !os.IsNotExist(err) {
		t.Fatalf("cache created for an attachment without a hash: %v", err)
	}
}

func TestRunAttachmentDownloadChecksAccessBeforeReadingLocally(t *testing.T) {
	cache := useTestWorkspacesRoot(t)
	body := []byte("someone else's file")
	if err := cache.write(contentSHA256(body), body); err != nil {
		t.Fatal(err)
	}
	srv := &attachmentTestServer{metaCode: http.StatusForbidden}
	srv.start(t)

	out, dest, err := runDownloadForTest(t)
	if err == nil {
		t.Fatalf("download succeeded without access: %q", out)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("file written without access: %v", statErr)
	}
}

func TestRunAttachmentUploadCachesUploadedBytes(t *testing.T) {
	cache := useTestWorkspacesRoot(t)
	body := []byte("chart bytes")
	sum := contentSHA256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "att-up", "filename": "chart.txt", "content_type": "text/plain",
			"markdown_url": "https://public.example/a", "content_sha256": sum,
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	path := filepath.Join(t.TempDir(), "chart.txt")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newAttachmentUploadTestCmd()
	_ = cmd.Flags().Set("task", "task-1")
	if _, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) }); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got, ok := cache.read(sum); !ok || string(got) != string(body) {
		t.Fatalf("uploaded bytes not cached: %q, %v", got, ok)
	}
}
