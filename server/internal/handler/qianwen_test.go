package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	qianwenHandlerTestConnectionID = "qwc_MDAwMDAwMDAwMDAwMDAwMDAw"
	qianwenHandlerTestAccessToken  = "qws_MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA"
	qianwenHandlerTestOtherToken   = "qws_MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE"
)

type fakeQianwenService struct {
	pairingSupportedFn func() bool
	installFn          func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error)
	listFn             func(context.Context, pgtype.UUID) ([]db.ChannelInstallation, error)
	getFn              func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error)
	pairFn             func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.PairingCodeResult, error)
	redeemFn           func(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error)
	revokeFn           func(context.Context, pgtype.UUID, pgtype.UUID) error
	submitFn           func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error)
	statusFn           func(context.Context, string, string, qianwen.StatusInvocation) (qianwen.RequestStatus, error)
}

func (f *fakeQianwenService) PairingSupported() bool {
	if f.pairingSupportedFn == nil {
		return true
	}
	return f.pairingSupportedFn()
}

type recordingQianwenRateLimiter struct {
	allow bool
	keys  []string
}

func (l *recordingQianwenRateLimiter) Allow(_ context.Context, key string) bool {
	l.keys = append(l.keys, key)
	return l.allow
}

type blockingQianwenDeadlineLimiter struct {
	hadDeadline bool
	remaining   time.Duration
	contextErr  error
}

type blockingQianwenRequestBody struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingQianwenRequestBody() *blockingQianwenRequestBody {
	return &blockingQianwenRequestBody{closed: make(chan struct{})}
}

func (b *blockingQianwenRequestBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, errors.New("blocking request body closed")
}

func (b *blockingQianwenRequestBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func (l *blockingQianwenDeadlineLimiter) Allow(ctx context.Context, _ string) bool {
	deadline, ok := ctx.Deadline()
	l.hadDeadline = ok
	if !ok {
		return false
	}
	l.remaining = time.Until(deadline)
	<-ctx.Done()
	l.contextErr = ctx.Err()
	return false
}

func (f *fakeQianwenService) InstallPersonal(ctx context.Context, workspaceID, agentID, installerID pgtype.UUID) (qianwen.InstallationResult, error) {
	if f.installFn == nil {
		return qianwen.InstallationResult{}, nil
	}
	return f.installFn(ctx, workspaceID, agentID, installerID)
}

func (f *fakeQianwenService) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
	if f.listFn == nil {
		return nil, nil
	}
	return f.listFn(ctx, workspaceID)
}

func (f *fakeQianwenService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (db.ChannelInstallation, error) {
	if f.getFn == nil {
		return db.ChannelInstallation{}, nil
	}
	return f.getFn(ctx, id, workspaceID)
}

func (f *fakeQianwenService) MintPairingCode(ctx context.Context, workspaceID, installationID, userID pgtype.UUID) (qianwen.PairingCodeResult, error) {
	if f.pairFn == nil {
		return qianwen.PairingCodeResult{}, nil
	}
	return f.pairFn(ctx, workspaceID, installationID, userID)
}

func (f *fakeQianwenService) RedeemPairingCode(ctx context.Context, connectionID, token string, req qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
	if f.redeemFn == nil {
		return qianwen.PairingBindingResult{}, nil
	}
	return f.redeemFn(ctx, connectionID, token, req)
}

func (f *fakeQianwenService) Revoke(ctx context.Context, workspaceID, id pgtype.UUID) error {
	if f.revokeFn == nil {
		return nil
	}
	return f.revokeFn(ctx, workspaceID, id)
}

func (f *fakeQianwenService) Submit(ctx context.Context, connectionID, token string, req qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
	if f.submitFn == nil {
		return qianwen.SubmitResult{}, nil
	}
	return f.submitFn(ctx, connectionID, token, req)
}

func (f *fakeQianwenService) Status(ctx context.Context, connectionID, token string, request qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
	if f.statusFn == nil {
		return qianwen.RequestStatus{}, nil
	}
	return f.statusFn(ctx, connectionID, token, request)
}

func qianwenRequest(method, target, body string, params ...string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	setQianwenHandlerInvocationHeaders(req)
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		rctx.URLParams.Add(params[i], params[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func setQianwenHandlerInvocationHeaders(req *http.Request) {
	req.Header.Set("X-Qianwen-Open-User-Id", "Opaque/OpenUserID+Ciphertext==")
	req.Header.Set("X-Qianwen-Open-Uuid", "CaseSensitive-OpenUUID-Ciphertext==")
	req.Header.Set("X-Qianwen-Timestamp", "1786726800123")
	req.Header.Set("X-Qianwen-Nonce", "0123456789abcdef0123456789abcdef")
	req.Header.Set("X-Qianwen-Signature", strings.Repeat("ab", sha256.Size))
}

func TestStartQianwenRequestDeadlineUsesProviderBudget(t *testing.T) {
	baseRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	req, finish := startQianwenRequestDeadline(baseRequest)
	defer finish()

	deadline, ok := req.Context().Deadline()
	if !ok {
		t.Fatal("Qianwen request context has no deadline")
	}
	budget := time.Until(deadline)
	if budget <= 2*time.Second || budget > qianwenRequestTimeout {
		t.Fatalf("Qianwen request budget = %s, want the configured 2.5s provider budget", budget)
	}
}

func TestQianwenPublicHandlersShareDeadlineWithIngressLimiter(t *testing.T) {
	const parentTimeout = 250 * time.Millisecond
	requestID := "56a41a0c-cb13-476a-a75b-230792a277e1"
	tests := []struct {
		name    string
		request func() *http.Request
		handle  func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{
			name: "redeem",
			request: func() *http.Request {
				req := qianwenRequest(http.MethodPost,
					"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/binding/redeem",
					`{"pairing_code":"01234567"}`,
					"connectionId", qianwenHandlerTestConnectionID,
				)
				req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
				return req
			},
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) {
				h.RedeemQianwenPairingCode(w, r)
			},
		},
		{
			name: "submit",
			request: func() *http.Request {
				req := qianwenRequest(http.MethodPost,
					"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests",
					`{"request_id":"`+requestID+`","query":"run tests"}`,
					"connectionId", qianwenHandlerTestConnectionID,
				)
				req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
				return req
			},
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) {
				h.SubmitQianwenRequest(w, r)
			},
		},
		{
			name: "status",
			request: func() *http.Request {
				req := qianwenRequest(http.MethodGet,
					"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID,
					"",
					"connectionId", qianwenHandlerTestConnectionID,
					"requestId", requestID,
				)
				req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
				return req
			},
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) {
				h.GetQianwenRequestStatus(w, r)
			},
		},
		{
			name: "current tasks",
			request: func() *http.Request {
				req := qianwenRequest(http.MethodGet,
					"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
					"",
					"connectionId", qianwenHandlerTestConnectionID,
				)
				req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
				return req
			},
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) {
				h.ListQianwenCurrentTasks(w, r)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter := &blockingQianwenDeadlineLimiter{}
			h := &Handler{
				Qianwen:                      &fakeQianwenService{},
				WebhookAbsoluteIPRateLimiter: limiter,
			}
			w := httptest.NewRecorder()
			req := tt.request()
			parentCtx, cancel := context.WithTimeout(req.Context(), parentTimeout)
			defer cancel()
			req = req.WithContext(parentCtx)
			started := time.Now()

			tt.handle(h, w, req)

			elapsed := time.Since(started)
			if !limiter.hadDeadline {
				t.Fatal("ingress limiter context has no shared request deadline")
			}
			if limiter.remaining <= 0 || limiter.remaining > parentTimeout {
				t.Fatalf("limiter deadline remaining = %s, want the earlier parent deadline", limiter.remaining)
			}
			if !errors.Is(limiter.contextErr, context.DeadlineExceeded) {
				t.Fatalf("limiter context error = %v, want deadline exceeded", limiter.contextErr)
			}
			if elapsed < 100*time.Millisecond || elapsed >= 2*time.Second {
				t.Fatalf("handler elapsed = %s, want the shared parent deadline to expire promptly", elapsed)
			}
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want %d after limiter budget expires; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
			}
		})
	}
}

