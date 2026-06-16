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

// TestCreateCloudRuntimeNodeForwardsBody is the post-MUL-2671 happy
// path for CreateCloudRuntimeNode: the handler no longer reads, asks
// for, or auto-generates an mul_ PAT — Cloud now mints its own
// node-scoped mcn_ PAT during /api/v1/nodes and ships it to the EC2
// instance via SSM. Multica-api just forwards the request body and
// the caller's user_id; there is no PAT plumbing on this endpoint.
func TestCreateCloudRuntimeNodeForwardsBody(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"X-Request-Id": []string{"fleet-request-id"}},
			Body:       []byte(`{"status":"launching"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodPost, "/api/cloud-runtime/nodes", map[string]any{
		"instance_type": "g5.xlarge",
	})
	req.Header.Set("X-Request-ID", "api-request-id")
	w := httptest.NewRecorder()

	testHandler.CreateCloudRuntimeNode(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !proxy.called {
		t.Fatal("cloud runtime proxy was not called")
	}
	if proxy.req.Method != http.MethodPost || proxy.req.Path != "/api/v1/nodes" {
		t.Fatalf("proxied request = %s %s", proxy.req.Method, proxy.req.Path)
	}
	if proxy.req.UserID != testUserID {
		t.Fatalf("proxied user id = %q", proxy.req.UserID)
	}
	if proxy.req.RequestID != "api-request-id" {
		t.Fatalf("proxied request id = %q", proxy.req.RequestID)
	}
	if got := w.Header().Get("X-Request-ID"); got != "fleet-request-id" {
		t.Fatalf("response request id = %q", got)
	}
}

func TestCloudRuntimeDisabledReturnsUnavailable(t *testing.T) {
	useCloudRuntimeProxy(t, &fakeCloudRuntimeProxy{enabled: false})

	req := newRequest(http.MethodGet, "/api/cloud-runtime/nodes", nil)
	w := httptest.NewRecorder()

	testHandler.ListCloudRuntimeNodes(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestListCloudRuntimeNodesForwardsQuery(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`[]`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-runtime/nodes?limit=10&offset=20", nil)
	w := httptest.NewRecorder()

	testHandler.ListCloudRuntimeNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !proxy.called {
		t.Fatal("cloud runtime proxy was not called")
	}
	if proxy.req.Method != http.MethodGet || proxy.req.Path != "/api/v1/nodes" {
		t.Fatalf("proxied request = %s %s", proxy.req.Method, proxy.req.Path)
	}
	if got := proxy.req.Query.Encode(); got != "limit=10&offset=20" {
		t.Fatalf("proxied query = %q", got)
	}
}

func TestCloudRuntimeNonJSONResponseIsWrapped(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusBadGateway,
			Body:       []byte("fleet failed\n"),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-runtime/healthz", nil)
	w := httptest.NewRecorder()

	testHandler.GetCloudRuntimeHealth(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	if got := w.Body.String(); !strings.Contains(got, `"error":"fleet failed"`) {
		t.Fatalf("body = %s", got)
	}
}

func TestCloudRuntimeEmptyResponseKeepsStatus(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusNoContent,
			Body:       nil,
		},
	}
	useCloudRuntimeProxy(t, proxy)

	req := newRequest(http.MethodGet, "/api/cloud-runtime/healthz", nil)
	w := httptest.NewRecorder()

	testHandler.GetCloudRuntimeHealth(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body = %s", body)
	}
}

func TestCreateCloudRuntimeNodeRejectsLargeBody(t *testing.T) {
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Body:       []byte(`{"status":"launching"}`),
		},
	}
	useCloudRuntimeProxy(t, proxy)

	body := bytes.NewReader(bytes.Repeat([]byte("a"), maxCloudRuntimeRequestBodySize+1))
	req := httptest.NewRequest(http.MethodPost, "/api/cloud-runtime/nodes", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	testHandler.CreateCloudRuntimeNode(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("cloud runtime proxy should not be called")
	}
}
