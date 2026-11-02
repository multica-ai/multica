package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Execute the rendered reply command's flags against the real CLI handler.
// The receipt must stay small without changing what reaches the comment API.
func TestCommentReplyReceipt(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	const parentID = "22222222-2222-4222-8222-222222222222"
	body := strings.Repeat("PASS regression\n", 4096) + "验收通过；integration verification pending."
	for _, mode := range []string{"suggested", "json"} {
		for _, status := range []int{http.StatusCreated, http.StatusForbidden} {
			name := mode + "/" + http.StatusText(status)
			t.Run(name, func(t *testing.T) {
				t.Chdir(t.TempDir())
				if err := os.WriteFile("reply.md", []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				var posts atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
						if err := json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "TST-1"}); err != nil {
							t.Error(err)
						}
					case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/comments":
						posts.Add(1)
						var submitted map[string]any
						if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if submitted["content"] != body || submitted["parent_id"] != parentID {
							t.Error("receipt mode changed the submitted body or reply target")
						}
						w.WriteHeader(status)
						response := map[string]any{"error": "forbidden"}
						if status == http.StatusCreated {
							response = map[string]any{"id": "new-comment", "content": body, "parent_id": parentID}
						}
						if err := json.NewEncoder(w).Encode(response); err != nil {
							t.Error(err)
						}
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer srv.Close()
				setCLITestServerEnv(t, srv.URL)
				t.Setenv("MULTICA_TOKEN", "mat_test-token")
				cmd := newIssueCommentAddTestCmd()
				var flags []string
				for _, line := range strings.Split(execenv.BuildCommentReplyInstructions("claude", issueID, parentID, false), "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), "multica issue comment add "+issueID+" ") {
						flags = strings.Fields(line)[5:]
					}
				}
				if len(flags) == 0 {
					t.Fatal("reply instructions contain no posting command")
				}
				if err := cmd.Flags().Parse(flags); err != nil {
					t.Fatal(err)
				}
				if mode == "json" {
					if err := cmd.Flags().Set("output", "json"); err != nil {
						t.Fatal(err)
					}
				}
				// A file avoids blocking on the large JSON receipt's pipe buffer.
				outFile, err := os.CreateTemp(t.TempDir(), "receipt-*")
				if err != nil {
					t.Fatal(err)
				}
				orig := os.Stdout
				t.Cleanup(func() {
					os.Stdout = orig
					if err := outFile.Close(); err != nil {
						t.Error(err)
					}
				})
				os.Stdout = outFile
				stderr := captureStderr(t)
				runErr := runIssueCommentAdd(cmd, []string{issueID})
				os.Stdout = orig
				confirmation := stderr.read()
				if _, err := outFile.Seek(0, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				out, err := io.ReadAll(outFile)
				if err != nil {
					t.Fatal(err)
				}
				if posts.Load() != 1 {
					t.Fatalf("POST count = %d, want exactly one", posts.Load())
				}
				if status == http.StatusForbidden {
					if runErr == nil || strings.Contains(confirmation, "Comment added") || len(out) != 0 {
						t.Fatalf("rejected post did not propagate failure: %v", runErr)
					}
					return
				}
				if runErr != nil || !strings.Contains(confirmation, "Comment added") {
					t.Fatalf("successful post lost confirmation: %v", runErr)
				}
				if mode == "suggested" {
					if len(out) != 0 {
						t.Fatalf("final reply echoed %d stdout bytes instead of only confirming success", len(out))
					}
				} else {
					var result map[string]any
					if err := json.Unmarshal(out, &result); err != nil {
						t.Fatal(err)
					}
					if result["id"] != "new-comment" || result["content"] != body || result["parent_id"] != parentID {
						t.Fatal("explicit JSON lost the returned comment object")
					}
				}
				t.Logf("body_bytes=%d stdout_bytes=%d confirmation_bytes=%d", len(body), len(out), len(confirmation))
			})
		}
	}
}