func TestSubmitQianwenRequestDeadlineInterruptsBlockingBodyRead(t *testing.T) {
	const parentTimeout = 250 * time.Millisecond
	called := 0
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
			called++
			return qianwen.SubmitResult{}, nil
		},
	}}
	body := newBlockingQianwenRequestBody()
	req := qianwenRequest(http.MethodPost,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests",
		"",
		"connectionId", qianwenHandlerTestConnectionID,
	)
	req.Body = body
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	parentCtx, cancel := context.WithTimeout(req.Context(), parentTimeout)
	defer cancel()
	req = req.WithContext(parentCtx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	started := time.Now()
	go func() {
		h.SubmitQianwenRequest(w, req)
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(started)
		if elapsed < 100*time.Millisecond || elapsed >= 2*time.Second {
			t.Fatalf("handler elapsed = %s, want body read interrupted by the shared parent deadline", elapsed)
		}
		if w.Code != http.StatusGatewayTimeout {
			t.Fatalf("status = %d, want %d for deadline-interrupted body; body=%s", w.Code, http.StatusGatewayTimeout, w.Body.String())
		}
		if called != 0 {
			t.Fatalf("Submit called %d times after body-read timeout, want 0", called)
		}
	case <-time.After(2 * time.Second):
		_ = body.Close()
		<-done
		t.Fatal("handler did not interrupt the blocking request body after its context deadline")
	}
}

func TestSubmitQianwenRequestRequiresBearer(t *testing.T) {
	called := 0
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	badCredentialDebt := &recordingQianwenRateLimiter{allow: true}
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
			called++
			return qianwen.SubmitResult{}, nil
		},
	}, WebhookRateLimiter: credentialLimiter, WebhookIPRateLimiter: badCredentialDebt}
	body := `{"request_id":"56a41a0c-cb13-476a-a75b-230792a277e1","query":"run tests"}`

	for _, tc := range []struct {
		name   string
		header string
	}{
		{name: "missing"},
		{name: "wrong scheme", header: "Basic " + qianwenHandlerTestAccessToken},
		{name: "missing token", header: "Bearer"},
		{name: "extra field", header: "Bearer " + qianwenHandlerTestAccessToken + " extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := qianwenRequest(http.MethodPost, "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests", body, "connectionId", qianwenHandlerTestConnectionID)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()

			h.SubmitQianwenRequest(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "invalid qianwen credentials") {
				t.Fatalf("body = %s, want stable credential error", w.Body.String())
			}
		})
	}
	if called != 0 {
		t.Fatalf("Submit called %d times without a valid bearer token, want 0", called)
	}
	if len(credentialLimiter.keys) != 0 {
		t.Fatalf("credential limiter received %d malformed bearer requests, want 0", len(credentialLimiter.keys))
	}
	if len(badCredentialDebt.keys) != 4 {
		t.Fatalf("bad-credential debt charges = %d, want 4", len(badCredentialDebt.keys))
	}
}

