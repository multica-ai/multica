package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func newChannelContextTestCmd(serverURL string) *cobra.Command {
	cmd := &cobra.Command{Use: "context <channel-id>"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().Int("recent", 20, "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().Bool("include-replies", false, "")
	cmd.Flags().String("output", "json", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	return cmd
}

func TestRunChannelContextSendsTriggerMessageAndRepliesFlags(t *testing.T) {
	var gotPath string
	var gotQuery map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = map[string]string{
			"recent":          r.URL.Query().Get("recent"),
			"message":         r.URL.Query().Get("message"),
			"include-replies": r.URL.Query().Get("include-replies"),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"channel": map[string]any{"id": "channel-1"},
			"trigger_message": map[string]any{
				"id":      "message-1",
				"content": "trigger",
			},
			"messages": []any{},
			"replies":  []any{},
		})
	}))
	t.Cleanup(srv.Close)

	cmd := newChannelContextTestCmd(srv.URL)
	_ = cmd.Flags().Set("recent", "7")
	_ = cmd.Flags().Set("message", "message-1")
	_ = cmd.Flags().Set("include-replies", "true")

	if err := runChannelContext(cmd, []string{"channel-1"}); err != nil {
		t.Fatalf("runChannelContext: %v", err)
	}

	if gotPath != "/api/channels/channel-1/context" {
		t.Fatalf("path = %q, want /api/channels/channel-1/context", gotPath)
	}
	if gotQuery["recent"] != "7" {
		t.Fatalf("recent query = %q, want 7", gotQuery["recent"])
	}
	if gotQuery["message"] != "message-1" {
		t.Fatalf("message query = %q, want message-1", gotQuery["message"])
	}
	if gotQuery["include-replies"] != "true" {
		t.Fatalf("include-replies query = %q, want true", gotQuery["include-replies"])
	}
}
