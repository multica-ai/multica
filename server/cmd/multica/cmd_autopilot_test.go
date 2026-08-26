package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func newAutopilotGetTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("show-secrets", false, "")
	return cmd
}

func TestRunAutopilotGetRedactsWebhookCredentialsByDefault(t *testing.T) {
	const (
		autopilotID  = "11111111-1111-1111-1111-111111111111"
		webhookToken = "awt_super-secret-7890"
		webhookPath  = "/api/webhooks/autopilots/" + webhookToken
		webhookURL   = "https://hooks.example.com" + webhookPath
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autopilot": map[string]any{"id": autopilotID, "title": "Deploy"},
			"triggers": []map[string]any{
				{
					"id":                  "trigger-1",
					"kind":                "webhook",
					"webhook_token":       webhookToken,
					"webhook_path":        webhookPath,
					"webhook_url":         webhookURL,
					"has_signing_secret":  true,
					"signing_secret_hint": "abcd",
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	out, err := captureStdout(t, func() error {
		return runAutopilotGet(newAutopilotGetTestCmd(), []string{autopilotID})
	})
	if err != nil {
		t.Fatalf("runAutopilotGet: %v", err)
	}
	for _, secret := range []string{webhookToken, webhookPath, webhookURL} {
		if strings.Contains(out, secret) {
			t.Fatalf("default output leaked webhook credential %q:\n%s", secret, out)
		}
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("default output is not valid JSON: %v\n%s", err, out)
	}
	triggers, ok := got["triggers"].([]any)
	if !ok || len(triggers) != 1 {
		t.Fatalf("triggers = %#v, want one trigger", got["triggers"])
	}
	trigger, ok := triggers[0].(map[string]any)
	if !ok {
		t.Fatalf("trigger = %#v, want object", triggers[0])
	}
	for _, field := range []string{"webhook_token", "webhook_path", "webhook_url"} {
		if trigger[field] != nil {
			t.Errorf("%s = %#v, want null", field, trigger[field])
		}
	}
	if trigger["has_webhook_token"] != true {
		t.Errorf("has_webhook_token = %#v, want true", trigger["has_webhook_token"])
	}
	if trigger["webhook_token_hint"] != "7890" {
		t.Errorf("webhook_token_hint = %#v, want %q", trigger["webhook_token_hint"], "7890")
	}
	if trigger["signing_secret_hint"] != "abcd" {
		t.Errorf("signing_secret_hint = %#v, want existing non-credential metadata preserved", trigger["signing_secret_hint"])
	}
}

func TestRunAutopilotGetShowSecretsIsExplicitAndWarns(t *testing.T) {
	const (
		autopilotID  = "11111111-1111-1111-1111-111111111111"
		webhookToken = "awt_super-secret-7890"
		webhookPath  = "/api/webhooks/autopilots/" + webhookToken
		webhookURL   = "https://hooks.example.com" + webhookPath
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autopilot": map[string]any{"id": autopilotID, "title": "Deploy"},
			"triggers": []map[string]any{
				{
					"id":            "trigger-1",
					"kind":          "webhook",
					"webhook_token": webhookToken,
					"webhook_path":  webhookPath,
					"webhook_url":   webhookURL,
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotGetTestCmd()
	_ = cmd.Flags().Set("show-secrets", "true")
	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error {
		return runAutopilotGet(cmd, []string{autopilotID})
	})
	errOut := stderr.read()
	if err != nil {
		t.Fatalf("runAutopilotGet: %v", err)
	}
	for _, secret := range []string{webhookToken, webhookPath, webhookURL} {
		if !strings.Contains(out, secret) {
			t.Errorf("--show-secrets output missing %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(strings.ToLower(errOut), "warning") ||
		!strings.Contains(strings.ToLower(errOut), "webhook credentials") ||
		!strings.Contains(strings.ToLower(errOut), "logs") {
		t.Fatalf("stderr = %q, want credential exposure warning", errOut)
	}
	if strings.Contains(errOut, webhookToken) {
		t.Fatalf("warning itself leaked webhook token: %q", errOut)
	}
}

func TestRunAutopilotGetTableOutputDoesNotExposeWebhookCredentials(t *testing.T) {
	const (
		autopilotID  = "11111111-1111-1111-1111-111111111111"
		webhookToken = "awt_super-secret-7890"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autopilot": map[string]any{
				"id":             autopilotID,
				"title":          "Deploy",
				"status":         "active",
				"execution_mode": "run_only",
			},
			"triggers": []map[string]any{
				{
					"id":            "trigger-1",
					"kind":          "webhook",
					"webhook_token": webhookToken,
					"webhook_path":  "/api/webhooks/autopilots/" + webhookToken,
					"webhook_url":   "https://hooks.example.com/api/webhooks/autopilots/" + webhookToken,
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotGetTestCmd()
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error {
		return runAutopilotGet(cmd, []string{autopilotID})
	})
	if err != nil {
		t.Fatalf("runAutopilotGet: %v", err)
	}
	if strings.Contains(out, webhookToken) {
		t.Fatalf("table output leaked webhook token:\n%s", out)
	}
	if !strings.Contains(out, "Deploy") || !strings.Contains(out, "run_only") {
		t.Fatalf("table output lost ordinary autopilot details:\n%s", out)
	}
}

func TestRedactAutopilotWebhookCredentialsDoesNotDependOnTriggerKind(t *testing.T) {
	const webhookToken = "awt_schema-drift-secret-1234"
	trigger := map[string]any{
		"id":            "trigger-1",
		"webhook_token": webhookToken,
		"webhook_path":  "/api/webhooks/autopilots/" + webhookToken,
		"webhook_url":   "https://hooks.example.com/api/webhooks/autopilots/" + webhookToken,
	}
	resp := map[string]any{"triggers": []any{trigger}}

	redactAutopilotWebhookCredentials(resp)

	for _, field := range []string{"webhook_token", "webhook_path", "webhook_url"} {
		if trigger[field] != nil {
			t.Errorf("%s = %#v, want null", field, trigger[field])
		}
	}
	if trigger["has_webhook_token"] != true || trigger["webhook_token_hint"] != "1234" {
		t.Fatalf("redaction metadata = has:%#v hint:%#v", trigger["has_webhook_token"], trigger["webhook_token_hint"])
	}
}

func TestRedactAutopilotWebhookCredentialsReportsServerRedactedToken(t *testing.T) {
	trigger := map[string]any{
		"id":            "trigger-1",
		"kind":          "webhook",
		"webhook_token": nil,
		"webhook_path":  nil,
		"webhook_url":   nil,
	}
	resp := map[string]any{"triggers": []any{trigger}}

	redactAutopilotWebhookCredentials(resp)

	if trigger["has_webhook_token"] != true {
		t.Fatalf("has_webhook_token = %#v, want true for a permission-redacted webhook", trigger["has_webhook_token"])
	}
	if trigger["webhook_token_hint"] != nil {
		t.Fatalf("webhook_token_hint = %#v, want null when the server withheld the token", trigger["webhook_token_hint"])
	}
}

func TestWebhookTokenHintDoesNotExposeShortToken(t *testing.T) {
	const shortToken = "abc"
	if hint := webhookTokenHint(shortToken); hint != "" {
		t.Fatalf("webhookTokenHint(%q) = %q, want empty hint", shortToken, hint)
	}
}

func TestRunAutopilotGetRejectsShowSecretsWithTableOutput(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotGetTestCmd()
	_ = cmd.Flags().Set("output", "table")
	_ = cmd.Flags().Set("show-secrets", "true")

	err := runAutopilotGet(cmd, []string{"11111111-1111-1111-1111-111111111111"})
	if err == nil {
		t.Fatal("expected --show-secrets with table output to fail")
	}
	if !strings.Contains(err.Error(), "--show-secrets requires --output json") {
		t.Fatalf("error = %v, want output-mode guidance", err)
	}
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

func newAutopilotTriggerListTestCmd(output string) *cobra.Command {
	cmd := &cobra.Command{Use: "trigger-list"}
	cmd.Flags().String("output", output, "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

// TestRunAutopilotTriggerListSurfacesIDs covers MUL-6680: the trigger ids that
// trigger-update / trigger-delete / trigger-rotate-url require must be readable
// from a command of their own, not only by knowing that `get --output json`
// returns "triggers" as a sibling of "autopilot".
func TestRunAutopilotTriggerListSurfacesIDs(t *testing.T) {
	const (
		autopilotID = "11111111-1111-1111-1111-111111111111"
		triggerID   = "22222222-2222-2222-2222-222222222222"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autopilot": map[string]any{"id": autopilotID, "title": "Deploy"},
			"triggers": []map[string]any{
				{
					"id":              triggerID,
					"kind":            "schedule",
					"enabled":         true,
					"cron_expression": "0 9 * * 1-5",
					"timezone":        "America/New_York",
					"label":           "weekday morning",
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// A task-scoped mat_ token so the test also passes inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject a plain token.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runAutopilotTriggerList(newAutopilotTriggerListTestCmd("table"), []string{autopilotID})
	})
	if err != nil {
		t.Fatalf("runAutopilotTriggerList: %v", err)
	}
	// The short id prefix is what trigger-update accepts, so it must be visible.
	for _, want := range []string{triggerID[:8], "schedule", "0 9 * * 1-5", "weekday morning"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}

	out, err = captureStdout(t, func() error {
		return runAutopilotTriggerList(newAutopilotTriggerListTestCmd("json"), []string{autopilotID})
	})
	if err != nil {
		t.Fatalf("runAutopilotTriggerList (json): %v", err)
	}
	var got struct {
		Triggers []map[string]any `json:"triggers"`
		Total    int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json output is not valid JSON: %v\n%s", err, out)
	}
	if got.Total != 1 || len(got.Triggers) != 1 {
		t.Fatalf("expected exactly one trigger, got total=%d triggers=%v", got.Total, got.Triggers)
	}
	if got.Triggers[0]["id"] != triggerID {
		t.Errorf("id = %#v, want %q", got.Triggers[0]["id"], triggerID)
	}
}

// Webhook triggers carry a URL that grants the ability to fire the autopilot,
// so trigger-list must redact it the same way `get` does.
func TestRunAutopilotTriggerListRedactsWebhookCredentials(t *testing.T) {
	const (
		autopilotID  = "11111111-1111-1111-1111-111111111111"
		webhookToken = "awt_super-secret-7890"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autopilot": map[string]any{"id": autopilotID},
			"triggers": []map[string]any{
				{
					"id":            "22222222-2222-2222-2222-222222222222",
					"kind":          "webhook",
					"enabled":       true,
					"webhook_token": webhookToken,
					"webhook_path":  "/api/webhooks/autopilots/" + webhookToken,
					"webhook_url":   "https://hooks.example.com/api/webhooks/autopilots/" + webhookToken,
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// A task-scoped mat_ token so the test also passes inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject a plain token.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runAutopilotTriggerList(newAutopilotTriggerListTestCmd("json"), []string{autopilotID})
	})
	if err != nil {
		t.Fatalf("runAutopilotTriggerList: %v", err)
	}
	if strings.Contains(out, webhookToken) {
		t.Fatalf("trigger-list leaked webhook credential:\n%s", out)
	}
}

func TestRelativeTimestampAt(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string {
		return now.Add(d).Format(time.RFC3339)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		// An autopilot with no schedule must read as visibly different from
		// one that is scheduled — that ambiguity is the MUL-6680 misdiagnosis.
		{"empty", "", "—"},
		{"unparseable", "not-a-timestamp", "—"},
		{"seconds ahead", at(30 * time.Second), "in 30s"},
		{"minutes ahead", at(45 * time.Minute), "in 45m"},
		{"hours ahead", at(2 * time.Hour), "in 2h"},
		{"days ahead", at(72 * time.Hour), "in 3d"},
		{"minutes past", at(-45 * time.Minute), "45m ago"},
		{"hours past", at(-2 * time.Hour), "2h ago"},
		{"days past", at(-72 * time.Hour), "3d ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relativeTimestampAt(tc.in, now); got != tc.want {
				t.Errorf("relativeTimestampAt(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
