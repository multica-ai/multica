package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func provisionTestCommand() *cobra.Command {
	cmd := newWorkspaceMemberInviteTestCmd()
	cmd.Flags().String("name", "", "")
	return cmd
}

func TestWorkspaceMemberProvisionPayloadAndFailures(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "fixture-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "default-workspace")
	var gotBody map[string][]map[string]any
	raw := `{"results":[{"email":"seed@example.test","status":"created","role":"member","user_id":"user","member_id":"member"}]}`
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/workspaces/default-workspace/members/provision" || r.Header.Get("X-Workspace-ID") != "default-workspace" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	cmd := provisionTestCommand()
	if _, err := captureStdout(t, func() error { return runWorkspaceMemberProvision(cmd, []string{" Seed@Example.Test "}) }); err != nil {
		t.Fatal(err)
	}
	if _, exists := gotBody["entries"][0]["name"]; exists {
		t.Fatal("omitted name was sent")
	}
	_ = cmd.Flags().Set("name", "  Display Name ")
	if _, err := captureStdout(t, func() error { return runWorkspaceMemberProvision(cmd, []string{"seed@example.test"}) }); err != nil {
		t.Fatal(err)
	}
	if gotBody["entries"][0]["name"] != "Display Name" {
		t.Fatalf("body=%v", gotBody)
	}
	for _, payload := range []string{`{}`, `{"results":[]}`, `{"results":[{"status":"created"}]}`, `{"results":[{"status":"future"}]}`, `{"results":[{"status":"failed","email":"seed@example.test","error":"blocked"}]}`} {
		raw = payload
		if _, err := captureStdout(t, func() error { return runWorkspaceMemberProvision(cmd, []string{"seed@example.test"}) }); err == nil {
			t.Fatalf("accepted malformed/failed response: %s", payload)
		}
	}
	before := calls
	for _, tc := range []struct{ email, flag, value string }{
		{"not-email", "", ""}, {"seed@example.test", "role", "owner"}, {"seed@example.test", "role", "unknown"}, {"seed@example.test", "name", "  "}, {"seed@example.test", "output", "yaml"},
	} {
		cmd := provisionTestCommand()
		if tc.flag != "" {
			_ = cmd.Flags().Set(tc.flag, tc.value)
		}
		if err := runWorkspaceMemberProvision(cmd, []string{tc.email}); err == nil {
			t.Fatalf("accepted invalid input: %+v", tc)
		}
	}
	if calls != before {
		t.Fatal("invalid CLI inputs made HTTP calls")
	}
	raw = `{"results":[{"email":"seed@example.test","status":"already_member","role":"admin","user_id":"user","member_id":"member"}]}`
	cmd = provisionTestCommand()
	_ = cmd.Flags().Set("output", "table")
	output, err := captureStdout(t, func() error { return runWorkspaceMemberProvision(cmd, []string{"seed@example.test"}) })
	if err != nil || !strings.Contains(output, "already_member") || !strings.Contains(output, "admin") {
		t.Fatalf("table=%q err=%v", output, err)
	}
}

func TestWorkspaceMemberProvisionCommandRegistered(t *testing.T) {
	cmd, remaining, err := rootCmd.Find([]string{"workspace", "member", "provision"})
	if err != nil || cmd.Name() != "provision" || len(remaining) != 0 {
		t.Fatalf("provision command missing: cmd=%s remaining=%v err=%v", cmd.Name(), remaining, err)
	}
	for _, flag := range []string{"name", "role", "output"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("missing --%s", flag)
		}
	}
}
