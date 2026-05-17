package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidFromBytes(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

func TestCanManageSquad(t *testing.T) {
	uA := uuidFromBytes(0xAA)
	uB := uuidFromBytes(0xBB)

	cases := []struct {
		name   string
		member db.Member
		squad  db.Squad
		want   bool
	}{
		{
			name:   "owner who is not the creator",
			member: db.Member{UserID: uA, Role: "owner"},
			squad:  db.Squad{CreatorID: uB},
			want:   true,
		},
		{
			name:   "admin who is not the creator",
			member: db.Member{UserID: uA, Role: "admin"},
			squad:  db.Squad{CreatorID: uB},
			want:   true,
		},
		{
			name:   "plain member who is the creator",
			member: db.Member{UserID: uA, Role: "member"},
			squad:  db.Squad{CreatorID: uA},
			want:   true,
		},
		{
			name:   "plain member who is not the creator",
			member: db.Member{UserID: uA, Role: "member"},
			squad:  db.Squad{CreatorID: uB},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManageSquad(tc.member, tc.squad); got != tc.want {
				t.Fatalf("canManageSquad: got %v, want %v", got, tc.want)
			}
		})
	}
}

// createPlainMember inserts a new user and adds them to testWorkspaceID
// as a workspace `member`. Returns the new user's UUID. Registers
// cleanup that deletes the user (cascade removes the membership row).
func createPlainMember(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	email := label + "@multica.test"

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, label, email).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add %s as member: %v", label, err)
	}
	return userID
}

func TestCreateSquad_MemberCanCreate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-creator-member")
	leaderID := createHandlerTestAgent(t, "squad-create-leader", []byte(`{}`))

	body := map[string]any{"name": "MemberSquad", "leader_id": leaderID}
	req := newRequestAs(memberID, http.MethodPost, "/api/squads", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad as plain member: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad response: %v", err)
	}
	if resp.CreatorID != memberID {
		t.Fatalf("creator_id: got %s, want %s", resp.CreatorID, memberID)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
}

func TestCreateSquad_OwnerCanStillCreate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "squad-create-leader-owner", []byte(`{}`))

	body := map[string]any{"name": "OwnerSquad", "leader_id": leaderID}
	req := newRequest(http.MethodPost, "/api/squads", body) // testUserID is workspace owner
	req = withURLParam(req, "workspaceId", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad as owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
}

// memberOwnedSquadFixture creates two plain workspace members and a
// squad owned by the first. Returns (squadID, creatorID, plainMemberID,
// leaderAgentID). All four entities are cleaned up on test teardown.
func memberOwnedSquadFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	creatorID := createPlainMember(t, "squad-owner-member")
	otherID := createPlainMember(t, "squad-bystander-member")
	leaderID := createHandlerTestAgent(t, "squad-fixture-leader", []byte(`{}`))

	body := map[string]any{"name": "FixtureSquad", "leader_id": leaderID}
	req := newRequestAs(creatorID, http.MethodPost, "/api/squads", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture CreateSquad: got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("fixture decode squad: %v", err)
	}
	squadID := resp.ID
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID, creatorID, otherID, leaderID
}

func TestUpdateSquad_CreatorMemberCanUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, _, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "Renamed"}
	req := newRequestAs(creatorID, http.MethodPut, "/api/squads/"+squadID, body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as creator-member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquad_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "Hijacked"}
	req := newRequestAs(otherID, http.MethodPut, "/api/squads/"+squadID, body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquad as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquad_AdminCanUpdateOthersSquad(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, _, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "AdminEdit"}
	req := newRequest(http.MethodPut, "/api/squads/"+squadID, body) // testUserID is owner
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteSquad_CreatorMemberCanDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, _, _ := memberOwnedSquadFixture(t)

	req := newRequestAs(creatorID, http.MethodDelete, "/api/squads/"+squadID, nil)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSquad as creator-member: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteSquad_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	req := newRequestAs(otherID, http.MethodDelete, "/api/squads/"+squadID, nil)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteSquad as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddSquadMember_CreatorMemberCanAdd(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(creatorID, http.MethodPost, "/api/squads/"+squadID+"/members", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember as creator-member: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddSquadMember_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"member_type": "member", "member_id": otherID}
	// otherID is both the caller AND the proposed new member, but the perm
	// check runs first so the body never gets exercised.
	req := newRequestAs(otherID, http.MethodPost, "/api/squads/"+squadID+"/members", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("AddSquadMember as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// addSquadMemberDirect inserts a squad membership row directly via SQL to
// avoid going through the AddSquadMember endpoint. Used by tests that
// need a pre-populated member without exercising the endpoint they are
// about to test.
func addSquadMemberDirect(t *testing.T, squadID, memberID, memberType, role string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, $2, $3, $4)
	`, squadID, memberType, memberID, role); err != nil {
		t.Fatalf("addSquadMemberDirect: %v", err)
	}
}

func TestRemoveSquadMember_CreatorMemberCanRemove(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(creatorID, http.MethodDelete, "/api/squads/"+squadID+"/members", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.RemoveSquadMember(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RemoveSquadMember as creator-member: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRemoveSquadMember_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(otherID, http.MethodDelete, "/api/squads/"+squadID+"/members", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.RemoveSquadMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("RemoveSquadMember as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquadMemberRole_CreatorMemberCanUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID, "role": "contributor"}
	req := newRequestAs(creatorID, http.MethodPatch, "/api/squads/"+squadID+"/members/role", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquadMemberRole(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquadMemberRole as creator-member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquadMemberRole_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID, "role": "contributor"}
	req := newRequestAs(otherID, http.MethodPatch, "/api/squads/"+squadID+"/members/role", body)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquadMemberRole(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquadMemberRole as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
