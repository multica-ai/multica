package dictation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testUserID      = "00000000-0000-0000-0000-000000000001"
	otherTestUserID = "00000000-0000-0000-0000-000000000002"
	testWorkspaceID = "00000000-0000-0000-0000-000000000101"
)

func TestStreamSendsErrorWhenBackendMissing(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)
	handler := New(Options{Queries: newMemberQueries(testUserID, testWorkspaceID)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(r.Context(), testWorkspaceID, db.Member{})
		r = r.WithContext(ctx)
		r.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: tokenString})
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

func TestStreamAcceptsBrowserFirstMessageAuth(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)

	handler := New(Options{Queries: newMemberQueries(testUserID, testWorkspaceID)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(r.Context(), testWorkspaceID, db.Member{})
		handler.Stream(w, r.WithContext(ctx))
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth","payload":{"token":"`+tokenString+`"}}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"type":"auth_ack"`) {
		t.Fatalf("expected auth_ack, got %s", got)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read backend error: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"code":"backend_not_configured"`) {
		t.Fatalf("expected backend_not_configured error, got %s", got)
	}
}

func TestStreamRejectsMembershipWhenQueriesMissing(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)
	handler := New(Options{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(r.Context(), testWorkspaceID, db.Member{})
		handler.Stream(w, r.WithContext(ctx))
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth","payload":{"token":"`+tokenString+`"}}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"type":"auth_ack"`) {
		t.Fatalf("expected auth_ack, got %s", got)
	}
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read unauthorized error: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"code":"unauthorized"`) {
		t.Fatalf("expected unauthorized error, got %s", got)
	}
}

func TestStreamIgnoresClientControlledXUserID(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)
	upstreamUserID := make(chan string, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamUserID <- r.URL.Query().Get("user_id")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade: %v", err)
			return
		}
		conn.Close()
	}))
	defer upstream.Close()

	handler := New(Options{StreamURL: upstream.URL, Queries: newMemberQueries(testUserID, testWorkspaceID)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(context.Background(), testWorkspaceID, db.Member{})
		r = r.WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+tokenString)
		r.Header.Set("X-User-ID", otherTestUserID)
		handler.Stream(w, r)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case got := <-upstreamUserID:
		if got != testUserID {
			t.Fatalf("upstream user_id = %q, want authenticated token subject %q", got, testUserID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream connection")
	}
}

func TestStreamProxiesTextAndBinaryFrames(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("workspace_id"); got != testWorkspaceID {
			t.Errorf("workspace_id query = %q", got)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != testWorkspaceID {
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

	handler := New(Options{StreamURL: upstream.URL, Queries: newMemberQueries(testUserID, testWorkspaceID)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.SetMemberContext(context.Background(), testWorkspaceID, db.Member{})
		r = r.WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+tokenString)
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

func signedTestToken(t *testing.T, userID string) string {
	t.Helper()
	t.Setenv("JWT_SECRET", strings.Repeat("a", auth.MinJWTSecretBytes))
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID})
	tokenString, err := token.SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenString
}

func newMemberQueries(userID, workspaceID string) *db.Queries {
	return db.New(&memberDB{
		userID:      util.MustParseUUID(userID),
		workspaceID: util.MustParseUUID(workspaceID),
	})
}

type memberDB struct {
	db.DBTX
	userID      pgtype.UUID
	workspaceID pgtype.UUID
}

func (m *memberDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return &memberRow{ok: len(args) == 2 && args[0] == m.userID && args[1] == m.workspaceID}
}

func (m *memberDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0"), nil
}

type memberRow struct {
	pgx.Row
	ok bool
}

func (m *memberRow) Scan(dest ...interface{}) error {
	if !m.ok {
		return pgx.ErrNoRows
	}
	return nil
}
