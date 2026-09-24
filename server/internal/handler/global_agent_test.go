package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// globalAgentTestWorkspace is a second workspace the handler test user owns,
// with one runtime the user can bind agents to.
type globalAgentTestWorkspace struct {
	id        string
	name      string
	runtimeID string
}

func newGlobalAgentTestWorkspace(t *testing.T, label string) globalAgentTestWorkspace {
	t.Helper()
	suffix := time.Now().UnixNano()
	name := fmt.Sprintf("GA %s %d", label, suffix)
	wsID := dbfx.Workspace(t, name, fmt.Sprintf("ga-%s-%d", strings.ToLower(label), suffix))
	dbfx.Member(t, wsID, testUserID, "owner")
	runtimeID := dbfx.Runtime(t, "GA runtime "+label, testutil.Cols{"workspace_id": wsID})
	return globalAgentTestWorkspace{id: wsID, name: name, runtimeID: runtimeID}
}

func uniqueGlobalAgentName(label string) string {
	return fmt.Sprintf("%s %d", label, time.Now().UnixNano())
}

// createGlobalAgentForTest creates a global agent as the handler test user and
// removes it, and every workspace copy still linked to it, when the test ends.
func createGlobalAgentForTest(t *testing.T, body map[string]any) GlobalAgentResponse {
	t.Helper()
	var created GlobalAgentResponse
	testutil.Call(t, testHandler.CreateGlobalAgent, newRequest(http.MethodPost, "/api/global-agents", body)).
		Want(http.StatusCreated).
		JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM global_agent WHERE id = $1`, created.ID)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE global_agent_id = $1`, created.ID)
	return created
}

func enableGlobalAgentForTest(t *testing.T, globalID, workspaceID, runtimeID string, want int) AgentResponse {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/global-agents/"+globalID+"/workspaces", map[string]any{
		"workspace_id": workspaceID,
		"runtime_id":   runtimeID,
	}), "id", globalID)
	var agent AgentResponse
	res := testutil.Call(t, testHandler.EnableGlobalAgentInWorkspace, req).Want(want)
	if want == http.StatusCreated || want == http.StatusOK {
		res.JSON(&agent)
		dbfx.Cleanup(t, `DELETE FROM agent WHERE id = $1`, agent.ID)
	}
	return agent
}

func loadAgentRow(t *testing.T, agentID string) db.Agent {
	t.Helper()
	a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent %s: %v", agentID, err)
	}
	return a
}

func updateAgentAs(userID, workspaceID, agentID string, body map[string]any) *http.Request {
	req := newRequestAs(userID, http.MethodPut, "/api/agents/"+agentID, body)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return withURLParam(req, "id", agentID)
}

