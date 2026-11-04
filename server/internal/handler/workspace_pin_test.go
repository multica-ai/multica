package handler

import (
    "net/http"
    "testing"
    "time"
    "github.com/google/uuid"
    "github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspacePinIsPerMemberAndOrdersList(t *testing.T) {
    suffix := uuid.NewString()
    first := dbfx.Workspace(t, "First", "pin-first-"+suffix, testutil.Cols{"created_at": time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
    second := dbfx.Workspace(t, "Second", "pin-second-"+suffix, testutil.Cols{"created_at": time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)})
    third := dbfx.Workspace(t, "Third", "pin-third-"+suffix, testutil.Cols{"created_at": time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)})
    other := dbfx.User(t, "Other pin user", "pin-other-"+suffix+"@example.test")
    for _, id := range []string{first, second, third} {
        dbfx.Member(t, id, testUserID, "member")
        dbfx.Member(t, id, other, "member")
    }
    pin := func(user, id string, pinned bool) *string {
        t.Helper()
        req := withURLParam(newRequestAs(user, http.MethodPut, "/api/workspaces/"+id+"/pin", map[string]any{"pinned": pinned}), "id", id)
        var got struct { PinnedAt *string `json:"pinned_at"` }
        testutil.Call(t, testHandler.SetWorkspacePinned, req).Want(http.StatusOK).JSON(&got)
        return got.PinnedAt
    }
    list := func(user string) []WorkspaceResponse {
        t.Helper()
        var got []WorkspaceResponse
        testutil.Call(t, testHandler.ListWorkspaces, newRequestAs(user, http.MethodGet, "/api/workspaces", nil)).Want(http.StatusOK).JSON(&got)
        return got
    }
    assertRelative := func(got []WorkspaceResponse, ids ...string) {
        t.Helper()
        seen := make([]string, 0, len(ids))
        for _, item := range got {
            for _, id := range ids { if item.ID == id { seen = append(seen, id) } }
        }
        for i, id := range ids { if i >= len(seen) || seen[i] != id { t.Fatalf("relative order = %v, want %v", seen, ids) } }
    }
    assertRelative(list(testUserID), first, second, third)
    stamp := pin(testUserID, first, true)
    if stamp == nil { t.Fatal("pin did not return a timestamp") }
    if again := pin(testUserID, first, true); again == nil || *again != *stamp { t.Fatalf("repeat pin changed time: %v -> %v", stamp, again) }
    if pin(testUserID, second, true) == nil { t.Fatal("second pin missing timestamp") }
    assertRelative(list(testUserID), second, first, third)
    assertRelative(list(other), first, second, third)
    if pin(testUserID, second, false) != nil { t.Fatal("unpin should return null") }
    assertRelative(list(testUserID), first, second, third)
    pin(other, third, true)
    assertRelative(list(other), third, first, second)
    assertRelative(list(testUserID), first, second, third)
    outsider := dbfx.User(t, "Pin outsider", "pin-outsider-"+suffix+"@example.test")
    req := withURLParam(newRequestAs(outsider, http.MethodPut, "/api/workspaces/"+first+"/pin", map[string]any{"pinned": true}), "id", first)
    testutil.Call(t, testHandler.SetWorkspacePinned, req).Want(http.StatusNotFound)
    bad := withURLParam(newRequest(http.MethodPut, "/api/workspaces/not-uuid/pin", map[string]any{"pinned": true}), "id", "not-uuid")
    testutil.Call(t, testHandler.SetWorkspacePinned, bad).Want(http.StatusBadRequest)
    missing := withURLParam(newRequest(http.MethodPut, "/api/workspaces/"+first+"/pin", map[string]any{}), "id", first)
    testutil.Call(t, testHandler.SetWorkspacePinned, missing).Want(http.StatusBadRequest)
}
