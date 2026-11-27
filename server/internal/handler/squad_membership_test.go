package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAddSquadMember_WaitsForMemberRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	userID := dbfx.User(t, "Squad member", "squad-revocation-race@multica.ai")
	memberID := dbfx.Member(t, testWorkspaceID, userID, "member")
	leaderID := dbfx.Agent(t, "Squad revocation leader", "")
	squadID := dbfx.Squad(t, "Squad revocation", leaderID)
	dbfx.Cleanup(t, `DELETE FROM squad_member WHERE squad_id = $1`, squadID)

	revokeTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin member revocation: %v", err)
	}
	defer revokeTx.Rollback(context.Background())
	holderPID := holderBackendPID(t, ctx, revokeTx)
	qtx := testHandler.Queries.WithTx(revokeTx)
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		t.Fatalf("lock member revocation: %v", err)
	}
	if err := qtx.DeleteMember(ctx, parseUUID(memberID)); err != nil {
		t.Fatalf("delete workspace membership: %v", err)
	}

	req := newRequest(http.MethodPost, "/api/squads/"+squadID+"/members", map[string]string{
		"member_type": "member",
		"member_id":   userID,
	}).WithContext(ctx)
	req = withURLParams(req, "workspaceId", testWorkspaceID, "id", squadID)
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.AddSquadMember(w, req)
		done <- w.Code
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		t.Fatal("adding the squad member did not wait for workspace revocation")
	}
	if err := revokeTx.Commit(ctx); err != nil {
		t.Fatalf("commit member revocation: %v", err)
	}
	select {
	case status := <-done:
		if status != http.StatusBadRequest {
			t.Fatalf("add squad member status = %d, want 400 after revocation", status)
		}
	case <-ctx.Done():
		t.Fatal("adding the squad member did not finish after revocation")
	}

	inSquad, err := testHandler.Queries.IsSquadMember(ctx, db.IsSquadMemberParams{
		SquadID:    parseUUID(squadID),
		MemberType: "member",
		MemberID:   parseUUID(userID),
	})
	if err != nil {
		t.Fatalf("read squad membership: %v", err)
	}
	if inSquad {
		t.Fatal("removed workspace member was added to the squad")
	}
}
