package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type tagHTTPClock struct{ now time.Time }

func (c tagHTTPClock) Now() time.Time { return c.now }

type tagHTTPMirrorFixture struct {
	usersByMultica map[string]string
	usersByVIBES   map[string]string
	workspaces     map[string]string
	err            error
}

func (f tagHTTPMirrorFixture) MulticaUserID(_ context.Context, vibesUserID string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	id, ok := f.usersByVIBES[vibesUserID]
	return id, ok, nil
}

func (f tagHTTPMirrorFixture) VIBESUserID(_ context.Context, multicaUserID string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	id, ok := f.usersByMultica[multicaUserID]
	return id, ok, nil
}

func (f tagHTTPMirrorFixture) MulticaWorkspaceID(_ context.Context, vibesWorkspaceID string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	id, ok := f.workspaces[vibesWorkspaceID]
	return id, ok, nil
}

func setTagHTTPAssertion(t *testing.T, request *http.Request, assertion tagaccess.HTTPAssertion, key []byte) {
	t.Helper()
	payload, err := json.Marshal(assertion)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := tagaccess.CanonicalHTTPAssertion(assertion)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	request.Header.Set(tagaccess.HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
	request.Header.Set(tagaccess.HTTPAssertionSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set(tagaccess.HTTPAssertionKeyIDHeader, assertion.KeyID)
}

func applyTagProjection(t *testing.T, access *tagaccess.AuthenticatedAccess, keyID string, key []byte, event tagaccess.ProjectionEvent) {
	t.Helper()
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		DeliveryID:    event.EventID, CorrelationID: "correlation-" + event.EventID,
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: event.AuthorityVersion - 1,
			Projections: []tagaccess.ProjectionEvent{event},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: keyID},
	}
	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	if _, err := access.Ingress.Deliver(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
}

func applyIdentityRestriction(t *testing.T, access *tagaccess.AuthenticatedAccess, keyID string, key []byte, delivery tagaccess.IdentityRestrictionDelivery) {
	t.Helper()
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, Delivery: delivery,
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: keyID},
	}
	payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	if _, err := access.IdentityIngress.Deliver(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
}

type tagHTTPFixture struct {
	store         *tagaccess.MemoryStore
	access        *tagaccess.AuthenticatedAccess
	authenticator *TagHTTPBrowserAuthenticator
	mirrors       tagHTTPMirrorFixture
	now           time.Time
	assertionKey  []byte
	projectionKey []byte
}

