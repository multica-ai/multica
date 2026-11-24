package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func runtimeShare(t *testing.T, runtimeID string) (string, []string) {
	t.Helper()
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, runtimeID))
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	return rt.Visibility, runtimeSharedWithStrings(rt.SharedWith)
}

func wantRuntimeShare(t *testing.T, runtimeID, visibility string, sharedWith ...string) {
	t.Helper()
	gotVisibility, got := runtimeShare(t, runtimeID)
	if gotVisibility != visibility || len(got) != len(sharedWith) {
		t.Fatalf("runtime access = %s %v, want %s %v", gotVisibility, got, visibility, sharedWith)
	}
	for _, id := range sharedWith {
		found := false
		for _, g := range got {
			found = found || g == id
		}
		if !found {
			t.Fatalf("runtime access = %s %v, want %s %v", gotVisibility, got, visibility, sharedWith)
		}
	}
}

// Sharing a private runtime with a member under each action.
func TestRuntimeMemberSharingAddUnderEachAction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range featureCases {
		t.Run(tc.name, func(t *testing.T) {
			rtID, ownerID, memberID := runtimeVisibilityFixture(t)
			usePaidFeatureOn(t, testHandler, entitlement.FeatureRuntimeMemberSharing, tc.decision)
			resp := runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}})
			if tc.locked {
				wantFeatureLockedFor(t, resp, entitlement.FeatureRuntimeMemberSharing)
				wantRuntimeShare(t, rtID, "private")
				wantAttempts(t, entitlement.FeatureRuntimeMemberSharing, featureAttemptCounts{enforced: 1, lastOperation: "add_members"})
				return
			}
			resp.Want(http.StatusOK)
			wantRuntimeShare(t, rtID, "private", memberID)
			want := featureAttemptCounts{}
			if tc.observed > 0 {
				want = featureAttemptCounts{observed: 1, lastOperation: "add_members"}
			}
			wantAttempts(t, entitlement.FeatureRuntimeMemberSharing, want)
		})
	}
}

