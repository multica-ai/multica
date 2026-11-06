package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunAutopilotTriggerListShowsRecordedOwners(t *testing.T) {
	const (
		autopilotID = "11111111-1111-1111-1111-111111111111"
		triggerID   = "22222222-2222-2222-2222-222222222222"
		ownerID     = "33333333-3333-3333-3333-333333333333"
	)
	cases := []struct {
		name  string
		owner map[string]any
		want  string
	}{
		{"member", map[string]any{"created_by_type": "member", "created_by_id": ownerID}, "member:"},
		{"agent", map[string]any{"created_by_type": "agent", "created_by_id": ownerID}, "agent:"},
		{"future type", map[string]any{"created_by_type": "service", "created_by_id": ownerID}, "service:"},
		{"legacy null", map[string]any{"created_by_type": nil, "created_by_id": nil}, "unknown"},
		{"older server", nil, "unknown"},
		{"missing id", map[string]any{"created_by_type": "member", "created_by_id": nil}, "unknown"},
		{"missing type", map[string]any{"created_by_type": nil, "created_by_id": ownerID}, "unknown"},
		{"malformed type", map[string]any{"created_by_type": true, "created_by_id": ownerID}, "unknown"},
		{"malformed id", map[string]any{"created_by_type": "member", "created_by_id": 42}, "unknown"},
		{"blank type", map[string]any{"created_by_type": " ", "created_by_id": ownerID}, "unknown"},
	}
	for _, tc := range cases {
		for _, fullID := range []bool{false, true} {
			name := tc.name + "/short"
			if fullID {
				name = tc.name + "/full"
			}
			t.Run(name, func(t *testing.T) {
				trigger := map[string]any{
					"id": triggerID, "kind": "webhook", "enabled": true,
					"webhook_token": "owner-test-secret", "published_by_type": "member",
					"published_by_id": "44444444-4444-4444-4444-444444444444",
				}
				for key, value := range tc.owner {
					trigger[key] = value
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/api/autopilots/"+autopilotID {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"autopilot": map[string]any{"id": autopilotID}, "triggers": []map[string]any{trigger},
					})
				}))
				defer srv.Close()
				t.Setenv("MULTICA_SERVER_URL", srv.URL)
				t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
				t.Setenv("MULTICA_TOKEN", "mat_test-token")
				cmd := newAutopilotTriggerListTestCmd("table")
				if fullID {
					_ = cmd.Flags().Set("full-id", "true")
				}
				out, err := captureStdout(t, func() error {
					return runAutopilotTriggerList(cmd, []string{autopilotID})
				})
				if err != nil {
					t.Fatal(err)
				}
				want := tc.want
				if want != "unknown" {
					if fullID {
						want += ownerID
					} else {
						want += ownerID[:8]
					}
				}
				if !strings.Contains(out, "OWNER") || !strings.Contains(out, want) {
					t.Errorf("trigger list must show OWNER %q; got:\n%s", want, out)
				}
				if strings.Contains(out, "owner-test-secret") || strings.Contains(out, "44444444") {
					t.Errorf("trigger list leaked secret or substituted publisher: %s", out)
				}
			})
		}
	}
}