func setupTagHTTP(t *testing.T) tagHTTPFixture {
	t.Helper()
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	projectionKey := []byte("projection-authentication-key-32-bytes")
	store := tagaccess.NewMemoryStore()
	access, err := tagaccess.NewAuthenticatedAccess(store, tagHTTPClock{now}, map[string][]byte{"projection": projectionKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyTagProjection(t, access, "projection", projectionKey, tagaccess.ProjectionEvent{
		EventID: "event-1", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 7,
		MembershipGeneration: 3, AuthorityVersion: 1,
	})
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID:   tagaccess.BrowserTagSessionID("vibes-user-1", "vibes-session-1"),
		VIBESSessionID: "vibes-session-1", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		AccountEpoch: 7, SessionWorkspaceGeneration: 5, MembershipGeneration: 3, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	assertionKey := []byte("gateway-assertion-key-at-least-32-bytes")
	verifier, err := tagaccess.NewHTTPAssertionVerifier(map[string][]byte{"gateway": assertionKey}, tagHTTPClock{now})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := tagaccess.NewMemoryHTTPAssertionReplayStore(tagHTTPClock{now})
	if err != nil {
		t.Fatal(err)
	}
	mirrors := tagHTTPMirrorFixture{
		usersByMultica: map[string]string{"11111111-1111-1111-1111-111111111111": "vibes-user-1"},
		usersByVIBES:   map[string]string{"vibes-user-1": "11111111-1111-1111-1111-111111111111"},
		workspaces:     map[string]string{"vibes-workspace-1": "22222222-2222-2222-2222-222222222222"},
	}
	authenticator, err := NewTagHTTPBrowserAuthenticator(access.Gate, verifier, replay, mirrors)
	if err != nil {
		t.Fatal(err)
	}
	return tagHTTPFixture{store, access, authenticator, mirrors, now, assertionKey, projectionKey}
}

func fixtureTagHTTPAssertion(now time.Time) tagaccess.HTTPAssertion {
	return tagaccess.HTTPAssertion{
		SchemaVersion: tagaccess.HTTPAssertionSchemaVersion, Issuer: tagaccess.HTTPAssertionIssuer,
		Audience: tagaccess.HTTPAssertionAudience, KeyID: "gateway", Method: http.MethodGet,
		Path: "/api/issues", Query: "", BodySHA256: "",
		UserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1", SessionID: "vibes-session-1",
		AccountEpoch: 7, SessionWorkspaceGeneration: 5, AuthorityVersion: 1, MembershipGeneration: 3,
		IssuedAt: now.Add(-time.Second).UnixMilli(), ExpiresAt: now.Add(4 * time.Second).UnixMilli(),
		RequestID: "request-1", Nonce: "nonce-1",
	}
}

func newTagHTTPRequest(t *testing.T, now time.Time, key []byte, mutate func(*tagaccess.HTTPAssertion)) *http.Request {
	t.Helper()
	assertion := fixtureTagHTTPAssertion(now)
	if mutate != nil {
		mutate(&assertion)
	}
	target := assertion.Path
	if assertion.Query != "" {
		target += "?" + assertion.Query
	}
	request := httptest.NewRequest(assertion.Method, target, nil)
	request.Header.Set("X-Workspace-ID", "33333333-3333-3333-3333-333333333333")
	request.Header.Set("X-VIBES-User-ID", "forged-browser-user")
	request.Header.Set("Authorization", "Bearer forged-native-fallback")
	request.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: "forged-cookie"})
	setTagHTTPAssertion(t, request, assertion, key)
	return request
}

func tagHTTPPipeline(fixture tagHTTPFixture, next http.Handler) http.Handler {
	return AuthenticateTagHTTPBrowser(fixture.authenticator)(
		Auth(nil, nil, nil)(RequireTagHTTP(fixture.mirrors)(next)),
	)
}

func tagHTTPRequest(t *testing.T, fixture tagHTTPFixture, mutate func(*tagaccess.HTTPAssertion), roles ...tagaccess.Role) *httptest.ResponseRecorder {
	t.Helper()
	expectedRole := tagaccess.RoleOwner
	if len(roles) > 0 {
		expectedRole = roles[0]
	}
	request := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, mutate)
	response := httptest.NewRecorder()
	tagHTTPPipeline(fixture, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := TagHTTPIdentityFromContext(r.Context())
		if !ok || identity.VIBESUserID != "vibes-user-1" || identity.Role != expectedRole || identity.SessionWorkspaceGeneration != 5 {
			t.Fatalf("missing Tag HTTP identity: %#v, ok=%v", identity, ok)
		}
		if r.Header.Get("X-User-ID") != "11111111-1111-1111-1111-111111111111" ||
			r.Header.Get("X-Workspace-ID") != "22222222-2222-2222-2222-222222222222" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" ||
			r.Header.Get("X-VIBES-User-ID") != "" || r.Header.Get(tagaccess.HTTPAssertionHeader) != "" ||
			r.Header.Get(tagaccess.HTTPAssertionSignatureHeader) != "" || r.Header.Get(tagaccess.HTTPAssertionKeyIDHeader) != "" {
			t.Fatalf("upstream headers were not normalized: %#v", r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	return response
}

func TestTagHTTPBrowserAssertionAuthenticatesBeforeNativeAuth(t *testing.T) {
	fixture := setupTagHTTP(t)
	response := tagHTTPRequest(t, fixture, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestTagHTTPBrowserAssertionFailsClosedOnPartialDuplicateAndUnavailableAdapter(t *testing.T) {
	fixture := setupTagHTTP(t)
	for name, mutate := range map[string]func(*http.Request){
		"missing signature": func(r *http.Request) { r.Header.Del(tagaccess.HTTPAssertionSignatureHeader) },
		"duplicate payload": func(r *http.Request) {
			r.Header.Add(tagaccess.HTTPAssertionHeader, r.Header.Get(tagaccess.HTTPAssertionHeader))
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, nil)
			mutate(request)
			response := httptest.NewRecorder()
			tagHTTPPipeline(fixture, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("invalid assertion reached handler")
			})).ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}
	request := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, nil)
	response := httptest.NewRecorder()
	AuthenticateTagHTTPBrowser(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unconfigured assertion adapter reached handler")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable adapter status = %d", response.Code)
	}
}

