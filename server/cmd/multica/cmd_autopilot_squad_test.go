package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newAutopilotListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func TestRunAutopilotListDisplaysSquadName(t *testing.T) {
	const squadID = "11111111-1111-1111-1111-111111111111"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autopilots":
			json.NewEncoder(w).Encode(map[string]any{
				"autopilots": []map[string]any{{
					"id":             "autopilot-1",
					"title":          "Daily planner",
					"status":         "active",
					"execution_mode": "create_issue",
					"assignee_type":  "squad",
					"assignee_id":    squadID,
					"last_run_at":    "",
				}},
				"total": 1,
			})
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{{"id": squadID, "name": "Frontend Team"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	out, err := captureStdout(t, func() error {
		return runAutopilotList(newAutopilotListTestCmd(), nil)
	})
	if err != nil {
		t.Fatalf("runAutopilotList: %v", err)
	}
	if !strings.Contains(out, "Frontend Team") {
		t.Fatalf("output = %q, want squad name", out)
	}
	if strings.Contains(out, squadID) {
		t.Fatalf("output = %q, must not expose squad UUID", out)
	}
}

func TestRunAutopilotListDefaultsMissingAssigneeTypeToAgent(t *testing.T) {
	const agentID = "11111111-1111-1111-1111-111111111111"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autopilots":
			json.NewEncoder(w).Encode(map[string]any{
				"autopilots": []map[string]any{{
					"id":             "autopilot-1",
					"title":          "Daily planner",
					"status":         "active",
					"execution_mode": "create_issue",
					"assignee_id":    agentID,
					"last_run_at":    "",
				}},
				"total": 1,
			})
		case "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{{"id": agentID, "name": "CodeBot"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	out, err := captureStdout(t, func() error {
		return runAutopilotList(newAutopilotListTestCmd(), nil)
	})
	if err != nil {
		t.Fatalf("runAutopilotList: %v", err)
	}
	if !strings.Contains(out, "CodeBot") {
		t.Fatalf("output = %q, want legacy agent name", out)
	}
	if strings.Contains(out, agentID) {
		t.Fatalf("output = %q, must not expose agent UUID", out)
	}
}

func TestRunAutopilotGetDisplaysSquadName(t *testing.T) {
	const (
		autopilotID = "22222222-2222-2222-2222-222222222222"
		squadID     = "11111111-1111-1111-1111-111111111111"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autopilots/" + autopilotID:
			json.NewEncoder(w).Encode(map[string]any{
				"autopilot": map[string]any{
					"id":             autopilotID,
					"title":          "Daily planner",
					"status":         "active",
					"execution_mode": "create_issue",
					"assignee_type":  "squad",
					"assignee_id":    squadID,
					"last_run_at":    "",
				},
				"triggers": []any{},
			})
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{{"id": squadID, "name": "Frontend Team"}})
		default:
			http.NotFound(w, r)
		}
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
	if !strings.Contains(out, "Frontend Team") {
		t.Fatalf("output = %q, want squad name", out)
	}
	if strings.Contains(out, squadID) {
		t.Fatalf("output = %q, must not expose squad UUID", out)
	}
}