func TestSubmitQianwenRequestRejectsDuplicateAuthorizationHeaders(t *testing.T) {
	called := 0
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	badCredentialDebt := &recordingQianwenRateLimiter{allow: true}
	h := &Handler{
		Qianwen: &fakeQianwenService{
			submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
				called++
				return qianwen.SubmitResult{Status: "accepted"}, nil
			},
		},
		WebhookRateLimiter:   credentialLimiter,
		WebhookIPRateLimiter: badCredentialDebt,
	}
	req := qianwenRequest(
		http.MethodPost,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests",
		`{"request_id":"56a41a0c-cb13-476a-a75b-230792a277e1","query":"run tests"}`,
		"connectionId",
		qianwenHandlerTestConnectionID,
	)
	req.Header.Add("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	req.Header.Add("Authorization", "Bearer "+qianwenHandlerTestOtherToken)
	w := httptest.NewRecorder()

	h.SubmitQianwenRequest(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if called != 0 {
		t.Fatalf("Submit called %d times for duplicate Authorization headers, want 0", called)
	}
	if len(credentialLimiter.keys) != 0 {
		t.Fatalf("credential limiter received duplicate Authorization header key(s): %q", credentialLimiter.keys)
	}
	if len(badCredentialDebt.keys) != 1 {
		t.Fatalf("bad-credential debt charges = %d, want 1", len(badCredentialDebt.keys))
	}
}

func TestSubmitQianwenRequestRejectsMalformedConnectionBeforeCredentialLimiter(t *testing.T) {
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	badCredentialDebt := &recordingQianwenRateLimiter{allow: true}
	called := 0
	h := &Handler{
		Qianwen: &fakeQianwenService{
			submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
				called++
				return qianwen.SubmitResult{}, nil
			},
		},
		WebhookRateLimiter:   credentialLimiter,
		WebhookIPRateLimiter: badCredentialDebt,
	}
	connectionID := "qwc_" + strings.Repeat("A", 1024)
	req := qianwenRequest(http.MethodPost, "/api/channels/qianwen/"+connectionID+"/requests",
		`{"request_id":"56a41a0c-cb13-476a-a75b-230792a277e1","query":"run tests"}`,
		"connectionId", connectionID)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.SubmitQianwenRequest(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if called != 0 {
		t.Fatalf("Submit called %d times for malformed connection, want 0", called)
	}
	if len(credentialLimiter.keys) != 0 {
		t.Fatalf("credential limiter received malformed connection key(s): %q", credentialLimiter.keys)
	}
	if len(badCredentialDebt.keys) != 1 {
		t.Fatalf("bad-credential debt charges = %d, want 1", len(badCredentialDebt.keys))
	}
}

func TestQianwenPublicBudgetsSeparateWritesReadsAndKeepAggregateCaps(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	readLimiter := &recordingQianwenRateLimiter{allow: true}
	writeLimiter := &recordingQianwenRateLimiter{allow: true}
	aggregateLimiter := &recordingQianwenRateLimiter{allow: true}
	h := &Handler{
		Qianwen: &fakeQianwenService{
			submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
				return qianwen.SubmitResult{RequestID: requestID, Status: "accepted"}, nil
			},
			statusFn: func(context.Context, string, string, qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
				return qianwen.RequestStatus{RequestID: requestID, Status: "completed"}, nil
			},
		},
		WebhookRateLimiter:           readLimiter,
		WebhookIPRateLimiter:         writeLimiter,
		WebhookAbsoluteIPRateLimiter: aggregateLimiter,
	}

	submitReq := qianwenRequest(http.MethodPost, "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests",
		`{"request_id":"`+requestID+`","query":"run tests"}`,
		"connectionId", qianwenHandlerTestConnectionID)
	submitReq.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	submitW := httptest.NewRecorder()
	h.SubmitQianwenRequest(submitW, submitReq)
	if submitW.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want %d; body=%s", submitW.Code, http.StatusAccepted, submitW.Body.String())
	}

	statusReq := qianwenRequest(http.MethodGet,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID, "",
		"connectionId", qianwenHandlerTestConnectionID, "requestId", requestID)
	statusReq.Header.Set("Authorization", "Bearer "+qianwenHandlerTestOtherToken)
	statusW := httptest.NewRecorder()
	h.GetQianwenRequestStatus(statusW, statusReq)
	if statusW.Code != http.StatusOK {
		t.Fatalf("status query = %d, want %d; body=%s", statusW.Code, http.StatusOK, statusW.Body.String())
	}

	if len(writeLimiter.keys) != 1 {
		t.Fatalf("write limiter calls = %d, want submit only; keys=%q", len(writeLimiter.keys), writeLimiter.keys)
	}
	if want := "qianwen:write:" + qianwenCredentialRateLimitKey(qianwenHandlerTestConnectionID, qianwenHandlerTestAccessToken); writeLimiter.keys[0] != want {
		t.Fatalf("write key = %q, want scoped installation digest %q", writeLimiter.keys[0], want)
	}
	if len(readLimiter.keys) != 1 {
		t.Fatalf("read limiter calls = %d, want credential-scoped parsed status identity only; keys=%q", len(readLimiter.keys), readLimiter.keys)
	}
	identity, _, ok := qianwenInvocationMetadataFromHeaders(statusReq)
	if !ok {
		t.Fatal("status fixture identity is invalid")
	}
	if want := qianwenReadIdentityRateLimitKey(qianwenHandlerTestConnectionID, qianwenHandlerTestOtherToken, identity); readLimiter.keys[0] != want {
		t.Fatalf("read key = %q, want credential-scoped exact parsed identity digest %q", readLimiter.keys[0], want)
	}
	if len(aggregateLimiter.keys) != 4 {
		t.Fatalf("aggregate limiter calls = %d, want IP + installation for each endpoint; keys=%q", len(aggregateLimiter.keys), aggregateLimiter.keys)
	}
	if aggregateLimiter.keys[0] != aggregateLimiter.keys[2] {
		t.Fatalf("same client IP used different aggregate buckets: %q vs %q", aggregateLimiter.keys[0], aggregateLimiter.keys[2])
	}
	if aggregateLimiter.keys[1] == aggregateLimiter.keys[3] {
		t.Fatalf("different installation credentials shared aggregate bucket %q", aggregateLimiter.keys[1])
	}
	allKeys := append(append(append([]string{}, writeLimiter.keys...), readLimiter.keys...), aggregateLimiter.keys...)
	for _, key := range allKeys {
		for _, secret := range []string{qianwenHandlerTestConnectionID, qianwenHandlerTestAccessToken, qianwenHandlerTestOtherToken, identity.OpenUserID, identity.OpenUUID} {
			if strings.Contains(key, secret) {
				t.Fatalf("rate-limit key leaked plaintext credential or identity material %q: %q", secret, key)
			}
		}
	}
}

