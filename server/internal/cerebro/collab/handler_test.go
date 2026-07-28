package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/auth"
)

// allowAll says yes to everything, so any peer that gets past authentication
// reaches a welcome. A test that still sees auth_error therefore failed at
// authentication and nowhere else.
type allowAll struct{ asked []string }

func (a *allowAll) CanSee(_ context.Context, _, userID string) (bool, error) {
	a.asked = append(a.asked, userID)
	return true, nil
}

func (a *allowAll) CanEdit(_ context.Context, _, _ string) (bool, error) { return true, nil }

func (a *allowAll) LiveEnabled(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

type controlledAccess struct {
	enabled  bool
	readOnly map[string]bool
}

func (a *controlledAccess) CanSee(context.Context, string, string) (bool, error) {
	return true, nil
}

func (a *controlledAccess) CanEdit(_ context.Context, _, userID string) (bool, error) {
	return !a.readOnly[userID], nil
}

func (a *controlledAccess) LiveEnabled(context.Context, string, string) (bool, error) {
	return a.enabled, nil
}

func authHeader(t *testing.T, userID string) http.Header {
	t.Helper()
	t.Setenv("JWT_SECRET", strings.Repeat("a", auth.MinJWTSecretBytes))
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
	}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign auth token: %v", err)
	}
	header := http.Header{}
	header.Set("Cookie", (&http.Cookie{Name: auth.AuthCookieName, Value: token}).String())
	return header
}

func dialCollab(t *testing.T, access Access, header http.Header) (*websocket.Conn, func()) {
	t.Helper()

	h := &Handler{
		Rooms:        NewRegistry(),
		Access:       access,
		CheckOrigin:  func(*http.Request) bool { return true },
		WriteTimeout: 2 * time.Second,
	}
	router := chi.NewRouter()
	router.Get("/api/cerebro/notes/{id}/collab", h.ServeWS)
	srv := httptest.NewServer(router)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/api/cerebro/notes/11111111-1111-4111-8111-111111111111/collab"
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() { conn.Close(); srv.Close() }
}

func firstMessageType(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return msg.Type
}

func readUntilType(t *testing.T, conn *websocket.Conn, want string, out any) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %q: %v", want, err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode envelope %q: %v", data, err)
		}
		if envelope.Type != want {
			continue
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				t.Fatalf("decode %q %q: %v", want, data, err)
			}
		}
		return
	}
}

// This endpoint is registered outside the auth middleware, so an X-User-ID
// header on it is never a value the middleware stamped — it is whatever the
// caller typed. Honouring it let anyone who can reach the route claim any user
// id and read or write that person's notes. The header must carry no authority
// at all: the connection still has to authenticate, and it fails when it
// cannot.
func TestSpoofedUserHeaderDoesNotAuthenticate(t *testing.T) {
	victim := "22222222-2222-4222-8222-222222222222"
	access := &allowAll{}

	header := http.Header{}
	header.Set("X-User-ID", victim)
	conn, cleanup := dialCollab(t, access, header)
	defer cleanup()

	// Only an authenticating connection is asked for a token, so being asked at
	// all is the proof that the header bought nothing. Answer with a garbage one.
	if err := conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"type":"auth","token":"not-a-real-token"}`)); err != nil {
		t.Fatalf("write auth frame: %v", err)
	}

	if got := firstMessageType(t, conn); got != MsgAuthError {
		t.Fatalf("a spoofed X-User-ID must not authenticate: want %q, got %q", MsgAuthError, got)
	}
	for _, asked := range access.asked {
		if asked == victim {
			t.Fatalf("note access was evaluated as the spoofed user %q", victim)
		}
	}
}

func TestDisabledLiveEditingRejectsDirectConnection(t *testing.T) {
	access := &controlledAccess{enabled: false}
	conn, cleanup := dialCollab(
		t,
		access,
		authHeader(t, "22222222-2222-4222-8222-222222222222"),
	)
	defer cleanup()

	var msg SimpleMessage
	readUntilType(t, conn, MsgAuthError, &msg)
	if msg.Reason != "live editing is disabled" {
		t.Fatalf("disabled flag reason = %q, want %q", msg.Reason, "live editing is disabled")
	}
}

func TestReadOnlyPeerCannotReplaceRoomSnapshot(t *testing.T) {
	const (
		editorID = "22222222-2222-4222-8222-222222222222"
		viewerID = "33333333-3333-4333-8333-333333333333"
		joinerID = "44444444-4444-4444-8444-444444444444"
	)
	access := &controlledAccess{
		enabled:  true,
		readOnly: map[string]bool{viewerID: true},
	}
	h := &Handler{
		Rooms:        NewRegistry(),
		Access:       access,
		CheckOrigin:  func(*http.Request) bool { return true },
		WriteTimeout: 2 * time.Second,
	}
	router := chi.NewRouter()
	router.Get("/api/cerebro/notes/{id}/collab", h.ServeWS)
	srv := httptest.NewServer(router)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/api/cerebro/notes/11111111-1111-4111-8111-111111111111/collab"

	dial := func(userID string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(url, authHeader(t, userID))
		if err != nil {
			t.Fatalf("dial as %s: %v", userID, err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}

	editor := dial(editorID)
	readUntilType(t, editor, MsgWelcome, nil)
	viewer := dial(viewerID)
	var viewerWelcome WelcomeMessage
	readUntilType(t, viewer, MsgWelcome, &viewerWelcome)
	if viewerWelcome.CanEdit {
		t.Fatal("viewer unexpectedly received edit access")
	}

	if err := viewer.WriteJSON(ClientMessage{
		Type:    MsgSnapshot,
		Version: 0,
		Doc:     json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`),
	}); err != nil {
		t.Fatalf("viewer sends snapshot: %v", err)
	}
	readUntilType(t, viewer, MsgError, nil)

	joiner := dial(joinerID)
	var joinerWelcome WelcomeMessage
	readUntilType(t, joiner, MsgWelcome, &joinerWelcome)
	if joinerWelcome.Snapshot != nil {
		t.Fatalf("read-only snapshot affected the room: %+v", joinerWelcome.Snapshot)
	}
}
