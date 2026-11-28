package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cloudruntime"
)

type fakeCloudRuntimeProxy struct {
	enabled bool
	req     cloudruntime.Request
	resp    *cloudruntime.Response
	err     error
	called  bool
}

func (f *fakeCloudRuntimeProxy) Enabled() bool {
	return f.enabled
}

func (f *fakeCloudRuntimeProxy) Do(ctx context.Context, req cloudruntime.Request) (*cloudruntime.Response, error) {
	f.called = true
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func useCloudRuntimeProxy(t *testing.T, proxy cloudRuntimeProxy) {
	t.Helper()

	prevProxy := testHandler.CloudRuntime
	testHandler.CloudRuntime = proxy
	t.Cleanup(func() { testHandler.CloudRuntime = prevProxy })
}

func TestProxyCloudRuntimeForwardsBodyAndRequestID(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"X-Request-Id": []string{"cloud-request-id"}},
			Body:       []byte(`{"status":"ok"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodPost, "/api/cloud-billing/checkout-sessions", map[string]any{
		"amount": 10,
	})
	req.Header.Set("X-Request-ID", "api-request-id")
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodPost, "/api/v1/billing/checkout-sessions", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !proxy.called {
		t.Fatal("cloud proxy was not called")
	}
	if proxy.req.Method != http.MethodPost || proxy.req.Path != "/api/v1/billing/checkout-sessions" {
		t.Fatalf("proxied request = %s %s", proxy.req.Method, proxy.req.Path)
	}
	if proxy.req.UserID != testUserID {
		t.Fatalf("proxied user id = %q", proxy.req.UserID)
	}
	if proxy.req.RequestID != "api-request-id" {
		t.Fatalf("proxied request id = %q", proxy.req.RequestID)
	}
	if got := w.Header().Get("X-Request-ID"); got != "cloud-request-id" {
		t.Fatalf("response request id = %q", got)
	}
}

func TestProxyCloudRuntimeDisabledReturnsForbidden(t *testing.T) {
	useCloudRuntimeProxy(t, &fakeCloudRuntimeProxy{enabled: false})

	req := newRequest(http.MethodGet, "/api/cloud-billing/balance", nil)
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodGet, "/api/v1/billing/balance", cloudRuntimeProxyOptions{withUserID: true})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != "cloud_runtime_not_configured" {
		t.Fatalf("unexpected disabled response: body=%v err=%v", body, err)
	}
}

func TestProxyCloudRuntimeForwardsQuery(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`[]`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-billing/transactions?limit=10&offset=20", nil)
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodGet, "/api/v1/billing/transactions", cloudRuntimeProxyOptions{
		withUserID: true,
		withQuery:  true,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := proxy.req.Query.Encode(); got != "limit=10&offset=20" {
		t.Fatalf("proxied query = %q", got)
	}
}

func TestProxyCloudRuntimeNonJSONResponseIsWrapped(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusBadGateway,
			Body:       []byte("upstream failed\n"),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-billing/price-tiers", nil)
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodGet, "/api/v1/billing/price-tiers", cloudRuntimeProxyOptions{})

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	if got := w.Body.String(); !strings.Contains(got, `"error":"upstream failed"`) {
		t.Fatalf("body = %s", got)
	}
}

func TestProxyCloudRuntimeEmptyResponseKeepsStatus(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusNoContent,
			Body:       nil,
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-billing/price-tiers", nil)
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodGet, "/api/v1/billing/price-tiers", cloudRuntimeProxyOptions{})

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body = %s", body)
	}
}

func TestProxyCloudRuntimeRejectsLargeBody(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Body:       []byte(`{"status":"ok"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	body := bytes.NewReader(bytes.Repeat([]byte("a"), maxCloudRuntimeRequestBodySize+1))
	req := httptest.NewRequest(http.MethodPost, "/api/cloud-billing/checkout-sessions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	testHandler.proxyCloudRuntime(w, req, http.MethodPost, "/api/v1/billing/checkout-sessions", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("cloud proxy should not be called")
	}
}