func TestGlobalAgent_EditsReachEveryWorkspaceCopy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Sync")
	name := uniqueGlobalAgentName("Reviewer")
	global := createGlobalAgentForTest(t, map[string]any{
		"name":         name,
		"description":  "Reviews pull requests",
		"instructions": "Be thorough.",
		"conversation_starters": []map[string]string{
			{"label": "Review", "prompt": "Review the latest PR"},
		},
	})
	if global.AvatarURL == nil || !strings.HasPrefix(*global.AvatarURL, agentEmojiAvatarPrefix) {
		t.Fatalf("expected a default emoji avatar, got %v", global.AvatarURL)
	}

	here := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)
	there := enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusCreated)
	for _, a := range []AgentResponse{here, there} {
		if a.GlobalAgentID == nil || *a.GlobalAgentID != global.ID {
			t.Fatalf("agent %s: global_agent_id = %v, want %s", a.ID, a.GlobalAgentID, global.ID)
		}
		if a.Name != name || a.Instructions != "Be thorough." || len(a.ConversationStarters) != 1 {
			t.Fatalf("agent %s was not created from the definition: %+v", a.ID, a)
		}
		if a.PermissionMode != permissionModePrivate {
			t.Fatalf("agent %s: permission_mode = %q, want private", a.ID, a.PermissionMode)
		}
	}
	enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusConflict)

	// Editing the definition rewrites every copy.
	renamed := uniqueGlobalAgentName("Senior Reviewer")
	testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
		"name":         renamed,
		"instructions": "Be thorough and kind.",
	}), "id", global.ID)).Want(http.StatusOK)
	for _, id := range []string{here.ID, there.ID} {
		row := loadAgentRow(t, id)
		if row.Name != renamed || row.Instructions != "Be thorough and kind." || row.Description != "Reviews pull requests" {
			t.Fatalf("agent %s after global edit: name=%q instructions=%q description=%q", id, row.Name, row.Instructions, row.Description)
		}
	}

	// The owner editing a copy in one workspace edits the definition too.
	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(testUserID, testWorkspaceID, here.ID, map[string]any{
		"description":          "Reviews every pull request",
		"max_concurrent_tasks": 3,
	})).Want(http.StatusOK)
	thereRow := loadAgentRow(t, there.ID)
	if thereRow.Description != "Reviews every pull request" {
		t.Fatalf("copy in the other workspace kept description %q", thereRow.Description)
	}
	if thereRow.MaxConcurrentTasks == 3 {
		t.Fatalf("max_concurrent_tasks is per workspace but reached the other copy")
	}
	if hereRow := loadAgentRow(t, here.ID); hereRow.MaxConcurrentTasks != 3 {
		t.Fatalf("max_concurrent_tasks = %d, want 3 on the edited copy", hereRow.MaxConcurrentTasks)
	}

	var listed []GlobalAgentResponse
	testutil.Call(t, testHandler.ListGlobalAgents, newRequest(http.MethodGet, "/api/global-agents", nil)).
		Want(http.StatusOK).
		JSON(&listed)
	var found *GlobalAgentResponse
	for i := range listed {
		if listed[i].ID == global.ID {
			found = &listed[i]
		}
	}
	if found == nil {
		t.Fatalf("global agent %s missing from list", global.ID)
	}
	if found.Description != "Reviews every pull request" || len(found.Links) != 2 {
		t.Fatalf("listed global agent: description=%q links=%d", found.Description, len(found.Links))
	}
}

func TestGlobalAgent_OnlyTheOwnerChangesSyncedFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	adminID := dbfx.User(t, "GA Admin", fmt.Sprintf("ga-admin-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, adminID, "admin")
	name := uniqueGlobalAgentName("Planner")
	global := createGlobalAgentForTest(t, map[string]any{"name": name, "instructions": "Plan."})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	denied := testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(adminID, testWorkspaceID, linked.ID, map[string]any{
		"instructions": "Plan differently.",
	})).Want(http.StatusForbidden)
	if !strings.Contains(denied.Body.String(), "synced from a global agent") {
		t.Fatalf("admin rejected for another reason: %s", denied.Body.String())
	}

	// Per-workspace fields stay editable by an admin, and an unchanged echo of
	// a synced field is not a synced write.
	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(adminID, testWorkspaceID, linked.ID, map[string]any{
		"name":                 name,
		"instructions":         "Plan.",
		"max_concurrent_tasks": 2,
	})).Want(http.StatusOK)

	agentReq := updateAgentAs(testUserID, testWorkspaceID, linked.ID, map[string]any{"instructions": "Injected."})
	agentReq.Header.Set("X-Actor-Source", "task_token")
	agentReq.Header.Set("X-Agent-ID", linked.ID)
	denied = testutil.Call(t, testHandler.UpdateAgent, agentReq).Want(http.StatusForbidden)
	if !strings.Contains(denied.Body.String(), "synced from a global agent") {
		t.Fatalf("agent actor rejected for another reason: %s", denied.Body.String())
	}

	if row := loadAgentRow(t, linked.ID); row.Instructions != "Plan." {
		t.Fatalf("instructions = %q after rejected writes", row.Instructions)
	}
}

