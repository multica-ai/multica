package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cloudruntime"
)

// proxyExpectation captures the assertions every standard
// cloud-billing endpoint shares: it must call the cloud proxy with a
// specific method/path, must stamp X-User-ID from the authenticated
// context, and must return the upstream response untouched.
//
// Reusing this table-driven helper keeps the per-endpoint tests small
// — the interesting per-endpoint logic lives in `withQuery` /
// `withBody` / dynamic-path-param branches.
type billingProxyCase struct {
	name   string
	method string
	path   string // path on OUR router, e.g. /api/cloud-billing/balance
	body   any    // nil for GET; map / struct for POST bodies
	wantPx string // expected upstream path
	wantQ  string // expected upstream query (encoded form), "" if none
	invoke func(t *testing.T, w http.ResponseWriter, r *http.Request)
}

// TestCloudBillingProxiesForwardCorrectly walks every standard
// endpoint at once: each one must hit the right upstream path with
// the right method and the caller's user id. Single test = single
// stub configuration; we just rotate which handler we invoke. This
// is the cheapest way to keep all 7 standard endpoints covered
// without duplicating the proxy plumbing per test.
func TestCloudBillingProxiesForwardCorrectly(t *testing.T) {
	cases := []billingProxyCase{
		{
			name:   "balance",
			method: http.MethodGet,
			path:   "/api/cloud-billing/balance",
			wantPx: "/api/v1/billing/balance",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.GetCloudBillingBalance(w, r)
			},
		},
		{
			name:   "transactions list with paging",
			method: http.MethodGet,
			path:   "/api/cloud-billing/transactions?page=2&page_size=50",
			wantPx: "/api/v1/billing/transactions",
			wantQ:  "page=2&page_size=50",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.ListCloudBillingTransactions(w, r)
			},
		},
		{
			name:   "batches list",
			method: http.MethodGet,
			path:   "/api/cloud-billing/batches?page_size=10",
			wantPx: "/api/v1/billing/batches",
			wantQ:  "page_size=10",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.ListCloudBillingBatches(w, r)
			},
		},
		{
			name:   "topups list",
			method: http.MethodGet,
			path:   "/api/cloud-billing/topups",
			wantPx: "/api/v1/billing/topups",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.ListCloudBillingTopups(w, r)
			},
		},
		{
			name:   "price tiers",
			method: http.MethodGet,
			path:   "/api/cloud-billing/price-tiers",
			wantPx: "/api/v1/billing/price-tiers",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.ListCloudBillingPriceTiers(w, r)
			},
		},
		{
			name:   "create checkout session",
			method: http.MethodPost,
			path:   "/api/cloud-billing/checkout-sessions",
			body:   map[string]any{"tier_id": "starter"},
			wantPx: "/api/v1/billing/checkout-sessions",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.CreateCloudBillingCheckoutSession(w, r)
			},
		},
		{
			name:   "create portal session",
			method: http.MethodPost,
			path:   "/api/cloud-billing/portal-sessions",
			wantPx: "/api/v1/billing/portal-sessions",
			invoke: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				testHandler.CreateCloudBillingPortalSession(w, r)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := &fakeCloudRuntimeProxy{
				enabled: true,
				resp: &cloudruntime.Response{
					StatusCode: http.StatusOK,
					Body:       []byte(`{"ok":true}`),
				},
			}
			useCloudRuntimeProxy(t, proxy)

			req := newRequest(tc.method, tc.path, tc.body)
			w := httptest.NewRecorder()
			tc.invoke(t, w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if !proxy.called {
				t.Fatal("expected cloud proxy to be called")
			}
			if proxy.req.Method != tc.method {
				t.Errorf("upstream method = %s, want %s", proxy.req.Method, tc.method)
			}
			if proxy.req.Path != tc.wantPx {
				t.Errorf("upstream path = %s, want %s", proxy.req.Path, tc.wantPx)
			}
			if proxy.req.UserID != testUserID {
				t.Errorf("upstream user_id = %q, want %q", proxy.req.UserID, testUserID)
			}
			if got := proxy.req.Query.Encode(); got != tc.wantQ {
				t.Errorf("upstream query = %q, want %q", got, tc.wantQ)
			}
			// Body should be present on POST cases and absent on GET.
			if tc.method == http.MethodPost && tc.body != nil && len(proxy.req.Body) == 0 {
				t.Error("expected upstream body on POST, got empty")
			}
			if tc.method == http.MethodGet && len(proxy.req.Body) > 0 {
				t.Errorf("upstream body should be empty on GET, got %s", proxy.req.Body)
			}
		})
	}
}