func TestTagHTTPRejectsDirectOriginNativeCredentialFallback(t *testing.T) {
	fixture := setupTagHTTP(t)
	protected := tagHTTPPipeline(fixture, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("stale native credential reached handler")
	}))

	t.Run("cookie", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "11111111-1111-1111-1111-111111111111", "exp": time.Now().Add(time.Hour).Unix(),
		})
		signed, err := token.SignedString(auth.JWTSecret())
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
		request.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: signed})
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("cookie status = %d, body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("pat", func(t *testing.T) {
		redisClient, redisMock := redismock.NewClientMock()
		const rawToken = "mul_stale_mirrored_pat"
		redisMock.ExpectGet("mul:auth:pat:" + auth.HashToken(rawToken)).SetVal("11111111-1111-1111-1111-111111111111")
		request := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
		request.Header.Set("Authorization", "Bearer "+rawToken)
		response := httptest.NewRecorder()
		AuthenticateTagHTTPBrowser(fixture.authenticator)(
			Auth(nil, auth.NewPATCache(redisClient), nil)(RequireTagHTTP(fixture.mirrors)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("stale PAT reached handler")
			}))),
		).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("PAT status = %d, body=%s", response.Code, response.Body.String())
		}
		if err := redisMock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTagHTTPRejectsStaleCrossWorkspaceGapConflictAndOutage(t *testing.T) {
	for name, mutate := range map[string]func(*tagaccess.HTTPAssertion){
		"stale generation": func(a *tagaccess.HTTPAssertion) { a.MembershipGeneration = 2 },
		"stale version":    func(a *tagaccess.HTTPAssertion) { a.AuthorityVersion = 2 },
		"stale session Workspace generation": func(a *tagaccess.HTTPAssertion) {
			a.SessionWorkspaceGeneration = 4
		},
		"cross Workspace": func(a *tagaccess.HTTPAssertion) { a.WorkspaceID = "vibes-workspace-2" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := setupTagHTTP(t)
			if name == "cross Workspace" {
				fixture.mirrors.workspaces["vibes-workspace-2"] = "44444444-4444-4444-4444-444444444444"
			}
			response := tagHTTPRequest(t, fixture, mutate)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}

	for name, event := range map[string]tagaccess.ProjectionEvent{
		"gap": {
			EventID: "event-3", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
			Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 7,
			MembershipGeneration: 3, AuthorityVersion: 3,
		},
		"conflict": {
			EventID: "event-conflict", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
			Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7,
			MembershipGeneration: 3, AuthorityVersion: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := setupTagHTTP(t)
			applyTagProjection(t, fixture.access, "projection", fixture.projectionKey, event)
			response := tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) { a.AuthorityVersion = event.AuthorityVersion })
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}

	fixture := setupTagHTTP(t)
	fixture.store.SetFailure(errors.New("projection store unavailable"))
	response := tagHTTPRequest(t, fixture, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("outage status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestTagHTTPAtomicallyConsumesEveryExactRequestTuple(t *testing.T) {
	fixture := setupTagHTTP(t)
	var handled int
	var mu sync.Mutex
	handler := tagHTTPPipeline(fixture, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		handled++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	request := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", first.Code)
	}
	replay := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, nil)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, replay)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, body=%s", second.Code, second.Body.String())
	}
	differentTuple := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, func(a *tagaccess.HTTPAssertion) {
		a.Nonce = "nonce-2"
	})
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, differentTuple)
	if third.Code != http.StatusNoContent {
		t.Fatalf("different tuple status = %d, body=%s", third.Code, third.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if handled != 2 {
		t.Fatalf("handled = %d, want 2", handled)
	}
}

func TestTagHTTPTracksRoleRemovalReinviteAndSessionWorkspaceSwitch(t *testing.T) {
	fixture := setupTagHTTP(t)
	applyTagProjection(t, fixture.access, "projection", fixture.projectionKey, tagaccess.ProjectionEvent{
		EventID: "event-2", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7,
		MembershipGeneration: 3, AuthorityVersion: 2,
	})
	response := tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
		a.AuthorityVersion = 2
		a.RequestID, a.Nonce = "request-2", "nonce-2"
	}, tagaccess.RoleMember)
	if response.Code != http.StatusNoContent {
		t.Fatalf("downgrade status = %d, body=%s", response.Code, response.Body.String())
	}

	applyTagProjection(t, fixture.access, "projection", fixture.projectionKey, tagaccess.ProjectionEvent{
		EventID: "event-3", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleMember, Status: tagaccess.StatusRemoved, AccountEpoch: 7,
		MembershipGeneration: 3, AuthorityVersion: 3,
	})
	response = tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
		a.AuthorityVersion = 3
		a.RequestID, a.Nonce = "request-3", "nonce-3"
	})
	if response.Code != http.StatusForbidden {
		t.Fatalf("removal status = %d, body=%s", response.Code, response.Body.String())
	}

	applyTagProjection(t, fixture.access, "projection", fixture.projectionKey, tagaccess.ProjectionEvent{
		EventID: "event-4", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7,
		MembershipGeneration: 4, AuthorityVersion: 4,
	})
	response = tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
		a.AuthorityVersion, a.MembershipGeneration = 4, 4
		a.RequestID, a.Nonce = "request-4", "nonce-4"
	}, tagaccess.RoleMember)
	if response.Code != http.StatusNoContent {
		t.Fatalf("reinvite status = %d, body=%s", response.Code, response.Body.String())
	}

	const (
		workspaceB        = "vibes-workspace-2"
		multicaWorkspaceB = "44444444-4444-4444-4444-444444444444"
	)
	fixture.mirrors.workspaces[workspaceB] = multicaWorkspaceB
	applyTagProjection(t, fixture.access, "projection", fixture.projectionKey, tagaccess.ProjectionEvent{
		EventID: "workspace-b-1", VIBESUserID: "vibes-user-1", WorkspaceID: workspaceB,
		Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7,
		MembershipGeneration: 1, AuthorityVersion: 1,
	})
	withoutHandoff := tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
		a.WorkspaceID, a.SessionWorkspaceGeneration = workspaceB, 6
		a.MembershipGeneration, a.AuthorityVersion = 1, 1
		a.RequestID, a.Nonce = "request-5", "nonce-5"
	})
	if withoutHandoff.Code != http.StatusForbidden {
		t.Fatalf("unbound switch status = %d, body=%s", withoutHandoff.Code, withoutHandoff.Body.String())
	}
	if err := fixture.access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID:   tagaccess.BrowserTagSessionID("vibes-user-1", "vibes-session-1"),
		VIBESSessionID: "vibes-session-1", VIBESUserID: "vibes-user-1", WorkspaceID: workspaceB,
		AccountEpoch: 7, SessionWorkspaceGeneration: 6, MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: fixture.now.Add(time.Hour), GrantExpiresAt: fixture.now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	request := newTagHTTPRequest(t, fixture.now, fixture.assertionKey, func(a *tagaccess.HTTPAssertion) {
		a.WorkspaceID, a.SessionWorkspaceGeneration = workspaceB, 6
		a.MembershipGeneration, a.AuthorityVersion = 1, 1
		a.RequestID, a.Nonce = "request-6", "nonce-6"
	})
	response = httptest.NewRecorder()
	tagHTTPPipeline(fixture, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := TagHTTPIdentityFromContext(r.Context())
		if !ok || identity.VIBESWorkspaceID != workspaceB || identity.MulticaWorkspaceID != multicaWorkspaceB || identity.SessionWorkspaceGeneration != 6 {
			t.Fatalf("switched identity = %#v", identity)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("bound switch status = %d, body=%s", response.Code, response.Body.String())
	}
	oldWorkspace := tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
		a.AuthorityVersion, a.MembershipGeneration = 4, 4
		a.RequestID, a.Nonce = "request-7", "nonce-7"
	})
	if oldWorkspace.Code != http.StatusForbidden {
		t.Fatalf("superseded workspace status = %d, body=%s", oldWorkspace.Code, oldWorkspace.Body.String())
	}
}