// After a downgrade a runtime's member list keeps working; removing people,
// making it public or "only me", and re-saving are allowed; adding someone,
// or going from public back to specific members, is refused.
func TestRuntimeMemberSharingEnforcedKeepsExistingAndAllowsReductions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	third := createPermissionTestMember(t, "rms-third-"+uuid.NewString()[:8]+"@multica.test")
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID, testUserID}}).Want(http.StatusOK)

	usePaidFeatureOn(t, testHandler, entitlement.FeatureRuntimeMemberSharing, enforced())

	// The shared member can still create agents on it.
	// The revocation below unbinds the agent, so clean it up by owner.
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE owner_id = $1`, memberID) })
	testutil.Call(t, testHandler.CreateAgent, newRequestAs(memberID, http.MethodPost, "/api/agents", map[string]any{
		"name": "rms shared member " + uuid.NewString()[:8], "runtime_id": rtID,
	})).Want(http.StatusCreated)
	testutil.Call(t, testHandler.CreateAgent, newRequestAs(third, http.MethodPost, "/api/agents", map[string]any{
		"name": "rms outsider " + uuid.NewString()[:8], "runtime_id": rtID,
	})).Want(http.StatusForbidden)

	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{testUserID, memberID}}).Want(http.StatusOK)
	wantFeatureLockedFor(t, runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID, testUserID, third}}), entitlement.FeatureRuntimeMemberSharing)
	wantRuntimeShare(t, rtID, "private", memberID, testUserID)
	// testUserID has no agent on the runtime, so removing them needs no review.
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusOK)
	wantRuntimeShare(t, rtID, "private", memberID)
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"visibility": "public"}).Want(http.StatusOK)
	wantRuntimeShare(t, rtID, "public")
	// Public back to specific members: refused on both entry points.
	wantFeatureLockedFor(t, runtimeSharingPatch(t, rtID, ownerID, map[string]any{"visibility": "private", "shared_with": []string{memberID}}), entitlement.FeatureRuntimeMemberSharing)
	wantFeatureLockedFor(t, runtimePrivacyRequest(t, rtID, ownerID, map[string]any{
		"shared_with": []string{memberID}, "expected_nonowner_agent_ids": []string{}, "expected_task_ids": []string{},
	}), entitlement.FeatureRuntimeMemberSharing)
	wantRuntimeShare(t, rtID, "public")
	// Public to "only me" is a reduction (the member's agent is reviewed first).
	var plan struct {
		Agents []struct {
			ID string `json:"id"`
		} `json:"nonowner_agents"`
	}
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"visibility": "private"}).Want(http.StatusConflict).JSON(&plan)
	expected := []string{}
	for _, a := range plan.Agents {
		expected = append(expected, a.ID)
	}
	runtimePrivacyRequest(t, rtID, ownerID, map[string]any{
		"expected_nonowner_agent_ids": expected, "expected_task_ids": []string{},
	}).Want(http.StatusOK)
	wantRuntimeShare(t, rtID, "private")

	wantAttempts(t, entitlement.FeatureRuntimeMemberSharing, featureAttemptCounts{enforced: 3, lastOperation: "add_members"})
}

// Only the runtime owner may change sharing; anyone else gets the original
// error before the feature is consulted.
func TestRuntimeMemberSharingPermissionChecksComeFirst(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rtID, _, memberID := runtimeVisibilityFixture(t)
	usePaidFeatureOn(t, testHandler, entitlement.FeatureRuntimeMemberSharing, enforced())
	// testUserID administers the workspace but does not own the runtime.
	wantPlainForbidden(t, runtimeSharingPatch(t, rtID, testUserID, map[string]any{"shared_with": []string{memberID}}))
	wantPlainForbidden(t, runtimePrivacyRequest(t, rtID, testUserID, map[string]any{
		"shared_with": []string{memberID}, "expected_nonowner_agent_ids": []string{}, "expected_task_ids": []string{},
	}))
	wantNoAttempts(t, entitlement.FeatureRuntimeMemberSharing)
}

func TestRuntimeMemberSharingAddsClassification(t *testing.T) {
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	owner, a, b := id(), id(), id()
	rt := func(visibility string, shared ...pgtype.UUID) db.AgentRuntime {
		return db.AgentRuntime{OwnerID: owner, Visibility: visibility, SharedWith: shared}
	}
	for _, tc := range []struct {
		name           string
		previous, next db.AgentRuntime
		want           bool
	}{
		{"only me to members", rt("private"), rt("private", a), true},
		{"public to members", rt("public"), rt("private", a), true},
		{"add", rt("private", a), rt("private", a, b), true},
		{"swap", rt("private", a), rt("private", b), true},
		{"same", rt("private", a, b), rt("private", a, b), false},
		{"remove", rt("private", a, b), rt("private", a), false},
		{"to only me", rt("private", a), rt("private"), false},
		{"to public", rt("private", a), rt("public"), false},
		{"public to only me", rt("public"), rt("private"), false},
	} {
		if got := runtimeMemberSharingAdds(tc.previous, tc.next); got != tc.want {
			t.Errorf("%s: adds = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The addition is decided against the locked row, not the copy read before
// the transaction: a concurrent save that removed someone cannot be undone by
// a stale request putting them back while the feature is enforced.
func TestRuntimeMemberSharingDecidesOnLockedRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusOK)
	stale, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, rtID))
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	// Another save removes the member after this request read the runtime.
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{}}).Want(http.StatusOK)

	usePaidFeatureOn(t, testHandler, entitlement.FeatureRuntimeMemberSharing, enforced())
	owner, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: mustTestUUID(t, ownerID), WorkspaceID: mustTestUUID(t, testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load owner member: %v", err)
	}
	shared := []string{memberID}
	req := UpdateAgentRuntimeRequest{SharedWith: &shared, sharedWithIDs: []pgtype.UUID{mustTestUUID(t, memberID)}}
	sharing := testHandler.paidFeature(context.Background(), stale.WorkspaceID, entitlement.FeatureRuntimeMemberSharing)
	_, _, _, _, err = testHandler.updateRuntimeAccessIfNoDependencies(context.Background(), stale, owner, req, sharing)
	if err != errRuntimeFeatureLocked {
		t.Fatalf("stale re-add err = %v, want errRuntimeFeatureLocked", err)
	}
	wantRuntimeShare(t, rtID, "private")
}
