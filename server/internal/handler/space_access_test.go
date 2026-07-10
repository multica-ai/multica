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