func TestResolveSquad(t *testing.T) {
	squadsResp := []map[string]any{
		{"id": "11111111-1111-1111-1111-111111111111", "name": "Frontend Team"},
		{"id": "22222222-2222-2222-2222-222222222222", "name": "Backend Team"},
		{"id": "33333333-3333-3333-3333-333333333333", "name": "Archived Team", "archived_at": "2026-01-01T00:00:00Z"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/squads" {
			json.NewEncoder(w).Encode(squadsResp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("passes through a UUID without lookup", func(t *testing.T) {
		id := "44444444-4444-4444-4444-444444444444"
		got, err := resolveSquad(ctx, client, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != id {
			t.Errorf("got %q, want %q", got, id)
		}
	})

	t.Run("exact name match", func(t *testing.T) {
		got, err := resolveSquad(ctx, client, "Frontend Team")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("got %q, want Frontend Team's UUID", got)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		got, err := resolveSquad(ctx, client, "backend")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "22222222-2222-2222-2222-222222222222" {
			t.Errorf("got %q, want Backend Team's UUID", got)
		}
	})

	t.Run("archived squad is ignored", func(t *testing.T) {
		_, err := resolveSquad(ctx, client, "Archived")
		if err == nil {
			t.Fatal("expected error for archived squad")
		}
		if !strings.Contains(err.Error(), "no squad found") {
			t.Errorf("expected no squad found error, got: %v", err)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := resolveSquad(ctx, client, "nobody")
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("ambiguous match", func(t *testing.T) {
		_, err := resolveSquad(ctx, client, "team")
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("expected ambiguous error, got: %v", err)
		}
	})

	t.Run("missing workspace ID for name lookup", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, err := resolveSquad(ctx, noWSClient, "Frontend Team")
		if err == nil {
			t.Fatal("expected error when workspace ID is missing")
		}
	})

	t.Run("UUID works without workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		id := "55555555-5555-5555-5555-555555555555"
		got, err := resolveSquad(ctx, noWSClient, id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != id {
			t.Errorf("got %q, want %q", got, id)
		}
	})
}

func TestRunAutopilotCreateRequiresExactlyOneAssignee(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	t.Run("requires agent or squad", func(t *testing.T) {
		cmd := newAutopilotCreateTestCmd()
		_ = cmd.Flags().Set("title", "Daily planner")
		_ = cmd.Flags().Set("mode", "create_issue")

		err := runAutopilotCreate(cmd, nil)
		if err == nil || !strings.Contains(err.Error(), "one of --agent or --squad is required") {
			t.Fatalf("error = %v, want required assignee error", err)
		}
	})

	t.Run("rejects both agent and squad", func(t *testing.T) {
		cmd := newAutopilotCreateTestCmd()
		_ = cmd.Flags().Set("title", "Daily planner")
		_ = cmd.Flags().Set("agent", "agent")
		_ = cmd.Flags().Set("squad", "squad")
		_ = cmd.Flags().Set("mode", "create_issue")

		err := runAutopilotCreate(cmd, nil)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("error = %v, want mutually exclusive error", err)
		}
	})
}

func TestRunAutopilotCreateSendsSquadAssignee(t *testing.T) {
	const squadID = "11111111-1111-1111-1111-111111111111"

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":            "autopilot-1",
			"title":         "Daily planner",
			"assignee_type": body["assignee_type"],
			"assignee_id":   body["assignee_id"],
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotCreateTestCmd()
	_ = cmd.Flags().Set("title", "Daily planner")
	_ = cmd.Flags().Set("squad", squadID)
	_ = cmd.Flags().Set("mode", "create_issue")

	if err := runAutopilotCreate(cmd, nil); err != nil {
		t.Fatalf("runAutopilotCreate: %v", err)
	}
	if got := body["assignee_type"]; got != "squad" {
		t.Fatalf("assignee_type = %#v, want squad", got)
	}
	if got := body["assignee_id"]; got != squadID {
		t.Fatalf("assignee_id = %#v, want %q", got, squadID)
	}
}

func TestRunAutopilotUpdateSquadSwitchesAssigneeType(t *testing.T) {
	const (
		autopilotID = "33333333-3333-3333-3333-333333333333"
		squadID     = "11111111-1111-1111-1111-111111111111"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{{"id": squadID, "name": "Frontend Team"}})
		case "/api/autopilots/" + autopilotID:
			if r.Method != http.MethodPatch {
				t.Errorf("method = %s, want PATCH", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
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
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("squad", "Frontend Team")
	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotUpdate: %v", err)
	}
	if got := body["assignee_type"]; got != "squad" {
		t.Fatalf("assignee_type = %#v, want squad", got)
	}
	if got := body["assignee_id"]; got != squadID {
		t.Fatalf("assignee_id = %#v, want %q", got, squadID)
	}
}

func TestRunAutopilotUpdateRejectsAgentAndSquad(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("agent", "agent")
	_ = cmd.Flags().Set("squad", "squad")

	err := runAutopilotUpdate(cmd, []string{"33333333-3333-3333-3333-333333333333"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want mutually exclusive error", err)
	}
}
