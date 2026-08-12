package memoryhub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	raw, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	var body map[string]string
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorCode
	}{
		{401, ErrorUnauthorized},
		{402, ErrorPaymentRequired},
		{403, ErrorForbidden},
		{404, ErrorNotFound},
		{409, ErrorConflict},
		{422, ErrorUnprocessable},
		{429, ErrorRateLimited},
		{500, ErrorUpstream},
		{503, ErrorUpstream},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"code":"boom","message":"x"}}`))
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL})
			err := c.VerifyUserKey(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			mhErr, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type = %T, want *Error", err)
			}
			if mhErr.Code != tc.want {
				t.Fatalf("code = %q, want %q", mhErr.Code, tc.want)
			}
			if mhErr.Status != tc.status {
				t.Fatalf("status = %d, want %d", mhErr.Status, tc.status)
			}
		})
	}
}

func TestAuthFailureAndRetryable(t *testing.T) {
	if !IsAuthFailure(&Error{Code: ErrorUnauthorized}) {
		t.Fatal("unauthorized should be auth failure")
	}
	if IsAuthFailure(&Error{Code: ErrorUpstream}) {
		t.Fatal("upstream should not be auth failure")
	}
	if !IsRetryable(&Error{Code: ErrorUpstream}) {
		t.Fatal("upstream should be retryable")
	}
	if IsRetryable(&Error{Code: ErrorConflict}) {
		t.Fatal("conflict should not be retryable")
	}
}

func TestFindOrCreateSendsServiceIDHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("x-tdai-service-id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"team_id":"team-1","agent_id":"agent-1"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, ServiceID: "space-abc"})
	ref, err := c.FindOrCreateAgent(context.Background(), FindOrCreateRequest{Name: "a"})
	if err != nil {
		t.Fatalf("FindOrCreateAgent: %v", err)
	}
	if gotHeader != "space-abc" {
		t.Fatalf("service id header = %q, want space-abc", gotHeader)
	}
	if ref.TeamID != "team-1" || ref.AgentID != "agent-1" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// hang until the client context times out
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.VerifyUserKey(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	mhErr, ok := err.(*Error)
	if !ok || mhErr.Code != ErrorTimeout {
		t.Fatalf("error = %v, want timeout classification", err)
	}
}