func TestTagHTTPRejectsLoggedOutAndBannedSessionsOnNextRequest(t *testing.T) {
	for name, delivery := range map[string]tagaccess.IdentityRestrictionDelivery{
		"logged out session": {
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "logout-1", CorrelationID: "correlation-logout-1",
			IdempotencyKey: "logout-key-1", VIBESUserID: "vibes-user-1", VIBESSessionID: "vibes-session-1",
			AccountEpoch: 7, IdentityRestrictionVersion: 1,
			CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "vibes-user-1", VIBESSessionID: "vibes-session-1"},
		},
		"banned account": {
			Kind: tagaccess.IdentityRestrictionAccountBan, EventID: "ban-1", CorrelationID: "correlation-ban-1",
			IdempotencyKey: "ban-key-1", VIBESUserID: "vibes-user-1", AccountEpoch: 8, IdentityRestrictionVersion: 1,
			CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "vibes-user-1"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := setupTagHTTP(t)
			applyIdentityRestriction(t, fixture.access, "projection", fixture.projectionKey, delivery)
			response := tagHTTPRequest(t, fixture, func(a *tagaccess.HTTPAssertion) {
				a.RequestID, a.Nonce = "restricted-request", "restricted-nonce"
			})
			if response.Code != http.StatusForbidden {
				t.Fatalf("restricted status = %d, body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTagHTTPPreservesExplicitServicesAndFencesAuthorityWriters(t *testing.T) {
	fixture := setupTagHTTP(t)
	request := httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	request.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
	request.Header.Set("X-Actor-Source", "task_token")
	response := httptest.NewRecorder()
	RequireTagHTTP(fixture.mirrors)(DenyMirroredAuthorityWriter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("service identity reached native authority writer")
	}))).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("service writer status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	request.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
	request.Header.Set("X-Actor-Source", "task_token")
	response = httptest.NewRecorder()
	fixture.mirrors.err = errors.New("mirror store unavailable")
	RequireTagHTTP(fixture.mirrors)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("service status = %d", response.Code)
	}

	fixture = setupTagHTTP(t)
	request = newTagHTTPRequest(t, fixture.now, fixture.assertionKey, nil)
	response = httptest.NewRecorder()
	tagHTTPPipeline(fixture, DenyMirroredAuthorityWriter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("mirrored browser reached native authority writer")
	}))).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("browser writer status = %d", response.Code)
	}
}

