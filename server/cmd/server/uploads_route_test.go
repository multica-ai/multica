package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestUploadsRouteServesLocalFilesWhenS3Configured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmp)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "https://multica.example")
	t.Setenv("S3_BUCKET", "multica")
	t.Setenv("S3_REGION", "cn-east-3")
	t.Setenv("AWS_ENDPOINT_URL", "https://obs.cn-east-3.myhuaweicloud.com")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-ak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-sk")

	key := filepath.Join("workspaces", "ws-1", "avatar.png")
	path := filepath.Join(tmp, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/uploads/workspaces/ws-1/avatar.png", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "png-bytes" {
		t.Fatalf("body = %q, want %q", got, "png-bytes")
	}
}

func TestUploadsRouteServesHeadWhenS3Configured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmp)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "https://multica.example")
	t.Setenv("S3_BUCKET", "multica")
	t.Setenv("S3_REGION", "cn-east-3")
	t.Setenv("AWS_ENDPOINT_URL", "https://obs.cn-east-3.myhuaweicloud.com")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-ak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-sk")

	key := filepath.Join("workspaces", "ws-1", "avatar.png")
	path := filepath.Join(tmp, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/uploads/workspaces/ws-1/avatar.png", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "" {
		t.Fatalf("HEAD body = %q, want empty", got)
	}
}
