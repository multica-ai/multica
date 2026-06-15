package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportIssue_Markdown(t *testing.T) {
	// 1. Create an issue
	issueID := createExportTestIssue(t, "Export Test Title", "This is some **markdown** description.")

	// 2. Add a comment
	wComment := httptest.NewRecorder()
	reqComment := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "This is a **comment** on the issue.",
	})
	reqComment = withURLParam(reqComment, "id", issueID)
	testHandler.CreateComment(wComment, reqComment)
	if wComment.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", wComment.Code, wComment.Body.String())
	}

	// 3. Export as Markdown with comments
	wExport := httptest.NewRecorder()
	reqExport := newRequest("POST", "/api/issues/"+issueID+"/export?format=md&include_comments=true", nil)
	reqExport = withURLParam(reqExport, "id", issueID)
	testHandler.ExportIssue(wExport, reqExport)

	if wExport.Code != http.StatusOK {
		t.Fatalf("ExportIssue: expected 200, got %d: %s", wExport.Code, wExport.Body.String())
	}

	contentType := wExport.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/markdown") {
		t.Errorf("expected Content-Type text/markdown, got %q", contentType)
	}

	body := wExport.Body.String()
	if !strings.Contains(body, "# Export Test Title") {
		t.Errorf("missing title in export: %s", body)
	}
	if !strings.Contains(body, "This is some **markdown** description.") {
		t.Errorf("missing description in export: %s", body)
	}
	if !strings.Contains(body, "This is a **comment** on the issue.") {
		t.Errorf("missing comment in export: %s", body)
	}
}

func TestExportIssue_PDF(t *testing.T) {
	_, err := exec.LookPath("weasyprint")
	if err != nil {
		t.Skip("weasyprint not found in PATH, skipping PDF export test")
	}

	issueID := createExportTestIssue(t, "Export Test Title PDF", "This is some **markdown** description for PDF.")

	wExport := httptest.NewRecorder()
	reqExport := newRequest("POST", "/api/issues/"+issueID+"/export?format=pdf&include_comments=false", nil)
	reqExport = withURLParam(reqExport, "id", issueID)
	testHandler.ExportIssue(wExport, reqExport)

	if wExport.Code != http.StatusOK {
		t.Fatalf("ExportIssue PDF: expected 200, got %d: %s", wExport.Code, wExport.Body.String())
	}

	contentType := wExport.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/pdf") {
		t.Errorf("expected Content-Type application/pdf, got %q", contentType)
	}
}

// TestRenderPDF_CJKWithoutCharset is a DB-free regression test for OPE-2905.
// It feeds RenderPDF an HTML document that contains CJK text but no
// <meta charset> (exactly the shape processHTMLImages produces for the
// export-html path), then extracts the text back with pdftotext and asserts
// the Chinese survives instead of becoming latin-1 mojibake. Without the
// "--encoding utf-8" flag on weasyprint this output is garbled.
func TestRenderPDF_CJKWithoutCharset(t *testing.T) {
	if _, err := exec.LookPath("weasyprint"); err != nil {
		t.Skip("weasyprint not found in PATH, skipping")
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not found in PATH, skipping")
	}

	const cjk = "中文测试黑客松赞助方案"
	// Note: no <meta charset> — mirrors the real export-html path where
	// processHTMLImages wraps the fragment in <html> but never adds charset.
	htmlContent := "<html><head></head><body><p>" + cjk + "</p></body></html>"

	pdfBytes, err := RenderPDF(context.Background(), htmlContent)
	if err != nil {
		t.Fatalf("RenderPDF: %v", err)
	}
	if len(pdfBytes) == 0 {
		t.Fatal("RenderPDF returned empty output")
	}

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	out, err := exec.Command("pdftotext", pdfPath, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	if !strings.Contains(string(out), cjk) {
		t.Errorf("CJK text not preserved in PDF; got %q, want it to contain %q (charset/encoding regression)", string(out), cjk)
	}
}

func createExportTestIssue(t *testing.T, title, description string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"description": description,
		"status":      "todo",
		"priority":    "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createExportTestIssue: expected 201, got %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	return issue.ID
}
