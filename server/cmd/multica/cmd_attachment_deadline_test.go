package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestRunAttachmentDownloadCallerBudget(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "completes within command budget"
		if interrupted {
			name = "interrupted body preserves target"
		}
		t.Run(name, func(t *testing.T) {
			var dir string
			t.Cleanup(func() {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Errorf("fixture cleanup: path=%s err=%v", dir, err)
				}
			})
			dir = t.TempDir()
			t.Logf("owned fixture before download I/O: %s", dir)
			target := filepath.Join(dir, "report.txt")
			if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer mat_test-token" {
					t.Error("missing relative request authentication")
				}
				switch r.URL.Path {
				case "/api/attachments/att-budget":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id": "att-budget", "filename": "../report.txt", "download_url": "/download", "size_bytes": 14,
					})
				case "/download":
					if interrupted {
						w.Header().Set("Content-Length", "100")
					}
					_, _ = io.WriteString(w, "first-")
					w.(http.Flusher).Flush()
					time.Sleep(300 * time.Millisecond)
					_, _ = io.WriteString(w, "complete")
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-download-test")
			t.Setenv("MULTICA_TOKEN", "mat_test-token")
			t.Setenv("MULTICA_HTTP_TIMEOUT", "100ms")
			cmd := &cobra.Command{Use: "download"}
			cmd.Flags().String("output-dir", dir, "")
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			stdout := os.Stdout
			os.Stdout = writer
			defer func() { os.Stdout = stdout; _ = writer.Close() }()
			err = runAttachmentDownload(cmd, []string{"att-budget"})
			os.Stdout = stdout
			_ = writer.Close()
			out, readErr := io.ReadAll(reader)
			if readErr != nil {
				t.Fatal(readErr)
			}
			data, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if interrupted {
				if !errors.Is(err, io.ErrUnexpectedEOF) || cli.ExitCodeFor(err) != cli.ExitNetwork || string(data) != "original" || len(out) != 0 {
					t.Fatalf("interrupted download: data=%q stdout=%q error=%v", data, out, err)
				}
			} else {
				if err != nil || string(data) != "first-complete" {
					t.Fatalf("download within command budget: data=%q error=%v", data, err)
				}
				var result map[string]any
				if err := json.Unmarshal(out, &result); err != nil || result["path"] != target || result["filename"] != "report.txt" || result["id"] != "att-budget" {
					t.Fatalf("download JSON: %s error=%v", out, err)
				}
			}
			if requests.Load() != 2 {
				t.Fatalf("requests=%d, want metadata + one download", requests.Load())
			}
		})
	}
}
