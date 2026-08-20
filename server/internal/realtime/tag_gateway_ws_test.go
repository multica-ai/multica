package realtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type tagWebSocketMirrors struct {
	users      map[string]string
	workspaces map[string]string
}

func (m tagWebSocketMirrors) MulticaUserID(_ context.Context, id string) (string, bool, error) {
	value, ok := m.users[id]
	return value, ok, nil
}

func (m tagWebSocketMirrors) VIBESUserID(_ context.Context, id string) (string, bool, error) {
	for vibesID, multicaID := range m.users {
		if multicaID == id {
			return vibesID, true, nil
		}
	}
	return "", false, nil
}

func (m tagWebSocketMirrors) MulticaWorkspaceID(_ context.Context, id string) (string, bool, error) {
	value, ok := m.workspaces[id]
	return value, ok, nil
}

func applyTagWebSocketProjection(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, event tagaccess.ProjectionEvent) {
	t.Helper()
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		DeliveryID:    "delivery-" + event.EventID, CorrelationID: "correlation-" + event.EventID,
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: event.AuthorityVersion - 1,
			Projections: []tagaccess.ProjectionEvent{event},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "projection"},
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

func setTagWebSocketAssertion(t *testing.T, headers http.Header, assertion tagaccess.HTTPAssertion, key []byte) {
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
	headers.Set(tagaccess.HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
	headers.Set(tagaccess.HTTPAssertionSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	headers.Set(tagaccess.HTTPAssertionKeyIDHeader, assertion.KeyID)
}

func TestTagGatewayWebSocketReusesHandoffGrantReplayStoreAndExactMetadata(t *testing.T) {
	now := time.Now().UTC()
	projectionKey := []byte("projection-authentication-key-32-bytes")
	assertionKey := []byte("gateway-assertion-key-at-least-32-bytes")
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{"projection": projectionKey}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	event := tagaccess.ProjectionEvent{
		EventID: "event-1", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleAdmin, Status: tagaccess.StatusActive, AccountEpoch: 7,
		MembershipGeneration: 3, AuthorityVersion: 1,
	}
	applyTagWebSocketProjection(t, access, projectionKey, event)
	tagSessionID := tagaccess.BrowserTagSessionID(event.VIBESUserID, "vibes-session-1")
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: "vibes-session-1",
		VIBESUserID: event.VIBESUserID, WorkspaceID: event.WorkspaceID,
		AccountEpoch: 7, SessionWorkspaceGeneration: 5, MembershipGeneration: 3, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	verifier, err := tagaccess.NewWebSocketAssertionVerifier(
		map[string][]byte{"gateway": assertionKey}, tagaccess.SystemClock{},
	)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := tagaccess.NewMemoryHTTPAssertionReplayStore(tagaccess.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	hub.SetInstanceID("instance-a")
	go hub.Run()
	mirrors := tagWebSocketMirrors{
		users:      map[string]string{"vibes-user-1": "multica-user-1"},
		workspaces: map[string]string{"vibes-workspace-1": "multica-workspace-1"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, request *http.Request) {
		HandleTagGatewayWebSocket(hub, access.Gate, verifier, replay, mirrors, w, request)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?workspace_id=vibes-workspace-1"
	assertion := tagaccess.HTTPAssertion{
		SchemaVersion: tagaccess.HTTPAssertionSchemaVersion, Issuer: tagaccess.HTTPAssertionIssuer,
		Audience: tagaccess.WebSocketAssertionAudience, KeyID: "gateway", Method: http.MethodGet,
		Path: "/ws", Query: "workspace_id=vibes-workspace-1", BodySHA256: "",
		UserID: event.VIBESUserID, WorkspaceID: event.WorkspaceID, SessionID: "vibes-session-1",
		AccountEpoch: 7, SessionWorkspaceGeneration: 5, MembershipGeneration: 3, AuthorityVersion: 1,
		IssuedAt: now.Add(-time.Second).UnixMilli(), ExpiresAt: now.Add(5 * time.Second).UnixMilli(),
		RequestID: "request-ws-1", Nonce: "nonce-ws-1",
	}
	headers := http.Header{}
	// These stale native credentials are deliberately ignored by browser WS admission.
	headers.Set("Authorization", "Bearer stale-native-pat")
	headers.Set("Cookie", "multica_auth=stale-cookie")
	setTagWebSocketAssertion(t, headers, assertion, assertionKey)
	conn, response, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("dial response=%v error=%v", response, err)
	}
	defer conn.Close()
	waitFor(t, "Tag Gateway registry", func() bool { return totalClients(hub) == 1 })
	hub.mu.RLock()
	var client *Client
	for current := range hub.clients {
		client = current
	}
	hub.mu.RUnlock()
	if client == nil || client.userID != "multica-user-1" || client.workspaceID != "multica-workspace-1" ||
		client.metadata.ConnectionID == "" || client.metadata.InstanceID != "instance-a" ||
		client.metadata.VIBESUserID != event.VIBESUserID || client.metadata.WorkspaceID != event.WorkspaceID ||
		client.metadata.TagSessionID != tagSessionID || client.metadata.VIBESSessionID != "vibes-session-1" ||
		client.metadata.AccountEpoch != 7 || client.metadata.SessionWorkspaceGeneration != 5 ||
		client.metadata.MembershipGeneration != 3 || client.metadata.AuthorityVersion != 1 {
		t.Fatalf("registered client = %#v", client)
	}

	if replayed, denied, dialErr := websocket.DefaultDialer.Dial(url, headers); dialErr == nil || denied == nil || denied.StatusCode != http.StatusUnauthorized {
		if replayed != nil {
			replayed.Close()
		}
		t.Fatalf("replay response=%v error=%v, want 401", denied, dialErr)
	}

	stale := assertion
	stale.SessionWorkspaceGeneration = 4
	stale.RequestID, stale.Nonce = "request-ws-2", "nonce-ws-2"
	stale.IssuedAt, stale.ExpiresAt = time.Now().Add(-time.Second).UnixMilli(), time.Now().Add(4*time.Second).UnixMilli()
	staleHeaders := http.Header{}
	setTagWebSocketAssertion(t, staleHeaders, stale, assertionKey)
	if revived, denied, dialErr := websocket.DefaultDialer.Dial(url, staleHeaders); dialErr == nil || denied == nil || denied.StatusCode != http.StatusForbidden {
		if revived != nil {
			revived.Close()
		}
		t.Fatalf("stale generation response=%v error=%v, want 403", denied, dialErr)
	}
}

func TestNativeWebSocketGuardRejectsMirroredCookieWithoutGatewayAssertion(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	mirrors := tagWebSocketMirrors{users: map[string]string{"vibes-user-1": "multica-user-1"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, request *http.Request) {
		HandleWebSocketWithTagMirrorGuard(
			hub,
			&workspaceMembershipChecker{allowed: map[string]bool{"multica-user-1:workspace-1": true}},
			nil, nil, mirrors, w, request,
		)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	headers := http.Header{}
	headers.Set("Cookie", "multica_auth="+makeTestTokenForUser(t, "multica-user-1", ""))
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?workspace_id=workspace-1"
	conn, response, err := websocket.DefaultDialer.Dial(url, headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mirrored cookie response=%v error=%v, want 401", response, err)
	}
	if totalClients(hub) != 0 {
		t.Fatal("mirrored native fallback registered a socket")
	}
}
