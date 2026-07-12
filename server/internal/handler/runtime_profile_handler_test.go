package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// insertRuntimeProfileFixture creates a runtime_profile in testWorkspaceID and
// returns its id, registering cleanup.
func insertRuntimeProfileFixture(t *testing.T, ctx context.Context, displayName, protocolFamily, commandName string) string {
	t.Helper()
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_profile (workspace_id, display_name, protocol_family, command_name, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, displayName, protocolFamily, commandName, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert runtime_profile fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})
	return profileID
}

// insertProfileRuntimeFixture creates an agent_runtime instance bound to the
// given profile (so profile_id is set), returning its id.
func insertProfileRuntimeFixture(t *testing.T, ctx context.Context, profileID, name, provider string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, profile_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'local', $3, 'online', $4, '{}'::jsonb, $5, $6, now())
		RETURNING id
	`, testWorkspaceID, name, provider, name+" device", testUserID, profileID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert profile runtime fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// TestDeleteRuntimeProfile_ArchivedAgentCascade is the regression guard for the
// FK-RESTRICT 500: a profile whose only remaining agent is ARCHIVED must still
// delete cleanly. agent.runtime_id is ON DELETE RESTRICT, so without the
// per-runtime archived-agent teardown the DELETE on agent_runtime would raise a
// raw FK error and the handler would 500. The cascade must hard-delete the
// archived agent, the runtime row, and the profile.
func TestDeleteRuntimeProfile_ArchivedAgentCascade(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	profileID := insertRuntimeProfileFixture(t, ctx, "Cascade Profile Archived", "codex", "company-codex-arch")
	runtimeID := insertProfileRuntimeFixture(t, ctx, profileID, "Cascade Profile Runtime", "codex")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade Profile Archived Agent")

	// Archive the agent — the active-agent guard passes, but the FK still pins
	// the runtime row until the archived cascade clears it.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	// Give the archived agent a channel installation (+ a dependent binding).
	// channel_* has no FK to agent (MUL-3515 §4), so this DeleteRuntimeProfile
	// teardown entry point must sweep it explicitly — otherwise the bot's app_id
	// slot stays occupied after the agent is hard-deleted, and the bot can never
	// rebind anywhere (#4810). This pins that sweep on the profile-delete path.
	const rpApp = "cli_rp_cascade"
	const rpChat = "cc000000-0000-4000-8000-0000000000f7"
	_, _ = testPool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = $1`, rpApp)
	_, _ = testPool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rpChat)
	var rpInstallID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'active')
RETURNING id`, testWorkspaceID, agentID, rpApp, testUserID).Scan(&rpInstallID); err != nil {
		t.Fatalf("seed channel installation: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_rp', 'p2p')`, rpChat, rpInstallID); err != nil {
		t.Fatalf("seed chat-session binding: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE config->>'app_id' = $1`, rpApp)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rpChat)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	testHandler.DeleteRuntimeProfile(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var profileRows, rtRows, agentRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM runtime_profile WHERE id = $1`, profileID).Scan(&profileRows); err != nil {
		t.Fatalf("count profile rows: %v", err)
	}
	if profileRows != 0 {
		t.Fatalf("expected profile deleted, found %d", profileRows)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("expected runtime row deleted by cascade, found %d", rtRows)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent WHERE id = $1`, agentID).Scan(&agentRows); err != nil {
		t.Fatalf("count agent rows: %v", err)
	}
	if agentRows != 0 {
		t.Fatalf("expected archived agent hard-deleted by cascade, found %d", agentRows)
	}

	var instRows, bindingRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE id = $1`, rpInstallID).Scan(&instRows); err != nil {
		t.Fatalf("count channel installation: %v", err)
	}
	if instRows != 0 {
		t.Fatalf("archived agent's channel installation not swept on runtime-profile delete: %d rows (its bot's app_id slot stays occupied, #4810)", instRows)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, rpInstallID).Scan(&bindingRows); err != nil {
		t.Fatalf("count channel chat-session binding: %v", err)
	}
	if bindingRows != 0 {
		t.Fatalf("channel chat-session binding not swept: %d dangling rows", bindingRows)
	}
}

