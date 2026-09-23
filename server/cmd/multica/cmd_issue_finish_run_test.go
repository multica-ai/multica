package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueFinishRunUsesOnlyCurrentTask(t *testing.T) {
	t.Chdir(t.TempDir())
	const task = "a57c0511-1ebc-471d-a314-438ca16cc75d"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/tasks/"+task+"/complete" || r.Header.Get("Authorization") != "Bearer mat_test" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(202)
		_ = json.NewEncoder(w).Encode(map[string]bool{"requested": true})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test")
	t.Setenv("MULTICA_TASK_ID", task)
	var output bytes.Buffer
	issueFinishRunCmd.SetOut(&output)
	t.Cleanup(func() { issueFinishRunCmd.SetOut(nil) })
	if err := issueFinishRunCmd.RunE(issueFinishRunCmd, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !bytes.Contains(output.Bytes(), []byte("Deliver your final reply")) {
		t.Fatalf("calls=%d output=%s", calls, output.String())
	}
	t.Setenv("MULTICA_TOKEN", "personal-token")
	if err := issueFinishRunCmd.RunE(issueFinishRunCmd, nil); err == nil {
		t.Fatal("accepted non-task credentials")
	}
	if calls != 1 {
		t.Fatal("sent unauthorized completion")
	}
}
