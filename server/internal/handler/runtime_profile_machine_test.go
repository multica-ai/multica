package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestRuntimeProfile_MachineIsolation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	a, b := "machine-a-"+uuid.NewString(), "machine-b-"+uuid.NewString()
	for _, id := range []string{a, b} {
		dbfx.Runtime(t, id, testutil.Cols{"daemon_id": id, "runtime_mode": "local"})
	}
	create := withURLParam(newRequest("POST", "/", map[string]any{
		"display_name": "Scoped " + uuid.NewString(), "runtime_type": "codex", "command_name": "same-command", "daemon_id": a,
	}), "id", testWorkspaceID)
	var profile RuntimeProfileResponse
	testutil.Call(t, testHandler.CreateRuntimeProfile, create).Want(http.StatusCreated).JSON(&profile)
	dbfx.Cleanup(t, `DELETE FROM runtime_profile WHERE id = $1`, profile.ID)
	dbfx.Cleanup(t, `DELETE FROM agent_runtime WHERE profile_id = $1`, profile.ID)
	if profile.DaemonID == nil || *profile.DaemonID != a {
		t.Fatalf("missing machine binding: %+v", profile)
	}

	for _, machine := range []string{a, b} {
		req := withURLParam(newDaemonTokenRequest("GET", "/", nil, testWorkspaceID, machine), "workspaceId", testWorkspaceID)
		var out struct {
			Profiles []RuntimeProfileResponse `json:"runtime_profiles"`
		}
		testutil.Call(t, testHandler.DaemonListRuntimeProfiles, req).Want(http.StatusOK).JSON(&out)
		found := false
		for _, p := range out.Profiles {
			if p.ID == profile.ID {
				found = true
			}
		}
		if found != (machine == a) {
			t.Fatalf("machine %s sees profile = %v", machine, found)
		}
	}
	mismatch := withURLParam(newDaemonTokenRequest("GET", "/?daemon_id="+a, nil, testWorkspaceID, b), "workspaceId", testWorkspaceID)
	testutil.Call(t, testHandler.DaemonListRuntimeProfiles, mismatch).Want(http.StatusForbidden)

	// Older PAT daemons retain the owner's profiles; registration fences each machine.
	other := dbfx.User(t, "Other profile owner", "other-profile-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, other, "member")
	foreignMachine := "foreign-owner-" + uuid.NewString()
	dbfx.Runtime(t, "Other owner's machine", testutil.Cols{"daemon_id": foreignMachine, "runtime_mode": "local", "owner_id": other})
	foreignProfile := insertRuntimeProfileFixture(t, context.Background(), "Other owner's profile "+uuid.NewString(), "codex", "codex")
	dbfx.Exec(t, `UPDATE runtime_profile SET created_by=$2, daemon_id=$3 WHERE id=$1`, foreignProfile, other, foreignMachine)
	old := withURLParam(newOwnerDaemonRequest("GET", "/", nil), "workspaceId", testWorkspaceID)
	var oldOut struct {
		Profiles []RuntimeProfileResponse `json:"runtime_profiles"`
	}
	testutil.Call(t, testHandler.DaemonListRuntimeProfiles, old).Want(http.StatusOK).JSON(&oldOut)
	if len(oldOut.Profiles) != 1 || oldOut.Profiles[0].ID != profile.ID {
		t.Fatalf("older daemon lost its profile: %+v", oldOut.Profiles)
	}

	// Identical commands on B must not create either an online instance or an
	// offline error placeholder. Its built-in runtime still registers normally.
	for _, failed := range []bool{false, true} {
		body := map[string]any{
			"workspace_id": testWorkspaceID, "daemon_id": b,
			"runtimes": []map[string]any{{"name": "Builtin", "type": "codex", "status": "online"}},
		}
		if failed {
			body["failed_profiles"] = []map[string]any{{"profile_id": profile.ID, "reason": "missing command"}}
		} else {
			body["runtimes"] = append(body["runtimes"].([]map[string]any), map[string]any{"name": "Custom", "type": "codex", "profile_id": profile.ID, "status": "online"})
		}
		testutil.Call(t, testHandler.DaemonRegister, newOwnerDaemonRequest("POST", "/", body)).Want(http.StatusOK)
		if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id = $1 AND daemon_id = $2`, profile.ID, b); n != 0 {
			t.Fatalf("copied profile to B: %d", n)
		}
	}
	testutil.Call(t, testHandler.DaemonRegister, newOwnerDaemonRequest("POST", "/", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": a,
		"runtimes": []map[string]any{{"name": "Custom", "type": "codex", "profile_id": profile.ID, "status": "online"}},
	})).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id = $1 AND daemon_id = $2 AND status = 'online'`, profile.ID, a); n != 1 {
		t.Fatalf("owner machine instance count = %d", n)
	}
	dbfx.Exec(t, `UPDATE runtime_profile SET enabled = false WHERE id = $1`, profile.ID)
	testutil.Call(t, testHandler.DaemonRegister, newOwnerDaemonRequest("POST", "/", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": b,
		"runtimes": []map[string]any{
			{"name": "Builtin", "type": "codex", "status": "online"},
			{"name": "Custom", "type": "codex", "profile_id": profile.ID, "status": "online"},
		},
	})).Want(http.StatusOK)

	// A token for another workspace cannot read this workspace's profile set.
	foreign := withURLParam(newDaemonTokenRequest("GET", "/", nil, uuid.NewString(), a), "workspaceId", testWorkspaceID)
	testutil.Call(t, testHandler.DaemonListRuntimeProfiles, foreign).Want(http.StatusNotFound)
	otherReq := withURLParam(newRequest("GET", "/?daemon_id="+a, nil), "workspaceId", testWorkspaceID)
	otherReq.Header.Set("X-User-ID", other)
	testutil.Call(t, testHandler.DaemonListRuntimeProfiles, otherReq).Want(http.StatusNotFound)
}

