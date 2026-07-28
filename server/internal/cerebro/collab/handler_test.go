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
	"github.com/gorilla/websocket"
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