func TestTagHTTPBindsUnsafeRawBodyBytes(t *testing.T) {
	fixture := setupTagHTTP(t)
	body := []byte(`{"title":"raw body"}`)
	digest := sha256.Sum256(body)
	assertion := fixtureTagHTTPAssertion(fixture.now)
	assertion.Method = http.MethodPost
	assertion.BodySHA256 = hex.EncodeToString(digest[:])
	assertion.RequestID, assertion.Nonce = "unsafe-request", "unsafe-nonce"
	request := httptest.NewRequest(http.MethodPost, assertion.Path, bytes.NewReader(body))
	setTagHTTPAssertion(t, request, assertion, fixture.assertionKey)
	response := httptest.NewRecorder()
	tagHTTPPipeline(fixture, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		restored := new(bytes.Buffer)
		_, _ = restored.ReadFrom(r.Body)
		if !bytes.Equal(restored.Bytes(), body) {
			t.Fatalf("restored body = %q", restored.Bytes())
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unsafe status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestStripWorkspaceScopeQueryPreservesSignedPairOrder(t *testing.T) {
	target := &url.URL{RawQuery: "b=2&workspace%5Fid=forged&a=first&a=second&workspace_slug=forged"}
	stripWorkspaceScopeQuery(target)
	if target.RawQuery != "b=2&a=first&a=second" {
		t.Fatalf("RawQuery = %q", target.RawQuery)
	}
}
