package handler

import (
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspaceOrder(t *testing.T) {
	suffix := uuid.NewString()
	user := dbfx.User(t, "Order user", "order-"+suffix+"@example.test")
	other := dbfx.User(t, "Other user", "other-order-"+suffix+"@example.test")
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = dbfx.Workspace(t, fmt.Sprintf("Order %d", i), fmt.Sprintf("order-%d-%s", i, suffix), testutil.Cols{
			"created_at": time.Date(2025, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC),
		})
		dbfx.Member(t, ids[i], user, "member")
		dbfx.Member(t, ids[i], other, "member")
	}
	list := func(who string) []WorkspaceResponse {
		t.Helper()
		var got []WorkspaceResponse
		testutil.Call(t, testHandler.ListWorkspaces, newRequestAs(who, http.MethodGet, "/api/workspaces", nil)).Want(http.StatusOK).JSON(&got)
		return got
	}
	assertOrder := func(who string, want []string) {
		t.Helper()
		got := list(who)
		actual := make([]string, len(got))
		for i, ws := range got {
			actual[i] = ws.ID
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("order = %v, want %v", actual, want)
		}
	}
	save := func(order []string, status int) {
		t.Helper()
		testutil.Call(t, testHandler.SetWorkspaceOrder, newRequestAs(user, http.MethodPut, "/api/workspaces/order", map[string]any{"workspace_ids": order})).Want(status)
	}
	pin := func(id string, pinned bool) {
		t.Helper()
		req := withURLParam(newRequestAs(user, http.MethodPut, "/api/workspaces/"+id+"/pin", map[string]any{"pinned": pinned}), "id", id)
		testutil.Call(t, testHandler.SetWorkspacePinned, req).Want(http.StatusOK)
	}

	// Untouched accounts retain the old created-at and pin-time rules.
	assertOrder(user, ids)
	pin(ids[0], true)
	pin(ids[1], true)
	assertOrder(user, []string{ids[1], ids[0], ids[2]})
	pin(ids[0], false)
	pin(ids[1], false)
	assertOrder(user, ids)

	reversed := []string{ids[2], ids[1], ids[0]}
	save(reversed, http.StatusNoContent)
	assertOrder(user, reversed)
	assertOrder(other, ids)
	beforePin := list(user)
	pin(ids[0], true)
	assertOrder(user, []string{ids[0], ids[2], ids[1]})
	pin(ids[0], false)
	assertOrder(user, reversed)
	if !reflect.DeepEqual(list(user), beforePin) {
		t.Fatal("pin/unpin changed saved positions")
	}

	foreign := dbfx.Workspace(t, "Foreign", "foreign-order-"+suffix)
	save([]string{ids[2], ids[1], foreign}, http.StatusBadRequest)
	save([]string{ids[2], ids[1], ids[1]}, http.StatusBadRequest)
	save(ids[:2], http.StatusConflict)
	save([]string{"not-a-uuid"}, http.StatusBadRequest)
	assertOrder(user, reversed)

	// Concurrent whole-list replacements may choose either winner, not a mix.
	for round := 0; round < 10; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, order := range [][]string{ids, reversed} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				save(order, http.StatusNoContent)
			}()
		}
		close(start)
		wg.Wait()
		got := list(user)
		actual := make([]string, len(got))
		for i, ws := range got {
			actual[i] = ws.ID
			if ws.WorkspacePosition == nil || *ws.WorkspacePosition != int32(i) {
				t.Fatalf("partial concurrent order: %+v", got)
			}
		}
		if !reflect.DeepEqual(actual, ids) && !reflect.DeepEqual(actual, reversed) {
			t.Fatalf("mixed concurrent order: %v", actual)
		}
	}

	save(reversed, http.StatusNoContent)
	// Even an older workspace newly joined after sorting goes to the end.
	joined := dbfx.Workspace(t, "Joined", "joined-order-"+suffix, testutil.Cols{"created_at": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
	dbfx.Member(t, joined, user, "member")
	assertOrder(user, append(append([]string{}, reversed...), joined))
	pin(ids[2], true)
	pin(joined, true)
	assertOrder(user, []string{ids[2], joined, ids[1], ids[0]})
}
