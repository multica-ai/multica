package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
)

// Keep the shared test fake source-compatible with the production service
// interface. Tests that care about current-task behavior use the recording
// wrapper below; every older handler test gets the harmless empty default.
func (f *fakeQianwenService) ListCurrentTasks(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
	return qianwen.CurrentTaskList{}, nil
}

type recordingQianwenCurrentTasksService struct {
	*fakeQianwenService
	listCurrentTasksFn func(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error)
}

func (f *recordingQianwenCurrentTasksService) ListCurrentTasks(ctx context.Context, connectionID, token string, invocation qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
	return f.listCurrentTasksFn(ctx, connectionID, token, invocation)
}

func TestListQianwenCurrentTasksReturnsSafePageWithNormalizedPagination(t *testing.T) {
	createdAt := time.Date(2026, time.August, 15, 8, 30, 0, 0, time.UTC)
	want := qianwen.CurrentTaskList{
		Tasks: []qianwen.CurrentTaskSummary{{
			TaskID:       "38a72677-81db-4eb8-8062-01a15881b331",
			RequestID:    "10eb608c-d84a-4019-8cbf-c5ed6f2fce64",
			DisplayTitle: "Run the API tests",
			Source:       "qianwen",
			AgentName:    "Backend agent",
			Status:       "running",
			CreatedAt:    createdAt,
		}},
		HasMore:    true,
		NextCursor: "next_page_Abc-123_",
	}
	called := 0
	h := &Handler{Qianwen: &recordingQianwenCurrentTasksService{
		fakeQianwenService: &fakeQianwenService{},
		listCurrentTasksFn: func(ctx context.Context, connectionID, token string, invocation qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
			called++
			if connectionID != qianwenHandlerTestConnectionID {
				t.Fatalf("connectionID = %q, want %q", connectionID, qianwenHandlerTestConnectionID)
			}
			if token != qianwenHandlerTestAccessToken {
				t.Fatalf("token = %q, want supplied bearer", token)
			}
			if invocation.Request.Limit != 10 || invocation.Request.Cursor != "" {
				t.Fatalf("pagination = %+v, want default limit 10 and empty cursor", invocation.Request)
			}
			if invocation.Identity.OpenUserID != "Opaque/OpenUserID+Ciphertext==" || invocation.Identity.OpenUUID != "CaseSensitive-OpenUUID-Ciphertext==" {
				t.Fatalf("identity = %+v, want exact opaque header values", invocation.Identity)
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("service context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 2*time.Second || remaining > qianwenRequestTimeout {
				t.Fatalf("service deadline remaining = %s, want a 2.5s request budget", remaining)
			}
			return want, nil
		},
	}}
	req := qianwenRequest(http.MethodGet,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
		"", "connectionId", qianwenHandlerTestConnectionID)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.ListQianwenCurrentTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var got qianwen.CurrentTaskList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if len(got.Tasks) != 1 || got.Tasks[0] != want.Tasks[0] || !got.HasMore || got.NextCursor != want.NextCursor {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
	if called != 1 {
		t.Fatalf("ListCurrentTasks called %d times, want 1", called)
	}
}

func TestListQianwenCurrentTasksRejectsAmbiguousOrInvalidQueryBeforeService(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rawQuery string
	}{
		{name: "unknown parameter", rawQuery: "other=value"},
		{name: "duplicate limit", rawQuery: "limit=10&limit=11"},
		{name: "duplicate cursor", rawQuery: "cursor=first&cursor=second"},
		{name: "blank limit", rawQuery: "limit="},
		{name: "non numeric limit", rawQuery: "limit=ten"},
		{name: "leading zero limit", rawQuery: "limit=010"},
		{name: "signed limit", rawQuery: "limit=%2B10"},
		{name: "space padded limit", rawQuery: "limit=%2010"},
		{name: "limit below range", rawQuery: "limit=0"},
		{name: "limit above range", rawQuery: "limit=21"},
		{name: "cursor too long", rawQuery: "cursor=" + strings.Repeat("a", 513)},
		{name: "cursor contains carriage return", rawQuery: "cursor=page%0Dnext"},
		{name: "cursor contains newline", rawQuery: "cursor=page%0Anext"},
		{name: "cursor contains nul", rawQuery: "cursor=page%00next"},
		{name: "malformed encoding", rawQuery: "cursor=%zz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			h := &Handler{Qianwen: &recordingQianwenCurrentTasksService{
				fakeQianwenService: &fakeQianwenService{},
				listCurrentTasksFn: func(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
					called++
					return qianwen.CurrentTaskList{}, nil
				},
			}}
			req := qianwenRequest(http.MethodGet,
				"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
				"", "connectionId", qianwenHandlerTestConnectionID)
			req.URL.RawQuery = tc.rawQuery
			req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
			w := httptest.NewRecorder()

			h.ListQianwenCurrentTasks(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			if called != 0 {
				t.Fatalf("ListCurrentTasks called %d times for invalid query, want 0", called)
			}
		})
	}
}

