// CEREBRO-PATCH(autopilot-scope-cli-test): tests for autopilot scope CLI helpers (JEH-725).
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

const (
	cerebroTestOwnerID = "11111111-1111-1111-1111-111111111111"
	cerebroTestGroupID = "22222222-2222-2222-2222-222222222222"
)

func cerebroTestAutopilotCommandsExposeScopeFlags(t *testing.T) {
	t.Helper()
	tests := []struct {
		name     string
		cmd      *cobra.Command
		defaults map[string]string
	}{
		{
			name: "create",
			cmd:  autopilotCreateCmd,
			defaults: map[string]string{
				"scope": cerebroAutopilotScopeWorkspace,
				"group": "",
				"owner": "",
			},
		},
		{
			name: "update",
			cmd:  autopilotUpdateCmd,
			defaults: map[string]string{
				"scope": "",
				"group": "",
				"owner": "",
			},
		},
		{
			name: "list",
			cmd:  autopilotListCmd,
			defaults: map[string]string{
				"scope": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for flagName, wantDefault := range tt.defaults {
				flag := tt.cmd.Flags().Lookup(flagName)
				if flag == nil {
					t.Fatalf("expected --%s flag on autopilot %s", flagName, tt.name)
				}
				if flag.DefValue != wantDefault {
					t.Errorf("--%s default = %q, want %q", flagName, flag.DefValue, wantDefault)
				}
			}
		})
	}
}

func TestCerebroAutopilotListScopePath(t *testing.T) {
	cmd := &cobra.Command{}
	cerebroAddAutopilotListScopeFlag(cmd)

	got, err := cerebroAutopilotListScopePath(cmd, "/api/autopilots?status=active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/api/autopilots?status=active" {
		t.Fatalf("path without scope = %q", got)
	}

	if err := cmd.Flags().Set("scope", "mine"); err != nil {
		t.Fatal(err)
	}
	got, err = cerebroAutopilotListScopePath(cmd, "/api/autopilots?status=active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/api/autopilots?status=active&scope=mine" {
		t.Errorf("path with scope = %q", got)
	}

	if err := cmd.Flags().Set("scope", "invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := cerebroAutopilotListScopePath(cmd, "/api/autopilots"); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestCerebroAutopilotCreateScopeBody(t *testing.T) {
	client, closeServer := cerebroAutopilotScopeTestClient(t)
	defer closeServer()

	tests := []struct {
		name      string
		setFlags  map[string]string
		wantBody  map[string]any
		wantError string
	}{
		{
			name:     "default workspace leaves body unchanged",
			setFlags: map[string]string{},
			wantBody: map[string]any{},
		},
		{
			name:     "personal defaults owner server-side",
			setFlags: map[string]string{"scope": "personal"},
			wantBody: map[string]any{"scope": "personal"},
		},
		{
			name:     "personal owner name resolves to user id",
			setFlags: map[string]string{"scope": "personal", "owner": "alice"},
			wantBody: map[string]any{"scope": "personal", "owner_user_id": cerebroTestOwnerID},
		},
		{
			name:     "group name resolves to group id",
			setFlags: map[string]string{"scope": "group", "group": "engineering"},
			wantBody: map[string]any{"scope": "group", "group_id": cerebroTestGroupID},
		},
		{
			name:      "group scope requires group",
			setFlags:  map[string]string{"scope": "group"},
			wantError: "--group is required",
		},
		{
			name:      "owner requires personal scope",
			setFlags:  map[string]string{"owner": "alice"},
			wantError: "--owner requires --scope personal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cerebroAddAutopilotCreateScopeFlags(cmd)
			for name, value := range tt.setFlags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			body := map[string]any{}
			err := cerebroAutopilotCreateScopeBody(context.Background(), client, cmd, body)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertCerebroAutopilotScopeBody(t, body, tt.wantBody)
		})
	}
}

func TestCerebroAutopilotUpdateScopeBody(t *testing.T) {
	client, closeServer := cerebroAutopilotScopeTestClient(t)
	defer closeServer()

	tests := []struct {
		name      string
		setFlags  map[string]string
		wantBody  map[string]any
		wantError string
	}{
		{
			name:     "scope group includes group id",
			setFlags: map[string]string{"scope": "group", "group": cerebroTestGroupID},
			wantBody: map[string]any{"scope": "group", "group_id": cerebroTestGroupID},
		},
		{
			name:     "group alone can update existing group scope",
			setFlags: map[string]string{"group": "engineering"},
			wantBody: map[string]any{"group_id": cerebroTestGroupID},
		},
		{
			name:     "owner alone can update existing personal scope",
			setFlags: map[string]string{"owner": "alice"},
			wantBody: map[string]any{"owner_user_id": cerebroTestOwnerID},
		},
		{
			name:      "scope group requires group",
			setFlags:  map[string]string{"scope": "group"},
			wantError: "--group is required",
		},
		{
			name:      "personal scope rejects group",
			setFlags:  map[string]string{"scope": "personal", "group": "engineering"},
			wantError: "--group cannot be used with --scope personal",
		},
		{
			name:      "owner and group need explicit scope",
			setFlags:  map[string]string{"owner": "alice", "group": "engineering"},
			wantError: "--owner and --group cannot be updated together",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cerebroAddAutopilotUpdateScopeFlags(cmd)
			for name, value := range tt.setFlags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			body := map[string]any{}
			err := cerebroAutopilotUpdateScopeBody(context.Background(), client, cmd, body)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertCerebroAutopilotScopeBody(t, body, tt.wantBody)
		})
	}
}

func cerebroAutopilotScopeTestClient(t *testing.T) (*cli.APIClient, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{
				{"user_id": cerebroTestOwnerID, "name": "Alice Smith"},
			})
		case "/api/cerebro/groups":
			json.NewEncoder(w).Encode(map[string]any{
				"groups": []map[string]any{
					{"id": cerebroTestGroupID, "name": "Engineering"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return cli.NewAPIClient(srv.URL, "ws-1", "test-token"), srv.Close
}

func assertCerebroAutopilotScopeBody(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("body = %+v, want %+v", got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("body[%q] = %v, want %v; full body %+v", key, got[key], wantValue, got)
		}
	}
}
