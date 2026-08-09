package handler

// CEREBRO-PATCH(document-image-attachments): FIR-4699 — a document image gets
// an owner row via attachment.artifact_id. These tests cover the upload
// contract (validate the artifact belongs to the caller's workspace, reject a
// foreign artifact) and the GET /api/artifacts/{id}/attachments list endpoint.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// createHandlerTestArtifact inserts a workspace-scoped note artifact and returns
// its UUID, cleaned up after the test.
func createHandlerTestArtifact(t *testing.T, workspaceID, authorID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO artifact (workspace_id, kind, title, author_type, author_id)
		VALUES ($1, 'note', 'Handler Test Doc', 'member', $2)
		RETURNING id::text
	`, workspaceID, authorID).Scan(&id); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM artifact WHERE id = $1`, id)
	})
	return id
}

// createForeignWorkspaceArtifact stands up an isolated workspace + user and a
// note artifact inside it, returning the artifact UUID.
func createForeignWorkspaceArtifact(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var wsID, userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Foreign Doc User', 'foreign-doc@example.com')
		RETURNING id::text
	`).Scan(&userID); err != nil {
		t.Fatalf("seed foreign user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign Doc WS', 'foreign-doc-ws', 'x', 'FDW')
		RETURNING id::text
	`).Scan(&wsID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	var artID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO artifact (workspace_id, kind, title, author_type, author_id)
		VALUES ($1, 'note', 'Foreign Doc', 'member', $2)
		RETURNING id::text
	`, wsID, userID).Scan(&artID); err != nil {
		t.Fatalf("seed foreign artifact: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM artifact WHERE id = $1`, artID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return artID
}

// uploadFileWithArtifact POSTs a small PNG to UploadFile bound to artifactID in
// testWorkspace, returning the recorder.
func uploadFileWithArtifact(t *testing.T, artifactID string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "doc-image.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	if err := writer.WriteField("artifact_id", artifactID); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	return w
}

func TestUploadFile_AttachesToArtifact(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	artifactID := createHandlerTestArtifact(t, testWorkspaceID, testUserID)

	w := uploadFileWithArtifact(t, artifactID)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadFile with artifact_id: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	if resp.ArtifactID == nil || *resp.ArtifactID != artifactID {
		t.Fatalf("artifact_id in response: want %s, got %v", artifactID, resp.ArtifactID)
	}
	if resp.IssueID != nil || resp.CommentID != nil || resp.ChatSessionID != nil {
		t.Fatalf("other associations should be NULL: %+v", resp)
	}

	var dbArtifact *string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT artifact_id::text FROM attachment WHERE id = $1`,
		resp.ID,
	).Scan(&dbArtifact); err != nil {
		t.Fatalf("query attachment row: %v", err)
	}
	if dbArtifact == nil || *dbArtifact != artifactID {
		t.Fatalf("DB artifact_id mismatch: want %s, got %v", artifactID, dbArtifact)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, resp.ID)
	})
}

// A foreign-workspace artifact_id must be rejected, never silently dropped —
// otherwise a caller could bind a document image to another tenant's document.
func TestUploadFile_RejectsForeignArtifact(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	foreignArtifactID := createForeignWorkspaceArtifact(t)

	w := uploadFileWithArtifact(t, foreignArtifactID)
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("UploadFile with foreign artifact_id: expected 4xx, got %d: %s", w.Code, w.Body.String())
	}
}

// GET /api/artifacts/{id}/attachments lists a document's images, workspace-scoped.
func TestListArtifactAttachments_endpoint(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	artifactID := createHandlerTestArtifact(t, testWorkspaceID, testUserID)

	up := uploadFileWithArtifact(t, artifactID)
	if up.Code != http.StatusOK {
		t.Fatalf("seed upload: expected 200, got %d: %s", up.Code, up.Body.String())
	}
	var uploaded AttachmentResponse
	json.Unmarshal(up.Body.Bytes(), &uploaded)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, uploaded.ID)
	})

	req := httptest.NewRequest("GET", "/api/artifacts/"+artifactID+"/attachments", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", artifactID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.ListArtifactAttachments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListArtifactAttachments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v; body: %s", err, w.Body.String())
	}
	if len(list) != 1 || list[0].ID != uploaded.ID {
		t.Fatalf("expected exactly the uploaded attachment, got %+v", list)
	}
}
