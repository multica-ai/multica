package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestPresignUpload_HappyPath verifies that a request with a valid
// size / workspace / filename returns the pre-signed URL + an
// attachment record with size_bytes=0 (sentinel for in-flight).
func TestPresignUpload_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	payload := map[string]any{
		"filename":     "MEMORY.DMP",
		"content_type": "application/octet-stream",
		"size_bytes":   1710221071, // 1.71 GB — well over the 100 MB cap
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/upload-file/presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PresignUpload(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Attachment struct {
			ID        string `json:"id"`
			Filename  string `json:"filename"`
			SizeBytes int64  `json:"size_bytes"`
			URL       string `json:"url"`
		} `json:"attachment"`
		Upload struct {
			URL    string            `json:"upload_url"`
			Method string            `json:"upload_method"`
			Headers map[string]string `json:"required_headers"`
		} `json:"upload"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Attachment.Filename != "MEMORY.DMP" {
		t.Fatalf("filename mismatch: %q", resp.Attachment.Filename)
	}
	if resp.Attachment.SizeBytes != 0 {
		t.Fatalf("expected size_bytes=0 pre-confirm, got %d", resp.Attachment.SizeBytes)
	}
	if resp.Upload.Method != "PUT" {
		t.Fatalf("expected PUT, got %q", resp.Upload.Method)
	}
	if resp.Upload.URL == "" {
		t.Fatal("empty pre-signed URL")
	}
	if resp.Upload.Headers["Content-Type"] != "application/octet-stream" {
		t.Fatalf("missing Content-Type header in response: %+v", resp.Upload.Headers)
	}

	// Clean up so reruns don't accumulate.
	_, _ = testPool.Exec(
		context.Background(),
		`DELETE FROM attachment WHERE id = $1`, resp.Attachment.ID,
	)
}

// TestPresignUpload_RejectsSmallFile checks we steer clients back to
// /api/upload-file for files that fit in a normal multipart request.
// Pre-signed uploads are a two-round-trip protocol and not worth the
// complexity for a 2 MB screenshot.
func TestPresignUpload_RejectsSmallFile(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	payload := map[string]any{
		"filename":   "small.txt",
		"size_bytes": 1024,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/upload-file/presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PresignUpload(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for small file, got %d: %s", w.Code, w.Body.String())
	}
}

// TestConfirmAttachmentUpload_HappyPath finalises an in-flight
// attachment and verifies size_bytes is populated from StatObject.
func TestConfirmAttachmentUpload_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	// Pre-create an in-flight attachment via presign.
	presignBody, _ := json.Marshal(map[string]any{
		"filename":   "confirm-test.bin",
		"size_bytes": 1 << 30, // 1 GB
	})
	presignReq := httptest.NewRequest("POST", "/api/upload-file/presign",
		bytes.NewReader(presignBody))
	presignReq.Header.Set("Content-Type", "application/json")
	presignReq.Header.Set("X-User-ID", testUserID)
	presignReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	presignW := httptest.NewRecorder()
	testHandler.PresignUpload(presignW, presignReq)
	if presignW.Code != http.StatusOK {
		t.Fatalf("presign failed: %d %s", presignW.Code, presignW.Body.String())
	}
	var presignResp struct {
		Attachment struct {
			ID string `json:"id"`
		} `json:"attachment"`
	}
	if err := json.NewDecoder(presignW.Body).Decode(&presignResp); err != nil {
		t.Fatalf("decode presign resp: %v", err)
	}
	defer func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM attachment WHERE id = $1`, presignResp.Attachment.ID)
	}()

	confirmReq := httptest.NewRequest("POST",
		"/api/attachments/"+presignResp.Attachment.ID+"/confirm", nil)
	confirmReq.Header.Set("X-User-ID", testUserID)
	confirmReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", presignResp.Attachment.ID)
	confirmReq = confirmReq.WithContext(context.WithValue(confirmReq.Context(), chi.RouteCtxKey, rctx))

	confirmW := httptest.NewRecorder()
	testHandler.ConfirmAttachmentUpload(confirmW, confirmReq)
	if confirmW.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d: %s", confirmW.Code, confirmW.Body.String())
	}
	var confirmResp struct {
		ID        string `json:"id"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := json.NewDecoder(confirmW.Body).Decode(&confirmResp); err != nil {
		t.Fatalf("decode confirm resp: %v", err)
	}
	if confirmResp.SizeBytes != 123456 { // mockStorage.StatObject returns 123456
		t.Fatalf("expected size=123456 from mock StatObject, got %d", confirmResp.SizeBytes)
	}
}