// TestDeleteRuntimeProfile_ActiveAgentBlocks confirms the guard still refuses
// (409) while an ACTIVE agent is bound to one of the profile's runtimes, and
// leaves the profile + runtime intact.
func TestDeleteRuntimeProfile_ActiveAgentBlocks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	profileID := insertRuntimeProfileFixture(t, ctx, "Cascade Profile Active", "codex", "company-codex-active")
	runtimeID := insertProfileRuntimeFixture(t, ctx, profileID, "Cascade Profile Active Runtime", "codex")
	_ = createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade Profile Active Agent")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	testHandler.DeleteRuntimeProfile(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var profileRows, rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM runtime_profile WHERE id = $1`, profileID).Scan(&profileRows); err != nil {
		t.Fatalf("count profile rows: %v", err)
	}
	if profileRows != 1 {
		t.Fatalf("expected profile to survive 409, found %d", profileRows)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 1 {
		t.Fatalf("expected runtime to survive 409, found %d", rtRows)
	}
}

func TestDeleteRuntimeProfile_MissingProfileWithOrphanRuntimesCleansUp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	profileID := insertRuntimeProfileFixture(t, ctx, "Orphaned Profile Cleanup", "codex", "orphaned-profile-codex")
	runtimeID := insertProfileRuntimeFixture(t, ctx, profileID, "Orphaned Profile Runtime", "codex")
	if _, err := testPool.Exec(ctx, `DELETE FROM runtime_profile WHERE id = $1`, profileID); err != nil {
		t.Fatalf("delete profile row: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	testHandler.DeleteRuntimeProfile(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("expected orphaned runtime row deleted, found %d", rtRows)
	}
}

func TestDeleteRuntimeProfile_MissingProfileNoOrphansStillReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	missingProfileID := "00000000-0000-0000-0000-000000415800"
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+missingProfileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", missingProfileID)
	testHandler.DeleteRuntimeProfile(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateRuntimeProfile_ForcesWorkspaceVisibility is the regression guard
// for the visibility leak: visibility=private is not user-settable in v1
// because the read paths don't enforce it. A client that POSTs
// visibility:"private" must get a profile stored as 'workspace' — never
// private — so a "private" profile can't leak to other members or be
// registered by other daemons. Belt-and-suspenders: also assert the row in
// the DB is 'workspace'.
func TestCreateRuntimeProfile_ForcesWorkspaceVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles", map[string]any{
		"display_name":    "Visibility Forced Profile",
		"protocol_family": "codex",
		"command_name":    "vis-forced-codex",
		"visibility":      "private", // must be ignored
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.CreateRuntimeProfile(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp RuntimeProfileResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, resp.ID)
	})

	if resp.Visibility != "workspace" {
		t.Fatalf("response visibility = %q, want workspace (private must be forced to workspace)", resp.Visibility)
	}
	var dbVis string
	if err := testPool.QueryRow(ctx, `SELECT visibility FROM runtime_profile WHERE id = $1`, resp.ID).Scan(&dbVis); err != nil {
		t.Fatalf("read stored visibility: %v", err)
	}
	if dbVis != "workspace" {
		t.Fatalf("stored visibility = %q, want workspace", dbVis)
	}
}

func TestCreateRuntimeProfile_ValidatesCommandAndFixedArgs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name        string
		commandName string
		fixedArgs   []string
		wantStatus  int
	}{
		{
			name:        "split command and args accepted",
			commandName: "agent",
			fixedArgs:   []string{"--model", "composer-2.5"},
			wantStatus:  http.StatusCreated,
		},
		{
			name:        "command line rejected",
			commandName: "agent --model composer-2.5",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "nul arg rejected",
			commandName: "agent",
			fixedArgs:   []string{"bad\x00arg"},
			wantStatus:  http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles", map[string]any{
				"display_name":    "Validation " + tc.name,
				"protocol_family": "codex",
				"command_name":    tc.commandName,
				"fixed_args":      tc.fixedArgs,
			})
			req = withURLParam(req, "id", testWorkspaceID)
			testHandler.CreateRuntimeProfile(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if w.Code == http.StatusCreated {
				var resp RuntimeProfileResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				t.Cleanup(func() {
					testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, resp.ID)
				})
				if got := strings.Join(resp.FixedArgs, " "); got != strings.Join(tc.fixedArgs, " ") {
					t.Fatalf("fixed_args = %v, want %v", resp.FixedArgs, tc.fixedArgs)
				}
			}
		})
	}
}
