package vibeshandoff

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientConsumesOpaqueCodeOverRestrictedLoopbackContract(t *testing.T) {
	const secret = "local-service-secret-at-least-32-bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tag-handoff/consume" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Fatalf("unexpected service auth")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "opaque-code" || body["audience"] != "vibes-tag-local" {
			t.Fatalf("unexpected body: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(Identity{
			UserID:        "vibes-user-1",
			WorkspaceID:   "vibes-workspace-1",
			WorkspaceSlug: "design-lab",
			WorkspaceName: "Design Lab",
			Name:          "VIBES User",
			Email:         "same@example.test",
			Role:          "owner",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/tag-handoff/consume", secret)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.Consume(context.Background(), "opaque-code", "vibes-tag-local")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "vibes-user-1" || identity.WorkspaceID != "vibes-workspace-1" {
		t.Fatalf("wrong stable identity: %#v", identity)
	}
}

func TestClientRejectsNonLoopbackAndWeakServiceConfiguration(t *testing.T) {
	if _, err := NewClient("https://vibes.college/api/tag-handoff/consume", "local-service-secret-at-least-32-bytes"); err == nil {
		t.Fatal("expected non-loopback URL to fail closed")
	}
	if _, err := NewClient("http://localhost:3101/api/tag-handoff/consume", "too-short"); err == nil {
		t.Fatal("expected weak service secret to fail closed")
	}
}

func TestClientNeverFollowsConsumeRedirects(t *testing.T) {
	var redirected atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leak" {
			redirected.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, server.URL+"/leak", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, strings.Repeat("s", 32))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Consume(context.Background(), strings.Repeat("a", 43), "vibes-tag-local"); !errors.Is(err, ErrRejected) {
		t.Fatalf("Consume error = %v, want ErrRejected", err)
	}
	if redirected.Load() {
		t.Fatal("consume client followed a redirect with the opaque handoff body")
	}
}

func TestClientFailsClosedWithoutReturningRemoteErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "opaque-code leaked in upstream detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "local-service-secret-at-least-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Consume(context.Background(), "opaque-code", "vibes-tag-local")
	if err == nil || err.Error() != "VIBES handoff rejected" {
		t.Fatalf("unexpected error: %v", err)
	}
}