func TestQianwenInvalidCredentialCannotExhaustValidCredentialReadBudget(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	h := &Handler{
		Qianwen: &fakeQianwenService{
			statusFn: func(_ context.Context, _ string, token string, _ qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
				if token != qianwenHandlerTestAccessToken {
					return qianwen.RequestStatus{}, qianwen.ErrUnauthorized
				}
				return qianwen.RequestStatus{RequestID: requestID, Status: "running"}, nil
			},
		},
		WebhookRateLimiter:           NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 60, Window: time.Minute}),
		WebhookIPRateLimiter:         NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 1_000, Window: time.Minute}),
		WebhookAbsoluteIPRateLimiter: NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1_000, Window: time.Minute}),
	}

	statusRequest := func(token string) *httptest.ResponseRecorder {
		req := qianwenRequest(http.MethodGet,
			"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID,
			"",
			"connectionId", qianwenHandlerTestConnectionID,
			"requestId", requestID,
		)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.GetQianwenRequestStatus(w, req)
		return w
	}

	// A shape-valid but unauthenticated credential may target the same exact
	// opaque identity. Its failed reads must not spend the valid credential's
	// per-identity polling budget.
	for requestNumber := 1; requestNumber <= 60; requestNumber++ {
		w := statusRequest(qianwenHandlerTestOtherToken)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid credential read %d status = %d, want %d; body=%s", requestNumber, w.Code, http.StatusUnauthorized, w.Body.String())
		}
	}

	valid := statusRequest(qianwenHandlerTestAccessToken)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid credential was affected by invalid credential exhaustion: status=%d, want %d; body=%s", valid.Code, http.StatusOK, valid.Body.String())
	}
}

func TestQianwenReadBudgetSupportsThreeBoundUsersPollingEveryTwoSeconds(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	type boundIdentity struct {
		openUserID string
		openUUID   string
	}
	identities := []boundIdentity{
		{openUserID: "Opaque-Bound-User-A", openUUID: "Opaque-Bound-Device-A"},
		{openUserID: "Opaque-Bound-User-B", openUUID: "Opaque-Bound-Device-B"},
		{openUserID: "Opaque-Bound-User-C", openUUID: "Opaque-Bound-Device-C"},
	}
	known := make(map[string]bool, len(identities))
	for _, identity := range identities {
		known[identity.openUserID+"\x00"+identity.openUUID] = true
	}
	h := &Handler{
		Qianwen: &fakeQianwenService{
			statusFn: func(_ context.Context, _ string, _ string, invocation qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
				key := invocation.Identity.OpenUserID + "\x00" + invocation.Identity.OpenUUID
				if !known[key] {
					return qianwen.RequestStatus{}, qianwen.ErrPairingAccessDenied
				}
				return qianwen.RequestStatus{RequestID: requestID, Status: "running"}, nil
			},
		},
		WebhookRateLimiter:           NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 60, Window: time.Minute}),
		WebhookIPRateLimiter:         NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 1_000, Window: time.Minute}),
		WebhookAbsoluteIPRateLimiter: NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1_000, Window: time.Minute}),
	}

	poll := func(identity boundIdentity) *httptest.ResponseRecorder {
		req := qianwenRequest(http.MethodGet,
			"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID,
			"",
			"connectionId", qianwenHandlerTestConnectionID,
			"requestId", requestID,
		)
		req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
		req.Header.Set(qianwenOpenUserIDHeader, identity.openUserID)
		req.Header.Set(qianwenOpenUUIDHeader, identity.openUUID)
		w := httptest.NewRecorder()
		h.GetQianwenRequestStatus(w, req)
		return w
	}

	// poll_after_ms=2000 means 30 status reads per minute per bound identity.
	// Three users on one installation must therefore complete 90 reads without
	// colliding in a shared installation credential bucket.
	for round := 0; round < 30; round++ {
		for userIndex, identity := range identities {
			w := poll(identity)
			if w.Code != http.StatusOK {
				t.Fatalf("poll round %d user %d status = %d, want %d; body=%s", round+1, userIndex+1, w.Code, http.StatusOK, w.Body.String())
			}
		}
	}

	// A single identity retains a finite 60/min read ceiling: its next 30
	// burst reads fit, while request 61 is rejected without affecting peers.
	for requestNumber := 31; requestNumber <= 60; requestNumber++ {
		w := poll(identities[0])
		if w.Code != http.StatusOK {
			t.Fatalf("identity A read %d status = %d, want %d; body=%s", requestNumber, w.Code, http.StatusOK, w.Body.String())
		}
	}
	limited := poll(identities[0])
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("identity A read 61 status = %d, want %d; body=%s", limited.Code, http.StatusTooManyRequests, limited.Body.String())
	}
	peer := poll(identities[1])
	if peer.Code != http.StatusOK {
		t.Fatalf("identity B was affected by identity A exhaustion: status=%d body=%s", peer.Code, peer.Body.String())
	}
}

