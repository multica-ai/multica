package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// FIR-4930 — `--on-behalf-of` resolves a workspace member the same way
// `--assignee` does, and refuses anything that is not a human.
func newOnBehalfOfTestClient(t *testing.T) *cli.APIClient {
	t.Helper()

	members := []map[string]any{
		{"user_id": "11111111-1111-1111-1111-111111111111", "name": "Nikolaj Owner"},
		{"user_id": "22222222-2222-2222-2222-222222222222", "name": "Jesper Hvejsel"},
	}
	agents := []map[string]any{
		{"id": "33333333-3333-3333-3333-333333333333", "name": "Michael"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(members)
		case "/api/agents":
			json.NewEncoder(w).Encode(agents)
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return cli.NewAPIClient(srv.URL, "ws-1", "test-token")
}

func newOnBehalfOfTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	registerOnBehalfOfFlag(cmd, "test")
	return cmd
}

func TestApplyOnBehalfOfFlag(t *testing.T) {
	ctx := context.Background()
	client := newOnBehalfOfTestClient(t)

	t.Run("flag omitted leaves the body untouched", func(t *testing.T) {
		body := map[string]any{}
		if err := applyOnBehalfOfFlag(ctx, client, newOnBehalfOfTestCmd(), body); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, present := body["on_behalf_of_user_id"]; present {
			t.Error("an omitted flag must not add on_behalf_of_user_id to the request")
		}
	})

	t.Run("resolves a member name to its user id", func(t *testing.T) {
		cmd := newOnBehalfOfTestCmd()
		cmd.Flags().Set(onBehalfOfFlag, "Nikolaj")
		body := map[string]any{}
		if err := applyOnBehalfOfFlag(ctx, client, cmd, body); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := body["on_behalf_of_user_id"]; got != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("got %v, want Nikolaj's user id", got)
		}
	})

	t.Run("accepts a canonical user id", func(t *testing.T) {
		cmd := newOnBehalfOfTestCmd()
		cmd.Flags().Set(onBehalfOfFlag, "22222222-2222-2222-2222-222222222222")
		body := map[string]any{}
		if err := applyOnBehalfOfFlag(ctx, client, cmd, body); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := body["on_behalf_of_user_id"]; got != "22222222-2222-2222-2222-222222222222" {
			t.Errorf("got %v, want the id passed in", got)
		}
	})

	// An agent is not a human. Accepting one here would put an agent id in a
	// column that references the user table and break the inbox routing this
	// flag exists to fix.
	t.Run("rejects an agent", func(t *testing.T) {
		cmd := newOnBehalfOfTestCmd()
		cmd.Flags().Set(onBehalfOfFlag, "Michael")
		if err := applyOnBehalfOfFlag(ctx, client, cmd, map[string]any{}); err == nil {
			t.Fatal("expected an agent name to be rejected")
		}
	})

	t.Run("rejects an unknown name", func(t *testing.T) {
		cmd := newOnBehalfOfTestCmd()
		cmd.Flags().Set(onBehalfOfFlag, "Nobody At All")
		if err := applyOnBehalfOfFlag(ctx, client, cmd, map[string]any{}); err == nil {
			t.Fatal("expected an unknown name to be rejected")
		}
	})

	// `--on-behalf-of ""` is a deliberate "clear the stamp" on update, and must
	// reach the server as an explicit empty value rather than being dropped.
	t.Run("explicit empty value clears the stamp", func(t *testing.T) {
		cmd := newOnBehalfOfTestCmd()
		cmd.Flags().Set(onBehalfOfFlag, "")
		body := map[string]any{}
		if err := applyOnBehalfOfFlag(ctx, client, cmd, body); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, present := body["on_behalf_of_user_id"]
		if !present || got != "" {
			t.Errorf("got (%v, present=%v), want an explicit empty string", got, present)
		}
	})
}
