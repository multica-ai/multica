package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// uploadRequest builds a multipart upload as a request from userID, with the
// fields and one file.
func uploadRequest(t *testing.T, userID string, fields map[string]string, fileName, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if fileName != "" {
		part, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := newRequestAs(userID, http.MethodPost, "/api/knowledge/documents/upload", nil)
	req.Body = io.NopCloser(&buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = int64(buf.Len())
	return req
}

// TestKnowledgeUploadLimitKindAndPublish covers the upload endpoint: the size
// limit answers 413 before any upload, the kind is required, a member's upload is
// published at once, and an agent's is refused.
func TestKnowledgeUploadLimitKindAndPublish(t *testing.T) {
	fx := newKnowledgeFixture(t)
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM knowledge_acl WHERE workspace_id = $1`,
			`DELETE FROM knowledge_owner_account WHERE workspace_id = $1`,
			`DELETE FROM knowledge_tenant_deprovision WHERE workspace_id = $1`,
		} {
			if _, err := testPool.Exec(context.Background(), stmt, testWorkspaceID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	testHandler.Knowledge.UploadMaxBytes = 16
	market := map[string]string{"kb": "market", "kind": "market_report"}

	// Over the limit: refused before WeKnora sees a byte.
	w := httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, uploadRequest(t, testUserID, market, "big.md", strings.Repeat("x", 17)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload = %d %s, want 413", w.Code, w.Body.String())
	}
	if n := fx.fake.Calls(knowledge.OpCreateFile); n != 0 {
		t.Fatalf("an oversized upload reached WeKnora %d times", n)
	}

	// Kind is required.
	w = httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, uploadRequest(t, testUserID, map[string]string{"kb": "market"}, "a.md", "x"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "kind is required") {
		t.Fatalf("upload without a kind = %d %s, want 400", w.Code, w.Body.String())
	}

	// A text file without its kind's headings is refused with the missing list.
	w = httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, uploadRequest(t, testUserID, market, "a.md", "# x\n"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"missing_sections":["Summary","Market size","Trends","Sources"]`) {
		t.Fatalf("upload missing its headings = %d %s, want 400 with the missing list", w.Code, w.Body.String())
	}

	// A PDF that cannot be read is refused before storage, since its headings
	// cannot be checked.
	w = httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, uploadRequest(t, testUserID, market, "a.pdf", "ok"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "cannot be read") {
		t.Fatalf("unreadable PDF = %d %s, want 400 before storage", w.Code, w.Body.String())
	}

	// A member's upload publishes at once. The limit is raised first: the file must
	// carry the kind's four headings, which the 16-byte limit cannot hold.
	testHandler.Knowledge.UploadMaxBytes = 1 << 20
	w = httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, uploadRequest(t, testUserID, market, "a.md", "# Summary\n\n## Market size\n\n## Trends\n\n## Sources\n"))
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"state":"published"`) {
		t.Fatalf("member upload = %d %s, want 201 published", w.Code, w.Body.String())
	}

	// An agent's upload is refused: agents write drafts.
	agentReq := asAgent(uploadRequest(t, testUserID, market, "b.md", "agent"), fx.agentID, fx.taskID)
	w = httptest.NewRecorder()
	testHandler.UploadKnowledgeDocument(w, agentReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent upload = %d %s, want 403", w.Code, w.Body.String())
	}
}