func TestQianwenInstallationAggregateCapRejectsAcrossDifferentClientIPsBeforeService(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	serviceCalls := 0
	h := &Handler{
		Qianwen: &fakeQianwenService{
			statusFn: func(context.Context, string, string, qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
				serviceCalls++
				return qianwen.RequestStatus{RequestID: requestID, Status: "running"}, nil
			},
		},
		WebhookRateLimiter:           NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 10, Window: time.Minute}),
		WebhookAbsoluteIPRateLimiter: NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute}),
	}

	statusRequest := func(remoteAddr string) *httptest.ResponseRecorder {
		req := qianwenRequest(http.MethodGet,
			"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID,
			"",
			"connectionId", qianwenHandlerTestConnectionID,
			"requestId", requestID,
		)
		req.RemoteAddr = remoteAddr
		req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
		w := httptest.NewRecorder()
		h.GetQianwenRequestStatus(w, req)
		return w
	}

	first := statusRequest("198.51.100.10:4000")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := statusRequest("198.51.100.11:4000")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status from a different IP = %d, want installation cap %d; body=%s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
	if serviceCalls != 1 {
		t.Fatalf("service calls = %d, want aggregate installation cap before second DB/service call", serviceCalls)
	}
}

func TestSubmitQianwenRequestRejectsOversizeAndTrailingJSON(t *testing.T) {
	called := 0
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
			called++
			return qianwen.SubmitResult{}, nil
		},
	}}

	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "body over limit",
			body: `{"request_id":"56a41a0c-cb13-476a-a75b-230792a277e1","query":"` + strings.Repeat("x", qianwenBodyLimit) + `"}`,
			want: http.StatusRequestEntityTooLarge,
		},
		{
			name: "second JSON value",
			body: `{"request_id":"56a41a0c-cb13-476a-a75b-230792a277e1","query":"run tests"} {}`,
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := qianwenRequest(http.MethodPost, "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests", tc.body, "connectionId", qianwenHandlerTestConnectionID)
			req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
			w := httptest.NewRecorder()

			h.SubmitQianwenRequest(w, req)

			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if called != 0 {
		t.Fatalf("Submit called %d times for invalid bodies, want 0", called)
	}
}

