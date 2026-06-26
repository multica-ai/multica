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