func TestCreateRuntimeProfile_RequiresRegisteredMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// A daemon from another workspace is not a valid target here.
	foreignWS := dbfx.Workspace(t, "Foreign profile workspace", "foreign-profile-"+uuid.NewString())
	foreign := "foreign-" + uuid.NewString()
	dbfx.Runtime(t, "Foreign machine", testutil.Cols{"workspace_id": foreignWS, "daemon_id": foreign, "runtime_mode": "local"})
	for _, machine := range []string{"unknown-" + uuid.NewString(), foreign} {
		req := withURLParam(newRequest("POST", "/", map[string]any{
			"display_name": "Invalid machine " + uuid.NewString(), "runtime_type": "codex", "command_name": "wrapper", "daemon_id": machine,
		}), "id", testWorkspaceID)
		testutil.Call(t, testHandler.CreateRuntimeProfile, req).Want(http.StatusBadRequest)
	}
}

func TestCreateRuntimeProfile_OlderClientRequiresUnambiguousMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Single machine workspace", "single-machine-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	machine := "only-machine-" + uuid.NewString()
	dbfx.Runtime(t, "Only local machine", testutil.Cols{"workspace_id": ws, "daemon_id": machine, "runtime_mode": "local"})
	create := func() *http.Request {
		return withURLParam(newRequest("POST", "/", map[string]any{
			"display_name": "Older client " + uuid.NewString(), "runtime_type": "codex", "command_name": "wrapper",
		}), "id", ws)
	}
	var out RuntimeProfileResponse
	testutil.Call(t, testHandler.CreateRuntimeProfile, create()).Want(http.StatusCreated).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM runtime_profile WHERE id = $1`, out.ID)
	if out.DaemonID == nil || *out.DaemonID != machine {
		t.Fatalf("wrong inferred machine: %+v", out)
	}
	// Older clients can still create on their sole owned machine in a multi-member workspace.
	other := dbfx.User(t, "Another machine owner", "machine-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, ws, other, "member")
	dbfx.Runtime(t, "Other local machine", testutil.Cols{"workspace_id": ws, "daemon_id": "other-" + uuid.NewString(), "runtime_mode": "local", "owner_id": other})
	var second RuntimeProfileResponse
	testutil.Call(t, testHandler.CreateRuntimeProfile, create()).Want(http.StatusCreated).JSON(&second)
	dbfx.Cleanup(t, `DELETE FROM runtime_profile WHERE id = $1`, second.ID)
	if second.DaemonID == nil || *second.DaemonID != machine {
		t.Fatalf("wrong owned machine: %+v", second)
	}
	dbfx.Runtime(t, "Second owned machine", testutil.Cols{"workspace_id": ws, "daemon_id": "second-" + uuid.NewString(), "runtime_mode": "local"})
	testutil.Call(t, testHandler.CreateRuntimeProfile, create()).Want(http.StatusBadRequest)
}

func TestRuntimeProfile_LegacyDoesNotSpread(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := insertRuntimeProfileFixture(t, context.Background(), "Legacy "+uuid.NewString(), "codex", "wrapper")
	existing, fresh := "legacy-existing-"+uuid.NewString(), "legacy-fresh-"+uuid.NewString()
	dbfx.Runtime(t, "Legacy instance", testutil.Cols{"daemon_id": existing, "runtime_mode": "local", "profile_id": id})
	dbfx.Runtime(t, "Fresh machine", testutil.Cols{"daemon_id": fresh, "runtime_mode": "local"})
	for _, machine := range []string{existing, fresh} {
		req := withURLParam(newOwnerDaemonRequest("GET", "/?daemon_id="+machine, nil), "workspaceId", testWorkspaceID)
		var out struct {
			Profiles []RuntimeProfileResponse `json:"runtime_profiles"`
		}
		testutil.Call(t, testHandler.DaemonListRuntimeProfiles, req).Want(http.StatusOK).JSON(&out)
		found := false
		for _, p := range out.Profiles {
			if p.ID == id {
				found = true
			}
		}
		if found != (machine == existing) {
			t.Fatalf("legacy profile on %s = %v", machine, found)
		}
	}
	testutil.Call(t, testHandler.DaemonRegister, newOwnerDaemonRequest("POST", "/", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": existing,
		"runtimes": []map[string]any{{"name": "Legacy", "type": "codex", "profile_id": id, "status": "online"}},
	})).Want(http.StatusOK)
	req := newOwnerDaemonRequest("POST", "/", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": fresh,
		"failed_profiles": []map[string]any{{"profile_id": id, "reason": "missing"}},
	})
	testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id = $1 AND daemon_id = $2`, id, fresh); n != 0 {
		t.Fatalf("legacy placeholder spread: %d", n)
	}
}
