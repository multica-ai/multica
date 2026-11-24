package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
)

// TestGuestsFailClosedWhenCloudPlansAreUnavailable assembles the real router
// with a Cloud URL the plan client rejects (MUL-9420). The server is still a
// Cloud deployment, so adding guests is refused rather than treated as
// self-host; with no Cloud URL at all, guests are offered.
func TestGuestsFailClosedWhenCloudPlansAreUnavailable(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	f := dbfx.New(testPool, testWorkspaceID, testUserID)
	f.WorkspaceID = f.Workspace(t, "Guest release", "guest-release-"+uuid.NewString())
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	projectID := f.Project(t, "Guest release project")
	f.Cleanup(t, `DELETE FROM workspace_invitation WHERE workspace_id = $1`, f.WorkspaceID)

	check := func(t *testing.T, cloudURL string, wantOffered bool) {
		t.Helper()
		t.Setenv("MULTICA_CLOUD_URL", cloudURL)
		router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})

		var config struct {
			FeatureFlags map[string]bool `json:"feature_flags"`
		}
		dbfx.Call(t, router.ServeHTTP, dbfx.JSONRequest(http.MethodGet, "/api/config", nil)).Want(http.StatusOK).JSON(&config)
		if got := config.FeatureFlags["workspace_guests"]; got != wantOffered {
			t.Fatalf("config workspace_guests = %v, want %v", got, wantOffered)
		}

		req := dbfx.JSONRequest(http.MethodPost, "/api/workspaces/"+f.WorkspaceID+"/members", map[string]any{
			"email": "guest-" + uuid.NewString()[:8] + "@example.test", "role": "guest", "project_ids": []string{projectID},
		})
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("X-Workspace-ID", f.WorkspaceID)
		resp := dbfx.Call(t, router.ServeHTTP, req)
		if wantOffered {
			resp.Want(http.StatusCreated)
			return
		}
		if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "workspace_guests_disabled") {
			t.Fatalf("guest invite = %d %s, want 403 workspace_guests_disabled", resp.Code, resp.Body.String())
		}
	}

	t.Run("malformed Cloud URL", func(t *testing.T) {
		check(t, "https://cloud.example.test/?region=us", false)
		if n := f.Count(t, `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1 AND role = 'guest'`, f.WorkspaceID); n != 0 {
			t.Fatalf("guest invitations = %d, want none", n)
		}
	})
	t.Run("self-host", func(t *testing.T) {
		check(t, "", true)
	})
}