func TestListQianwenCurrentTasksForwardsExplicitPagination(t *testing.T) {
	called := 0
	h := &Handler{Qianwen: &recordingQianwenCurrentTasksService{
		fakeQianwenService: &fakeQianwenService{},
		listCurrentTasksFn: func(_ context.Context, _ string, _ string, invocation qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
			called++
			if invocation.Request.Limit != 20 || invocation.Request.Cursor != "page_Abc-123_" {
				t.Fatalf("pagination = %+v, want exact decoded query values", invocation.Request)
			}
			return qianwen.CurrentTaskList{Tasks: []qianwen.CurrentTaskSummary{}}, nil
		},
	}}
	req := qianwenRequest(http.MethodGet,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks?limit=20&cursor=page_Abc-123_",
		"", "connectionId", qianwenHandlerTestConnectionID)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.ListQianwenCurrentTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if called != 1 {
		t.Fatalf("ListCurrentTasks called %d times, want 1", called)
	}
}

func TestListQianwenCurrentTasksRejectsMissingCredentialsOrIdentityBeforeService(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*http.Request)
		wantStatus int
	}{
		{
			name: "missing bearer",
			mutate: func(req *http.Request) {
				req.Header.Del("Authorization")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "missing opaque identity",
			mutate: func(req *http.Request) {
				req.Header.Del(qianwenOpenUserIDHeader)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "missing signature",
			mutate: func(req *http.Request) {
				req.Header.Del(qianwenSignatureHeader)
			},
			wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			h := &Handler{Qianwen: &recordingQianwenCurrentTasksService{
				fakeQianwenService: &fakeQianwenService{},
				listCurrentTasksFn: func(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
					called++
					return qianwen.CurrentTaskList{}, nil
				},
			}}
			req := qianwenRequest(http.MethodGet,
				"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
				"", "connectionId", qianwenHandlerTestConnectionID)
			req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
			tc.mutate(req)
			w := httptest.NewRecorder()

			h.ListQianwenCurrentTasks(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if called != 0 {
				t.Fatalf("ListCurrentTasks called %d times before authentication, want 0", called)
			}
		})
	}
}

func TestListQianwenCurrentTasksMapsServiceErrorsWithoutLeakingDetails(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "credential", err: qianwen.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantBody: "invalid qianwen credentials"},
		{name: "signature", err: qianwen.ErrInvalidInvocation, wantStatus: http.StatusUnauthorized, wantBody: "invalid qianwen invocation"},
		{name: "binding", err: qianwen.ErrPairingAccessDenied, wantStatus: http.StatusForbidden, wantBody: "qianwen identity is not bound"},
		{name: "invalid cursor", err: qianwen.ErrInvalidRequest, wantStatus: http.StatusBadRequest, wantBody: qianwen.ErrInvalidRequest.Error()},
		{name: "not found", err: qianwen.ErrRequestNotFound, wantStatus: http.StatusNotFound, wantBody: "qianwen request not found"},
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantBody: "qianwen request timed out"},
		{name: "internal", err: context.Canceled, wantStatus: http.StatusInternalServerError, wantBody: "qianwen request failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Qianwen: &recordingQianwenCurrentTasksService{
				fakeQianwenService: &fakeQianwenService{},
				listCurrentTasksFn: func(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
					return qianwen.CurrentTaskList{}, tc.err
				},
			}}
			req := qianwenRequest(http.MethodGet,
				"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
				"", "connectionId", qianwenHandlerTestConnectionID)
			req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
			w := httptest.NewRecorder()

			h.ListQianwenCurrentTasks(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Fatalf("body = %s, want stable message containing %q", w.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestListQianwenCurrentTasksHonorsCredentialRateLimitBeforeService(t *testing.T) {
	called := 0
	h := &Handler{
		Qianwen: &recordingQianwenCurrentTasksService{
			fakeQianwenService: &fakeQianwenService{},
			listCurrentTasksFn: func(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error) {
				called++
				return qianwen.CurrentTaskList{}, nil
			},
		},
		WebhookRateLimiter: &recordingQianwenRateLimiter{allow: false},
	}
	req := qianwenRequest(http.MethodGet,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/tasks",
		"", "connectionId", qianwenHandlerTestConnectionID)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	w := httptest.NewRecorder()

	h.ListQianwenCurrentTasks(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response is missing Retry-After")
	}
	if called != 0 {
		t.Fatalf("ListCurrentTasks called %d times after rate limit, want 0", called)
	}
}
