package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Shared helpers for the Business paid features wired in MUL-9491:
// agent_invocation_members, runtime_member_sharing and team_app_accounts.

// usePaidFeatureOn points h at a stub that returns decision for feature in
// the test workspace (every other feature stays off), and clears the
// workspace's recorded attempts before and after. A nil decision leaves h
// without a provider, as on a self-hosted deployment.
func usePaidFeatureOn(t *testing.T, h *Handler, feature entitlement.FeatureName, decision *entitlement.FeatureDecision) {
	t.Helper()
	prior := h.Features
	if decision == nil {
		h.Features = nil
	} else {
		stub := entitlementtest.New()
		stub.SetFeature(uuid.MustParse(testWorkspaceID), feature, *decision)
		h.Features = stub
	}
	clearFeatureAttempts(t)
	t.Cleanup(func() {
		h.Features = prior
		clearFeatureAttempts(t)
	})
}

// featureCases is the matrix every feature runs an addition through:
// self-hosted (no provider), off, observe and enforce.
var featureCases = []struct {
	name     string
	decision *entitlement.FeatureDecision
	locked   bool
	observed int64
}{
	{"self-hosted", nil, false, 0},
	{"off", &entitlement.FeatureDecision{Action: entitlement.ActionOff, Reason: entitlement.ReasonRefreshed}, false, 0},
	{"observe", &entitlement.FeatureDecision{Action: entitlement.ActionObserve, Reason: entitlement.ReasonRefreshed, PolicyRevision: 7}, false, 1},
	{"enforce", &entitlement.FeatureDecision{Action: entitlement.ActionEnforce, Reason: entitlement.ReasonRefreshed, PolicyRevision: 7}, true, 0},
}

func enforced() *entitlement.FeatureDecision {
	return &entitlement.FeatureDecision{Action: entitlement.ActionEnforce, Reason: entitlement.ReasonRefreshed, PolicyRevision: 7}
}

func observed() *entitlement.FeatureDecision {
	return &entitlement.FeatureDecision{Action: entitlement.ActionObserve, Reason: entitlement.ReasonRefreshed, PolicyRevision: 7}
}

func featureAttemptsFor(t *testing.T, feature entitlement.FeatureName) featureAttemptCounts {
	t.Helper()
	var c featureAttemptCounts
	err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(observed_count), 0), COALESCE(SUM(degraded_count), 0), COALESCE(SUM(enforced_count), 0),
		       COALESCE(MAX(last_operation), '')
		FROM entitlement_feature_attempt
		WHERE workspace_id = $1 AND feature = $2`, testWorkspaceID, string(feature),
	).Scan(&c.observed, &c.degraded, &c.enforced, &c.lastOperation)
	if err != nil {
		t.Fatalf("read feature attempts: %v", err)
	}
	return c
}

func wantAttempts(t *testing.T, feature entitlement.FeatureName, want featureAttemptCounts) {
	t.Helper()
	if got := featureAttemptsFor(t, feature); got != want {
		t.Fatalf("%s attempts = %+v, want %+v", feature, got, want)
	}
}

func wantNoAttempts(t *testing.T, feature entitlement.FeatureName) {
	t.Helper()
	wantAttempts(t, feature, featureAttemptCounts{})
}

func wantFeatureLockedFor(t *testing.T, resp *testutil.Response, feature entitlement.FeatureName) {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Feature string `json:"feature"`
	}
	resp.Want(http.StatusForbidden).JSON(&body)
	if body.Code != "feature_locked" || body.Feature != string(feature) {
		t.Fatalf("locked body = %+v, want feature_locked/%s", body, feature)
	}
}

// wantPlainForbidden asserts the ordinary permission error, not the paid
// feature: the restriction runs after every permission check.
func wantPlainForbidden(t *testing.T, resp *testutil.Response) {
	t.Helper()
	resp.Want(http.StatusForbidden)
	if strings.Contains(resp.Text(), "feature_locked") {
		t.Fatalf("permission failure answered with the paid feature: %s", resp.Text())
	}
}
