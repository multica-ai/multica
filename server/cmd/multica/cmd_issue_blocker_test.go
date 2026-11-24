package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestIssueBlockerFlagsResolveBeforeMutation(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config" {
			_ = json.NewEncoder(w).Encode(map[string]bool{"issue_blockers_supported": true})
			return
		}
		if r.Method != "GET" || r.URL.Path != "/api/issues/MUL-123" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": "MUL-123", "title": "Contract"})
	}))
	defer server.Close()
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("blocked-by", nil, "")
	cmd.Flags().StringArray("remove-blocked-by", nil, "")
	_ = cmd.Flags().Set("blocked-by", "MUL-123")
	_ = cmd.Flags().Set("blocked-by", id)
	_ = cmd.Flags().Set("remove-blocked-by", id)
	body := map[string]any{}
	if err := applyIssueBlockerFlags(context.Background(), cli.NewAPIClient(server.URL, "workspace", "token"), cmd, body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["blocked_by"], []string{id, id}) || !reflect.DeepEqual(body["remove_blocked_by"], []string{id}) {
		t.Fatalf("resolved payload: %#v", body)
	}
}

func TestIssueBlockerFlagsRefuseOlderServers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			t.Errorf("resolved a reference before capability validation: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("blocked-by", nil, "")
	_ = cmd.Flags().Set("blocked-by", "MUL-123")
	body := map[string]any{}
	if err := applyIssueBlockerFlags(context.Background(), cli.NewAPIClient(server.URL, "workspace", "token"), cmd, body); err == nil {
		t.Fatal("older server silently accepted blocker flags")
	}
	if len(body) != 0 {
		t.Fatalf("unsupported mutation payload was built: %#v", body)
	}
}