func TestSubmitQianwenRequestAcceptedContract(t *testing.T) {
	const (
		requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	)
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(ctx context.Context, gotConnectionID, gotToken string, req qianwen.SubmitInvocation) (qianwen.SubmitResult, error) {
			if gotConnectionID != qianwenHandlerTestConnectionID || gotToken != qianwenHandlerTestAccessToken {
				t.Fatalf("credentials = (%q, %q), want (%q, %q)", gotConnectionID, gotToken, qianwenHandlerTestConnectionID, qianwenHandlerTestAccessToken)
			}
			if req.Request.RequestID != requestID || req.Request.Query != "run tests" {
				t.Fatalf("request = %+v", req)
			}
			if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
				t.Fatal("Submit context has no deadline")
			}
			return qianwen.SubmitResult{RequestID: requestID, Status: "accepted"}, nil
		},
	}}
	req := qianwenRequest(http.MethodPost, "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests",
		`{"request_id":"`+requestID+`","query":"run tests"}`, "connectionId", qianwenHandlerTestConnectionID)
	req.Header.Set("Authorization", "bEaReR "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.SubmitQianwenRequest(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var got struct {
		RequestID   string `json:"request_id"`
		Status      string `json:"status"`
		PollAfterMS int    `json:"poll_after_ms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.RequestID != requestID || got.Status != "accepted" || got.PollAfterMS != 2000 {
		t.Fatalf("response = %+v, want accepted poll contract", got)
	}
}

func TestGetQianwenRequestStatusMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "unauthorized", err: fmt.Errorf("wrapped: %w", qianwen.ErrUnauthorized), want: http.StatusUnauthorized},
		{name: "invalid request", err: fmt.Errorf("wrapped: %w", qianwen.ErrInvalidRequest), want: http.StatusBadRequest},
		{name: "request conflict", err: fmt.Errorf("wrapped: %w", qianwen.ErrRequestConflict), want: http.StatusConflict},
		{name: "not found", err: fmt.Errorf("wrapped: %w", qianwen.ErrRequestNotFound), want: http.StatusNotFound},
		{name: "deadline", err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), want: http.StatusGatewayTimeout},
		{name: "internal", err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Qianwen: &fakeQianwenService{
				statusFn: func(context.Context, string, string, qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
					return qianwen.RequestStatus{}, tc.err
				},
			}}
			req := qianwenRequest(http.MethodGet,
				"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/56a41a0c-cb13-476a-a75b-230792a277e1", "",
				"connectionId", qianwenHandlerTestConnectionID, "requestId", "56a41a0c-cb13-476a-a75b-230792a277e1")
			req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
			w := httptest.NewRecorder()

			h.GetQianwenRequestStatus(w, req)

			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestGetQianwenRequestStatusSuccessContract(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	h := &Handler{Qianwen: &fakeQianwenService{
		statusFn: func(ctx context.Context, connectionID, token string, invocation qianwen.StatusInvocation) (qianwen.RequestStatus, error) {
			if connectionID != qianwenHandlerTestConnectionID || token != qianwenHandlerTestAccessToken || invocation.RequestID != requestID {
				t.Fatalf("Status args = (%q, %q, %q)", connectionID, token, invocation.RequestID)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("Status context has no deadline")
			}
			return qianwen.RequestStatus{
				RequestID: requestID,
				Status:    "completed",
				TaskID:    "3b09c951-5e58-4bdd-aabd-3272c4cb9921",
				Output:    "done",
			}, nil
		},
	}}
	req := qianwenRequest(http.MethodGet, "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests/"+requestID, "",
		"connectionId", qianwenHandlerTestConnectionID, "requestId", requestID)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.GetQianwenRequestStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got qianwen.RequestStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "completed" || got.Output != "done" || got.RequestID != requestID {
		t.Fatalf("response = %+v", got)
	}
}

func TestQianwenManagementReturnsTokenOnlyFromInstall(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Token Boundary", []byte(`{}`))
	installationID := uuid.NewString()
	row := db.ChannelInstallation{
		ID:              util.MustParseUUID(installationID),
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		AgentID:         util.MustParseUUID(agentID),
		ChannelType:     "qianwen",
		Config:          []byte(`{"app_id":"` + qianwenHandlerTestConnectionID + `","access_token_hash":"must-not-leak","mode":"personal_polling"}`),
		Status:          "active",
		InstallerUserID: util.MustParseUUID(testUserID),
	}
	service := &fakeQianwenService{
		installFn: func(_ context.Context, workspaceID, gotAgentID, installerID pgtype.UUID) (qianwen.InstallationResult, error) {
			if uuidToString(workspaceID) != testWorkspaceID || uuidToString(gotAgentID) != agentID || uuidToString(installerID) != testUserID {
				t.Fatalf("InstallPersonal ids = (%s, %s, %s)", uuidToString(workspaceID), uuidToString(gotAgentID), uuidToString(installerID))
			}
			return qianwen.InstallationResult{
				Installation: row,
				ConnectionID: qianwenHandlerTestConnectionID,
				AccessToken:  qianwenHandlerTestAccessToken,
			}, nil
		},
		listFn: func(_ context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
			if uuidToString(workspaceID) != testWorkspaceID {
				t.Fatalf("ListByWorkspace workspace = %s, want %s", uuidToString(workspaceID), testWorkspaceID)
			}
			return []db.ChannelInstallation{row}, nil
		},
	}
	h := *testHandler
	h.Qianwen = service

	installReq := newRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations?agent_id="+agentID, nil)
	installReq = withURLParam(installReq, "id", testWorkspaceID)
	installW := httptest.NewRecorder()
	h.InstallQianwenPersonal(installW, installReq)
	if installW.Code != http.StatusCreated {
		t.Fatalf("install status = %d, want %d; body=%s", installW.Code, http.StatusCreated, installW.Body.String())
	}
	var installed QianwenInstallResponse
	if err := json.Unmarshal(installW.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	if installed.AccessToken != qianwenHandlerTestAccessToken || !installed.TokenVisibleOnce {
		t.Fatalf("install credential fields = (%q, %v), want one-time token", installed.AccessToken, installed.TokenVisibleOnce)
	}
	if installed.SubmitPath != "/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/requests" {
		t.Fatalf("submit_path = %q", installed.SubmitPath)
	}
	if strings.Contains(installW.Body.String(), "must-not-leak") {
		t.Fatalf("install response leaked stored token digest: %s", installW.Body.String())
	}

	listReq := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/qianwen/installations", nil)
	listReq = withURLParam(listReq, "id", testWorkspaceID)
	listW := httptest.NewRecorder()
	h.ListQianwenInstallations(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listW.Code, http.StatusOK, listW.Body.String())
	}
	for _, secret := range []string{qianwenHandlerTestAccessToken, "access_token", "access_token_hash", "must-not-leak"} {
		if strings.Contains(listW.Body.String(), secret) {
			t.Fatalf("list response leaked %q: %s", secret, listW.Body.String())
		}
	}
	var listed struct {
		Installations    []QianwenInstallationResponse `json:"installations"`
		Configured       bool                          `json:"configured"`
		PairingSupported bool                          `json:"pairing_supported"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if !listed.Configured || !listed.PairingSupported || len(listed.Installations) != 1 || listed.Installations[0].ConnectionID != qianwenHandlerTestConnectionID {
		t.Fatalf("list response = %+v", listed)
	}
}

func TestListQianwenInstallationsReportsPairingCapability(t *testing.T) {
	tests := []struct {
		name           string
		service        QianwenService
		wantConfigured bool
		wantPairing    bool
		wantMode       string
	}{
		{
			name:           "integration disabled",
			wantConfigured: false,
			wantPairing:    false,
		},
		{
			name: "configured without pairing capability",
			service: &fakeQianwenService{
				pairingSupportedFn: func() bool { return false },
			},
			wantConfigured: true,
			wantPairing:    false,
			wantMode:       "personal_polling",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := *testHandler
			h.Qianwen = tt.service
			req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/qianwen/installations", nil)
			req = withURLParam(req, "id", testWorkspaceID)
			w := httptest.NewRecorder()

			h.ListQianwenInstallations(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
			}
			var got struct {
				Configured       *bool  `json:"configured"`
				PairingSupported *bool  `json:"pairing_supported"`
				Mode             string `json:"mode"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Configured == nil || *got.Configured != tt.wantConfigured {
				t.Fatalf("configured = %v, want present %v; body=%s", got.Configured, tt.wantConfigured, w.Body.String())
			}
			if got.PairingSupported == nil || *got.PairingSupported != tt.wantPairing {
				t.Fatalf("pairing_supported = %v, want present %v; body=%s", got.PairingSupported, tt.wantPairing, w.Body.String())
			}
			if got.Mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q; body=%s", got.Mode, tt.wantMode, w.Body.String())
			}
		})
	}
}

func TestInstallQianwenPersonalReturnsUnavailableWhenPairingIsDisabled(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Install Pairing Disabled", []byte(`{}`))
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		pairingSupportedFn: func() bool { return false },
		installFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error) {
			return qianwen.InstallationResult{}, qianwen.ErrPairingUnavailable
		},
	}
	req := newRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations?agent_id="+agentID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()

	h.InstallQianwenPersonal(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{"error":"qianwen pairing is not enabled"}`)
}

func TestInstallQianwenPersonalReturnsConflictForActiveInstallation(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Active Install Conflict", []byte(`{}`))
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		installFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error) {
			return qianwen.InstallationResult{}, qianwen.ErrInstallationAlreadyActive
		},
	}
	req := newRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations?agent_id="+agentID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()

	h.InstallQianwenPersonal(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{"error":"qianwen installation is already active"}`)
}

func TestMintQianwenPairingCodeHTTPContract(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Pairing Contract", []byte(`{}`))
	installationID := uuid.NewString()
	expiresAt := time.Date(2026, time.August, 14, 9, 10, 0, 0, time.UTC)
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		getFn: func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error) {
			return db.ChannelInstallation{
				ID:          util.MustParseUUID(installationID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
				AgentID:     util.MustParseUUID(agentID),
				ChannelType: string(qianwen.TypeQianwen),
				Status:      "active",
			}, nil
		},
		pairFn: func(_ context.Context, workspaceID, gotInstallationID, userID pgtype.UUID) (qianwen.PairingCodeResult, error) {
			if uuidToString(workspaceID) != testWorkspaceID {
				t.Fatalf("workspace id = %s, want %s", uuidToString(workspaceID), testWorkspaceID)
			}
			if uuidToString(gotInstallationID) != installationID {
				t.Fatalf("installation id = %s, want %s", uuidToString(gotInstallationID), installationID)
			}
			if uuidToString(userID) != testUserID {
				t.Fatalf("target user id = %s, want authenticated user %s", uuidToString(userID), testUserID)
			}
			return qianwen.PairingCodeResult{Code: "01234567", ExpiresAt: expiresAt}, nil
		},
	}
	req := qianwenRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations/"+installationID+"/pairing-codes", "",
		"id", testWorkspaceID, "installationId", installationID)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	h.MintQianwenPairingCode(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{
		"pairing_code":"01234567",
		"expires_at":"2026-08-14T09:10:00Z",
		"code_visible_once":true
	}`)
	if strings.Contains(w.Body.String(), "digest") {
		t.Fatalf("response exposed pairing-code digest metadata: %s", w.Body.String())
	}
}

func TestMintQianwenPairingCodeReturnsUnavailableWhenStrongSecretIsMissing(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Pairing Unavailable", []byte(`{}`))
	installationID := uuid.NewString()
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		getFn: func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error) {
			return db.ChannelInstallation{
				ID:          util.MustParseUUID(installationID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
				AgentID:     util.MustParseUUID(agentID),
				ChannelType: string(qianwen.TypeQianwen),
				Status:      "active",
			}, nil
		},
		pairFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.PairingCodeResult, error) {
			return qianwen.PairingCodeResult{}, qianwen.ErrPairingUnavailable
		},
	}
	req := qianwenRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations/"+installationID+"/pairing-codes", "",
		"id", testWorkspaceID, "installationId", installationID)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	h.MintQianwenPairingCode(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response exposed deployment-secret configuration: %s", w.Body.String())
	}
}

