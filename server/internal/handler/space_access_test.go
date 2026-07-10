package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createSpaceForAccessTest(t *testing.T, name, key, visibility string) SpaceResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateSpace(w, newRequest(http.MethodPost, "/api/spaces", map[string]any{
		"name":       name,
		"key":        key,
		"visibility": visibility,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSpace(%s): expected 201, got %d: %s", key, w.Code, w.Body.String())
	}
	var space SpaceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &space); err != nil {
		t.Fatalf("decode space: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, space.ID)
	})
	return space
}

func TestSpaceVisibilityJoinAndGuestCollaboration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	openSpace := createSpaceForAccessTest(t, "Open Access Probe", "OPENAP", "open")
	privateSpace := createSpaceForAccessTest(t, "Private Access Probe", "PRIVAP", "private")
	memberID := createPermissionTestMember(t, "space-phase2-member@multica.test")

	// A regular workspace member can discover Open Spaces but Private Spaces
	// are absent until explicitly invited.
	w := httptest.NewRecorder()
	testHandler.ListSpaces(w, newRequestAs(memberID, http.MethodGet, "/api/spaces", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListSpaces: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Spaces []SpaceResponse `json:"spaces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode spaces: %v", err)
	}
	foundOpen, foundPrivate := false, false
	for _, space := range list.Spaces {
		foundOpen = foundOpen || space.ID == openSpace.ID
		foundPrivate = foundPrivate || space.ID == privateSpace.ID
	}
	if !foundOpen || foundPrivate {
		t.Fatalf("regular member visibility: found open=%v private=%v, want true/false", foundOpen, foundPrivate)
	}

	// Open Spaces are self-joinable; Private Spaces remain invitation-only.
	w = httptest.NewRecorder()
	testHandler.JoinSpace(w, withURLParam(newRequestAs(memberID, http.MethodPost, "/api/spaces/"+openSpace.ID+"/join", nil), "id", openSpace.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("JoinSpace(open): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.JoinSpace(w, withURLParam(newRequestAs(memberID, http.MethodPost, "/api/spaces/"+privateSpace.ID+"/join", nil), "id", privateSpace.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("JoinSpace(private): expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// An invited Guest may view the Private Space but cannot mutate its work.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role, sort_order)
		VALUES ($1, $2, $3, 'guest', 1)
	`, testWorkspaceID, privateSpace.ID, memberID); err != nil {
		t.Fatalf("invite guest: %v", err)
	}
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequestAs(memberID, http.MethodPost, "/api/issues", map[string]any{
		"title":    "guest must not create",
		"space_id": privateSpace.ID,
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateIssue as guest: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDefaultSpaceCannotBecomePrivateOrArchive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var defaultSpaceID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM workspace_space
		WHERE workspace_id = $1 AND is_default = true
	`, testWorkspaceID).Scan(&defaultSpaceID); err != nil {
		t.Fatalf("load default space: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.UpdateSpace(w, withURLParam(newRequest(http.MethodPatch, "/api/spaces/"+defaultSpaceID, map[string]any{
		"visibility": "private",
	}), "id", defaultSpaceID))
	if w.Code != http.StatusConflict {
		t.Fatalf("make Default Space private: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ArchiveSpace(w, withURLParam(newRequest(http.MethodDelete, "/api/spaces/"+defaultSpaceID, nil), "id", defaultSpaceID))
	if w.Code != http.StatusConflict {
		t.Fatalf("archive Default Space: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSpacePreferencesDoNotGrantCollaboration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	openSpace := createSpaceForAccessTest(t, "Preference Probe", "PREFAP", "open")
	privateSpace := createSpaceForAccessTest(t, "Private Preference Probe", "PRFPAP", "private")
	memberID := createPermissionTestMember(t, "space-preference-member@multica.test")

	w := httptest.NewRecorder()
	testHandler.UpdateSpacePreference(w, withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/spaces/"+openSpace.ID+"/preferences", map[string]any{
		"is_pinned":   true,
		"is_followed": true,
	}), "id", openSpace.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSpacePreference(open): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pref SpacePreferenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &pref); err != nil {
		t.Fatalf("decode preference: %v", err)
	}
	if !pref.IsPinned || !pref.IsFollowed {
		t.Fatalf("preference = %+v, want pinned and followed", pref)
	}

	// Pinning/following is visible in the list but must not create a formal
	// membership or collaboration permission.
	w = httptest.NewRecorder()
	testHandler.ListSpaces(w, newRequestAs(memberID, http.MethodGet, "/api/spaces", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListSpaces: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Spaces []SpaceResponse `json:"spaces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode spaces: %v", err)
	}
	found := false
	for _, space := range list.Spaces {
		if space.ID == openSpace.ID {
			found = true
			if space.IsMember || !space.IsPinned || !space.IsFollowed {
				t.Fatalf("space preference view = %+v, want non-member pinned+followed", space)
			}
		}
	}
	if !found {
		t.Fatal("preferred Open Space missing from list")
	}

	canCollaborate, err := testHandler.Queries.CanCollaborateInWorkspaceSpace(
		context.Background(),
		db.CanCollaborateInWorkspaceSpaceParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			ID:          parseUUID(openSpace.ID),
			UserID:      parseUUID(memberID),
		},
	)
	if err != nil {
		t.Fatalf("CanCollaborateInWorkspaceSpace: %v", err)
	}
	if canCollaborate {
		t.Fatal("pin/follow unexpectedly granted collaboration")
	}

	// A Private Space that the member cannot view also cannot be pinned or
	// followed, so preferences never become an access side door.
	w = httptest.NewRecorder()
	testHandler.UpdateSpacePreference(w, withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/spaces/"+privateSpace.ID+"/preferences", map[string]any{
		"is_pinned": true,
	}), "id", privateSpace.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("UpdateSpacePreference(private): expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Archived Spaces can be unpinned/unfollowed, but cannot be newly added to
	// personal navigation or notification audiences.
	w = httptest.NewRecorder()
	testHandler.ArchiveSpace(w, withURLParam(newRequest(http.MethodDelete, "/api/spaces/"+openSpace.ID, nil), "id", openSpace.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveSpace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.UpdateSpacePreference(w, withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/spaces/"+openSpace.ID+"/preferences", map[string]any{
		"is_pinned": false,
	}), "id", openSpace.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("unpin archived Space: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.UpdateSpacePreference(w, withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/spaces/"+openSpace.ID+"/preferences", map[string]any{
		"is_pinned": true,
	}), "id", openSpace.ID))
	if w.Code != http.StatusConflict {
		t.Fatalf("pin archived Space: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSpaceFollowersAreDynamicIssueNotificationRecipients(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	space := createSpaceForAccessTest(t, "Follower Probe", "FOLAP", "open")
	followerID := createPermissionTestMember(t, "space-follower@multica.test")
	issue := createIssueForTest(t, map[string]any{
		"title":    "Space follower notification probe",
		"space_id": space.ID,
	})

	setFollow := func(value bool) {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.UpdateSpacePreference(w, withURLParam(newRequestAs(followerID, http.MethodPatch, "/api/spaces/"+space.ID+"/preferences", map[string]any{
			"is_followed": value,
		}), "id", space.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("set follow=%v: expected 200, got %d: %s", value, w.Code, w.Body.String())
		}
	}
	hasRecipient := func(userID string) bool {
		t.Helper()
		rows, err := testHandler.Queries.ListIssueNotificationRecipients(context.Background(), parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("ListIssueNotificationRecipients: %v", err)
		}
		for _, row := range rows {
			if row.UserType == "member" && uuidToString(row.UserID) == userID {
				return true
			}
		}
		return false
	}

	setFollow(true)
	if !hasRecipient(followerID) {
		t.Fatal("Space follower missing from issue notification recipients")
	}
	setFollow(false)
	if hasRecipient(followerID) {
		t.Fatal("unfollowed member remained in issue notification recipients")
	}
}

func TestSpaceArchiveRestoreLifecyclePreservesIntent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	space := createSpaceForAccessTest(t, "Lifecycle Probe", "LIFEAP", "open")
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load Agent: %v", err)
	}

	squad, err := testHandler.Queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SpaceID:     parseUUID(space.ID),
		Name:        "Lifecycle Squad",
		Description: "",
		LeaderID:    parseUUID(agentID),
		CreatorID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create Squad: %v", err)
	}
	active, err := testHandler.Queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		Title:         "Lifecycle active Autopilot",
		AssigneeType:  "agent",
		AssigneeID:    parseUUID(agentID),
		Status:        "active",
		ExecutionMode: "run_only",
		SpaceID:       parseUUID(space.ID),
		CreatedByType: "member",
		CreatedByID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create active Autopilot: %v", err)
	}
	manualPaused, err := testHandler.Queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		Title:         "Lifecycle manually paused Autopilot",
		AssigneeType:  "agent",
		AssigneeID:    parseUUID(agentID),
		Status:        "paused",
		ExecutionMode: "run_only",
		SpaceID:       parseUUID(space.ID),
		CreatedByType: "member",
		CreatedByID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create paused Autopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = ANY($1::uuid[])`, []string{uuidToString(active.ID), uuidToString(manualPaused.ID)})
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squad.ID)
	})

	archive := httptest.NewRecorder()
	testHandler.ArchiveSpace(archive, withURLParam(newRequest(http.MethodDelete, "/api/spaces/"+space.ID, nil), "id", space.ID))
	if archive.Code != http.StatusOK {
		t.Fatalf("ArchiveSpace: expected 200, got %d: %s", archive.Code, archive.Body.String())
	}

	archivedSquad, err := testHandler.Queries.GetSquad(ctx, squad.ID)
	if err != nil || !archivedSquad.ArchivedAt.Valid || !archivedSquad.ArchivedBySpaceAt.Valid {
		t.Fatalf("Squad archive marker = %+v, err=%v", archivedSquad, err)
	}
	activeAfterArchive, err := testHandler.Queries.GetAutopilot(ctx, active.ID)
	if err != nil || activeAfterArchive.Status != "paused" || !activeAfterArchive.PausedBySpaceAt.Valid || activeAfterArchive.StatusBeforeSpaceArchive.String != "active" {
		t.Fatalf("active Autopilot after archive = %+v, err=%v", activeAfterArchive, err)
	}
	pausedAfterArchive, err := testHandler.Queries.GetAutopilot(ctx, manualPaused.ID)
	if err != nil || pausedAfterArchive.Status != "paused" || pausedAfterArchive.PausedBySpaceAt.Valid {
		t.Fatalf("manually paused Autopilot after archive = %+v, err=%v", pausedAfterArchive, err)
	}

	update := httptest.NewRecorder()
	testHandler.UpdateSpace(update, withURLParam(newRequest(http.MethodPatch, "/api/spaces/"+space.ID, map[string]any{"name": "Must stay read-only"}), "id", space.ID))
	if update.Code != http.StatusConflict {
		t.Fatalf("UpdateSpace(archived): expected 409, got %d: %s", update.Code, update.Body.String())
	}

	restore := httptest.NewRecorder()
	testHandler.RestoreSpace(restore, withURLParam(newRequest(http.MethodPost, "/api/spaces/"+space.ID+"/restore", nil), "id", space.ID))
	if restore.Code != http.StatusOK {
		t.Fatalf("RestoreSpace: expected 200, got %d: %s", restore.Code, restore.Body.String())
	}
	var restoreResponse struct {
		PausedAutopilotCount int64 `json:"paused_autopilot_count"`
	}
	if err := json.Unmarshal(restore.Body.Bytes(), &restoreResponse); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if restoreResponse.PausedAutopilotCount != 1 {
		t.Fatalf("paused_autopilot_count = %d, want 1", restoreResponse.PausedAutopilotCount)
	}
	restoredSquad, err := testHandler.Queries.GetSquad(ctx, squad.ID)
	if err != nil || restoredSquad.ArchivedAt.Valid || restoredSquad.ArchivedBySpaceAt.Valid {
		t.Fatalf("Squad after restore = %+v, err=%v", restoredSquad, err)
	}
	activeBeforeConfirmation, err := testHandler.Queries.GetAutopilot(ctx, active.ID)
	if err != nil || activeBeforeConfirmation.Status != "paused" || !activeBeforeConfirmation.PausedBySpaceAt.Valid {
		t.Fatalf("auto-paused Autopilot resumed before confirmation: %+v, err=%v", activeBeforeConfirmation, err)
	}

	resume := httptest.NewRecorder()
	testHandler.ResumeSpaceAutopilots(resume, withURLParam(newRequest(http.MethodPost, "/api/spaces/"+space.ID+"/resume-autopilots", nil), "id", space.ID))
	if resume.Code != http.StatusOK {
		t.Fatalf("ResumeSpaceAutopilots: expected 200, got %d: %s", resume.Code, resume.Body.String())
	}
	activeAfterResume, err := testHandler.Queries.GetAutopilot(ctx, active.ID)
	if err != nil || activeAfterResume.Status != "active" || activeAfterResume.PausedBySpaceAt.Valid || activeAfterResume.StatusBeforeSpaceArchive.Valid {
		t.Fatalf("active Autopilot after confirmation = %+v, err=%v", activeAfterResume, err)
	}
	manualAfterResume, err := testHandler.Queries.GetAutopilot(ctx, manualPaused.ID)
	if err != nil || manualAfterResume.Status != "paused" {
		t.Fatalf("manual pause was not preserved: %+v, err=%v", manualAfterResume, err)
	}

	var auditCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1
		  AND action = ANY($2::text[])
		  AND details->>'space_id' = $3
	`, testWorkspaceID, []string{"space_archived", "space_restored", "space_autopilots_resumed"}, space.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count lifecycle audit entries: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("lifecycle audit count = %d, want 3", auditCount)
	}
}
