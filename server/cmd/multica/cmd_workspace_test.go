package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func workspaceTelegramTestCmd() *cobra.Command {
	cmd := testCmd()
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("bot-token", "", "")
	cmd.Flags().String("user-id", "", "")
	cmd.Flags().Bool("clear", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunWorkspaceTelegramMergesSettings(t *testing.T) {
	var patchBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":   "ws-1",
				"name": "Workspace",
				"settings": map[string]any{
					"existing": "kept",
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "ws-1",
				"name":     "Workspace",
				"settings": patchBody["settings"],
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")

	cmd := workspaceTelegramTestCmd()
	if err := cmd.Flags().Set("bot-token", "123:abc"); err != nil {
		t.Fatalf("set bot-token flag: %v", err)
	}
	if err := cmd.Flags().Set("user-id", "987654"); err != nil {
		t.Fatalf("set user-id flag: %v", err)
	}

	if err := runWorkspaceTelegram(cmd, nil); err != nil {
		t.Fatalf("runWorkspaceTelegram() error = %v", err)
	}

	settings, ok := patchBody["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings object, got %T", patchBody["settings"])
	}
	if got := settings["existing"]; got != "kept" {
		t.Fatalf("existing setting = %v, want %q", got, "kept")
	}
	telegram, ok := settings["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("expected telegram object, got %T", settings["telegram"])
	}
	if got := telegram["bot_token"]; got != "123:abc" {
		t.Fatalf("bot_token = %v, want %q", got, "123:abc")
	}
	if got := telegram["user_id"]; got != "987654" {
		t.Fatalf("user_id = %v, want %q", got, "987654")
	}
}

func TestRunWorkspaceTelegramClearRemovesSettings(t *testing.T) {
	var patchBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":   "ws-1",
				"name": "Workspace",
				"settings": map[string]any{
					"existing": "kept",
					"telegram": map[string]any{
						"bot_token": "old-token",
						"user_id":   "old-user",
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "ws-1",
				"name":     "Workspace",
				"settings": patchBody["settings"],
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")

	cmd := workspaceTelegramTestCmd()
	if err := cmd.Flags().Set("clear", "true"); err != nil {
		t.Fatalf("set clear flag: %v", err)
	}

	if err := runWorkspaceTelegram(cmd, nil); err != nil {
		t.Fatalf("runWorkspaceTelegram(clear) error = %v", err)
	}

	settings, ok := patchBody["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings object, got %T", patchBody["settings"])
	}
	if got := settings["existing"]; got != "kept" {
		t.Fatalf("existing setting = %v, want %q", got, "kept")
	}
	if _, exists := settings["telegram"]; exists {
		t.Fatalf("expected telegram settings to be removed, got %+v", settings["telegram"])
	}
}

func TestWorkspaceTelegramCommandExists(t *testing.T) {
	if _, _, err := workspaceCmd.Find([]string{"telegram"}); err != nil {
		t.Fatalf("expected workspace telegram command to exist: %v", err)
	}
}
