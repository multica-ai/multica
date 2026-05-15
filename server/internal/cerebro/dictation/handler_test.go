package dictation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStreamSendsErrorWhenBackendMissing(t *testing.T) {
	handler := New(Options{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(r.Context(), "ws-1", db.Member{})
		r = r.WithContext(ctx)
		r.Header.Set("X-User-ID", "user-1")
		handler.Stream(w, r)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"code":"backend_not_configured"`) {
		t.Fatalf("expected backend_not_configured error, got %s", got)
	}
}

func TestStreamProxiesTextAndBinaryFrames(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
			t.Errorf("workspace_id query = %q", got)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != "ws-1" {
			t.Errorf("X-Workspace-ID = %q", got)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, payload); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	handler := New(Options{StreamURL: upstream.URL})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(context.Background(), "ws-1", db.Member{})
		r = r.WithContext(ctx)
		r.Header.Set("X-User-ID", "user-1")
		handler.Stream(w, r)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"end_utterance"}`)); err != nil {
		t.Fatalf("write text: %v", err)
	}
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if mt != websocket.TextMessage || string(payload) != `{"type":"end_utterance"}` {
		t.Fatalf("unexpected text echo: type=%d payload=%s", mt, payload)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	mt, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if mt != websocket.BinaryMessage || string(payload) != string([]byte{1, 2, 3}) {
		t.Fatalf("unexpected binary echo: type=%d payload=%v", mt, payload)
	}
}

func wsURL(raw string) string {
	return "ws" + strings.TrimPrefix(raw, "http")
}
