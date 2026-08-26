package seatcapacity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		if got := r.Header.Get("Authorization"); got != "Bearer "+strings.Repeat("s", 32) {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"managed":true,"allowed":false,"reason":"capacity_full"}`))
	}))
	defer server.Close()

	client, err := New(Config{Enabled: true, BaseURL: server.URL + "/base", ServiceToken: strings.Repeat("s", 32)})
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

func TestClientRejectsRedirectAndDoesNotForwardCredential(t *testing.T) {
	var redirectedAuth string
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		redirectedAuth = r.Header.Get("Authorization")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client, err := New(Config{Enabled: true, BaseURL: source.URL, ServiceToken: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Release(context.Background(), uuid.New(), uuid.New())
	var remote *HTTPError
	if !errors.As(err, &remote) || remote.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v", err)
	}
	if redirectedAuth != "" {
		t.Fatalf("credential reached redirect target: %q", redirectedAuth)
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

func TestDisabledClientPreservesLegacyBehavior(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.ClaimShareJoin(context.Background(), uuid.New(), uuid.New())
	if err != nil || !decision.Allowed || decision.Managed {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestEnabledClientRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing URL", cfg: Config{Enabled: true, ServiceToken: strings.Repeat("s", 32)}},
		{name: "URL credentials", cfg: Config{Enabled: true, BaseURL: "https://user:password@example.com", ServiceToken: strings.Repeat("s", 32)}},
		{name: "short token", cfg: Config{Enabled: true, BaseURL: "https://example.com", ServiceToken: "short"}},
		{name: "token whitespace", cfg: Config{Enabled: true, BaseURL: "https://example.com", ServiceToken: strings.Repeat("s", 31) + "\n"}},
		{name: "excessive timeout", cfg: Config{Enabled: true, BaseURL: "https://example.com", ServiceToken: strings.Repeat("s", 32), Timeout: 6 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
