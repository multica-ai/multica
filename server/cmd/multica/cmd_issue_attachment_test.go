package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Run the production command tree in a fresh process so each argv uses the real
// flag registrations without leaking Cobra's global flag state between tests.
func TestIssueAttachmentArgvProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_ATTACHMENT_ARGV") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI argv")
	}
	// TestMain clears task credentials before tests run. Set only synthetic
	// credentials here, after that isolation, and let TestMain clean up normally.
	t.Setenv("MULTICA_SERVER_URL", os.Getenv("MULTICA_TEST_ATTACHMENT_SERVER"))
	t.Setenv("MULTICA_TOKEN", "mat_SYNTHETIC_ONLY")
	t.Setenv("MULTICA_WORKSPACE_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", filepath.Join(t.TempDir(), "config"))
	rootCmd.SetArgs(os.Args[separator+1:])
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueAttachmentLiteralArgv(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"create", "update", "comment"} {
		t.Run(command, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				flags     []string
				want      []string
				wantError bool
				fragments bool
			}{
				{name: "comma", flags: []string{"--attachment", "report,final.csv"}, want: []string{"report,final.csv"}},
				{name: "comma with fragments", flags: []string{"--attachment", "report,final.csv"}, want: []string{"report,final.csv"}, fragments: true},
				{name: "missing literal with fragments", flags: []string{"--attachment", "absent,ghost.txt"}, wantError: true, fragments: true},
				{name: "valid then missing", flags: []string{"--attachment", "first.txt", "--attachment", "missing.txt"}, wantError: true},
				{name: "repeated", flags: []string{"--attachment", "first.txt", "--attachment", "second.txt"}, want: []string{"first.txt", "second.txt"}},
				{name: "mixed repeated", flags: []string{"--attachment", "first.txt", "--attachment", "report,final.csv"}, want: []string{"first.txt", "report,final.csv"}},
				{name: "equals", flags: []string{"--attachment=report,final.csv", "--attachment=first.txt"}, want: []string{"report,final.csv", "first.txt"}},
				{name: "space", flags: []string{"--attachment", "with space.txt"}, want: []string{"with space.txt"}},
				{name: "literal quote", flags: []string{"--attachment", `say"hi.txt`}, want: []string{`say"hi.txt`}},
				{name: "unicode", flags: []string{"--attachment", "报告.txt"}, want: []string{"报告.txt"}},
				{name: "empty", flags: []string{"--attachment", ""}, wantError: true},
				{name: "empty equals", flags: []string{"--attachment="}, wantError: true},
				{name: "valid then empty", flags: []string{"--attachment", "first.txt", "--attachment="}, wantError: true},
				{name: "none"},
				{name: "old CSV quotes are literal", flags: []string{"--attachment", `"report,final.csv"`}, wantError: true},
				{name: "quoted filename exists", flags: []string{"--attachment", `"report,final.csv"`}, want: []string{`"report,final.csv"`}},
				{name: "external", flags: []string{"--attachment", "../outside.txt"}, wantError: true},
				{name: "external symlink", flags: []string{"--attachment", "link.txt"}, wantError: true},
				{name: "external allowed", flags: []string{"--attachment", "../outside.txt", "--allow-external-file"}, want: []string{"outside.txt"}},
				{name: "URL skipped", flags: []string{"--attachment", "https://example.invalid/a,b", "--attachment", "first.txt"}, want: []string{"first.txt"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if runtime.GOOS == "windows" && strings.Contains(strings.Join(tc.flags, ""), `"`) {
						t.Skip("Windows filenames cannot contain double quotes")
					}
					parent := t.TempDir()
					dir := filepath.Join(parent, "workdir")
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatal(err)
					}
					payload := func(name string) string { return "synthetic payload: " + name + "\n" }
					files := []string{"report,final.csv", "first.txt", "second.txt", "with space.txt", "报告.txt", "body.md"}
					if runtime.GOOS != "windows" {
						files = append(files, `say"hi.txt`)
					}
					if tc.name == "quoted filename exists" {
						files = append(files, `"report,final.csv"`)
					}
					if tc.fragments {
						files = append(files, "report", "final.csv", "absent", "ghost.txt")
					}
					for _, name := range files {
						if err := os.WriteFile(filepath.Join(dir, name), []byte(payload(name)), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					outside := filepath.Join(parent, "outside.txt")
					if err := os.WriteFile(outside, []byte(payload("outside.txt")), 0o600); err != nil {
						t.Fatal(err)
					}
					if tc.name == "external symlink" {
						if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
					}

					type upload struct{ name, content string }
					var mu sync.Mutex
					var uploads []upload
					mutations := 0
					issuePath := "/api/issues/" + issueID
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						w.Header().Set("Content-Type", "application/json")
						switch {
						case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
							file, header, err := r.FormFile("file")
							if err != nil {
								t.Errorf("multipart file: %v", err)
								w.WriteHeader(http.StatusBadRequest)
								return
							}
							defer file.Close()
							data, err := io.ReadAll(file)
							if err != nil {
								t.Errorf("read multipart file: %v", err)
							}
							uploads = append(uploads, upload{header.Filename, string(data)})
							_ = json.NewEncoder(w).Encode(map[string]any{
								"id": fmt.Sprintf("attachment-%d", len(uploads)), "filename": header.Filename,
								"content_type": "text/plain", "markdown_url": "https://example.invalid/attachment",
							})
						case r.Method == http.MethodGet && r.URL.Path == issuePath:
							_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "TEST-1", "description": "existing body", "revision": 1})
						case (r.Method == http.MethodPost && (r.URL.Path == "/api/issues" || r.URL.Path == issuePath+"/comments")) ||
							(r.Method == http.MethodPut && r.URL.Path == issuePath):
							mutations++
							_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "TEST-1"})
						default:
							t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
							w.WriteHeader(http.StatusNotFound)
						}
					}))
					defer server.Close()
					argv := []string{"issue", "create", "--title", "fixture"}
					if command == "update" {
						argv = []string{"issue", "update", issueID, "--title", "fixture"}
					} else if command == "comment" {
						argv = []string{"issue", "comment", "add", issueID, "--content-file", "body.md"}
					}
					argv = append(argv, tc.flags...)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					child := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestIssueAttachmentArgvProcess$", "--"}, argv...)...)
					child.Dir = dir
					for _, entry := range os.Environ() {
						if !strings.HasPrefix(entry, "MULTICA_") {
							child.Env = append(child.Env, entry)
						}
					}
					child.Env = append(child.Env,
						"MULTICA_TEST_ATTACHMENT_ARGV=1", "MULTICA_TEST_ATTACHMENT_SERVER="+server.URL)
					output, err := child.CombinedOutput()
					if ctx.Err() != nil {
						t.Fatalf("CLI timed out: %s", output)
					}
					if (err != nil) != tc.wantError {
						t.Errorf("argv %q: err=%v, wantError=%v; output: %s", argv, err, tc.wantError, output)
					}
					mu.Lock()
					defer mu.Unlock()
					var want []upload
					for _, name := range tc.want {
						want = append(want, upload{name, payload(name)})
					}
					if !reflect.DeepEqual(uploads, want) {
						t.Errorf("uploads = %#v, want %#v", uploads, want)
					}
					wantMutations := 1
					if tc.wantError {
						wantMutations = 0
					}
					if mutations != wantMutations {
						t.Errorf("issue/comment mutations = %d, want %d", mutations, wantMutations)
					}
				})
			}
		})
	}
}
