package seatcapacity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClientReserveInvitation(t *testing.T) {
	workspaceID, invitationID := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/base/api/v1/internal/subscriptions/"+workspaceID.String()+"/capacity/reserve" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization must be absent, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"managed":true,"allowed":false,"reason":"capacity_full"}`))
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL + "/base"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.ReserveInvitation(context.Background(), workspaceID, invitationID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Managed || decision.Allowed || decision.Reason != "capacity_full" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestClientRejectsRedirect(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirected = true
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client, err := New(Config{BaseURL: source.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Release(context.Background(), uuid.New(), uuid.New())
	var remote *HTTPError
	if !errors.As(err, &remote) || remote.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v", err)
	}
	if redirected {
		t.Fatal("redirect target received an internal Cloud request")
	}
}

func TestIsCapacityOvercommittedRequiresCloudConflictCode(t *testing.T) {
	if !IsCapacityOvercommitted(&HTTPError{StatusCode: http.StatusConflict, Code: "capacity_overcommitted"}) {
		t.Fatal("capacity_overcommitted conflict was not recognized")
	}
	if IsCapacityOvercommitted(&HTTPError{StatusCode: http.StatusConflict, Code: "capacity_full"}) ||
		IsCapacityOvercommitted(&HTTPError{StatusCode: http.StatusServiceUnavailable, Code: "capacity_overcommitted"}) {
		t.Fatal("unrelated Cloud error was recognized as capacity overcommit")
	}
}

func TestClientPreservesCapacityRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		// Simulate a proxy-generated response with no Cloud JSON error code.
		_, _ = w.Write([]byte(`rate limited by ingress`))
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ReserveInvitation(context.Background(), uuid.New(), uuid.New(), time.Now().Add(time.Hour))
	if !IsRateLimited(err) {
		t.Fatalf("error = %v, want capacity rate limit", err)
	}
	if retry := RateLimitRetryAfter(err); retry != 3*time.Second {
		t.Fatalf("retry after = %s, want 3s", retry)
	}
	if scope := RateLimitScopeOf(err); scope != "" {
		t.Fatalf("proxy rate-limit scope = %q, want unknown", scope)
	}
}

func TestClientPreservesCloudWorkspaceRateLimitScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.Header().Set(rateLimitScopeHeader, RateLimitScopeWorkspace)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ReserveInvitation(context.Background(), uuid.New(), uuid.New(), time.Now().Add(time.Hour))
	if scope := RateLimitScopeOf(err); scope != RateLimitScopeWorkspace {
		t.Fatalf("rate-limit scope = %q, want %q", scope, RateLimitScopeWorkspace)
	}
}

func TestRetryAfterDurationAcceptsHTTPDate(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	retryAt := now.Add(7 * time.Second)
	if got := retryAfterDurationAt(retryAt.Format(http.TimeFormat), now); got != 7*time.Second {
		t.Fatalf("retry after = %s, want 7s", got)
	}
}

func TestClientIsDisabledWhenCloudURLIsAbsent(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if client != nil {
		t.Fatalf("client = %#v, want nil for self-host", client)
	}
}

func TestCloudConnectedClientRejectsUnsafeURL(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://user:password@example.com"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}