func TestMintQianwenPairingCodeHTTPPersistsWithRealPostgres(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Pairing HTTP PG", []byte(`{}`))
	sessions := engine.NewChatSession(testHandler.Queries, testPool, qianwen.TypeQianwen, engine.SessionTitles{
		Direct:   "Qianwen glasses request",
		Fallback: "Qianwen glasses request",
	})
	service, err := qianwen.NewService(testHandler.Queries, sessions, testHandler.TaskService, testPool, []byte("qianwen-handler-test-deployment-secret"))
	if err != nil {
		t.Fatalf("construct real Qianwen service: %v", err)
	}
	installed, err := service.InstallPersonal(context.Background(), util.MustParseUUID(testWorkspaceID), util.MustParseUUID(agentID), util.MustParseUUID(testUserID))
	if err != nil {
		t.Fatalf("install Qianwen bridge: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM qianwen_pairing_code WHERE installation_id = $1`, installed.Installation.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installed.Installation.ID)
	})

	h := *testHandler
	h.Qianwen = service
	installationID := uuidToString(installed.Installation.ID)
	req := qianwenRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations/"+installationID+"/pairing-codes", "",
		"id", testWorkspaceID, "installationId", installationID)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	h.MintQianwenPairingCode(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var response QianwenPairingCodeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode pairing response: %v", err)
	}
	if len(response.PairingCode) != 8 || strings.Trim(response.PairingCode, "0123456789") != "" || !response.CodeVisibleOnce {
		t.Fatalf("pairing response = %+v, want one-time eight-digit code", response)
	}
	var digest []byte
	var expiresAt, createdAt time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT code_digest, expires_at, created_at
		FROM qianwen_pairing_code
		WHERE installation_id = $1 AND multica_user_id = $2
	`, installed.Installation.ID, testUserID).Scan(&digest, &expiresAt, &createdAt); err != nil {
		t.Fatalf("load persisted pairing row: %v", err)
	}
	if len(digest) != sha256.Size || string(digest) == response.PairingCode {
		t.Fatalf("persisted code material = %x, want only a 32-byte keyed digest", digest)
	}
	if !expiresAt.Equal(response.ExpiresAt) || expiresAt.Sub(createdAt) != 10*time.Minute {
		t.Fatalf("persisted TTL = %s - %s, response=%s; want exact 10m", expiresAt, createdAt, response.ExpiresAt)
	}
}

func TestInstallQianwenPersonalUsesAgentManagementPermission(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Permission Gate", []byte(`{}`))
	email := "qianwen-member-" + uuid.NewString() + "@multica.test"
	var memberID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ('Qianwen Plain Member', $1) RETURNING id`, email,
	).Scan(&memberID); err != nil {
		t.Fatalf("insert plain member: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberID,
	); err != nil {
		t.Fatalf("add plain workspace member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	called := 0
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		installFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error) {
			called++
			return qianwen.InstallationResult{}, nil
		},
	}
	req := newRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations?agent_id="+agentID, nil)
	req.Header.Set("X-User-ID", memberID)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()

	h.InstallQianwenPersonal(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if called != 0 {
		t.Fatalf("InstallPersonal called %d times after permission denial, want 0", called)
	}
}

func TestMintQianwenPairingCodeRequiresAgentInvocationPermission(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Qianwen Pairing Permission Gate", []byte(`{}`))
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET permission_mode = 'private' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("make pairing test agent private: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID); err != nil {
		t.Fatalf("remove pairing test agent invocation target: %v", err)
	}
	email := "qianwen-pairing-member-" + uuid.NewString() + "@multica.test"
	var memberID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ('Qianwen Pairing Plain Member', $1) RETURNING id`, email,
	).Scan(&memberID); err != nil {
		t.Fatalf("insert plain member: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberID,
	); err != nil {
		t.Fatalf("add plain workspace member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	installationID := uuid.NewString()
	called := 0
	h := *testHandler
	h.Qianwen = &fakeQianwenService{
		getFn: func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error) {
			return db.ChannelInstallation{
				ID:          util.MustParseUUID(installationID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
				AgentID:     util.MustParseUUID(agentID),
				ChannelType: string(qianwen.TypeQianwen),
				Status:      "active",
			}, nil
		},
		pairFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.PairingCodeResult, error) {
			called++
			return qianwen.PairingCodeResult{}, nil
		},
	}
	req := qianwenRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/qianwen/installations/"+installationID+"/pairing-codes", "",
		"id", testWorkspaceID, "installationId", installationID)
	req.Header.Set("X-User-ID", memberID)
	w := httptest.NewRecorder()

	h.MintQianwenPairingCode(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if called != 0 {
		t.Fatalf("MintPairingCode called %d times after invocation denial, want 0", called)
	}
}

func TestQianwenManagementRejectsMachineCredentialActors(t *testing.T) {
	for _, actorSource := range []string{"task_token", "cloud_pat"} {
		t.Run(actorSource, func(t *testing.T) {
			calls := 0
			h := *testHandler
			h.Qianwen = &fakeQianwenService{
				installFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error) {
					calls++
					return qianwen.InstallationResult{}, nil
				},
				listFn: func(context.Context, pgtype.UUID) ([]db.ChannelInstallation, error) {
					calls++
					return nil, nil
				},
				getFn: func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error) {
					calls++
					return db.ChannelInstallation{}, nil
				},
				pairFn: func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.PairingCodeResult, error) {
					calls++
					return qianwen.PairingCodeResult{}, nil
				},
				revokeFn: func(context.Context, pgtype.UUID, pgtype.UUID) error {
					calls++
					return nil
				},
			}

			requests := []struct {
				name   string
				method string
				target string
				params []string
				call   func(http.ResponseWriter, *http.Request)
			}{
				{
					name:   "install",
					method: http.MethodPost,
					target: "/api/workspaces/" + testWorkspaceID + "/qianwen/installations?agent_id=" + uuid.NewString(),
					params: []string{"id", testWorkspaceID},
					call:   h.InstallQianwenPersonal,
				},
				{
					name:   "list",
					method: http.MethodGet,
					target: "/api/workspaces/" + testWorkspaceID + "/qianwen/installations",
					params: []string{"id", testWorkspaceID},
					call:   h.ListQianwenInstallations,
				},
				{
					name:   "pairing code",
					method: http.MethodPost,
					target: "/api/workspaces/" + testWorkspaceID + "/qianwen/installations/" + uuid.NewString() + "/pairing-codes",
					params: []string{"id", testWorkspaceID, "installationId", uuid.NewString()},
					call:   h.MintQianwenPairingCode,
				},
				{
					name:   "revoke",
					method: http.MethodDelete,
					target: "/api/workspaces/" + testWorkspaceID + "/qianwen/installations/" + uuid.NewString(),
					params: []string{"id", testWorkspaceID, "installationId", uuid.NewString()},
					call:   h.RevokeQianwenInstallation,
				},
			}

			for _, tc := range requests {
				t.Run(tc.name, func(t *testing.T) {
					req := newRequest(tc.method, tc.target, nil)
					req.Header.Set("X-Actor-Source", actorSource)
					for i := 0; i < len(tc.params); i += 2 {
						req = withURLParam(req, tc.params[i], tc.params[i+1])
					}
					w := httptest.NewRecorder()
					tc.call(w, req)
					if w.Code != http.StatusForbidden {
						t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
					}
				})
			}
			if calls != 0 {
				t.Fatalf("Qianwen service called %d times for %s actor, want 0", calls, actorSource)
			}
		})
	}
}
