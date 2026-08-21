package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newAutopilotCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("mode", "", "")
	cmd.Flags().String("priority", "none", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("issue-title-template", "", "")
	cmd.Flags().StringArray("subscriber", nil, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newAutopilotUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("mode", "", "")
	cmd.Flags().String("issue-title-template", "", "")
	cmd.Flags().StringArray("subscriber", nil, "")
	cmd.Flags().Bool("clear-subscribers", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestResolveAgent(t *testing.T) {
	agentsResp := []map[string]any{
		{"id": "11111111-1111-1111-1111-111111111111", "name": "Lambda"},
		{"id": "22222222-2222-2222-2222-222222222222", "name": "Codex Agent"},
		{"id": "33333333-3333-3333-3333-333333333333", "name": "Claude Reviewer"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agents" {
			json.NewEncoder(w).Encode(agentsResp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("passes through a UUID without lookup", func(t *testing.T) {
		id := "44444444-4444-4444-4444-444444444444"
		got, err := resolveAgent(ctx, client, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != id {
			t.Errorf("got %q, want %q", got, id)
		}
	})

	t.Run("exact name match", func(t *testing.T) {
		got, err := resolveAgent(ctx, client, "Lambda")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("got %q, want Lambda's UUID", got)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		got, err := resolveAgent(ctx, client, "codex")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "22222222-2222-2222-2222-222222222222" {
			t.Errorf("got %q, want Codex Agent's UUID", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := resolveAgent(ctx, client, "nobody")
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("ambiguous match", func(t *testing.T) {
		_, err := resolveAgent(ctx, client, "a") // matches Lambda, Codex Agent, Claude Reviewer
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("expected ambiguous error, got: %v", err)
		}
	})

	t.Run("missing workspace ID for name lookup", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, err := resolveAgent(ctx, noWSClient, "Lambda")
		if err == nil {
			t.Fatal("expected error when workspace ID is missing")
		}
	})

	t.Run("UUID works without workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		id := "55555555-5555-5555-5555-555555555555"
		got, err := resolveAgent(ctx, noWSClient, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != id {
			t.Errorf("got %q, want %q", got, id)
		}
	})
}

func TestRunAutopilotCreateSendsProjectID(t *testing.T) {
	const (
		agentID   = "11111111-1111-1111-1111-111111111111"
		projectID = "22222222-2222-2222-2222-222222222222"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "autopilot-1",
			"title":      "Daily planner",
			"project_id": body["project_id"],
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotCreateTestCmd()
	_ = cmd.Flags().Set("title", "Daily planner")
	_ = cmd.Flags().Set("agent", agentID)
	_ = cmd.Flags().Set("mode", "create_issue")
	_ = cmd.Flags().Set("project", projectID)

	if err := runAutopilotCreate(cmd, nil); err != nil {
		t.Fatalf("runAutopilotCreate: %v", err)
	}
	if got := body["project_id"]; got != projectID {
		t.Fatalf("project_id = %#v, want %q", got, projectID)
	}
}

func TestRunAutopilotCreateSendsSubscribers(t *testing.T) {
	const (
		agentID = "11111111-1111-1111-1111-111111111111"
		userID  = "22222222-2222-2222-2222-222222222222"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{
				{"user_id": userID, "name": "Alice"},
			})
		case "/api/autopilots":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "autopilot-1",
				"title":       "Daily planner",
				"subscribers": body["subscribers"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotCreateTestCmd()
	_ = cmd.Flags().Set("title", "Daily planner")
	_ = cmd.Flags().Set("agent", agentID)
	_ = cmd.Flags().Set("mode", "create_issue")
	_ = cmd.Flags().Set("subscriber", "Alice")

	if err := runAutopilotCreate(cmd, nil); err != nil {
		t.Fatalf("runAutopilotCreate: %v", err)
	}
	assertAutopilotSubscriberBody(t, body, userID)
}

func TestRunAutopilotUpdateSendsProjectIDChanges(t *testing.T) {
	const (
		autopilotID = "33333333-3333-3333-3333-333333333333"
		projectID   = "44444444-4444-4444-4444-444444444444"
	)

	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		json.NewEncoder(w).Encode(map[string]any{
			"id":         autopilotID,
			"title":      "Daily planner",
			"project_id": body["project_id"],
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	t.Run("set project", func(t *testing.T) {
		cmd := newAutopilotUpdateTestCmd()
		_ = cmd.Flags().Set("project", projectID)
		if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
			t.Fatalf("runAutopilotUpdate: %v", err)
		}
		if got := bodies[len(bodies)-1]["project_id"]; got != projectID {
			t.Fatalf("project_id = %#v, want %q", got, projectID)
		}
	})

	t.Run("clear project", func(t *testing.T) {
		cmd := newAutopilotUpdateTestCmd()
		_ = cmd.Flags().Set("project", "")
		if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
			t.Fatalf("runAutopilotUpdate: %v", err)
		}
		got, ok := bodies[len(bodies)-1]["project_id"]
		if !ok {
			t.Fatalf("project_id key missing from update body")
		}
		if got != nil {
			t.Fatalf("project_id = %#v, want nil", got)
		}
	})
}

func TestRunAutopilotUpdateAgentSwitchesAssigneeType(t *testing.T) {
	const (
		autopilotID = "33333333-3333-3333-3333-333333333333"
		agentID     = "11111111-1111-1111-1111-111111111111"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{{"id": agentID, "name": "Codex Agent"}})
		case "/api/autopilots/" + autopilotID:
			if r.Method != http.MethodPatch {
				t.Errorf("method = %s, want PATCH", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body["assignee_type"] != "agent" {
				http.Error(w, "squad not found", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":            autopilotID,
				"assignee_type": body["assignee_type"],
				"assignee_id":   body["assignee_id"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("agent", "Codex Agent")
	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotUpdate: %v", err)
	}
	if got := body["assignee_id"]; got != agentID {
		t.Fatalf("assignee_id = %#v, want %q", got, agentID)
	}
	if got := body["assignee_type"]; got != "agent" {
		t.Fatalf("assignee_type = %#v, want agent", got)
	}
}

func TestRunAutopilotUpdateSendsSubscriberReplacement(t *testing.T) {
	const (
		autopilotID = "33333333-3333-3333-3333-333333333333"
		userID      = "22222222-2222-2222-2222-222222222222"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{
				{"user_id": userID, "name": "Alice"},
			})
		case "/api/autopilots/" + autopilotID:
			if r.Method != http.MethodPatch {
				t.Errorf("method = %s, want PATCH", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":          autopilotID,
				"title":       "Daily planner",
				"subscribers": body["subscribers"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("subscriber", "Alice")
	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotUpdate: %v", err)
	}
	assertAutopilotSubscriberBody(t, body, userID)
}

func TestRunAutopilotUpdateCanClearSubscribers(t *testing.T) {
	const autopilotID = "33333333-3333-3333-3333-333333333333"

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":          autopilotID,
			"title":       "Daily planner",
			"subscribers": body["subscribers"],
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("clear-subscribers", "true")
	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotUpdate: %v", err)
	}
	subscribers, ok := body["subscribers"].([]any)
	if !ok {
		t.Fatalf("subscribers = %#v, want array", body["subscribers"])
	}
	if len(subscribers) != 0 {
		t.Fatalf("subscribers length = %d, want 0", len(subscribers))
	}
}

func TestRunAutopilotUpdateRejectsSubscriberAndClear(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("subscriber", "Alice")
	_ = cmd.Flags().Set("clear-subscribers", "true")

	err := runAutopilotUpdate(cmd, []string{"33333333-3333-3333-3333-333333333333"})
	if err == nil {
		t.Fatal("expected mutually exclusive subscriber flags error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want mutually exclusive", err)
	}
}

func assertAutopilotSubscriberBody(t *testing.T, body map[string]any, userID string) {
	t.Helper()
	subscribers, ok := body["subscribers"].([]any)
	if !ok {
		t.Fatalf("subscribers = %#v, want array", body["subscribers"])
	}
	if len(subscribers) != 1 {
		t.Fatalf("subscribers length = %d, want 1", len(subscribers))
	}
	sub, ok := subscribers[0].(map[string]any)
	if !ok {
		t.Fatalf("subscriber = %#v, want object", subscribers[0])
	}
	if sub["user_type"] != "member" {
		t.Fatalf("user_type = %#v, want member", sub["user_type"])
	}
	if sub["user_id"] != userID {
		t.Fatalf("user_id = %#v, want %q", sub["user_id"], userID)
	}
}

func TestRunAutopilotGetRedactsWebhookSecrets(t *testing.T) {
	const autopilotID = "33333333-3333-3333-3333-333333333333"
	fixtureToken := "tok_live_abc123"
	fixtureURL := "https://example.com/hooks/tok_live_abc123"
	fixturePath := "/hooks/tok_live_abc123"
	baseResp := func() map[string]any {
		return map[string]any{
			"autopilot": map[string]any{"id": autopilotID, "title": "T"},
			"triggers": []any{
				map[string]any{"id": "trig-1", "kind": "webhook", "webhook_token": fixtureToken, "webhook_url": fixtureURL, "webhook_path": fixturePath, "enabled": true},
				map[string]any{"id": "trig-2", "kind": "schedule", "cron_expression": "0 * * * *", "enabled": true},
			},
		}
	}
	newGetCmd := func(showSecrets bool, output string) *cobra.Command {
		cmd := &cobra.Command{Use: "get"}
		cmd.Flags().String("output", "json", "")
		cmd.Flags().Bool("show-secrets", false, "")
		_ = cmd.Flags().Set("output", output)
		if showSecrets {
			_ = cmd.Flags().Set("show-secrets", "true")
		}
		return cmd
	}
	t.Run("json without show-secrets redacts and adds has_webhook_token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(baseResp())
		}))
		defer srv.Close()
		t.Setenv("MULTICA_SERVER_URL", srv.URL)
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "test-token")
		cmd := newGetCmd(false, "json")
		out, errOut := runAutopilotGetCaptured(t, cmd, autopilotID)
		if strings.Contains(out, fixtureToken) || strings.Contains(out, fixtureURL) || strings.Contains(out, fixturePath) {
			t.Fatalf("output should not contain token, url or path, got %q", out)
		}
		if !strings.Contains(out, `"has_webhook_token": true`) && !strings.Contains(out, `"has_webhook_token":true`) {
			t.Fatalf("expected has_webhook_token true, got %q", out)
		}
		if !strings.Contains(out, "[redacted]") {
			t.Fatalf("expected [redacted], got %q", out)
		}
		// schedule trigger unchanged - check its cron still present and not redacted
		if !strings.Contains(out, "0 * * * *") {
			t.Fatalf("schedule trigger should be unchanged, got %q", out)
		}
		if strings.Contains(errOut, "warning") {
			t.Fatalf("should not warn without show-secrets, stderr %q", errOut)
		}
	})
	t.Run("json with show-secrets contains token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(baseResp())
		}))
		defer srv.Close()
		t.Setenv("MULTICA_SERVER_URL", srv.URL)
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "test-token")
		cmd := newGetCmd(true, "json")
		out, errOut := runAutopilotGetCaptured(t, cmd, autopilotID)
		if !strings.Contains(out, fixtureToken) {
			t.Fatalf("expected token in output with show-secrets, got %q", out)
		}
		if !strings.Contains(errOut, "warning: output contains a live webhook credential") {
			t.Fatalf("expected warning on stderr, got %q", errOut)
		}
	})
	t.Run("missing token yields has_webhook_token false and no redacted", func(t *testing.T) {
		resp := map[string]any{
			"autopilot": map[string]any{"id": autopilotID},
			"triggers": []any{
				map[string]any{"id": "trig-1", "kind": "webhook", "enabled": true},
				map[string]any{"id": "trig-2", "kind": "webhook", "webhook_token": "", "enabled": true},
			},
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()
		t.Setenv("MULTICA_SERVER_URL", srv.URL)
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "test-token")
		cmd := newGetCmd(false, "json")
		out, _ := runAutopilotGetCaptured(t, cmd, autopilotID)
		if strings.Contains(out, "[redacted]") {
			t.Fatalf("should not contain [redacted] when token absent/empty, got %q", out)
		}
		// should contain has_webhook_token false
		if !strings.Contains(out, `"has_webhook_token": false`) && !strings.Contains(out, `"has_webhook_token":false`) {
			t.Fatalf("expected has_webhook_token false, got %q", out)
		}
	})
}

func runAutopilotGetCaptured(t *testing.T, cmd *cobra.Command, id string) (string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut := os.Stdout
	oldErr := os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	err := runAutopilotGet(cmd, []string{id})
	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	if err != nil {
		t.Fatalf("runAutopilotGet: %v", err)
	}
	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	return string(out), string(errOut)
}

func TestAutopilotGetFlagRegistered(t *testing.T) {
	f := autopilotGetCmd.Flags().Lookup("show-secrets")
	if f == nil {
		t.Fatal("autopilotGetCmd missing --show-secrets flag")
	}
	if f.DefValue != "false" {
		t.Fatalf("show-secrets default = %q, want false", f.DefValue)
	}
}

func TestUUIDRegexp(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"11111111-1111-1111-1111-111111111111", true},
		{"A1B2C3D4-1111-1111-1111-111111111111", true},
		{"not-a-uuid", false},
		{"11111111-1111-1111-1111-11111111111", false},   // too short
		{"11111111111111111111111111111111", false},      // missing dashes
		{"11111111-1111-1111-1111-1111111111111", false}, // too long
		{"", false},
	}
	for _, tt := range tests {
		if got := uuidRegexp.MatchString(tt.in); got != tt.want {
			t.Errorf("uuidRegexp.MatchString(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
