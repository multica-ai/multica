package handler

// CEREBRO-PATCH(squad-create-avatar-test): regression coverage for create-time avatar persistence.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateSquadPersistsAvatarURL(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "Create Squad Avatar Leader", nil)
	avatarURL := "https://cdn.example.test/squads/avatar.png"

	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/squads", map[string]any{
		"name":        "Avatar Squad",
		"description": "Tests create-time avatar persistence.",
		"leader_id":   leaderID,
		"avatar_url":  avatarURL,
	}), "workspaceId", testWorkspaceID)
	w := httptest.NewRecorder()

	testHandler.CreateSquad(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("CreateSquad: decode response: %v", err)
	}
	if resp.AvatarURL == nil || *resp.AvatarURL != avatarURL {
		t.Fatalf("CreateSquad: avatar_url = %v, want %q", resp.AvatarURL, avatarURL)
	}

	squad, err := testHandler.Queries.GetSquadInWorkspace(req.Context(), db.GetSquadInWorkspaceParams{
		ID:          parseUUID(resp.ID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("CreateSquad: reload squad: %v", err)
	}
	if !squad.AvatarUrl.Valid || squad.AvatarUrl.String != avatarURL {
		t.Fatalf("CreateSquad: persisted avatar_url = %q valid=%v, want %q", squad.AvatarUrl.String, squad.AvatarUrl.Valid, avatarURL)
	}
}