// TestGetCloudBillingCheckoutSession_AppendsSessionIDToPath pins the
// dynamic-path handler. The session id flows from chi URL param into
// the upstream URL, and the upstream therefore sees a different path
// than every other billing endpoint — easy to break by accident.
func TestGetCloudBillingCheckoutSession_AppendsSessionIDToPath(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"order_id":"o","status":"credited"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-billing/checkout-sessions/cs_test_abc", nil)
	req = withURLParam(req, "sessionId", "cs_test_abc")
	w := httptest.NewRecorder()

	testHandler.GetCloudBillingCheckoutSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.req.Path != "/api/v1/billing/checkout-sessions/cs_test_abc" {
		t.Errorf("upstream path = %s, want /api/v1/billing/checkout-sessions/cs_test_abc", proxy.req.Path)
	}
	if proxy.req.UserID != testUserID {
		t.Errorf("upstream user_id = %q", proxy.req.UserID)
	}
}

// TestGetCloudBillingCheckoutSession_RejectsPathTraversal pins the
// defensive bail when the session_id contains characters that would
// alter URL semantics. The cloud-runtime client rejects paths missing
// the leading slash but does not otherwise sanitize, so a stray `/`
// here would re-target the upstream request.
func TestGetCloudBillingCheckoutSession_RejectsPathTraversal(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, proxy)

	for _, sessionID := range []string{
		"cs_test/../admin",
		"cs?inject=1",
		"cs#frag",
	} {
		t.Run(sessionID, func(t *testing.T) {
			req := newRequest(http.MethodGet, "/api/cloud-billing/checkout-sessions/x", nil)
			req = withURLParam(req, "sessionId", sessionID)
			w := httptest.NewRecorder()
			testHandler.GetCloudBillingCheckoutSession(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if proxy.called {
				t.Fatal("upstream must not be called for invalid session_id")
			}
		})
	}
}

// TestGetCloudBillingCheckoutSession_MissingPathParamReturns400 pins
// the no-id branch (defensive — chi shouldn't route to us without a
// param, but we guard anyway).
func TestGetCloudBillingCheckoutSession_MissingPathParamReturns400(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-billing/checkout-sessions/", nil)
	// No URL param injected.
	w := httptest.NewRecorder()
	testHandler.GetCloudBillingCheckoutSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("upstream must not be called when session_id is missing")
	}
}

// TestCloudBillingDisabledReturnsUnavailable confirms self-hosted
// deployments (no cloud URL configured) get a clean 503 rather than
// a cryptic upstream error.
func TestCloudBillingDisabledReturnsUnavailable(t *testing.T) {
	useCloudRuntimeProxy(t, &fakeCloudRuntimeProxy{enabled: false})

	req := newRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
	w := httptest.NewRecorder()
	testHandler.GetCloudBillingBalance(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

// --- Stripe webhook ---

// TestStripeWebhookForwardsRawBodyAndSignature is the critical
// invariant for the webhook proxy: bytes go upstream byte-for-byte,
// and the Stripe-Signature header rides along. Even one stray
// transformation breaks Stripe's HMAC verification on the cloud side
// and leaves topups stuck in `pending` forever.
//
// We deliberately use a body that includes leading whitespace, a
// trailing newline, and unusual key ordering to catch any
// json.Unmarshal/Marshal round-trip the JSON proxy might
// inadvertently apply.
func TestStripeWebhookForwardsRawBodyAndSignature(t *testing.T) {
	rawBody := "  \n{\"id\":\"evt_test\",\"type\":\"checkout.session.completed\"}\n"
	const sig = "t=1700000000,v1=deadbeef0000aaaa"
	const ct = "application/json; charset=utf-8"

	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"received":true}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe", strings.NewReader(rawBody))
	req.Header.Set("Stripe-Signature", sig)
	req.Header.Set("Content-Type", ct)
	// Deliberately NO X-User-ID — the webhook must work without auth.
	w := httptest.NewRecorder()

	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !proxy.called {
		t.Fatal("upstream proxy must be called")
	}
	if proxy.req.Method != http.MethodPost || proxy.req.Path != "/api/v1/webhooks/stripe" {
		t.Fatalf("upstream %s %s", proxy.req.Method, proxy.req.Path)
	}
	if string(proxy.req.Body) != rawBody {
		t.Fatalf("upstream body = %q, want %q (byte-perfect)", proxy.req.Body, rawBody)
	}
	if got := proxy.req.Headers.Get("Stripe-Signature"); got != sig {
		t.Fatalf("upstream Stripe-Signature = %q, want %q", got, sig)
	}
	// Content-Type must arrive verbatim — preserving the
	// `; charset=utf-8` suffix Stripe always sends. cloudruntime's
	// default would otherwise strip it to plain `application/json`.
	if got := proxy.req.Headers.Get("Content-Type"); got != ct {
		t.Fatalf("upstream Content-Type = %q, want %q", got, ct)
	}
	if proxy.req.UserID != "" {
		t.Errorf("upstream user_id should be empty for webhook, got %q", proxy.req.UserID)
	}
}

// TestStripeWebhookMissingSignatureRejectedLocally pins the early
// 401 we now return when Stripe-Signature is absent. Real Stripe
// deliveries ALWAYS include the header; absence ≡ not from Stripe.
// Rejecting locally saves the upstream RTT (and prevents using us
// as a DoS amplifier against the cloud Billing service).
//
// The contract is documented in HandleCloudBillingStripeWebhook:
// our local 401 is a strict superset of what the upstream would do
// in this case, so Stripe's delivery dashboard sees the same
// outcome it would have if the request had reached cloud.
func TestStripeWebhookMissingSignatureRejectedLocally(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, proxy)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe",
		strings.NewReader(`{"id":"evt"}`))
	w := httptest.NewRecorder()
	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("upstream must NOT be called when Stripe-Signature is missing")
	}
}

