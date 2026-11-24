package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/featureflags"
)

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

func TestCloudWorkspaceAutopilotAddonSessionStatusIsForwarded(t *testing.T) {
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

	read("open")
	read("paid")

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
