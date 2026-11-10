package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestLocalUploadEntryIsDisabled(t *testing.T) {
	t.Setenv("S3_BUCKET", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")
	dir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", dir)
	key := "workspaces/" + testWorkspaceID + "/" + uuid.NewString() + ".png"
	full := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{full, full + ".meta.json"} {
		if err := os.WriteFile(name, []byte("PRIVATE"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	router := NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	for _, path := range []string{
		"/uploads",
		"/uploads/",
		"/uploads/workspaces/",
		"/uploads/workspaces/" + testWorkspaceID + "/",
		"/uploads/" + key,
		"/uploads/" + key + ".meta.json/",
	} {
		for _, token := range []string{"", testToken} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				req := httptest.NewRequest(method, path, nil)
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s %s (authenticated=%t) = %d, want 404", method, path, token != "", w.Code)
				}
			}
		}
	}
}

func TestLocalAttachmentAPIStillRequiresMembership(t *testing.T) {
	t.Setenv("S3_BUCKET", "")
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")
	dir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", dir)
	id := uuid.NewString()
	key := "workspaces/" + testWorkspaceID + "/" + id + ".png"
	full := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "PRIVATE IMAGE"
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := testutil.New(testPool, testWorkspaceID, testUserID)
	fixture.Insert(t, "attachment", testutil.Cols{
		"id": id, "workspace_id": testWorkspaceID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "private.png", "url": "/uploads/" + key,
		"content_type": "image/png", "size_bytes": len(body),
	})
	foreignToken, err := generateTestJWT(uuid.NewString(), "outsider@example.test", "Outsider")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	for _, tc := range []struct {
		name  string
		token string
		code  int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"non-member", foreignToken, http.StatusNotFound},
		{"member", testToken, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"/download", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.code, w.Body.String())
			}
			if tc.code == http.StatusOK {
				if got := w.Body.String(); got != body {
					t.Fatalf("body = %q, want %q", got, body)
				}
			}
		})
	}
}
