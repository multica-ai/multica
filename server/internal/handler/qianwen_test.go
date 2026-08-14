package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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
	installFn func(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error)
	listFn    func(context.Context, pgtype.UUID) ([]db.ChannelInstallation, error)
	getFn     func(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error)
	revokeFn  func(context.Context, pgtype.UUID) error
	submitFn  func(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error)
	statusFn  func(context.Context, string, string, string) (qianwen.RequestStatus, error)
}

type recordingQianwenRateLimiter struct {
	allow bool
	keys  []string
}

func (l *recordingQianwenRateLimiter) Allow(_ context.Context, key string) bool {
	l.keys = append(l.keys, key)
	return l.allow
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

func (f *fakeQianwenService) Revoke(ctx context.Context, id pgtype.UUID) error {
	if f.revokeFn == nil {
		return nil
	}
	return f.revokeFn(ctx, id)
}

func (f *fakeQianwenService) Submit(ctx context.Context, connectionID, token string, req qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
	if f.submitFn == nil {
		return qianwen.SubmitResult{}, nil
	}
	return f.submitFn(ctx, connectionID, token, req)
}

func (f *fakeQianwenService) Status(ctx context.Context, connectionID, token, requestID string) (qianwen.RequestStatus, error) {
	if f.statusFn == nil {
		return qianwen.RequestStatus{}, nil
	}
	return f.statusFn(ctx, connectionID, token, requestID)
}

func qianwenRequest(method, target, body string, params ...string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		rctx.URLParams.Add(params[i], params[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestSubmitQianwenRequestRequiresBearer(t *testing.T) {
	called := 0
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	badCredentialDebt := &recordingQianwenRateLimiter{allow: true}
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
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

func TestSubmitQianwenRequestRejectsMalformedConnectionBeforeCredentialLimiter(t *testing.T) {
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	badCredentialDebt := &recordingQianwenRateLimiter{allow: true}
	called := 0
	h := &Handler{
		Qianwen: &fakeQianwenService{
			submitFn: func(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
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

func TestQianwenCredentialLimiterHashesConnectionAndTokenAcrossPublicEndpoints(t *testing.T) {
	const requestID = "56a41a0c-cb13-476a-a75b-230792a277e1"
	credentialLimiter := &recordingQianwenRateLimiter{allow: true}
	h := &Handler{
		Qianwen: &fakeQianwenService{
			submitFn: func(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
				return qianwen.SubmitResult{RequestID: requestID, Status: "accepted"}, nil
			},
			statusFn: func(context.Context, string, string, string) (qianwen.RequestStatus, error) {
				return qianwen.RequestStatus{RequestID: requestID, Status: "completed"}, nil
			},
		},
		WebhookRateLimiter: credentialLimiter,
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

	if len(credentialLimiter.keys) != 2 {
		t.Fatalf("credential limiter calls = %d, want 2", len(credentialLimiter.keys))
	}
	if credentialLimiter.keys[0] == credentialLimiter.keys[1] {
		t.Fatalf("different bearer tokens shared limiter key %q", credentialLimiter.keys[0])
	}
	for i, token := range []string{qianwenHandlerTestAccessToken, qianwenHandlerTestOtherToken} {
		key := credentialLimiter.keys[i]
		if len(key) != sha256.Size*2 {
			t.Fatalf("key %d length = %d, want %d", i, len(key), sha256.Size*2)
		}
		if _, err := hex.DecodeString(key); err != nil {
			t.Fatalf("key %d is not fixed hexadecimal: %q: %v", i, key, err)
		}
		if strings.Contains(key, qianwenHandlerTestConnectionID) || strings.Contains(key, token) {
			t.Fatalf("key %d leaked plaintext credential material: %q", i, key)
		}
		sum := sha256.Sum256([]byte(qianwenHandlerTestConnectionID + "\x00" + token))
		if want := hex.EncodeToString(sum[:]); key != want {
			t.Fatalf("key %d = %q, want SHA-256 tuple digest %q", i, key, want)
		}
	}
}

func TestSubmitQianwenRequestRejectsOversizeAndTrailingJSON(t *testing.T) {
	called := 0
	h := &Handler{Qianwen: &fakeQianwenService{
		submitFn: func(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
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
		submitFn: func(ctx context.Context, gotConnectionID, gotToken string, req qianwen.SubmitRequest) (qianwen.SubmitResult, error) {
			if gotConnectionID != qianwenHandlerTestConnectionID || gotToken != qianwenHandlerTestAccessToken {
				t.Fatalf("credentials = (%q, %q), want (%q, %q)", gotConnectionID, gotToken, qianwenHandlerTestConnectionID, qianwenHandlerTestAccessToken)
			}
			if req.RequestID != requestID || req.Query != "run tests" {
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
				statusFn: func(context.Context, string, string, string) (qianwen.RequestStatus, error) {
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
		statusFn: func(ctx context.Context, connectionID, token, gotRequestID string) (qianwen.RequestStatus, error) {
			if connectionID != qianwenHandlerTestConnectionID || token != qianwenHandlerTestAccessToken || gotRequestID != requestID {
				t.Fatalf("Status args = (%q, %q, %q)", connectionID, token, gotRequestID)
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
		Installations []QianwenInstallationResponse `json:"installations"`
		Configured    bool                          `json:"configured"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if !listed.Configured || len(listed.Installations) != 1 || listed.Installations[0].ConnectionID != qianwenHandlerTestConnectionID {
		t.Fatalf("list response = %+v", listed)
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
				revokeFn: func(context.Context, pgtype.UUID) error {
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
