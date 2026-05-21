package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func TestCreateCloudRuntimeNodeForwardsValidatedPAT(t *testing.T) {
	rawPAT := "mul_cloud_runtime_test_valid_pat"
	_, err := testHandler.Queries.CreatePersonalAccessToken(context.Background(), db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(testUserID),
		Name:        "cloud runtime test",
		TokenHash:   auth.HashToken(rawPAT),
		TokenPrefix: rawPAT[:12],
		ExpiresAt:   pgtype.Timestamptz{},
	})
	if err != nil {
		t.Fatalf("create PAT: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE token_hash = $1`, auth.HashToken(rawPAT))
	})

	prevProxy := testHandler.CloudRuntime
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"X-Request-Id": []string{"fleet-request-id"}},
			Body:       []byte(`{"status":"launching"}`),
		},
	}
	testHandler.CloudRuntime = proxy
	t.Cleanup(func() { testHandler.CloudRuntime = prevProxy })

	req := newRequest(http.MethodPost, "/api/cloud-runtime/nodes", map[string]any{
		"instance_type": "g5.xlarge",
	})
	req.Header.Set("X-User-PAT", rawPAT)
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
	if proxy.req.UserPAT != rawPAT {
		t.Fatalf("proxied PAT = %q", proxy.req.UserPAT)
	}
	if proxy.req.RequestID != "api-request-id" {
		t.Fatalf("proxied request id = %q", proxy.req.RequestID)
	}
	if got := w.Header().Get("X-Request-ID"); got != "fleet-request-id" {
		t.Fatalf("response request id = %q", got)
	}
}

func TestCreateCloudRuntimeNodeRejectsUnownedPAT(t *testing.T) {
	prevProxy := testHandler.CloudRuntime
	proxy := &fakeCloudRuntimeProxy{
		enabled: true,
		resp: &cloudruntime.Response{
			StatusCode: http.StatusCreated,
			Body:       []byte(`{"status":"launching"}`),
		},
	}
	testHandler.CloudRuntime = proxy
	t.Cleanup(func() { testHandler.CloudRuntime = prevProxy })

	req := newRequest(http.MethodPost, "/api/cloud-runtime/nodes", map[string]any{
		"instance_type": "g5.xlarge",
	})
	req.Header.Set("X-User-PAT", "mul_cloud_runtime_test_missing_pat")
	w := httptest.NewRecorder()

	testHandler.CreateCloudRuntimeNode(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if proxy.called {
		t.Fatal("cloud runtime proxy should not be called")
	}
}
