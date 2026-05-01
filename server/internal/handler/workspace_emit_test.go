package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestWorkspaceEmit locks the team-app integration contract for the four
// workspace/member events the standalone team-app subscriber consumes. Each
// sub-test exercises a single handler entry point and asserts that exactly one
// event of the expected protocol.* type is published to the bus with the
// correct workspace_id and the canonical payload key. Story M-PR#2.
func TestWorkspaceEmit(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialised; DATABASE_URL likely unset")
	}

	t.Run("MemberAdded", func(t *testing.T) {
		// CreateMember auto-creates the user from the email if missing, so we
		// only need to clean up the user (the member is removed via cascade).
		email := uniqueTestEmail(t, "member-added")
		t.Cleanup(func() { deleteUserByEmail(t, email) })

		get, restore := captureBus(t, protocol.EventMemberAdded)
		defer restore()

		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", map[string]any{
			"email": email,
			"role":  "member",
		})
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.CreateMember(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateMember: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		evt := requireSingleEvent(t, get(), protocol.EventMemberAdded)
		payload := requireMapPayload(t, evt)
		if _, ok := payload["member"].(MemberWithUserResponse); !ok {
			t.Fatalf("payload[\"member\"]: expected MemberWithUserResponse, got %T", payload["member"])
		}
	})

	t.Run("MemberUpdated", func(t *testing.T) {
		email := uniqueTestEmail(t, "member-updated")
		_, memberID := createMemberFixture(t, email, "member")

		get, restore := captureBus(t, protocol.EventMemberUpdated)
		defer restore()

		w := httptest.NewRecorder()
		req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID, map[string]any{
			"role": "admin",
		})
		req = withURLParam(req, "id", testWorkspaceID)
		req = withURLParam(req, "memberId", memberID)
		testHandler.UpdateMember(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateMember: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		evt := requireSingleEvent(t, get(), protocol.EventMemberUpdated)
		payload := requireMapPayload(t, evt)
		member, ok := payload["member"].(MemberWithUserResponse)
		if !ok {
			t.Fatalf("payload[\"member\"]: expected MemberWithUserResponse, got %T", payload["member"])
		}
		if member.Role != "admin" {
			t.Fatalf("payload member role: expected %q, got %q", "admin", member.Role)
		}
	})

	t.Run("MemberRemoved", func(t *testing.T) {
		email := uniqueTestEmail(t, "member-removed")
		_, memberID := createMemberFixture(t, email, "member")

		get, restore := captureBus(t, protocol.EventMemberRemoved)
		defer restore()

		w := httptest.NewRecorder()
		req := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID, nil)
		req = withURLParam(req, "id", testWorkspaceID)
		req = withURLParam(req, "memberId", memberID)
		testHandler.DeleteMember(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
		}

		evt := requireSingleEvent(t, get(), protocol.EventMemberRemoved)
		payload := requireMapPayload(t, evt)
		if got, ok := payload["member_id"].(string); !ok || got != memberID {
			t.Fatalf("payload[\"member_id\"]: expected %q, got %v", memberID, payload["member_id"])
		}
		if got, ok := payload["workspace_id"].(string); !ok || got != testWorkspaceID {
			t.Fatalf("payload[\"workspace_id\"]: expected %q, got %v", testWorkspaceID, payload["workspace_id"])
		}
	})

	t.Run("WorkspaceUpdated", func(t *testing.T) {
		get, restore := captureBus(t, protocol.EventWorkspaceUpdated)
		defer restore()

		w := httptest.NewRecorder()
		req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{
			"description": "Workspace emit test description",
		})
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.UpdateWorkspace(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		evt := requireSingleEvent(t, get(), protocol.EventWorkspaceUpdated)
		payload := requireMapPayload(t, evt)
		ws, ok := payload["workspace"].(WorkspaceResponse)
		if !ok {
			t.Fatalf("payload[\"workspace\"]: expected WorkspaceResponse, got %T", payload["workspace"])
		}
		if ws.ID != testWorkspaceID {
			t.Fatalf("payload workspace.ID: expected %q, got %q", testWorkspaceID, ws.ID)
		}
	})
}

// captureBus swaps testHandler.Bus with a fresh bus that records every event of
// the given type. The caller drives reads via the returned getter and MUST
// defer restore() to put the original bus back. Each sub-test should call this
// once, keeping its captured state self-contained.
func captureBus(t *testing.T, eventType string) (func() []events.Event, func()) {
	t.Helper()

	var (
		mu       sync.Mutex
		captured []events.Event
	)
	bus := events.New()
	bus.Subscribe(eventType, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})

	origBus := testHandler.Bus
	testHandler.Bus = bus

	get := func() []events.Event {
		mu.Lock()
		defer mu.Unlock()
		out := make([]events.Event, len(captured))
		copy(out, captured)
		return out
	}
	restore := func() { testHandler.Bus = origBus }
	return get, restore
}

func requireSingleEvent(t *testing.T, captured []events.Event, wantType string) events.Event {
	t.Helper()
	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 %s event, got %d", wantType, len(captured))
	}
	if captured[0].Type != wantType {
		t.Fatalf("event type: expected %q, got %q", wantType, captured[0].Type)
	}
	if captured[0].WorkspaceID != testWorkspaceID {
		t.Fatalf("event workspace_id: expected %q, got %q", testWorkspaceID, captured[0].WorkspaceID)
	}
	return captured[0]
}

func requireMapPayload(t *testing.T, evt events.Event) map[string]any {
	t.Helper()
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload: expected map[string]any, got %T", evt.Payload)
	}
	return payload
}

// createMemberFixture inserts a user with the given email and a workspace
// member row with the given role. Both rows are removed on test cleanup.
// Returns (userID, memberID).
func createMemberFixture(t *testing.T, email, role string) (string, string) {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Workspace Emit Test User", email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { deleteUserByEmail(t, email) })

	var memberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
		RETURNING id
	`, testWorkspaceID, userID, role).Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	// User-cleanup cascades to member, but delete explicitly so a partial
	// failure later in the sub-test cannot leave a stale member row.
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE id = $1`, memberID)
	})

	return userID, memberID
}

func deleteUserByEmail(t *testing.T, email string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email); err != nil {
		t.Logf("cleanup user %s: %v", email, err)
	}
}

// uniqueTestEmail derives a per-sub-test email so re-runs and parallel-safe
// fixtures cannot collide. Slot identifies the sub-test for easier triage.
func uniqueTestEmail(t *testing.T, slot string) string {
	t.Helper()
	return fmt.Sprintf("workspace-emit-%s-%d@multica.ai", slot, nextEmailNonce())
}

var (
	emailNonceMu sync.Mutex
	emailNonce   uint64
)

func nextEmailNonce() uint64 {
	emailNonceMu.Lock()
	defer emailNonceMu.Unlock()
	emailNonce++
	return emailNonce
}