func TestGlobalAgent_DisableKeepsTheCopyAndEnableRestoresIt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Toggle")
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Triage")})
	linked := enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusCreated)

	disable := func(want int) {
		t.Helper()
		req := newRequest(http.MethodDelete, "/api/global-agents/"+global.ID+"/workspaces/"+other.id, nil)
		testutil.Call(t, testHandler.DisableGlobalAgentInWorkspace, withURLParams(req, "id", global.ID, "workspaceId", other.id)).Want(want)
	}
	disable(http.StatusOK)
	if row := loadAgentRow(t, linked.ID); !row.ArchivedAt.Valid {
		t.Fatalf("disabled copy is not archived")
	}
	disable(http.StatusConflict)

	// The definition changes while the copy is disabled; enabling brings the
	// same row back with the current definition.
	testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
		"instructions": "Updated while disabled.",
	}), "id", global.ID)).Want(http.StatusOK)
	restored := enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusOK)
	if restored.ID != linked.ID {
		t.Fatalf("enable created %s instead of restoring %s", restored.ID, linked.ID)
	}
	if restored.ArchivedAt != nil || restored.Instructions != "Updated while disabled." {
		t.Fatalf("restored copy: archived_at=%v instructions=%q", restored.ArchivedAt, restored.Instructions)
	}
}

