package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type autopilotCLIRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
}

func TestAutopilotCommandRegistration(t *testing.T) {
	for _, args := range [][]string{
		{"autopilot", "list"},
		{"autopilot", "get"},
		{"autopilot", "runs"},
		{"autopilot", "create"},
		{"autopilot", "update"},
		{"autopilot", "trigger"},
		{"autopilot", "delete"},
		{"autopilot", "trigger-add"},
		{"autopilot", "trigger-update"},
		{"autopilot", "trigger-delete"},
	} {
		if _, _, err := rootCmd.Find(args); err != nil {
			t.Fatalf("expected command %v to exist: %v", args, err)
		}
	}
}

func TestAutopilotCLIHTTPParity(t *testing.T) {
	var got []autopilotCLIRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if workspaceID := r.Header.Get("X-Workspace-ID"); workspaceID != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", workspaceID)
		}
		record := autopilotCLIRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   map[string]any{},
		}
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(strings.TrimSpace(string(data))) > 0 {
				if err := json.Unmarshal(data, &record.Body); err != nil {
					t.Fatalf("decode request body for %s %s: %v", r.Method, r.URL.Path, err)
				}
			}
		}
		got = append(got, record)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/autopilots":
			writeAutopilotTestJSON(t, w, map[string]any{"autopilots": []any{}, "total": 0, "has_more": false})
		case r.Method == http.MethodGet && r.URL.Path == "/api/autopilots/ap-1":
			writeAutopilotTestJSON(t, w, autopilotTestObject())
		case r.Method == http.MethodGet && r.URL.Path == "/api/autopilots/ap-1/runs":
			writeAutopilotTestJSON(t, w, map[string]any{"runs": []any{}, "total": 0, "has_more": false})
		case r.Method == http.MethodPost && r.URL.Path == "/api/autopilots":
			writeAutopilotTestJSON(t, w, autopilotTestObject())
		case r.Method == http.MethodPut && r.URL.Path == "/api/autopilots/ap-1":
			writeAutopilotTestJSON(t, w, autopilotTestObject())
		case r.Method == http.MethodPost && r.URL.Path == "/api/autopilots/ap-1/trigger":
			writeAutopilotTestJSON(t, w, autopilotRunTestObject())
		case r.Method == http.MethodDelete && r.URL.Path == "/api/autopilots/ap-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/autopilots/ap-1/triggers":
			writeAutopilotTestJSON(t, w, autopilotTriggerTestObject())
		case r.Method == http.MethodPut && r.URL.Path == "/api/autopilots/ap-1/triggers/tr-1":
			writeAutopilotTestJSON(t, w, autopilotTriggerTestObject())
		case r.Method == http.MethodDelete && r.URL.Path == "/api/autopilots/ap-1/triggers/tr-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("HOME", t.TempDir())

	listCmd := autopilotListTestCmd()
	mustSetFlag(t, listCmd, "output", "json")
	mustSetFlag(t, listCmd, "status", "active")
	mustSetFlag(t, listCmd, "limit", "10")
	mustSetFlag(t, listCmd, "offset", "5")
	if err := runAutopilotList(listCmd, nil); err != nil {
		t.Fatalf("runAutopilotList() error = %v", err)
	}

	getCmd := autopilotGetTestCmd()
	mustSetFlag(t, getCmd, "output", "json")
	if err := runAutopilotGet(getCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotGet() error = %v", err)
	}

	runsCmd := autopilotRunsTestCmd()
	mustSetFlag(t, runsCmd, "output", "json")
	mustSetFlag(t, runsCmd, "limit", "7")
	if err := runAutopilotRuns(runsCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotRuns() error = %v", err)
	}

	createCmd := autopilotCreateTestCmd()
	mustSetFlag(t, createCmd, "title", "Nightly planning")
	mustSetFlag(t, createCmd, "description", "Create a planning issue")
	mustSetFlag(t, createCmd, "agent", "BMad Dev")
	mustSetFlag(t, createCmd, "mode", "create_issue")
	mustSetFlag(t, createCmd, "status", "active")
	mustSetFlag(t, createCmd, "priority", "medium")
	mustSetFlag(t, createCmd, "project", "project-1")
	mustSetFlag(t, createCmd, "issue-title-template", "{{autopilot.title}}")
	if err := runAutopilotCreate(createCmd, nil); err != nil {
		t.Fatalf("runAutopilotCreate() error = %v", err)
	}

	updateCmd := autopilotUpdateTestCmd()
	mustSetFlag(t, updateCmd, "title", "Updated planning")
	mustSetFlag(t, updateCmd, "status", "paused")
	if err := runAutopilotUpdate(updateCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotUpdate() error = %v", err)
	}

	triggerCmd := autopilotOutputOnlyTestCmd()
	if err := runAutopilotTrigger(triggerCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotTrigger() error = %v", err)
	}

	deleteCmd := autopilotOutputOnlyTestCmd()
	mustSetFlag(t, deleteCmd, "output", "json")
	if err := runAutopilotDelete(deleteCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotDelete() error = %v", err)
	}

	triggerAddCmd := autopilotTriggerAddTestCmd()
	mustSetFlag(t, triggerAddCmd, "cron", "0 9 * * *")
	mustSetFlag(t, triggerAddCmd, "timezone", "Europe/Istanbul")
	mustSetFlag(t, triggerAddCmd, "status", "active")
	mustSetFlag(t, triggerAddCmd, "label", "Morning")
	if err := runAutopilotTriggerAdd(triggerAddCmd, []string{"ap-1"}); err != nil {
		t.Fatalf("runAutopilotTriggerAdd() error = %v", err)
	}

	triggerUpdateCmd := autopilotTriggerUpdateTestCmd()
	mustSetFlag(t, triggerUpdateCmd, "status", "paused")
	if err := runAutopilotTriggerUpdate(triggerUpdateCmd, []string{"ap-1", "tr-1"}); err != nil {
		t.Fatalf("runAutopilotTriggerUpdate() error = %v", err)
	}

	triggerDeleteCmd := autopilotOutputOnlyTestCmd()
	mustSetFlag(t, triggerDeleteCmd, "output", "json")
	if err := runAutopilotTriggerDelete(triggerDeleteCmd, []string{"ap-1", "tr-1"}); err != nil {
		t.Fatalf("runAutopilotTriggerDelete() error = %v", err)
	}

	want := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/autopilots"},
		{http.MethodGet, "/api/autopilots/ap-1"},
		{http.MethodGet, "/api/autopilots/ap-1/runs"},
		{http.MethodPost, "/api/autopilots"},
		{http.MethodPut, "/api/autopilots/ap-1"},
		{http.MethodPost, "/api/autopilots/ap-1/trigger"},
		{http.MethodDelete, "/api/autopilots/ap-1"},
		{http.MethodPost, "/api/autopilots/ap-1/triggers"},
		{http.MethodPut, "/api/autopilots/ap-1/triggers/tr-1"},
		{http.MethodDelete, "/api/autopilots/ap-1/triggers/tr-1"},
	}
	if len(got) != len(want) {
		t.Fatalf("recorded %d requests, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Method != want[i].method || got[i].Path != want[i].path {
			t.Fatalf("request %d = %s %s, want %s %s", i, got[i].Method, got[i].Path, want[i].method, want[i].path)
		}
	}
	if got[0].Query.Get("workspace_id") != "ws-1" || got[0].Query.Get("status") != "active" || got[0].Query.Get("limit") != "10" || got[0].Query.Get("offset") != "5" {
		t.Fatalf("list query = %v, want workspace/status/limit/offset", got[0].Query)
	}
	if got[2].Query.Get("limit") != "7" {
		t.Fatalf("runs query = %v, want limit=7", got[2].Query)
	}
	if got[3].Body["title"] != "Nightly planning" || got[3].Body["agent"] != "BMad Dev" || got[3].Body["mode"] != "create_issue" {
		t.Fatalf("create body = %+v", got[3].Body)
	}
	if got[4].Body["title"] != "Updated planning" || got[4].Body["status"] != "paused" {
		t.Fatalf("update body = %+v", got[4].Body)
	}
	if got[7].Body["cron"] != "0 9 * * *" || got[7].Body["timezone"] != "Europe/Istanbul" || got[7].Body["type"] != "schedule" {
		t.Fatalf("trigger add body = %+v", got[7].Body)
	}
	if got[8].Body["status"] != "paused" {
		t.Fatalf("trigger update body = %+v", got[8].Body)
	}
}

