package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func newUserNotificationsGetTestCmd(output string) *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", output, "")
	return cmd
}

func newUserNotificationsSetTestCmd(output string) *cobra.Command {
	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("mode", "", "")
	for _, f := range focusedNotificationFlags {
		cmd.Flags().Bool(f.flag, false, "")
	}
	cmd.Flags().String("output", output, "")
	return cmd
}

func TestRunUserNotificationsGetJSONPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/notification-preferences" {
			t.Fatalf("path = %q, want /api/notification-preferences", r.URL.Path)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workspace_id": "ws-1",
			"preferences":  map[string]string{"inbox_mode": "focused", "focused_blocked": "off", "comments": "muted"},
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runUserNotificationsGet(newUserNotificationsGetTestCmd("json"), nil)
	})
	if err != nil {
		t.Fatalf("runUserNotificationsGet: %v", err)
	}
	var got struct {
		WorkspaceID string            `json:"workspace_id"`
		Preferences map[string]string `json:"preferences"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got.WorkspaceID != "ws-1" || got.Preferences["inbox_mode"] != "focused" ||
		got.Preferences["focused_blocked"] != "off" || got.Preferences["comments"] != "muted" {
		t.Fatalf("stdout = %#v, want server response unchanged", got)
	}
}

func TestUserNotificationsTableAppliesDefaults(t *testing.T) {
	var buf bytes.Buffer
	printNotificationPreferencesTable(&buf, map[string]any{"focused_blocked": "off"})
	out := buf.String()

	for _, want := range []string{"MODE", "all", "ASSIGNED", "THREAD REPLY", "AGENT ACTIVITY"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		state := fields[len(fields)-1]
		switch {
		case strings.HasPrefix(line, "BLOCKED") && state != "OFF":
			t.Fatalf("BLOCKED = %s, want OFF", state)
		case !strings.HasPrefix(line, "BLOCKED") && !strings.HasPrefix(line, "MODE") && state != "ON":
			t.Fatalf("%q, want ON for absent key", line)
		}
	}
}

func TestRunUserNotificationsSetSendsOnlyChangedKeys(t *testing.T) {
	var body map[string]map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/notification-preferences" {
			t.Fatalf("path = %q, want /api/notification-preferences", r.URL.Path)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workspace_id": "ws-1", "preferences": body["preferences"]})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newUserNotificationsSetTestCmd("json")
	_ = cmd.Flags().Set("mode", "focused")
	_ = cmd.Flags().Set("blocked", "false")
	_ = cmd.Flags().Set("mentioned", "true")

	if _, err := captureStdout(t, func() error { return runUserNotificationsSet(cmd, nil) }); err != nil {
		t.Fatalf("runUserNotificationsSet: %v", err)
	}
	want := map[string]string{"inbox_mode": "focused", "focused_blocked": "off", "focused_mentioned": "on"}
	got := body["preferences"]
	if len(got) != len(want) {
		t.Fatalf("preferences = %#v, want exactly %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("preferences[%s] = %q, want %q (body %#v)", k, got[k], v, got)
		}
	}
}

func TestRunUserNotificationsSetRejectsInvalidModeWithoutRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newUserNotificationsSetTestCmd("table")
	_ = cmd.Flags().Set("mode", "quiet")

	err := runUserNotificationsSet(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--mode") {
		t.Fatalf("err = %v, want invalid --mode error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server called %d times, want 0", calls.Load())
	}
}

func TestRunUserNotificationsSetWithoutFlagsErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	err := runUserNotificationsSet(newUserNotificationsSetTestCmd("table"), nil)
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("err = %v, want nothing-to-update error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server called %d times, want 0", calls.Load())
	}
}

func TestUserNotificationsSetHelpListsEveryFlag(t *testing.T) {
	help := userNotificationsSetCmd.UsageString() + userNotificationsSetCmd.Long
	for _, want := range []string{"--mode", "--assigned", "--blocked", "--mentioned", "--thread-reply", "--agent-activity", "--output", "new notifications only", "subscribed"} {
		if !strings.Contains(help, want) {
			t.Fatalf("set help missing %q:\n%s", want, help)
		}
	}
}
