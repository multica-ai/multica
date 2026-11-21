//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// An attachment uploaded from macOS or Linux can carry characters Windows
// does not allow in a file name. Download must still land in a readable file
// rather than failing ("?", "*", "<", ">", "|", `"`) or writing into an NTFS
// alternate data stream of a different file (":").
func TestRunAttachmentDownloadWindowsReservedCharacters(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{filename: "what?.png", want: "what_.png"},
		{filename: `plan <v2> "final" | *.md`, want: "plan _v2_ _final_ _ _.md"},
		{filename: "notes:v2.txt", want: "notes_v2.txt"},
		{filename: "tab\there.txt", want: "tab_here.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			const body = "attachment body"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/attachments/att-win":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id": "att-win", "filename": tc.filename, "download_url": "/download",
					})
				case "/download":
					_, _ = w.Write([]byte(body))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_test-token")

			outputDir := t.TempDir()
			cmd := newAttachmentDownloadTestCmd()
			_ = cmd.Flags().Set("output-dir", outputDir)
			stderr := captureStderr(t)
			_, err := captureStdout(t, func() error { return runAttachmentDownload(cmd, []string{"att-win"}) })
			_ = stderr.read()
			if err != nil {
				t.Fatalf("runAttachmentDownload(%q): %v", tc.filename, err)
			}

			entries, err := os.ReadDir(outputDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != tc.want {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Fatalf("output dir holds %q, want exactly %q", names, tc.want)
			}
			data, err := os.ReadFile(filepath.Join(outputDir, tc.want))
			if err != nil || string(data) != body {
				t.Fatalf("read %s = %q, %v; want %q", tc.want, data, err, body)
			}
		})
	}
}