func TestAutopilotCLIValidation(t *testing.T) {
	createCmd := autopilotCreateTestCmd()
	mustSetFlag(t, createCmd, "title", "Invalid")
	mustSetFlag(t, createCmd, "agent", "BMad Dev")
	mustSetFlag(t, createCmd, "mode", "run_only")
	if err := runAutopilotCreate(createCmd, nil); err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Fatalf("runAutopilotCreate invalid mode error = %v, want invalid mode", err)
	}

	updateCmd := autopilotUpdateTestCmd()
	if err := runAutopilotUpdate(updateCmd, []string{"ap-1"}); err == nil || !strings.Contains(err.Error(), "no fields to update") {
		t.Fatalf("runAutopilotUpdate empty body error = %v, want no fields", err)
	}

	triggerAddCmd := autopilotTriggerAddTestCmd()
	if err := runAutopilotTriggerAdd(triggerAddCmd, []string{"ap-1"}); err == nil || !strings.Contains(err.Error(), "--cron is required") {
		t.Fatalf("runAutopilotTriggerAdd missing cron error = %v, want cron required", err)
	}
}

func autopilotListTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Int("offset", 0, "")
	return cmd
}

func autopilotGetTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func autopilotRunsTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().Int("offset", 0, "")
	return cmd
}

func autopilotCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("mode", "create_issue", "")
	cmd.Flags().String("status", "active", "")
	cmd.Flags().String("priority", "none", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("issue-title-template", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func autopilotUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("mode", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("issue-title-template", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func autopilotOutputOnlyTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func autopilotTriggerAddTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "schedule", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("timezone", "UTC", "")
	cmd.Flags().String("status", "active", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func autopilotTriggerUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func mustSetFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set flag %s=%q: %v", name, value, err)
	}
}

func writeAutopilotTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func autopilotTestObject() map[string]any {
	return map[string]any{
		"id":                   "ap-1",
		"workspace_id":         "ws-1",
		"title":                "Nightly planning",
		"description":          "Create a planning issue",
		"status":               "active",
		"mode":                 "create_issue",
		"agent_id":             "agent-1",
		"priority":             "medium",
		"issue_title_template": "{{autopilot.title}}",
		"created_at":           "2026-04-25T00:00:00Z",
		"updated_at":           "2026-04-25T00:00:00Z",
		"triggers":             []any{autopilotTriggerTestObject()},
	}
}

func autopilotRunTestObject() map[string]any {
	return map[string]any{
		"id":               "run-1",
		"workspace_id":     "ws-1",
		"autopilot_id":     "ap-1",
		"source":           "manual",
		"status":           "succeeded",
		"created_issue_id": "issue-1",
		"created_task_id":  "task-1",
		"created_at":       "2026-04-25T00:00:00Z",
	}
}

func autopilotTriggerTestObject() map[string]any {
	return map[string]any{
		"id":           "tr-1",
		"autopilot_id": "ap-1",
		"type":         "schedule",
		"label":        "Morning",
		"cron":         "0 9 * * *",
		"timezone":     "Europe/Istanbul",
		"status":       "active",
		"next_run_at":  "2026-04-26T06:00:00Z",
		"created_at":   "2026-04-25T00:00:00Z",
		"updated_at":   "2026-04-25T00:00:00Z",
	}
}
