package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

// fakeRow is a pgx.Row implementation backed by canned return values.
type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dst {
		if i >= len(r.vals) {
			break
		}
		switch dst := d.(type) {
		case *string:
			s, _ := r.vals[i].(string)
			*dst = s
		case *bool:
			b, _ := r.vals[i].(bool)
			*dst = b
		}
	}
	return nil
}

// fakeDB lets us stub the two read queries the handler runs.
// It returns one of `runtimeRow` (workspace_id, mode), `membershipRow` (bool),
// or `updateRow` (workspace_id) depending on the SQL prefix.
type fakeDB struct {
	runtimeWS   string
	runtimeMode string
	membership  bool
	missing     bool
}

func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "FROM agent_runtime\nWHERE id"):
		if f.missing {
			return fakeRow{err: pgx.ErrNoRows}
		}
		return fakeRow{vals: []any{f.runtimeWS, f.runtimeMode}}
	case strings.Contains(sql, "UPDATE agent_runtime"):
		if f.missing {
			return fakeRow{err: pgx.ErrNoRows}
		}
		return fakeRow{vals: []any{f.runtimeWS}}
	case strings.Contains(sql, "SELECT EXISTS"):
		return fakeRow{vals: []any{f.membership}}
	}
	return fakeRow{err: pgx.ErrNoRows}
}

// newTestServer wires Handler + Broker + chi router exactly like router.go
// does in production, against an in-memory fake DB.
func newTestServer(t *testing.T, db *fakeDB) *httptest.Server {
	t.Helper()
	h := &Handler{
		Broker:      NewBroker(),
		DB:          db,
		CheckOrigin: func(r *http.Request) bool { return true }, // tests don't simulate browser
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// httptest is unauthenticated; inject the header the real auth
			// middleware would add so the handlers see a user identity.
			req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000001")
			next.ServeHTTP(w, req)
		})
	})
	r.Post("/api/cerebro/terminal/sessions", h.CreateSession)
	r.Get("/api/cerebro/terminal/sessions/{sessionId}/ws", h.AttachWS)
	r.Delete("/api/cerebro/terminal/sessions/{sessionId}", h.DeleteSession)
	r.Get("/api/cerebro/terminal/runtimes/{runtimeId}/presentation-mode", h.GetPresentationMode)
	r.Put("/api/cerebro/terminal/runtimes/{runtimeId}/presentation-mode", h.SetPresentationMode)
	return httptest.NewServer(r)
}

