package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunIssueRerunTask(t *testing.T) {
	t.Chdir(t.TempDir())
	const issueID = "1881a167-4bb6-4602-944b-f40ce4192fe6"
	const runID = "aaaaaaaa-1111-4222-8333-444444444444"
	for _, tc := range []struct {
		name, input, scope, wantErr string
		fresh, ambiguous            bool
	}{
		{name: "full UUID needs no issue", input: runID},
		{name: "fresh", input: runID, fresh: true},
		{name: "short prefix", input: "aaaaaaaa", scope: issueID},
		{name: "short prefix needs issue", input: "aaaaaaaa", wantErr: "require --issue"},
		{name: "ambiguous prefix", input: "aaaa", scope: issueID, ambiguous: true, wantErr: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/issues/" + issueID + "/task-runs":
					runs := []map[string]any{{"id": runID}}
					if tc.ambiguous {
						runs = append(runs, map[string]any{"id": "aaaaabcd-1111-4222-8333-444444444444"})
					}
					_ = json.NewEncoder(w).Encode(runs)
				case "/api/tasks/" + runID + "/rerun":
					posts++
					if r.Method != http.MethodPost {
						t.Errorf("method = %s, want POST", r.Method)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode body: %v", err)
					}
					if len(body) != 1 || body["fresh"] != tc.fresh {
						t.Errorf("body = %v, want fresh=%v", body, tc.fresh)
					}
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-run", "agent_id": "original-agent"})
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := &cobra.Command{Use: "rerun-task"}
			cmd.Flags().String("output", "json", "")
			cmd.Flags().String("issue", tc.scope, "")
			cmd.Flags().Bool("fresh", tc.fresh, "")
			out, err := captureStdout(t, func() error { return runIssueRerunTask(cmd, []string{tc.input}) })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || posts != 0 {
					t.Fatalf("err=%v posts=%d, want %q and no mutation", err, posts, tc.wantErr)
				}
				return
			}
			if err != nil || posts != 1 || !strings.Contains(out, "new-run") {
				t.Fatalf("err=%v posts=%d output=%s", err, posts, out)
			}
		})
	}
}

func TestRunIssueRerunLegacyBody(t *testing.T) {
	t.Chdir(t.TempDir())
	const issueID = "1881a167-4bb6-4602-944b-f40ce4192fe6"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/issues/"+issueID+"/rerun" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 0 {
			t.Errorf("legacy body = %v, err=%v; want empty object", body, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-run"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	cmd := &cobra.Command{Use: "rerun"}
	cmd.Flags().String("output", "json", "")
	if _, err := captureStdout(t, func() error { return runIssueRerun(cmd, []string{issueID}) }); err != nil {
		t.Fatal(err)
	}
	if issueRerunCmd.Deprecated == "" || !issueRerunCmd.Hidden {
		t.Fatal("legacy command must be hidden and deprecated")
	}
}
