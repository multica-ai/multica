package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestIssueWakeupCLIPreservesInstructionAndDuration(t *testing.T) {
	t.Chdir(t.TempDir())
	const issue = "a57c0511-1ebc-471d-a314-438ca16cc75d"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/issues/"+issue+"/wakeups" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "wake", "enabled": true, "kind": "at", "mode": "once"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	const note = "检查部署\n保留真实换行"
	if err := os.WriteFile("instruction.md", []byte(note), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := newIssueWakeupCommand()
	cmd.SetArgs([]string{"create", issue, "--kind", "at", "--after", "10m", "--instruction-file", "./instruction.md"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["after_seconds"] != float64(600) || body["instruction"] != note {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestIssueWakeupCreateRetriesOnlyRolledBackSourceConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		want    int
		success bool
	}{
		{"source busy", 409, `{"code":"wakeup_source_busy","error":"busy"}`, 2, false},
		{"source unlocked", 409, `{"code":"wakeup_source_busy","error":"busy"}`, 2, true},
		{"other conflict", 409, `{"code":"revision_conflict","error":"busy"}`, 1, false},
		{"server failure", 500, `{"error":"failed"}`, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tc.success && calls == 2 {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":"wake","enabled":true}`))
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := newIssueWakeupCommand()
			cmd.SetArgs([]string{"create", "a57c0511-1ebc-471d-a314-438ca16cc75d", "--event", "task.completed", "--instruction", "inspect"})
			if (cmd.Execute() == nil) != tc.success || calls != tc.want {
				t.Fatalf("calls=%d want=%d", calls, tc.want)
			}
		})
	}
}

func TestIssueWakeupCLIActorFilter(t *testing.T) {
	t.Chdir(t.TempDir())
	const issue = "a57c0511-1ebc-471d-a314-438ca16cc75d"
	const person = "356c8712-6100-4fb2-ac42-513770588468"
	var body map[string]any
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "wake", "enabled": true})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	for _, action := range []string{"create", "update"} {
		cmd := newIssueWakeupCommand()
		args := []string{action, issue}
		wantMethod := "POST"
		if action == "update" {
			args = append(args, "wake")
			wantMethod = "PUT"
		}
		args = append(args, "--event", "comment.created", "--filter-actor-type", "member", "--filter-actor-id", person, "--instruction", "wait")
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if method != wantMethod || body["filter_actor_type"] != "member" || body["filter_actor_id"] != person {
			t.Fatalf("lost actor filter: %s %+v", method, body)
		}
	}
}
