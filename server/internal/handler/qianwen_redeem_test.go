package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	testQianwenOpenUserID = "Opaque/OpenUserID+Ciphertext=="
	testQianwenOpenUUID   = "CaseSensitive-OpenUUID-Ciphertext=="
	testQianwenTimestamp  = "1786726800123"
	testQianwenNonce      = "0123456789abcdef0123456789abcdef"
	testQianwenSignature  = "abababababababababababababababababababababababababababababababab"
)

var testQianwenRedeemHeaders = map[string]string{
	"X-Qianwen-Open-User-Id": testQianwenOpenUserID,
	"X-Qianwen-Open-Uuid":    testQianwenOpenUUID,
	"X-Qianwen-Timestamp":    testQianwenTimestamp,
	"X-Qianwen-Nonce":        testQianwenNonce,
	"X-Qianwen-Signature":    testQianwenSignature,
}

func newQianwenRedeemHTTPRequest(body string) *http.Request {
	req := qianwenRequest(
		http.MethodPost,
		"/api/channels/qianwen/"+qianwenHandlerTestConnectionID+"/binding/redeem",
		body,
		"connectionId", qianwenHandlerTestConnectionID,
	)
	req.Header.Set("Authorization", "Bearer "+qianwenHandlerTestAccessToken)
	for name, value := range testQianwenRedeemHeaders {
		req.Header.Set(name, value)
	}
	return req
}

func TestRedeemQianwenPairingCodePassesOnlyHeaderIdentityAndReturnsMinimalSuccess(t *testing.T) {
	called := 0
	h := &Handler{Qianwen: &fakeQianwenService{
		redeemFn: func(ctx context.Context, connectionID, token string, req qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
			called++
			if connectionID != qianwenHandlerTestConnectionID || token != qianwenHandlerTestAccessToken {
				t.Fatalf("credentials = (%q, %q), want (%q, %q)", connectionID, token, qianwenHandlerTestConnectionID, qianwenHandlerTestAccessToken)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("RedeemPairingCode context has no deadline")
			}
			want := qianwen.PairingRedeemRequest{
				Code: "01234567",
				Identity: qianwen.InvocationMetadata{
					OpenUserID: testQianwenOpenUserID,
					OpenUUID:   testQianwenOpenUUID,
					Timestamp:  testQianwenTimestamp,
					Nonce:      testQianwenNonce,
					Signature:  testQianwenSignature,
				},
			}
			if req != want {
				t.Fatalf("redeem request = %+v, want exact header-derived request %+v", req, want)
			}
			return qianwen.PairingBindingResult{
				InstallationID: util.MustParseUUID("41823b83-da83-4015-997e-7cc228f6f962"),
				MulticaUserID:  util.MustParseUUID("c7524ca4-7c16-4496-9b04-e3e47008de30"),
			}, nil
		},
	}}
	req := newQianwenRedeemHTTPRequest(`{"pairing_code":"01234567"}`)
	w := httptest.NewRecorder()

	h.RedeemQianwenPairingCode(w, req)

	if called != 1 {
		t.Fatalf("RedeemPairingCode calls = %d, want 1", called)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{"status":"paired"}`)
	for _, secret := range []string{"41823b83-da83-4015-997e-7cc228f6f962", "c7524ca4-7c16-4496-9b04-e3e47008de30", testQianwenOpenUserID, testQianwenOpenUUID} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("success response exposed private binding detail %q: %s", secret, w.Body.String())
		}
	}
}

func TestRedeemQianwenPairingCodeRejectsBodyIdentityForgeryWithoutCallingService(t *testing.T) {
	called := 0
	h := &Handler{Qianwen: &fakeQianwenService{
		redeemFn: func(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
			called++
			return qianwen.PairingBindingResult{}, nil
		},
	}}
	req := newQianwenRedeemHTTPRequest(`{"pairing_code":"01234567","open_user_id":"forged-body-identity"}`)
	w := httptest.NewRecorder()

	h.RedeemQianwenPairingCode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if called != 0 {
		t.Fatalf("RedeemPairingCode called %d times for forged body identity, want 0", called)
	}
}

func TestRedeemQianwenPairingCodeRequiresExactlyOneSignedMetadataHeader(t *testing.T) {
	tests := []struct {
		name       string
		headerName string
		mutate     func(*http.Request, string)
		wantStatus int
	}{
		{
			name:       "missing open user id",
			headerName: "X-Qianwen-Open-User-Id",
			mutate:     func(req *http.Request, name string) { req.Header.Del(name) },
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing open uuid",
			headerName: "X-Qianwen-Open-Uuid",
			mutate:     func(req *http.Request, name string) { req.Header.Del(name) },
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing timestamp",
			headerName: "X-Qianwen-Timestamp",
			mutate:     func(req *http.Request, name string) { req.Header.Del(name) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing nonce",
			headerName: "X-Qianwen-Nonce",
			mutate:     func(req *http.Request, name string) { req.Header.Del(name) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing signature",
			headerName: "X-Qianwen-Signature",
			mutate:     func(req *http.Request, name string) { req.Header.Del(name) },
			wantStatus: http.StatusUnauthorized,
		},
	}
	for name := range testQianwenRedeemHeaders {
		headerName := name
		tests = append(tests, struct {
			name       string
			headerName string
			mutate     func(*http.Request, string)
			wantStatus int
		}{
			name:       "repeated " + strings.ToLower(headerName),
			headerName: headerName,
			mutate: func(req *http.Request, name string) {
				req.Header.Add(name, "second-value")
			},
			wantStatus: http.StatusUnauthorized,
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			h := &Handler{Qianwen: &fakeQianwenService{
				redeemFn: func(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
					called++
					return qianwen.PairingBindingResult{}, nil
				},
			}}
			req := newQianwenRedeemHTTPRequest(`{"pairing_code":"01234567"}`)
			tc.mutate(req, tc.headerName)
			w := httptest.NewRecorder()

			h.RedeemQianwenPairingCode(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if called != 0 {
				t.Fatalf("RedeemPairingCode called %d times for invalid %s, want 0", called, tc.headerName)
			}
		})
	}
}

func TestRedeemQianwenPairingCodeRejectsNonStrictOrOversizeJSONWithoutCallingService(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "unknown field",
			body:       `{"pairing_code":"01234567","unexpected":true}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "trailing JSON value",
			body:       `{"pairing_code":"01234567"} {}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "body over two kibibytes",
			body:       `{"pairing_code":"` + strings.Repeat("0", 2*1024) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			h := &Handler{Qianwen: &fakeQianwenService{
				redeemFn: func(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
					called++
					return qianwen.PairingBindingResult{}, nil
				},
			}}
			req := newQianwenRedeemHTTPRequest(tc.body)
			w := httptest.NewRecorder()

			h.RedeemQianwenPairingCode(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if called != 0 {
				t.Fatalf("RedeemPairingCode called %d times for invalid body, want 0", called)
			}
		})
	}
}

func TestRedeemQianwenPairingCodeMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantStatus     int
		wantRetryAfter string
	}{
		{name: "invalid signature", err: qianwen.ErrInvalidInvocation, wantStatus: http.StatusUnauthorized},
		{name: "stale invocation", err: qianwen.ErrStaleInvocation, wantStatus: http.StatusUnauthorized},
		{name: "invalid bearer credential", err: qianwen.ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "identity unavailable", err: qianwen.ErrIdentityUnavailable, wantStatus: http.StatusForbidden},
		{name: "pairing access denied", err: qianwen.ErrPairingAccessDenied, wantStatus: http.StatusForbidden},
		{name: "pairing code invalid", err: qianwen.ErrPairingCodeInvalid, wantStatus: http.StatusGone},
		{name: "binding already assigned", err: qianwen.ErrBindingAlreadyAssigned, wantStatus: http.StatusConflict},
		{name: "invocation replay", err: qianwen.ErrInvocationReplay, wantStatus: http.StatusConflict},
		{name: "pairing rate limited", err: qianwen.ErrPairingRateLimited, wantStatus: http.StatusTooManyRequests, wantRetryAfter: "600"},
		{name: "pairing unavailable", err: qianwen.ErrPairingUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			h := &Handler{Qianwen: &fakeQianwenService{
				redeemFn: func(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error) {
					called++
					return qianwen.PairingBindingResult{}, fmt.Errorf("wrapped service error: %w", tc.err)
				},
			}}
			req := newQianwenRedeemHTTPRequest(`{"pairing_code":"01234567"}`)
			w := httptest.NewRecorder()

			h.RedeemQianwenPairingCode(w, req)

			if called != 1 {
				t.Fatalf("RedeemPairingCode calls = %d, want 1", called)
			}
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got != tc.wantRetryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, tc.wantRetryAfter)
			}
		})
	}
}
