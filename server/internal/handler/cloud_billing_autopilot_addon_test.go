package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/featureflags"
)

// seedAutopilotQuotaPeriod gives the test workspace a quota period covering
// now, the row a paid add-on marks.
func seedAutopilotQuotaPeriod(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	cleanup := func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, testWorkspaceID)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := testPool.Exec(ctx, `INSERT INTO autopilot_quota_period (workspace_id, period_start, period_end)
		VALUES ($1, $2, $3)`, testWorkspaceID, start, start.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("seed quota period: %v", err)
	}
}

func autopilotQuotaLimitRaised(t *testing.T) bool {
	t.Helper()
	var raised bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT limit_raised_at IS NOT NULL FROM autopilot_quota_period WHERE workspace_id = $1`, testWorkspaceID,
	).Scan(&raised); err != nil {
		t.Fatalf("read quota period: %v", err)
	}
	return raised
}

func TestCloudWorkspaceAutopilotAddonCheckoutForwardsOnlyAllowlistedFields(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BillingWorkspaceSubscriptions, true)
	session := []byte(`{"session_id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`)
	proxy := &fakeCloudRuntimeProxy{enabled: true, resp: &cloudruntime.Response{StatusCode: http.StatusOK, Body: session}}
	useCloudRuntimeProxy(t, proxy)
	req := withCloudSubscriptionWorkspace(newRequest(http.MethodPost, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions", map[string]any{
		"workspace_id":         "00000000-0000-0000-0000-000000000001",
		"runs":                 999999,
		"success_url":          "https://evil.example/",
		"packs":                3,
		"expected_unit_amount": 1000,
		"currency":             "USD",
		"idempotency_key":      "addon-request-1",
	}), "owner")
	w := httptest.NewRecorder()
	testHandler.CreateCloudWorkspaceAutopilotAddonCheckout(w, req)

	if w.Code != http.StatusOK || w.Body.String() != string(session) || proxy.req.Method != http.MethodPost ||
		proxy.req.Path != "/api/v1/subscriptions/"+testWorkspaceID+"/autopilot-addon/checkout-sessions" ||
		proxy.req.UserID != testUserID {
		t.Fatalf("status=%d upstream=%s %s body=%s", w.Code, proxy.req.Method, proxy.req.Path, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(proxy.req.Body, &body); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"workspace_id", "runs", "success_url"} {
		if _, ok := body[leaked]; ok {
			t.Fatalf("client %s leaked upstream: %v", leaked, body)
		}
	}
	if body["packs"] != float64(3) || body["expected_unit_amount"] != float64(1000) ||
		body["currency"] != "usd" || body["idempotency_key"] != "addon-request-1" {
		t.Fatalf("upstream body=%v", body)
	}
	if proxy.req.Headers.Get(idempotencyKeyHeader) != "addon-request-1" {
		t.Fatalf("upstream idempotency=%q", proxy.req.Headers.Get(idempotencyKeyHeader))
	}
}

func TestCloudWorkspaceAutopilotAddonCheckoutValidatesLocally(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BillingWorkspaceSubscriptions, true)
	cases := map[string]map[string]any{
		"zero packs":          {"packs": 0, "expected_unit_amount": 1000, "currency": "usd", "idempotency_key": "k"},
		"too many packs":      {"packs": 11, "expected_unit_amount": 1000, "currency": "usd", "idempotency_key": "k"},
		"missing price":       {"packs": 1, "currency": "usd", "idempotency_key": "k"},
		"bad currency":        {"packs": 1, "expected_unit_amount": 1000, "currency": "us", "idempotency_key": "k"},
		"missing idempotency": {"packs": 1, "expected_unit_amount": 1000, "currency": "usd"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			proxy := &fakeCloudRuntimeProxy{enabled: true}
			useCloudRuntimeProxy(t, proxy)
			w := httptest.NewRecorder()
			testHandler.CreateCloudWorkspaceAutopilotAddonCheckout(w, withCloudSubscriptionWorkspace(
				newRequest(http.MethodPost, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions", body), "owner"))
			if w.Code != http.StatusBadRequest || proxy.called {
				t.Fatalf("status=%d called=%v body=%s", w.Code, proxy.called, w.Body.String())
			}
		})
	}

	member := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, member)
	w := httptest.NewRecorder()
	testHandler.CreateCloudWorkspaceAutopilotAddonCheckout(w, withCloudSubscriptionWorkspace(
		newRequest(http.MethodPost, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions", map[string]any{
			"packs": 1, "expected_unit_amount": 1000, "currency": "usd", "idempotency_key": "member",
		}), "member"))
	if w.Code != http.StatusForbidden || member.called {
		t.Fatalf("member checkout status=%d called=%v", w.Code, member.called)
	}
}

func TestCloudWorkspaceAutopilotAddonPaidSessionMarksTheQuotaPeriod(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BillingWorkspaceSubscriptions, true)
	read := func(status string) *httptest.ResponseRecorder {
		t.Helper()
		body := []byte(`{"session_id":"cs_test_1","status":"` + status + `","runs":1000}`)
		proxy := &fakeCloudRuntimeProxy{enabled: true, resp: &cloudruntime.Response{StatusCode: http.StatusOK, Body: body}}
		useCloudRuntimeProxy(t, proxy)
		req := withURLParam(withCloudSubscriptionWorkspace(
			newRequest(http.MethodGet, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions/cs_test_1", nil), "owner"),
			"sessionId", "cs_test_1")
		w := httptest.NewRecorder()
		testHandler.GetCloudWorkspaceAutopilotAddonCheckout(w, req)
		if w.Code != http.StatusOK || w.Body.String() != string(body) ||
			proxy.req.Path != "/api/v1/subscriptions/"+testWorkspaceID+"/autopilot-addon/checkout-sessions/cs_test_1" {
			t.Fatalf("status=%d upstream=%s body=%s", w.Code, proxy.req.Path, w.Body.String())
		}
		return w
	}

	seedAutopilotQuotaPeriod(t)
	read("open")
	if autopilotQuotaLimitRaised(t) {
		t.Fatal("an unpaid session must not mark the quota period")
	}
	read("paid")
	if !autopilotQuotaLimitRaised(t) {
		t.Fatal("a paid session must mark the quota period for other API instances")
	}

	invalid := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, invalid)
	req := withURLParam(withCloudSubscriptionWorkspace(
		newRequest(http.MethodGet, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions/x", nil), "owner"),
		"sessionId", "../../billing/balance")
	w := httptest.NewRecorder()
	testHandler.GetCloudWorkspaceAutopilotAddonCheckout(w, req)
	if w.Code != http.StatusBadRequest || invalid.called {
		t.Fatalf("invalid id status=%d called=%v", w.Code, invalid.called)
	}

	member := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, member)
	req = withURLParam(withCloudSubscriptionWorkspace(
		newRequest(http.MethodGet, "/api/cloud-subscriptions/autopilot-addon/checkout-sessions/cs_test_1", nil), "member"),
		"sessionId", "cs_test_1")
	w = httptest.NewRecorder()
	testHandler.GetCloudWorkspaceAutopilotAddonCheckout(w, req)
	if w.Code != http.StatusForbidden || member.called {
		t.Fatalf("member read status=%d called=%v", w.Code, member.called)
	}
}