// TestStripeWebhookForwardsEmptyBody confirms we don't pre-reject an
// empty body — Stripe's webhook tester sometimes sends pings, and the
// upstream is the source of truth for what's an acceptable payload.
// (We do still cap large bodies; that's a separate test.) The
// signature header is set because, post-fix, the absence of it is
// itself a 401 — see TestStripeWebhookMissingSignatureRejectedLocally.
func TestStripeWebhookForwardsEmptyBody(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":"empty body"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe", http.NoBody)
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	w := httptest.NewRecorder()
	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if !proxy.called {
		t.Fatal("upstream must be called even on empty body")
	}
	if len(proxy.req.Body) != 0 {
		t.Errorf("upstream body = %q, want empty", proxy.req.Body)
	}
}

// TestStripeWebhookRejectsLargeBody pins the body cap. Stripe's
// real payloads are well under 1 MiB; an attacker (or a misconfigured
// sender) flooding us with multi-MB bodies must be cut off before we
// buffer the whole thing in memory, and before we spend a Cloud
// upstream round-trip on a doomed verification.
func TestStripeWebhookRejectsLargeBody(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, proxy)

	body := bytes.NewReader(bytes.Repeat([]byte("a"), maxStripeWebhookBodySize+1))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe", body)
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	w := httptest.NewRecorder()
	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("upstream must not be called for oversized webhook body")
	}
}

// TestStripeWebhookDisabledReturnsUnavailable mirrors the
// cloud-runtime disabled test but for the webhook path. Self-hosted
// deployments without a cloud URL must return 503, not crash.
func TestStripeWebhookDisabledReturnsUnavailable(t *testing.T) {
	useCloudRuntimeProxy(t, &fakeCloudRuntimeProxy{enabled: false})

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe",
		strings.NewReader(`{"id":"evt"}`))
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	w := httptest.NewRecorder()
	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestStripeWebhookRateLimited pins the per-IP rate-limit fast
// path. With a denying limiter installed the handler must 429
// BEFORE consulting upstream. Mirrors HandleAutopilotWebhook's
// behaviour: the public webhook ingress sits behind the same
// WebhookIPRateLimiter so a flood of bogus requests doesn't burn
// cloud-side budget.
func TestStripeWebhookRateLimited(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{enabled: true}
	useCloudRuntimeProxy(t, proxy)

	prevLimiter := testHandler.WebhookIPRateLimiter
	testHandler.WebhookIPRateLimiter = denyingWebhookIPRateLimiter{}
	t.Cleanup(func() { testHandler.WebhookIPRateLimiter = prevLimiter })

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/stripe",
		strings.NewReader(`{"id":"evt"}`))
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	req.RemoteAddr = "203.0.113.7:1234"
	w := httptest.NewRecorder()
	testHandler.HandleCloudBillingStripeWebhook(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("upstream must not be called when rate limited")
	}
}

// denyingWebhookIPRateLimiter is the smallest possible limiter that
// always says "no". It exists to drive the 429 branch without
// requiring a Redis test instance — the limiter interface is the
// same one HandleAutopilotWebhook uses.
type denyingWebhookIPRateLimiter struct{}

func (denyingWebhookIPRateLimiter) Allow(_ context.Context, _ string) bool { return false }