func TestGlobalAgent_DeleteKeepsWorkspaceAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Writer")})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	testutil.Call(t, testHandler.DeleteGlobalAgent, withURLParam(newRequest(http.MethodDelete, "/api/global-agents/"+global.ID, nil), "id", global.ID)).
		Want(http.StatusNoContent)

	row := loadAgentRow(t, linked.ID)
	if row.GlobalAgentID.Valid || row.ArchivedAt.Valid {
		t.Fatalf("copy after delete: global_agent_id=%v archived=%v, want a regular active agent", row.GlobalAgentID, row.ArchivedAt.Valid)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM global_agent WHERE id = $1`, global.ID); n != 0 {
		t.Fatalf("global agent row still present")
	}
	// A regular agent again: an admin edit no longer needs the owner.
	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(testUserID, testWorkspaceID, linked.ID, map[string]any{
		"instructions": "Standalone now.",
	})).Want(http.StatusOK)
}

func TestGlobalAgent_RenameReportsTheBlockingWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Rename")
	taken := uniqueGlobalAgentName("Taken")
	dbfx.Agent(t, taken, other.runtimeID, testutil.Cols{"workspace_id": other.id})
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Free")})
	enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusCreated)

	res := testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
		"name": taken,
	}), "id", global.ID)).Want(http.StatusConflict)
	if !strings.Contains(res.Body.String(), other.name) {
		t.Fatalf("conflict does not name the workspace %q: %s", other.name, res.Body.String())
	}

	// Enabling a global agent whose name is already used in the workspace is
	// refused rather than creating a second agent with that name.
	clash := createGlobalAgentForTest(t, map[string]any{"name": taken})
	enableGlobalAgentForTest(t, clash.ID, other.id, other.runtimeID, http.StatusConflict)
}

func TestGlobalAgent_IsInvisibleToOtherUsers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	strangerID := dbfx.User(t, "GA Stranger", fmt.Sprintf("ga-stranger-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, strangerID, "member")
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Private")})

	testutil.Call(t, testHandler.GetGlobalAgent, withURLParam(newRequestAs(strangerID, http.MethodGet, "/api/global-agents/"+global.ID, nil), "id", global.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.EnableGlobalAgentInWorkspace, withURLParam(newRequestAs(strangerID, http.MethodPost, "/api/global-agents/"+global.ID+"/workspaces", map[string]any{
		"workspace_id": testWorkspaceID,
		"runtime_id":   testRuntimeID,
	}), "id", global.ID)).Want(http.StatusNotFound)
}

func TestGlobalAgent_EnableValidatesWorkspaceAndRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Checker")})
	enable := func(body any) *testutil.Response {
		return testutil.Call(t, testHandler.EnableGlobalAgentInWorkspace, withURLParam(
			newRequest(http.MethodPost, "/api/global-agents/"+global.ID+"/workspaces", body), "id", global.ID))
	}
	enable("not json").Want(http.StatusBadRequest)
	enable(map[string]any{"workspace_id": "nope", "runtime_id": testRuntimeID}).Want(http.StatusBadRequest)

	// A workspace the caller does not belong to.
	suffix := time.Now().UnixNano()
	foreignWS := dbfx.Workspace(t, "GA Foreign", fmt.Sprintf("ga-foreign-%d", suffix))
	foreignRuntime := dbfx.Runtime(t, "GA foreign runtime", testutil.Cols{"workspace_id": foreignWS})
	enable(map[string]any{"workspace_id": foreignWS, "runtime_id": foreignRuntime}).Want(http.StatusNotFound)

	// A runtime from another workspace, and another member's private runtime.
	other := newGlobalAgentTestWorkspace(t, "Runtime")
	enable(map[string]any{"workspace_id": testWorkspaceID, "runtime_id": other.runtimeID}).Want(http.StatusBadRequest)
	peerID := dbfx.User(t, "GA Peer", fmt.Sprintf("ga-peer-%d@multica.ai", suffix))
	dbfx.Member(t, testWorkspaceID, peerID, "member")
	peerRuntime := dbfx.Runtime(t, "GA peer runtime", testutil.Cols{"owner_id": peerID, "visibility": "private"})
	enable(map[string]any{"workspace_id": testWorkspaceID, "runtime_id": peerRuntime}).Want(http.StatusForbidden)
}

func TestListGlobalAgentWorkspaces_OffersOnlyUsableRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Targets")
	suffix := time.Now().UnixNano()
	peerID := dbfx.User(t, "GA Lender", fmt.Sprintf("ga-lender-%d@multica.ai", suffix))
	dbfx.Member(t, other.id, peerID, "member")
	sharedRuntime := dbfx.Runtime(t, "GA shared runtime", testutil.Cols{"workspace_id": other.id, "owner_id": peerID, "visibility": "public"})
	hiddenRuntime := dbfx.Runtime(t, "GA hidden runtime", testutil.Cols{"workspace_id": other.id, "owner_id": peerID, "visibility": "private"})

	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Targets")})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	var targets []GlobalAgentWorkspaceTarget
	testutil.Call(t, testHandler.ListGlobalAgentWorkspaces, withURLParam(newRequest(http.MethodGet, "/api/global-agents/"+global.ID+"/workspaces", nil), "id", global.ID)).
		Want(http.StatusOK).
		JSON(&targets)
	byID := map[string]GlobalAgentWorkspaceTarget{}
	for _, target := range targets {
		byID[target.WorkspaceID] = target
	}
	home, ok := byID[testWorkspaceID]
	if !ok || home.Agent == nil || home.Agent.ID != linked.ID || home.Agent.Archived {
		t.Fatalf("home workspace target: %+v", home)
	}
	away, ok := byID[other.id]
	if !ok || away.Agent != nil {
		t.Fatalf("other workspace target: %+v", away)
	}
	offered := map[string]bool{}
	for _, rt := range away.Runtimes {
		offered[rt.ID] = true
	}
	if !offered[other.runtimeID] || !offered[sharedRuntime] || offered[hiddenRuntime] {
		t.Fatalf("runtimes offered in %s: %+v", other.id, away.Runtimes)
	}
	if away.SuggestedRuntimeID != other.runtimeID {
		t.Fatalf("suggested %s, want the caller's own online runtime %s", away.SuggestedRuntimeID, other.runtimeID)
	}
}

func TestSuggestGlobalAgentRuntime(t *testing.T) {
	me := "00000000-0000-0000-0000-000000000001"
	peer := "00000000-0000-0000-0000-000000000002"
	rt := func(id, daemon, provider, status, owner string) db.AgentRuntime {
		return db.AgentRuntime{
			ID:       parseUUID(id),
			DaemonID: pgtype.Text{String: daemon, Valid: daemon != ""},
			Provider: provider,
			Status:   status,
			OwnerID:  parseUUID(owner),
		}
	}
	sameMachineOffline := rt("00000000-0000-0000-0000-00000000000a", "laptop", "claude", "offline", me)
	otherMachineOnline := rt("00000000-0000-0000-0000-00000000000b", "desktop", "claude", "online", me)
	peerOnline := rt("00000000-0000-0000-0000-00000000000c", "server", "codex", "online", peer)
	preferred := map[string]bool{runtimeMachineKey(sameMachineOffline): true}

	cases := []struct {
		name      string
		runtimes  []db.AgentRuntime
		preferred map[string]bool
		want      pgtype.UUID
	}{
		{"none", nil, nil, pgtype.UUID{}},
		{"same machine and provider wins over online", []db.AgentRuntime{otherMachineOnline, sameMachineOffline}, preferred, sameMachineOffline.ID},
		{"online wins without a machine match", []db.AgentRuntime{sameMachineOffline, otherMachineOnline}, nil, otherMachineOnline.ID},
		{"own runtime wins among online ones", []db.AgentRuntime{peerOnline, otherMachineOnline}, nil, otherMachineOnline.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suggestGlobalAgentRuntime(tc.runtimes, me, tc.preferred)
			if got != uuidToString(tc.want) {
				t.Fatalf("suggested %q, want %q", got, uuidToString(tc.want))
			}
		})
	}
}

func TestMakeAgentGlobal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	name := uniqueGlobalAgentName("Existing")
	agentID := dbfx.Agent(t, name, testRuntimeID, testutil.Cols{"instructions": "Keep me."})
	dbfx.Cleanup(t, `DELETE FROM global_agent WHERE owner_id = $1 AND name = $2`, testUserID, name)

	adminID := dbfx.User(t, "GA Promoter", fmt.Sprintf("ga-promoter-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, adminID, "admin")
	makeGlobal := func(userID string) *testutil.Response {
		return testutil.Call(t, testHandler.MakeAgentGlobal, withURLParam(newRequestAs(userID, http.MethodPost, "/api/agents/"+agentID+"/make-global", nil), "id", agentID))
	}
	makeGlobal(adminID).Want(http.StatusForbidden)

	var global GlobalAgentResponse
	makeGlobal(testUserID).Want(http.StatusCreated).JSON(&global)
	if global.Name != name || global.Instructions != "Keep me." {
		t.Fatalf("global agent not built from the agent: %+v", global)
	}
	if len(global.Links) != 1 || global.Links[0].AgentID != agentID || global.Links[0].WorkspaceID != testWorkspaceID {
		t.Fatalf("links = %+v, want the source agent", global.Links)
	}
	if row := loadAgentRow(t, agentID); uuidToString(row.GlobalAgentID) != global.ID {
		t.Fatalf("source agent linked to %v, want %s", row.GlobalAgentID, global.ID)
	}
	makeGlobal(testUserID).Want(http.StatusConflict)
}

func TestGlobalAgent_LeavingAWorkspaceUnlinksTheCopyThere(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	suffix := time.Now().UnixNano()
	ownerID := dbfx.User(t, "GA Workspace Owner", fmt.Sprintf("ga-ws-owner-%d@multica.ai", suffix))
	wsName := fmt.Sprintf("GA Leave %d", suffix)
	wsID := dbfx.Workspace(t, wsName, fmt.Sprintf("ga-leave-%d", suffix))
	dbfx.Member(t, wsID, ownerID, "owner")
	memberRowID := dbfx.Member(t, wsID, testUserID, "member")
	// Someone else's shared machine: removal does not archive agents on it, so
	// the copy stays active and unlinking is what hands it back.
	runtimeID := dbfx.Runtime(t, "GA leave runtime", testutil.Cols{"workspace_id": wsID, "owner_id": ownerID, "visibility": "public"})

	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Leaver"), "instructions": "Before."})
	copyThere := enableGlobalAgentForTest(t, global.ID, wsID, runtimeID, http.StatusCreated)
	copyHere := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	if _, err := testHandler.revokeAndRemoveMember(t.Context(), parseUUID(wsID), parseUUID(testUserID), parseUUID(memberRowID), parseUUID(testUserID)); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}
	if row := loadAgentRow(t, copyThere.ID); row.GlobalAgentID.Valid || row.ArchivedAt.Valid {
		t.Fatalf("copy in the left workspace: linked=%v archived=%v, want an active regular agent", row.GlobalAgentID.Valid, row.ArchivedAt.Valid)
	}

	testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
		"instructions": "After.",
	}), "id", global.ID)).Want(http.StatusOK)
	if row := loadAgentRow(t, copyThere.ID); row.Instructions != "Before." {
		t.Fatalf("an edit after leaving reached the left workspace: %q", row.Instructions)
	}
	if row := loadAgentRow(t, copyHere.ID); row.Instructions != "After." {
		t.Fatalf("copy in a remaining workspace missed the edit: %q", row.Instructions)
	}

	// The workspace's own owner can manage the agent again.
	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(ownerID, wsID, copyThere.ID, map[string]any{
		"instructions": "Workspace-owned now.",
	})).Want(http.StatusOK)

	var got GlobalAgentResponse
	testutil.Call(t, testHandler.GetGlobalAgent, withURLParam(newRequest(http.MethodGet, "/api/global-agents/"+global.ID, nil), "id", global.ID)).
		Want(http.StatusOK).
		JSON(&got)
	for _, link := range got.Links {
		if link.WorkspaceID == wsID {
			t.Fatalf("left workspace %q still listed in links", wsName)
		}
	}
}

func TestGlobalAgent_OwnerEditOfACopyIsAllOrNothing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Atomic")
	taken := uniqueGlobalAgentName("Occupied")
	dbfx.Agent(t, taken, other.runtimeID, testutil.Cols{"workspace_id": other.id})
	name := uniqueGlobalAgentName("Atomic")
	global := createGlobalAgentForTest(t, map[string]any{"name": name})
	here := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)
	enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusCreated)
	before := loadAgentRow(t, here.ID)

	res := testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(testUserID, testWorkspaceID, here.ID, map[string]any{
		"name":                 taken,
		"max_concurrent_tasks": before.MaxConcurrentTasks + 1,
	})).Want(http.StatusConflict)
	if !strings.Contains(res.Body.String(), other.name) {
		t.Fatalf("conflict does not name the workspace %q: %s", other.name, res.Body.String())
	}
	after := loadAgentRow(t, here.ID)
	if after.Name != name || after.MaxConcurrentTasks != before.MaxConcurrentTasks {
		t.Fatalf("rejected edit partly applied: name=%q max_concurrent_tasks=%d", after.Name, after.MaxConcurrentTasks)
	}

	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(testUserID, testWorkspaceID, here.ID, map[string]any{
		"name": "  ",
	})).Want(http.StatusBadRequest)
}

func TestGlobalAgent_SyncedColumnsIgnoreDirectWritesToACopy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Guarded"), "instructions": "Definition."})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	// A write that read the agent before it was linked reaches the query
	// without the handler's routing; the query itself must keep the copy in
	// line with its definition.
	updated, err := testHandler.Queries.UpdateAgent(t.Context(), db.UpdateAgentParams{
		ID:                 parseUUID(linked.ID),
		Instructions:       pgtype.Text{String: "Stale write.", Valid: true},
		MaxConcurrentTasks: pgtype.Int4{Int32: 4, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if updated.Instructions != "Definition." || updated.MaxConcurrentTasks != 4 {
		t.Fatalf("instructions=%q max_concurrent_tasks=%d; want the synced column kept and the per-workspace one written", updated.Instructions, updated.MaxConcurrentTasks)
	}
}

func TestGlobalAgent_AdminEchoOfEverySyncedFieldIsAccepted(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	adminID := dbfx.User(t, "GA Echo Admin", fmt.Sprintf("ga-echo-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, adminID, "admin")
	global := createGlobalAgentForTest(t, map[string]any{
		"name":        uniqueGlobalAgentName("Echo"),
		"description": "Echoed.",
		"conversation_starters": []map[string]string{
			{"label": "Start", "prompt": "Begin here"},
		},
	})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)
	row := loadAgentRow(t, linked.ID)

	testutil.Call(t, testHandler.UpdateAgent, updateAgentAs(adminID, testWorkspaceID, linked.ID, map[string]any{
		"name":                  row.Name,
		"description":           row.Description,
		"instructions":          row.Instructions,
		"avatar_url":            row.AvatarUrl.String,
		"conversation_starters": linked.ConversationStarters,
		"max_concurrent_tasks":  5,
	})).Want(http.StatusOK)
	if got := loadAgentRow(t, linked.ID); got.MaxConcurrentTasks != 5 {
		t.Fatalf("max_concurrent_tasks = %d, want 5", got.MaxConcurrentTasks)
	}
}

func TestGlobalAgent_MachineCredentialsAreRefused(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Machine")})
	linked := enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			asMachine := func(req *http.Request) *http.Request {
				req.Header.Set("X-Actor-Source", source)
				return req
			}
			testutil.Call(t, testHandler.ListGlobalAgents, asMachine(newRequest(http.MethodGet, "/api/global-agents", nil))).
				Want(http.StatusForbidden)
			testutil.Call(t, testHandler.CreateGlobalAgent, asMachine(newRequest(http.MethodPost, "/api/global-agents", map[string]any{"name": "Nope"}))).
				Want(http.StatusForbidden)
			testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(asMachine(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
				"instructions": "Injected.",
			})), "id", global.ID)).Want(http.StatusForbidden)
			denied := testutil.Call(t, testHandler.UpdateAgent, asMachine(updateAgentAs(testUserID, testWorkspaceID, linked.ID, map[string]any{
				"instructions": "Injected.",
			}))).Want(http.StatusForbidden)
			if !strings.Contains(denied.Body.String(), "synced from a global agent") {
				t.Fatalf("rejected for another reason: %s", denied.Body.String())
			}
		})
	}
}

func TestGlobalAgent_StrangersGetNotFoundEverywhere(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	strangerID := dbfx.User(t, "GA Outsider", fmt.Sprintf("ga-outsider-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, strangerID, "admin")
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Mine")})
	enableGlobalAgentForTest(t, global.ID, testWorkspaceID, testRuntimeID, http.StatusCreated)

	as := func(method, path string, body any) *http.Request {
		return newRequestAs(strangerID, method, path, body)
	}
	base := "/api/global-agents/" + global.ID
	testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(as(http.MethodPut, base, map[string]any{"name": "Hijacked"}), "id", global.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.DeleteGlobalAgent, withURLParam(as(http.MethodDelete, base, nil), "id", global.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.ListGlobalAgentWorkspaces, withURLParam(as(http.MethodGet, base+"/workspaces", nil), "id", global.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.DisableGlobalAgentInWorkspace, withURLParams(as(http.MethodDelete, base+"/workspaces/"+testWorkspaceID, nil), "id", global.ID, "workspaceId", testWorkspaceID)).
		Want(http.StatusNotFound)

	var listed []GlobalAgentResponse
	testutil.Call(t, testHandler.ListGlobalAgents, as(http.MethodGet, "/api/global-agents", nil)).Want(http.StatusOK).JSON(&listed)
	for _, g := range listed {
		if g.ID == global.ID {
			t.Fatalf("another user's global agent is listed")
		}
	}
}

func TestMakeAgentGlobal_Refusals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	makeGlobal := func(agentID string) *testutil.Response {
		return testutil.Call(t, testHandler.MakeAgentGlobal, withURLParam(newRequest(http.MethodPost, "/api/agents/"+agentID+"/make-global", nil), "id", agentID))
	}

	archived := dbfx.Agent(t, uniqueGlobalAgentName("Archived"), testRuntimeID, testutil.Cols{"archived_at": testutil.Raw("now()")})
	makeGlobal(archived).Want(http.StatusBadRequest)

	system := dbfx.Agent(t, uniqueGlobalAgentName("System"), testRuntimeID, testutil.Cols{"system_key": fmt.Sprintf("ga_test_%d", time.Now().UnixNano())})
	makeGlobal(system).Want(http.StatusBadRequest)

	clash := uniqueGlobalAgentName("Clash")
	createGlobalAgentForTest(t, map[string]any{"name": clash})
	sameName := dbfx.Agent(t, clash, testRuntimeID)
	makeGlobal(sameName).Want(http.StatusConflict)
	if row := loadAgentRow(t, sameName); row.GlobalAgentID.Valid {
		t.Fatalf("refused make-global still linked the agent")
	}
}

func TestGlobalAgent_RestoringOnAnotherProviderDropsRuntimeNativeSettings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Provider")
	claudeRuntime := dbfx.Runtime(t, "GA claude runtime", testutil.Cols{"workspace_id": other.id, "provider": "claude"})
	codexRuntime := dbfx.Runtime(t, "GA codex runtime", testutil.Cols{"workspace_id": other.id, "provider": "codex"})
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Mover")})
	linked := enableGlobalAgentForTest(t, global.ID, other.id, claudeRuntime, http.StatusCreated)
	dbfx.Exec(t, `UPDATE agent SET model = 'claude-sonnet-4-5', thinking_level = 'high' WHERE id = $1`, linked.ID)

	testutil.Call(t, testHandler.DisableGlobalAgentInWorkspace, withURLParams(
		newRequest(http.MethodDelete, "/api/global-agents/"+global.ID+"/workspaces/"+other.id, nil),
		"id", global.ID, "workspaceId", other.id)).Want(http.StatusOK)
	restored := enableGlobalAgentForTest(t, global.ID, other.id, codexRuntime, http.StatusOK)

	if restored.ID != linked.ID || restored.RuntimeID != codexRuntime {
		t.Fatalf("restored %s on %s, want %s on %s", restored.ID, restored.RuntimeID, linked.ID, codexRuntime)
	}
	if restored.Model != "" || restored.ThinkingLevel != "" {
		t.Fatalf("model=%q thinking_level=%q carried onto another provider", restored.Model, restored.ThinkingLevel)
	}
}

func TestGlobalAgent_SyncSkipsCopiesWhoseOwnerLeftTheWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	other := newGlobalAgentTestWorkspace(t, "Stale")
	global := createGlobalAgentForTest(t, map[string]any{"name": uniqueGlobalAgentName("Stale"), "instructions": "Kept."})
	stale := enableGlobalAgentForTest(t, global.ID, other.id, other.runtimeID, http.StatusCreated)

	// A copy that stayed linked after its owner left, e.g. across a removal
	// that raced a link: the membership check alone must keep edits out.
	dbfx.Exec(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, other.id, testUserID)

	testutil.Call(t, testHandler.UpdateGlobalAgent, withURLParam(newRequest(http.MethodPut, "/api/global-agents/"+global.ID, map[string]any{
		"instructions": "Changed.",
	}), "id", global.ID)).Want(http.StatusOK)
	if row := loadAgentRow(t, stale.ID); row.Instructions != "Kept." {
		t.Fatalf("an edit reached a workspace the owner left: %q", row.Instructions)
	}
}
