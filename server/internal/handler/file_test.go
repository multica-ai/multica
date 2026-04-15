package handler

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockStorage struct{}

func (m *mockStorage) Upload(_ context.Context, key string, _ []byte, _ string, _ string) (string, error) {
	return fmt.Sprintf("https://cdn.example.com/%s", key), nil
}

func (m *mockStorage) Delete(_ context.Context, _ string)        {}
func (m *mockStorage) DeleteKeys(_ context.Context, _ []string)  {}
func (m *mockStorage) KeyFromURL(rawURL string) string            { return rawURL }
func (m *mockStorage) CdnDomain() string                         { return "cdn.example.com" }

func TestUploadFileForeignWorkspace(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("hello world"))
	writer.Close()

	foreignWorkspaceID := "00000000-0000-0000-0000-000000000099"
	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", foreignWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UploadFile with foreign workspace: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
