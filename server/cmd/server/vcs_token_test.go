package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestUpdateVCSConnectionTokenRoutePermissions(t *testing.T) {
	t.Setenv("MULTICA_VCS_INTEGRATION_ENABLED", "true")
	t.Setenv("MULTICA_VCS_SECRET_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32)))
	server := httptest.NewServer(NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil))
	defer server.Close()
	f := testutil.New(testPool, testWorkspaceID, testUserID)
	for _, tt := range []struct {
		role   string
		status int
	}{
		{"owner", http.StatusNotFound},
		{"admin", http.StatusNotFound},
		{"member", http.StatusForbidden},
		{"outsider", http.StatusNotFound},
		{"anonymous", http.StatusUnauthorized},
	} {
		t.Run(tt.role, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost,
				server.URL+"/api/workspaces/"+testWorkspaceID+"/vcs/connections/"+uuid.NewString()+"/token", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.role != "anonymous" {
				email := "vcs-token-" + uuid.NewString() + "@example.test"
				id := f.User(t, "VCS token test", email)
				if tt.role != "outsider" {
					f.Member(t, testWorkspaceID, id, tt.role)
				}
				token, err := generateTestJWT(id, email, "VCS token test")
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("role %s: status = %d, want %d", tt.role, resp.StatusCode, tt.status)
			}
			if tt.role == "owner" || tt.role == "admin" {
				var body struct {
					Error string `json:"error"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Error != "vcs connection not found" {
					t.Fatalf("admin request did not reach token handler: %s", body.Error)
				}
			}
		})
	}
}