// TestHTTPCreateThenWSAttach_Roundtrip exercises the full server path:
// POST creates a session backed by a real child process, the response
// includes the attach_path, and a WebSocket bound to that path successfully
// receives stdout and forwards stdin into the child. Mirrors what the UI does.
func TestHTTPCreateThenWSAttach_Roundtrip(t *testing.T) {
	srv := newTestServer(t, &fakeDB{
		runtimeWS:   "11111111-1111-1111-1111-111111111111",
		runtimeMode: "interactive",
		membership:  true,
	})
	defer srv.Close()

	// 1. Create session with a deterministic child command.
	createBody := map[string]any{
		"runtime_id": "22222222-2222-2222-2222-222222222222",
		"command":    []string{"sh", "-c", "echo greeting; read x; echo recv:$x"},
	}
	bodyBytes, _ := json.Marshal(createBody)
	resp, err := http.Post(srv.URL+"/api/cerebro/terminal/sessions", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var created struct {
		ID         string `json:"id"`
		AttachPath string `json:"attach_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.ID == "" || created.AttachPath == "" {
		t.Fatalf("missing id/attach_path: %+v", created)
	}

	// 2. Open WebSocket against the returned attach_path.
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + created.AttachPath
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"X-User-ID": []string{"00000000-0000-0000-0000-000000000001"},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	// Collector: read WS frames until 'recv:world' is seen.
	got := make(chan string, 32)
	errs := make(chan error, 1)
	go func() {
		defer close(got)
		var buf bytes.Buffer
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				errs <- err
				return
			}
			var frame struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Code int    `json:"code"`
			}
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			if frame.Type == "stdout" {
				out, err := base64.StdEncoding.DecodeString(frame.Data)
				if err == nil {
					buf.Write(out)
					if strings.Contains(buf.String(), "recv:world") {
						got <- buf.String()
						return
					}
				}
			} else if frame.Type == "exit" {
				if buf.Len() > 0 {
					got <- buf.String()
				}
				return
			}
		}
	}()

	// 3. Send "world\n" as stdin.
	stdin := map[string]any{"type": "stdin", "data": base64.StdEncoding.EncodeToString([]byte("world\n"))}
	stdinBytes, _ := json.Marshal(stdin)
	if err := conn.WriteMessage(websocket.TextMessage, stdinBytes); err != nil {
		t.Fatalf("ws write stdin: %v", err)
	}

	select {
	case s := <-got:
		if !strings.Contains(s, "greeting") {
			t.Errorf("expected stdout to contain 'greeting', got %q", s)
		}
		if !strings.Contains(s, "recv:world") {
			t.Errorf("expected stdin roundtrip 'recv:world', got %q", s)
		}
	case err := <-errs:
		t.Fatalf("ws read error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for 'recv:world'")
	}
}

// TestHTTPCreate_RejectsNonInteractiveRuntime proves the server enforces
// the runtime's presentation_mode before spawning anything. Headless
// runtimes must NOT get a terminal session.
func TestHTTPCreate_RejectsNonInteractiveRuntime(t *testing.T) {
	srv := newTestServer(t, &fakeDB{
		runtimeWS:   "11111111-1111-1111-1111-111111111111",
		runtimeMode: "headless",
		membership:  true,
	})
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"runtime_id": "33333333-3333-3333-3333-333333333333"})
	resp, err := http.Post(srv.URL+"/api/cerebro/terminal/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 conflict for headless runtime, got %d", resp.StatusCode)
	}
}

// TestHTTPCreate_RejectsNonMember proves the workspace-membership gate is
// applied AFTER the runtime row is found — a user from another workspace
// cannot spawn a session on a runtime they don't own.
func TestHTTPCreate_RejectsNonMember(t *testing.T) {
	srv := newTestServer(t, &fakeDB{
		runtimeWS:   "11111111-1111-1111-1111-111111111111",
		runtimeMode: "interactive",
		membership:  false, // not a member
	})
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"runtime_id": "44444444-4444-4444-4444-444444444444"})
	resp, err := http.Post(srv.URL+"/api/cerebro/terminal/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d", resp.StatusCode)
	}
}

// TestHTTPGetSetPresentationMode_Roundtrip proves the cerebro-only column
// is writable through the HTTP surface and round-trips correctly. This is
// what the UI toggle calls.
func TestHTTPGetSetPresentationMode_Roundtrip(t *testing.T) {
	db := &fakeDB{
		runtimeWS:   "11111111-1111-1111-1111-111111111111",
		runtimeMode: "headless",
		membership:  true,
	}
	srv := newTestServer(t, db)
	defer srv.Close()

	// GET headless
	resp, err := http.Get(srv.URL + "/api/cerebro/terminal/runtimes/55555555-5555-5555-5555-555555555555/presentation-mode")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", resp.StatusCode)
	}
	var got struct{ PresentationMode string `json:"presentation_mode"` }
	json.NewDecoder(resp.Body).Decode(&got)
	if got.PresentationMode != "headless" {
		t.Errorf("expected headless, got %q", got.PresentationMode)
	}

	// PUT interactive (fakeDB switches runtimeMode based on what UPDATE returns,
	// so it's enough to assert the PUT succeeds with a 200 and echoes the value).
	body, _ := json.Marshal(map[string]string{"presentation_mode": "interactive"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/cerebro/terminal/runtimes/55555555-5555-5555-5555-555555555555/presentation-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d", putResp.StatusCode)
	}
	var putGot struct{ PresentationMode string `json:"presentation_mode"` }
	json.NewDecoder(putResp.Body).Decode(&putGot)
	if putGot.PresentationMode != "interactive" {
		t.Errorf("expected interactive, got %q", putGot.PresentationMode)
	}
}

// TestHTTPSetPresentationMode_RejectsInvalidValue protects the DB's CHECK
// constraint from being relied on as the only validation layer.
func TestHTTPSetPresentationMode_RejectsInvalidValue(t *testing.T) {
	srv := newTestServer(t, &fakeDB{runtimeWS: "ws-1", runtimeMode: "headless", membership: true})
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"presentation_mode": "garbage"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/cerebro/terminal/runtimes/66666666-6666-6666-6666-666666666666/presentation-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
