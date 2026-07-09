package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newAttachmentDownloadTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "download"}
	cmd.Flags().StringP("output-dir", "o", ".", "")
	return cmd
}

func TestRunAttachmentDownloadWritesBasenameIntoOutputDir(t *testing.T) {
	const attachmentID = "att-123"
	const fileBody = "attachment body"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/"+attachmentID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           attachmentID,
				"filename":     "../report.txt",
				"download_url": "/downloads/report.txt",
				"size_bytes":   "15",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/downloads/report.txt":
			if r.Header.Get("Authorization") == "" {
				t.Fatalf("relative download missing auth header")
			}
			_, _ = w.Write([]byte(fileBody))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	outputDir := t.TempDir()
	cmd := newAttachmentDownloadTestCmd()
	_ = cmd.Flags().Set("output-dir", outputDir)

	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error { return runAttachmentDownload(cmd, []string{attachmentID}) })
	errOut := stderr.read()
	if err != nil {
		t.Fatalf("runAttachmentDownload: %v", err)
	}

	dest := filepath.Join(outputDir, "report.txt")
	data, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read downloaded file: %v", readErr)
	}
	if string(data) != fileBody {
		t.Fatalf("downloaded body = %q, want %q", data, fileBody)
	}
	if strings.Contains(out, "../") {
		t.Fatalf("stdout path should use sanitized basename, got %q", out)
	}
	if !strings.Contains(out, `"filename": "report.txt"`) || !strings.Contains(out, dest) {
		t.Fatalf("stdout = %q, want JSON with sanitized file path", out)
	}
	if !strings.Contains(errOut, "Downloaded:") || !strings.Contains(errOut, dest) {
		t.Fatalf("stderr = %q, want downloaded path", errOut)
	}
}

func newAttachmentUploadTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "upload"}
	cmd.Flags().String("task", "", "")
	return cmd
}

func TestRunAttachmentUploadSendsTaskIDAndPrintsContract(t *testing.T) {
	const taskID = "task-abc"
	var gotTaskID string
	var gotFile string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/upload-file" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotTaskID = r.FormValue("task_id")
		if f, _, err := r.FormFile("file"); err == nil {
			_ = f.Close()
			gotFile = "present"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "att-999",
			"filename":     "chart.png",
			"url":          "https://cdn.example/chart.png",
			"download_url": "https://signed.example/chart.png?sig=x",
			"markdown_url": "https://public.example/api/attachments/att-999/download",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	// An agent upload always carries a task-scoped mat_ token; set one so the
	// daemon-managed-context gate in newAPIClient admits the request.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\nbytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	cmd := newAttachmentUploadTestCmd()
	_ = cmd.Flags().Set("task", taskID)

	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{imgPath}) })
	if err != nil {
		t.Fatalf("runAttachmentUpload: %v", err)
	}
	if gotTaskID != taskID {
		t.Fatalf("server task_id = %q, want %q", gotTaskID, taskID)
	}
	if gotFile != "present" {
		t.Fatalf("server did not receive a file part")
	}
	// Output contract: id, markdown_url, and a ready-to-paste markdown snippet.
	if !strings.Contains(out, `"id": "att-999"`) {
		t.Fatalf("stdout missing id: %q", out)
	}
	if !strings.Contains(out, `"markdown_url": "https://public.example/api/attachments/att-999/download"`) {
		t.Fatalf("stdout missing markdown_url: %q", out)
	}
	if !strings.Contains(out, `![chart.png](https://public.example/api/attachments/att-999/download)`) {
		t.Fatalf("stdout missing markdown snippet: %q", out)
	}
}

func TestRunAttachmentUploadRequiresTask(t *testing.T) {
	t.Setenv("MULTICA_TASK_ID", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	cmd := newAttachmentUploadTestCmd() // no --task, no MULTICA_TASK_ID
	if err := runAttachmentUpload(cmd, []string{imgPath}); err == nil || !strings.Contains(err.Error(), "no chat task in context") {
		t.Fatalf("runAttachmentUpload error = %v, want no-chat-task error", err)
	}
}

func TestRunAttachmentDownloadRequiresDownloadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/attachments/att-no-url" {
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "att-no-url",
			"filename": "missing.txt",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newAttachmentDownloadTestCmd()
	if err := runAttachmentDownload(cmd, []string{"att-no-url"}); err == nil || !strings.Contains(err.Error(), "no download URL") {
		t.Fatalf("runAttachmentDownload error = %v, want missing download URL", err)
	}
}
