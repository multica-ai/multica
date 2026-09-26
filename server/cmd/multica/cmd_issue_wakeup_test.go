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

func TestIssueWakeupCLISendsExpiry(t *testing.T) {
	t.Chdir(t.TempDir())
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "wake", "enabled": true})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	cmd := newIssueWakeupCommand()
	cmd.SetArgs([]string{"create", "a57c0511-1ebc-471d-a314-438ca16cc75d", "--event", "comment.created", "--instruction", "follow up", "--expires-in", "72h", "--on-timeout", "wake"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["expires_in_seconds"] != float64(259200) || body["on_timeout"] != "wake" {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestIssueWakeupCLIConditionsAndManagement(t *testing.T) {
	t.Chdir(t.TempDir())
	const issue = "a57c0511-1ebc-471d-a314-438ca16cc75d"
	type call struct {
		method, path string
		body         map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.Path, body})
		switch {
		case r.Method == "GET":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "run", "status": "completed", "checkin_note": "fine"}})
		case r.Method == "POST" && r.URL.Path == "/api/issues/"+issue+"/wakeups":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "wake"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	run := func(args ...string) error {
		cmd := newIssueWakeupCommand()
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		return cmd.Execute()
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--until-status", "in_review"}, `{"field":"status","type":"issue_field","value":"in_review"}`},
		{[]string{"--until-property", "p1=\"approved\""}, `{"field":"property","property_id":"p1","type":"issue_field","value":"approved"}`},
		{[]string{"--until-property", "p1=approved"}, `{"field":"property","property_id":"p1","type":"issue_field","value":"approved"}`},
		{[]string{"--until-children-done", "--stage", "2"}, `{"stage":2,"type":"children_done"}`},
		{[]string{"--until-pr", "checks", "--mode", "continuous", "--max-fires", "5"}, `{"event":"checks_finished","type":"pull_request"}`},
		{[]string{"--until-assignee", "agent:a1"}, `{"assignee_id":"a1","assignee_type":"agent","field":"assignee","type":"issue_field"}`},
	} {
		calls = nil
		if err := run(append([]string{"create", issue, "--instruction", "go"}, tc.args...)...); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		got, _ := json.Marshal(calls[len(calls)-1].body["condition"])
		if string(got) != tc.want {
			t.Errorf("%v: condition %s, want %s", tc.args, got, tc.want)
		}
		if tc.args[0] == "--until-pr" && calls[len(calls)-1].body["max_fires"] != float64(5) {
			t.Errorf("max_fires not sent: %+v", calls[len(calls)-1].body)
		}
	}
	for _, bad := range [][]string{
		{"--until-status", "done", "--until-label", "l1"},
		{"--stage", "2"},
		{"--until-pr", "reviews"},
		{"--until-assignee", "a1"},
	} {
		if err := run(append([]string{"create", issue, "--instruction", "go"}, bad...)...); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	calls = nil
	for _, args := range [][]string{
		{"trigger", issue, "w1"},
		{"delete", issue, "w1"},
		{"checkin", issue, "w1", "--note", "CI still running"},
		{"runs", issue, "w1"},
	} {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := []string{"POST /api/issues/" + issue + "/wakeups/w1/trigger", "DELETE /api/issues/" + issue + "/wakeups/w1", "POST /api/issues/" + issue + "/wakeups/w1/checkin", "GET /api/issues/" + issue + "/wakeups/w1/runs"}
	if len(calls) != len(want) {
		t.Fatalf("calls %+v", calls)
	}
	for i, c := range calls {
		if c.method+" "+c.path != want[i] {
			t.Errorf("call %d = %s %s, want %s", i, c.method, c.path, want[i])
		}
	}
	if calls[2].body["note"] != "CI still running" {
		t.Errorf("check-in body %+v", calls[2].body)
	}
	if err := run("checkin", issue, "w1"); err == nil {
		t.Error("check-in without a note accepted")
	}
}
