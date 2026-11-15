package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/handler"
)

// Each child parses the production command tree with fresh Cobra flag state.
func TestIssueMetadataArgvProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_METADATA_ARGV") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI argv")
	}
	t.Setenv("MULTICA_SERVER_URL", os.Getenv("MULTICA_TEST_METADATA_SERVER"))
	t.Setenv("MULTICA_TOKEN", "mat_SYNTHETIC_ONLY")
	t.Setenv("MULTICA_WORKSPACE_ID", "11111111-1111-4111-8111-111111111111")
	rootCmd.SetArgs(os.Args[separator+1:])
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueMetadataMutationKeyArgv(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	const metadataPath = "/api/issues/" + issueID + "/metadata"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		key            string
		wantError      string
		wantLocalError bool
	}{
		{name: "prefix", key: "pipeline_status"},
		{name: "punctuation", key: "_a.B-c9"},
		{name: "maximum length", key: strings.Repeat("a", 64)},
		{name: "fragment", key: "pipeline_status#draft", wantError: "key must match"},
		{name: "query", key: "pipeline_status?draft", wantError: "key must match"},
		{name: "slash", key: "pipeline_status/draft", wantError: "key must match"},
		{name: "percent", key: "pipeline_status%draft", wantError: "key must match"},
		{name: "encoded fragment", key: "pipeline_status%23draft", wantError: "key must match"},
		{name: "empty", key: "", wantError: "--key is required", wantLocalError: true},
		{name: "dot segment", key: ".", wantError: "key must match", wantLocalError: true},
		{name: "parent segment", key: "..", wantError: "key must match", wantLocalError: true},
		{name: "too long", key: strings.Repeat("a", 65), wantError: "key must match"},
		{name: "leading digit", key: "9start", wantError: "key must match"},
		{name: "leading dot", key: ".start", wantError: "key must match"},
		{name: "leading hyphen", key: "-start", wantError: "key must match"},
		{name: "space", key: "has space", wantError: "key must match"},
		{name: "newline", key: "a\nb", wantError: "key must match"},
		{name: "non ASCII", key: "中文", wantError: "key must match"},
	}
	for _, operation := range []string{"set", "delete"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					initial := map[string]any{"pipeline_status": "original", "untouched": true}
					stored := map[string]any{"pipeline_status": "original", "untouched": true}
					var mu sync.Mutex
					var requests []string
					var rejectionStatus int
					router := chi.NewRouter()
					// Invalid keys are rejected before the production handlers access a DB.
					h := &handler.Handler{}
					router.Put(metadataPath+"/{key}", h.SetIssueMetadataKey)
					router.Delete(metadataPath+"/{key}", h.DeleteIssueMetadataKey)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						requests = append(requests, r.Method+" "+r.URL.RequestURI())
						// Keep the prefix writable so a truncated URL would mutate stored data.
						if tc.wantError != "" && r.URL.Path != metadataPath+"/pipeline_status" &&
							(r.Method == http.MethodPut || r.Method == http.MethodDelete) {
							recorder := httptest.NewRecorder()
							router.ServeHTTP(recorder, r)
							rejectionStatus = recorder.Code
							w.WriteHeader(recorder.Code)
							_, _ = w.Write(recorder.Body.Bytes())
							return
						}
						switch {
						case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
							_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID})
						case r.Method == http.MethodGet && r.URL.Path == metadataPath:
							_ = json.NewEncoder(w).Encode(map[string]any{"metadata": stored})
						case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, metadataPath+"/"):
							var body struct {
								Value any `json:"value"`
							}
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								http.Error(w, err.Error(), http.StatusBadRequest)
								return
							}
							stored[strings.TrimPrefix(r.URL.Path, metadataPath+"/")] = body.Value
							_ = json.NewEncoder(w).Encode(map[string]any{"metadata": stored})
						case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, metadataPath+"/"):
							delete(stored, strings.TrimPrefix(r.URL.Path, metadataPath+"/"))
							_ = json.NewEncoder(w).Encode(map[string]any{"metadata": stored})
						default:
							http.NotFound(w, r)
						}
					}))
					defer server.Close()
					argv := []string{"issue", "metadata", operation, issueID, "--key=" + tc.key, "--output=json"}
					if operation == "set" {
						argv = append(argv, "--value=waiting")
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					child := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestIssueMetadataArgvProcess$", "--"}, argv...)...)
					child.Env = append(os.Environ(), "MULTICA_TEST_METADATA_ARGV=1", "MULTICA_TEST_METADATA_SERVER="+server.URL)
					output, runErr := child.CombinedOutput()
					if ctx.Err() != nil {
						t.Fatal(ctx.Err())
					}
					mu.Lock()
					defer mu.Unlock()
					method := http.MethodDelete
					if operation == "set" {
						method = http.MethodPut
					}
					wantRequests := []string{method + " " + metadataPath + "/" + url.PathEscape(tc.key)}
					if tc.wantError != "" {
						if runErr == nil || !strings.Contains(string(output), tc.wantError) {
							t.Errorf("argv %q: error=%v output=%s, want %q", argv, runErr, output, tc.wantError)
						}
						if tc.wantLocalError {
							wantRequests = nil
						} else if rejectionStatus != http.StatusBadRequest {
							t.Errorf("invalid key response status = %d, want 400", rejectionStatus)
						}
						if !reflect.DeepEqual(requests, wantRequests) {
							t.Errorf("requests=%v, want %v", requests, wantRequests)
						}
						if !reflect.DeepEqual(stored, initial) {
							t.Errorf("invalid key mutated metadata: %#v", stored)
						}
						return
					}
					if runErr != nil {
						t.Fatalf("argv %q: %v\n%s", argv, runErr, output)
					}
					expected := initial
					if operation == "set" {
						expected[tc.key] = "waiting"
					} else {
						delete(expected, tc.key)
					}
					if !reflect.DeepEqual(stored, expected) {
						t.Errorf("metadata=%#v, want %#v", stored, expected)
					}
					if operation == "delete" {
						wantRequests = append(wantRequests, "GET "+metadataPath)
					}
					if !reflect.DeepEqual(requests, wantRequests) {
						t.Errorf("requests=%v, want %v", requests, wantRequests)
					}
				})
			}
		})
	}
}
