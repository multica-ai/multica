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
