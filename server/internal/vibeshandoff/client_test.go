package vibeshandoff

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoMatchesVIBESCLIPKCEVector(t *testing.T) {
	data, err := os.ReadFile("testdata/vibes-cli-exchange-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		SchemaVersion                         int `json:"schemaVersion"`
		Audience, CodeVerifier, CodeChallenge string
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(vector.CodeVerifier))
	if got := base64.RawURLEncoding.EncodeToString(digest[:]); vector.SchemaVersion != CLISchemaVersion || vector.Audience != CLIAudience || got != vector.CodeChallenge {
		t.Fatalf("Go PKCE vector mismatch: got %q", got)
	}
}

func TestCLIClientConsumesPKCEBoundCodeOverTLSContract(t *testing.T) {
	const secret = "cli-service-secret-at-least-32-bytes"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tag-cli/consume" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Fatalf("unexpected request")
		}
		var body CLIConsumeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Code == "" || body.CodeVerifier == "" || body.ReceiverID == "" || body.ReceiverURI == "" || body.Audience != CLIAudience {
			t.Fatalf("missing binding: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(CLIIdentity{
			SchemaVersion:    CLISchemaVersion,
			Identity:         Identity{UserID: "vibes-user-1", SessionID: "session-1", WorkspaceID: "workspace-1", WorkspaceSlug: "design-lab", WorkspaceName: "Design Lab", Name: "VIBES User", Role: "owner", AccountEpoch: 2, SessionWorkspaceGeneration: 3, AuthorityVersion: 4, MembershipGeneration: 5},
			SessionExpiresAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		})
	}))
	defer server.Close()
	client, err := NewCLIClient(server.URL+"/api/tag-cli/consume", secret)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = server.Client()
	identity, err := client.ConsumeCLI(context.Background(), CLIConsumeRequest{
		SchemaVersion: CLISchemaVersion, Code: strings.Repeat("a", 43), CodeVerifier: strings.Repeat("b", 43), ReceiverID: strings.Repeat("c", 43),
		ReceiverURI: "http://127.0.0.1:43123/callback", Audience: CLIAudience,
	})
	if err != nil || identity.SessionExpiresAt.IsZero() {
		t.Fatalf("ConsumeCLI = %#v, %v", identity, err)
	}
}

func TestCLIClientRejectsInsecureRemoteConsumeURL(t *testing.T) {
	if _, err := NewCLIClient("http://vibes.college/api/tag-cli/consume", strings.Repeat("s", 32)); err == nil {
		t.Fatal("expected insecure remote URL to fail closed")
	}
}

func TestValidateCLIConfigAllowsDisabledButRejectsPartialConfiguration(t *testing.T) {
	if err := ValidateCLIConfig("", ""); err != nil {
		t.Fatalf("all-empty config should be explicitly disabled: %v", err)
	}
	for _, config := range []struct{ url, secret string }{
		{url: "https://vibes.college/api/tag-cli/consume"},
		{secret: "cli-exchange-service-secret-32-bytes-minimum"},
		{url: "http://vibes.college/api/tag-cli/consume", secret: "cli-exchange-service-secret-32-bytes-minimum"},
		{url: "https://vibes.college/api/tag-handoff/consume", secret: "cli-exchange-service-secret-32-bytes-minimum"},
		{url: "https://vibes.college/api/tag-cli/consume?debug=1", secret: "cli-exchange-service-secret-32-bytes-minimum"},
	} {
		if err := ValidateCLIConfig(config.url, config.secret); err == nil {
			t.Fatalf("partial/invalid config was accepted: %#v", config)
		}
	}
}

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
			UserID:                     "vibes-user-1",
			SessionID:                  "vibes-session-1",
			WorkspaceID:                "vibes-workspace-1",
			WorkspaceSlug:              "design-lab",
			WorkspaceName:              "Design Lab",
			Name:                       "VIBES User",
			Email:                      "",
			Role:                       "owner",
			AccountEpoch:               7,
			SessionWorkspaceGeneration: 5,
			AuthorityVersion:           11,
			MembershipGeneration:       3,
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

func TestClientRejectsHandoffWithoutSessionAuthorityBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Identity{
			UserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
			WorkspaceSlug: "design-lab", WorkspaceName: "Design Lab",
			Name: "VIBES User", Email: "user@example.test", Role: "member",
		})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "local-service-secret-at-least-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Consume(context.Background(), "opaque-code", "vibes-tag-local"); !errors.Is(err, ErrRejected) {
		t.Fatalf("Consume error = %v, want ErrRejected", err)
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
