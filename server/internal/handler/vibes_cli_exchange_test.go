package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/multica-ai/multica/server/internal/vibeshandoff"
)

func TestVIBESCLIExchangeMintsOneScopedPATAndRejectsReplay(t *testing.T) {
	const secret = "cli-exchange-service-secret-32-bytes-minimum"
	code := strings.Repeat("a", 43)
	verifier := strings.Repeat("b", 43)
	receiverID := strings.Repeat("c", 43)
	receiverURI := "http://127.0.0.1:43123/callback"
	var consumed atomic.Bool
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret || consumed.Swap(true) {
			http.Error(w, "rejected", http.StatusUnauthorized)
			return
		}
		var request vibeshandoff.CLIConsumeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Code != code || request.CodeVerifier != verifier || request.ReceiverID != receiverID || request.ReceiverURI != receiverURI {
			http.Error(w, "rejected", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(vibeshandoff.CLIIdentity{
			SchemaVersion: vibeshandoff.CLISchemaVersion,
			Identity: vibeshandoff.Identity{
				UserID: "vibes-cli-user-1", SessionID: "vibes-cli-session-1", WorkspaceID: "vibes-cli-workspace-1",
				WorkspaceSlug: "vibes-cli-workspace", WorkspaceName: "VIBES CLI Workspace", Name: "VIBES CLI User",
				Role: "owner", AccountEpoch: 1, SessionWorkspaceGeneration: 1, AuthorityVersion: 1, MembershipGeneration: 1,
			},
			SessionExpiresAt: time.Now().Add(time.Hour).UTC(),
		})
	}))
	defer issuer.Close()

	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "vibes-cli-delivery-1", CorrelationID: "vibes-cli-correlation-1",
		Delivery: tagaccess.ProjectionDelivery{Kind: tagaccess.DeliveryIncremental, Projections: []tagaccess.ProjectionEvent{{
			EventID: "vibes-cli-event-1", VIBESUserID: "vibes-cli-user-1", WorkspaceID: "vibes-cli-workspace-1",
			Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 1, MembershipGeneration: 1, AuthorityVersion: 1,
		}}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	canonical, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	envelope.Authentication.MAC = mac.Sum(nil)
	if _, err := access.Ingress.Deliver(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}

	previousConfig := testHandler.cfg
	previousGate := testHandler.TagAccessGate
	testHandler.cfg.VIBESCLIConsumeURL = issuer.URL
	testHandler.cfg.VIBESCLIServiceSecret = secret
	testHandler.TagAccessGate = access.Gate
	t.Cleanup(func() {
		testHandler.cfg = previousConfig
		testHandler.TagAccessGate = previousGate
	})
	cleanupVIBESMirrorTest(t)
	_, _ = testHandler.DB.Exec(context.Background(), `DELETE FROM vibes_cli_pat_binding WHERE vibes_user_id = 'vibes-cli-user-1'`)
	t.Cleanup(func() {
		_, _ = testHandler.DB.Exec(context.Background(), `DELETE FROM vibes_cli_pat_binding WHERE vibes_user_id = 'vibes-cli-user-1'`)
		cleanupVIBESMirrorTest(t)
	})

	body, _ := json.Marshal(vibesCLIExchangeRequest{
		Code: code, CodeVerifier: verifier, ReceiverID: receiverID, ReceiverURI: receiverURI,
		Audience: vibeshandoff.CLIAudience, DeviceName: "CLI (test-host)",
	})
	call := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		testHandler.VIBESCLIExchange(response, httptest.NewRequest(http.MethodPost, "/api/auth/vibes-cli-exchange", bytes.NewReader(body)))
		return response
	}
	first := call()
	if first.Code != http.StatusOK {
		t.Fatalf("first exchange = %d: %s", first.Code, first.Body.String())
	}
	var result vibesCLIExchangeResponse
	if err := json.NewDecoder(first.Body).Decode(&result); err != nil || !strings.HasPrefix(result.Token, "mul_") || result.WorkspaceID == "" {
		t.Fatalf("invalid exchange response: %#v, %v", result, err)
	}
	binding, err := testHandler.Queries.GetVIBESCLIPATBindingByTokenHash(context.Background(), auth.HashToken(result.Token))
	if err != nil || binding.VibesSessionID != "vibes-cli-session-1" || binding.VibesWorkspaceID != "vibes-cli-workspace-1" {
		t.Fatalf("binding = %#v, %v", binding, err)
	}

	// Exercise the same protected chain used by /api/me. The bound PAT must
	// carry a private mirrored identity context through RequireTagHTTP, while a
	// forged caller workspace remains unable to replace the server binding.
	mirrors := middleware.NewPostgresTagHTTPMirrorResolver(testHandler.DB)
	protected := middleware.Auth(testHandler.Queries, nil, nil, access.Gate)(
		middleware.RequireTagHTTP(mirrors)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := middleware.TagHTTPIdentityFromContext(r.Context())
			if !ok || !identity.Mirrored || identity.Role != tagaccess.RoleOwner ||
				identity.MulticaWorkspaceID != result.WorkspaceID ||
				middleware.ResolveWorkspaceIDFromRequest(r, testHandler.Queries) != result.WorkspaceID {
				t.Fatalf("bound identity was not preserved: %#v", identity)
			}
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/me?workspace_id=00000000-0000-0000-0000-000000000001", nil)
	protectedRequest.Header.Set("Authorization", "Bearer "+result.Token)
	protectedRequest.Header.Set("X-Actor-Source", "task_token")
	protectedRequest.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000002")
	protectedResponse := httptest.NewRecorder()
	protected.ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusNoContent {
		t.Fatalf("bound PAT protected request = %d: %s", protectedResponse.Code, protectedResponse.Body.String())
	}

	requestStatus := func(handler http.Handler) int {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		request.Header.Set("Authorization", "Bearer "+result.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for name, unavailable := range map[string]http.Handler{
		"regular": middleware.Auth(testHandler.Queries, nil, nil, nil)(next),
		"daemon":  middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, mirrors, nil)(next),
	} {
		if status := requestStatus(unavailable); status != http.StatusServiceUnavailable {
			t.Fatalf("%s bound PAT without Gate = %d, want %d", name, status, http.StatusServiceUnavailable)
		}
	}

	logout := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		Delivery: tagaccess.IdentityRestrictionDelivery{
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "vibes-cli-logout-1",
			CorrelationID: "vibes-cli-logout-correlation-1", IdempotencyKey: "vibes-cli-logout-key-1",
			VIBESUserID: "vibes-cli-user-1", VIBESSessionID: "vibes-cli-session-1",
			AccountEpoch: 1, IdentityRestrictionVersion: 1,
			CloseTarget: tagaccess.ConnectionCloseTarget{
				Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "vibes-cli-user-1", VIBESSessionID: "vibes-cli-session-1",
			},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	canonicalLogout, err := tagaccess.CanonicalIdentityRestrictionEnvelope(logout)
	if err != nil {
		t.Fatal(err)
	}
	logoutMAC := hmac.New(sha256.New, key)
	_, _ = logoutMAC.Write(canonicalLogout)
	logout.Authentication.MAC = logoutMAC.Sum(nil)
	if _, err := access.IdentityIngress.Deliver(context.Background(), logout); err != nil {
		t.Fatal(err)
	}
	for name, denied := range map[string]http.Handler{
		"regular": middleware.Auth(testHandler.Queries, nil, nil, access.Gate)(next),
		"daemon":  middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, mirrors, access.Gate)(next),
	} {
		if status := requestStatus(denied); status != http.StatusForbidden {
			t.Fatalf("%s logged-out bound PAT = %d, want %d", name, status, http.StatusForbidden)
		}
	}
	if replay := call(); replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay exchange = %d: %s", replay.Code, replay.Body.String())
	}
}
