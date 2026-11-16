package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runInboxListTest(t *testing.T, flags map[string]string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "list"}
	registerInboxListFlags(cmd)
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	return captureStdout(t, func() error { return runInboxList(cmd, nil) })
}

func TestInboxListFiltersAndCommentAnchors(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/inbox" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("X-Workspace-ID") != "ws-1" || r.Header.Get("Authorization") != "Bearer mat_inbox-test" {
			t.Error("lost workspace or task credential")
		}
		_, _ = w.Write([]byte(`[
			{"id":"n1","type":"mentioned","title":"Need input","issue_id":"i1","issue_project_id":"aaaaaaaa-1111-1111-1111-111111111111","read":false,"created_at":"2026-11-16T08:00:00Z","details":{"comment_id":"c1","original_prompt":"not a list field"}},
			{"id":"n2","type":"mentioned","title":"Another project","issue_project_id":"bbbbbbbb-2222-2222-2222-222222222222","read":true},
			{"id":"n3","type":"new_comment","issue_project_id":"aaaaaaaa-1111-1111-1111-111111111111","details":{}},
			{"id":"n4","type":"system","issue_project_id":null,"details":null}
		]`))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_inbox-test")
	t.Setenv("MULTICA_AGENT_ID", "agent-1")
	t.Setenv("MULTICA_TASK_ID", "task-1")

	for _, tc := range []struct {
		name  string
		flags map[string]string
		ids   string
	}{
		{"all", map[string]string{}, "n1,n2,n3,n4"},
		{"project", map[string]string{"project": "aaaaaaaa-1111-1111-1111-111111111111"}, "n1,n3"},
		{"type", map[string]string{"type": "mentioned"}, "n1,n2"},
		{"intersection", map[string]string{"project": "aaaaaaaa-1111-1111-1111-111111111111", "type": "mentioned"}, "n1"},
		{"empty", map[string]string{"project": "cccccccc-3333-3333-3333-333333333333"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.flags["output"] = "json"
			out, err := runInboxListTest(t, tc.flags)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out) == "null" {
				t.Fatal("empty list must be []")
			}
			var rows []map[string]any
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row["id"].(string))
				if _, ok := row["details"]; ok {
					t.Fatal("raw details leaked into list output")
				}
				if row["id"] == "n1" && (row["comment_id"] != "c1" || row["issue_id"] != "i1" || row["read"] != false || row["created_at"] != "2026-11-16T08:00:00Z") {
					t.Fatalf("lost notification fields: %v", row)
				}
				if row["id"] != "n1" {
					if _, ok := row["comment_id"]; ok {
						t.Fatalf("invented comment anchor: %v", row)
					}
				}
			}
			if strings.Join(ids, ",") != tc.ids {
				t.Fatalf("ids = %v, want %s", ids, tc.ids)
			}
		})
	}
	if requests != 5 {
		t.Fatalf("requests = %d, want one GET per invocation", requests)
	}
}

func TestInboxListTableAndDeniedRequest(t *testing.T) {
	status := http.StatusOK
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/inbox" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(status)
		if status == http.StatusForbidden {
			_, _ = w.Write([]byte(`{"error":"inbox access denied"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"n1","type":"mentioned","title":"Need input","issue_id":"i1","issue_project_id":"p1","read":true,"details":{"comment_id":"c1"}}]`))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_inbox-test")
	out, err := runInboxListTest(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"ID", "TYPE", "TITLE", "ISSUE", "COMMENT", "PROJECT", "READ", "CREATED", "n1", "c1", "true"} {
		if !strings.Contains(out, value) {
			t.Errorf("table missing %q: %s", value, out)
		}
	}
	status = http.StatusForbidden
	out, err = runInboxListTest(t, map[string]string{"output": "json"})
	if err == nil || out != "" {
		t.Fatalf("denied request: output=%q error=%v", out, err)
	}
	if requests != 2 {
		t.Fatalf("denial retried or fell back to another credential: %d requests", requests)
	}
	_, err = runInboxListTest(t, map[string]string{"output": "yaml"})
	if err == nil || requests != 2 {
		t.Fatalf("invalid output made request or succeeded: %v", err)
	}
}

func TestInboxListRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"inbox", "list"})
	if err != nil || cmd.Name() != "list" || cmd.Parent().Name() != "inbox" {
		t.Fatalf("inbox list not registered: %v", err)
	}
	if cmd.Flags().Lookup("user") != nil {
		t.Fatal("inbox must not select another recipient")
	}
}

func TestInboxListResolvesProjectID(t *testing.T) {
	const projectA = "aaaaaaaa-1111-1111-1111-111111111111"
	const projectB = "bbbbbbbb-2222-2222-2222-222222222222"
	const collision = "aaaaaaaa-3333-3333-3333-333333333333"
	for _, tc := range []struct {
		name, input, wantID, wantError string
		projectRequests, inboxRequests int
	}{
		{"full UUID", projectB, "n2", "", 0, 1},
		{"uppercase full UUID", strings.ToUpper(projectB), "n2", "", 0, 1},
		{"unique short ID", "bbbbbbbb", "n2", "", 1, 1},
		{"longer prefix disambiguates", "aaaaaaaa-1111", "n1", "", 1, 1},
		{"no match", "deadbeef", "", `no project found matching id prefix "deadbeef"`, 1, 0},
		{"ambiguous prefix", "aaaaaaaa", "", `ambiguous project id prefix "aaaaaaaa"`, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRequests, inboxRequests := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("X-Workspace-ID") != "ws-1" ||
					r.Header.Get("Authorization") != "Bearer mat_inbox-test" {
					t.Errorf("unexpected method or execution credentials: %s %s", r.Method, r.URL)
				}
				switch r.URL.Path {
				case "/api/projects":
					projectRequests++
					if r.URL.Query().Get("workspace_id") != "ws-1" || r.URL.Query().Get("include_archived") != "true" {
						t.Errorf("project resolution lost existing catalog scope: %s", r.URL)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"projects": []any{
						map[string]any{"id": projectA, "title": "A"},
						map[string]any{"id": projectB, "title": "B"},
						map[string]any{"id": collision, "title": "No notifications, but still ambiguous"},
					}})
				case "/api/inbox":
					inboxRequests++
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"id": "n1", "type": "mentioned", "issue_project_id": projectA},
						{"id": "n2", "type": "mentioned", "issue_project_id": projectB},
						{"id": "n3", "type": "new_comment", "issue_project_id": projectB},
					})
				default:
					t.Errorf("unexpected path: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "mat_inbox-test")
			t.Setenv("MULTICA_AGENT_ID", "agent-1")
			t.Setenv("MULTICA_TASK_ID", "task-1")
			out, err := runInboxListTest(t, map[string]string{"project": tc.input, "type": "mentioned", "output": "json"})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || out != "" {
					t.Fatalf("output=%q error=%v, want %s without list output", out, err, tc.wantError)
				}
				if tc.name == "ambiguous prefix" && (!strings.Contains(err.Error(), projectA) || !strings.Contains(err.Error(), collision)) {
					t.Fatalf("ambiguity error omitted matching project IDs: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var rows []struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0].ID != tc.wantID {
					t.Fatalf("filtered output=%s, want %s", out, tc.wantID)
				}
			}
			if projectRequests != tc.projectRequests || inboxRequests != tc.inboxRequests {
				t.Fatalf("project/inbox requests=%d/%d, want %d/%d", projectRequests, inboxRequests, tc.projectRequests, tc.inboxRequests)
			}
		})
	}
}
