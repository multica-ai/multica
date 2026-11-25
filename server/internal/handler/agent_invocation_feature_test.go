package handler

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func memberTargets(ids ...string) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"target_type": "member", "target_id": id})
	}
	return out
}

func workspaceTarget() []map[string]any {
	return []map[string]any{{"target_type": "workspace", "target_id": testWorkspaceID}}
}

func createAgentWithAccessAs(t *testing.T, userID, runtimeID, name, mode string, targets []map[string]any) *testutil.Response {
	t.Helper()
	body := map[string]any{"name": name, "runtime_id": runtimeID, "permission_mode": mode}
	if targets != nil {
		body["invocation_targets"] = targets
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1 AND name = $2)`, testWorkspaceID, name)
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
	})
	return testutil.Call(t, testHandler.CreateAgent, newRequestAs(userID, http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, body))
}

func agentCountByName(t *testing.T, name string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name).Scan(&n); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	return n
}

func updateAgentAccessAs(t *testing.T, userID, agentID, mode string, targets []map[string]any) *testutil.Response {
	t.Helper()
	body := map[string]any{"permission_mode": mode}
	if targets != nil {
		body["invocation_targets"] = targets
	}
	req := withURLParam(newRequestAs(userID, http.MethodPut, "/api/agents/"+agentID, body), "id", agentID)
	return testutil.Call(t, testHandler.UpdateAgent, req)
}

// agentAccess is an agent's stored permission mode and sorted target keys.
func agentAccess(t *testing.T, agentID string) (string, []string) {
	t.Helper()
	ctx := context.Background()
	a, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	rows, err := testHandler.Queries.ListAgentInvocationTargets(ctx, a.ID)
	if err != nil {
		t.Fatalf("load targets: %v", err)
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.TargetType+":"+uuidToString(row.TargetID))
	}
	sort.Strings(keys)
	return a.PermissionMode, keys
}

func memberKeys(ids ...string) []string {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, "member:"+id)
	}
	sort.Strings(keys)
	return keys
}

func wantAgentAccess(t *testing.T, agentID, wantMode string, wantKeys []string) {
	t.Helper()
	mode, keys := agentAccess(t, agentID)
	if mode != wantMode || len(keys) != len(wantKeys) {
		t.Fatalf("access = %s %v, want %s %v", mode, keys, wantMode, wantKeys)
	}
	for i := range keys {
		if keys[i] != wantKeys[i] {
			t.Fatalf("access = %s %v, want %s %v", mode, keys, wantMode, wantKeys)
		}
	}
}

// Creating an agent with a member list under each action: allowed when
// self-hosted, off or observed (observe records it), rejected under enforce
// without writing an agent.
func TestAgentInvocationMembersCreateUnderEachAction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	member := createPermissionTestMember(t, "aim-create-"+uuid.NewString()[:8]+"@multica.test")
	runtimeID := handlerTestRuntimeID(t)
	for _, tc := range featureCases {
		t.Run(tc.name, func(t *testing.T) {
			usePaidFeatureOn(t, testHandler, entitlement.FeatureAgentInvocationMembers, tc.decision)
			name := "aim create " + tc.name + " " + uuid.NewString()[:8]
			resp := createAgentWithAccessAs(t, testUserID, runtimeID, name, "public_to", memberTargets(member))
			if tc.locked {
				wantFeatureLockedFor(t, resp, entitlement.FeatureAgentInvocationMembers)
				if n := agentCountByName(t, name); n != 0 {
					t.Fatalf("rejected create wrote %d agents", n)
				}
				wantAttempts(t, entitlement.FeatureAgentInvocationMembers, featureAttemptCounts{enforced: 1, lastOperation: "create"})
				return
			}
			resp.Want(http.StatusCreated)
			want := featureAttemptCounts{}
			if tc.observed > 0 {
				want = featureAttemptCounts{observed: 1, lastOperation: "create"}
			}
			wantAttempts(t, entitlement.FeatureAgentInvocationMembers, want)
		})
	}
}

// "Only me" and "whole workspace" are free: creating either is never
// restricted or recorded.
func TestAgentInvocationMembersFreeCreatesIgnoreFeature(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	usePaidFeatureOn(t, testHandler, entitlement.FeatureAgentInvocationMembers, enforced())
	runtimeID := handlerTestRuntimeID(t)
	createAgentWithAccessAs(t, testUserID, runtimeID, "aim free private "+uuid.NewString()[:8], "private", nil).Want(http.StatusCreated)
	createAgentWithAccessAs(t, testUserID, runtimeID, "aim free workspace "+uuid.NewString()[:8], "public_to", workspaceTarget()).Want(http.StatusCreated)
	// An empty public_to normalises to the whole workspace.
	createAgentWithAccessAs(t, testUserID, runtimeID, "aim free empty "+uuid.NewString()[:8], "public_to", []map[string]any{}).Want(http.StatusCreated)
	wantNoAttempts(t, entitlement.FeatureAgentInvocationMembers)
}

// After a downgrade an agent's member list keeps working exactly as
// configured. Removing people, switching to "only me" or "whole workspace",
// and re-saving are allowed; adding someone, or going back to a member list
// from "only me" or "whole workspace", is refused and leaves access unchanged.
func TestAgentInvocationMembersEnforcedKeepsExistingAndAllowsReductions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPermissionTestMember(t, "aim-a-"+uuid.NewString()[:8]+"@multica.test")
	b := createPermissionTestMember(t, "aim-b-"+uuid.NewString()[:8]+"@multica.test")
	c := createPermissionTestMember(t, "aim-c-"+uuid.NewString()[:8]+"@multica.test")
	var created AgentResponse
	createAgentWithAccessAs(t, testUserID, handlerTestRuntimeID(t), "aim enforced "+uuid.NewString()[:8], "public_to", memberTargets(a, b)).
		Want(http.StatusCreated).JSON(&created)

	usePaidFeatureOn(t, testHandler, entitlement.FeatureAgentInvocationMembers, enforced())

	agentRow, err := testHandler.Queries.GetAgent(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	for _, tc := range []struct {
		user string
		want bool
	}{{a, true}, {b, true}, {c, false}} {
		if got := testHandler.canInvokeAgent(ctx, agentRow, "member", tc.user, "", testWorkspaceID); got != tc.want {
			t.Fatalf("canInvokeAgent(%s) = %v under enforce, want %v", tc.user, got, tc.want)
		}
	}

	// Same list re-saved: allowed.
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(b, a)).Want(http.StatusOK)
	// Adding someone: refused, unchanged.
	wantFeatureLockedFor(t, updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a, b, c)), entitlement.FeatureAgentInvocationMembers)
	wantAgentAccess(t, created.ID, "public_to", memberKeys(a, b))
	// Swapping someone out for someone new is still an addition.
	wantFeatureLockedFor(t, updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a, c)), entitlement.FeatureAgentInvocationMembers)
	// Removing someone: allowed.
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a)).Want(http.StatusOK)
	wantAgentAccess(t, created.ID, "public_to", memberKeys(a))
	// Whole workspace, then back to a member list: the second is an addition.
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", workspaceTarget()).Want(http.StatusOK)
	wantFeatureLockedFor(t, updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a)), entitlement.FeatureAgentInvocationMembers)
	// Only me, then back to a member list: the second is an addition.
	updateAgentAccessAs(t, testUserID, created.ID, "private", nil).Want(http.StatusOK)
	wantFeatureLockedFor(t, updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a)), entitlement.FeatureAgentInvocationMembers)
	wantAgentAccess(t, created.ID, "private", []string{})
	// An emptied list is the whole workspace, which is free.
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", []map[string]any{}).Want(http.StatusOK)
	wantAgentAccess(t, created.ID, "public_to", []string{"workspace:" + testWorkspaceID})

	wantAttempts(t, entitlement.FeatureAgentInvocationMembers, featureAttemptCounts{enforced: 4, lastOperation: "add_members"})
}

// An observed addition is admitted and recorded once, after the write.
func TestAgentInvocationMembersObservedUpdateIsRecorded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	a := createPermissionTestMember(t, "aim-obs-a-"+uuid.NewString()[:8]+"@multica.test")
	b := createPermissionTestMember(t, "aim-obs-b-"+uuid.NewString()[:8]+"@multica.test")
	var created AgentResponse
	createAgentWithAccessAs(t, testUserID, handlerTestRuntimeID(t), "aim observe "+uuid.NewString()[:8], "public_to", memberTargets(a)).
		Want(http.StatusCreated).JSON(&created)
	usePaidFeatureOn(t, testHandler, entitlement.FeatureAgentInvocationMembers, observed())
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(a, b)).Want(http.StatusOK)
	wantAgentAccess(t, created.ID, "public_to", memberKeys(a, b))
	// A reduction is not recorded.
	updateAgentAccessAs(t, testUserID, created.ID, "public_to", memberTargets(b)).Want(http.StatusOK)
	wantAttempts(t, entitlement.FeatureAgentInvocationMembers, featureAttemptCounts{observed: 1, lastOperation: "add_members"})
}

// The restriction runs after the ordinary permission checks: a caller who
// could not make the change gets the original 403 and nothing is recorded.
func TestAgentInvocationMembersPermissionChecksComeFirst(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createPermissionTestMember(t, "aim-owner-"+uuid.NewString()[:8]+"@multica.test")
	other := createPermissionTestMember(t, "aim-other-"+uuid.NewString()[:8]+"@multica.test")
	usePaidFeatureOn(t, testHandler, entitlement.FeatureAgentInvocationMembers, enforced())

	// A workspace admin who does not own the agent may not change its access.
	agentID := dbfx.Agent(t, "aim not mine "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": owner, "permission_mode": "private", "visibility": "private",
	})
	wantPlainForbidden(t, updateAgentAccessAs(t, testUserID, agentID, "public_to", memberTargets(other)))

	// A private runtime the caller cannot use is refused before the feature.
	runtimeID := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "aim private runtime " + uuid.NewString()[:8],
		"runtime_mode": "local", "provider": "claude", "status": "online", "device_info": "test",
		"metadata": testutil.Raw(`'{}'::jsonb`), "owner_id": owner, "visibility": "private",
	})
	wantPlainForbidden(t, createAgentWithAccessAs(t, other, runtimeID, "aim no runtime "+uuid.NewString()[:8], "public_to", memberTargets(owner)))

	wantNoAttempts(t, entitlement.FeatureAgentInvocationMembers)
}

func TestAgentInvocationMembersAddsClassification(t *testing.T) {
	ws := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	a := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	b := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	team := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	members := func(ids ...pgtype.UUID) resolvedPermission {
		p := resolvedPermission{mode: permissionModePublicTo}
		for _, id := range ids {
			p.targets = append(p.targets, targetSpec{targetType: invocationTargetMember, targetID: id})
		}
		return p
	}
	rows := func(specs ...targetSpec) []db.AgentInvocationTarget {
		out := make([]db.AgentInvocationTarget, 0, len(specs))
		for _, s := range specs {
			out = append(out, db.AgentInvocationTarget{TargetType: s.targetType, TargetID: s.targetID})
		}
		return out
	}
	workspace := targetSpec{targetType: invocationTargetWorkspace, targetID: ws}
	memberA := targetSpec{targetType: invocationTargetMember, targetID: a}
	memberB := targetSpec{targetType: invocationTargetMember, targetID: b}
	for _, tc := range []struct {
		name       string
		beforeMode string
		before     []db.AgentInvocationTarget
		after      resolvedPermission
		want       bool
	}{
		{"create private", permissionModePrivate, nil, resolvedPermission{mode: permissionModePrivate}, false},
		{"create workspace", permissionModePrivate, nil, resolvedPermission{mode: permissionModePublicTo, targets: []targetSpec{workspace}}, false},
		{"create members", permissionModePrivate, nil, members(a), true},
		{"create team", permissionModePrivate, nil, resolvedPermission{mode: permissionModePublicTo, targets: []targetSpec{{targetType: invocationTargetTeam, targetID: team}}}, true},
		{"workspace plus members is workspace", permissionModePrivate, nil, resolvedPermission{mode: permissionModePublicTo, targets: []targetSpec{memberA, workspace}}, false},
		{"same list", permissionModePublicTo, rows(memberA, memberB), members(b, a), false},
		{"remove", permissionModePublicTo, rows(memberA, memberB), members(a), false},
		{"add", permissionModePublicTo, rows(memberA), members(a, b), true},
		{"swap", permissionModePublicTo, rows(memberA), members(b), true},
		{"workspace to members", permissionModePublicTo, rows(workspace), members(a), true},
		{"workspace and member to the same member", permissionModePublicTo, rows(workspace, memberA), members(a), true},
		{"private to members", permissionModePrivate, nil, members(a), true},
		{"members to private", permissionModePublicTo, rows(memberA), resolvedPermission{mode: permissionModePrivate}, false},
		{"members to workspace", permissionModePublicTo, rows(memberA), resolvedPermission{mode: permissionModePublicTo, targets: []targetSpec{workspace}}, false},
	} {
		if got := agentInvocationMembersAdds(tc.beforeMode, tc.before, tc.after); got != tc.want {
			t.Errorf("%s: adds = %v, want %v", tc.name, got, tc.want)
		}
	}
}
