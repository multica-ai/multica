package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This file is a characterization (snapshot) test of the daemon's OUTBOUND
// header set — on every HTTP request helper AND on the wakeup WebSocket
// handshake. It asserts the FULL set rather than "contains X", so a refactor
// that moves header construction into a transport layer shared with
// internal/cli (MUL-6638) cannot silently change the wire, and the HTTP and WS
// paths cannot drift apart unnoticed.
//
// The WS case matters most: the server derives claim-time capability gating
// from the handshake headers, and before this test the handshake header set
// had no coverage at all.

// daemonWireHeaderKeys is the set of request headers this snapshot tracks.
// Everything else on the wire (Host, User-Agent, Content-Length, the
// Sec-WebSocket-* handshake headers) is set by net/http or gorilla, not by
// daemon code.
var daemonWireHeaderKeys = []string{
	"Authorization",
	"Content-Type",
	"If-None-Match",
	"X-Client-Capabilities",
	"X-Client-OS",
	"X-Client-Platform",
	"X-Client-Version",
}

func captureDaemonWireHeaders(h http.Header) string {
	var parts []string
	for _, key := range daemonWireHeaderKeys {
		values, ok := h[http.CanonicalHeaderKey(key)]
		if !ok {
			continue
		}
		parts = append(parts, key+": "+strings.Join(values, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// daemonCapabilitiesSnapshot is the exact X-Client-Capabilities value, in
// order. The server reads this header to gate skill bundles, coalesced
// comments, local worktrees and WS RPC, so both its contents and the fact
// that it is one comma-joined value are part of the protocol.
const daemonCapabilitiesSnapshot = "skill-bundles-v1,coalesced-comments-v1,execution-manifest-v1,agent-skill-v1,remote-mcp-v1,local-worktree-v1,rpc-v1"

// TestDaemonCapabilityHeaderValueSnapshot pins the literal header value.
// daemonClientCapabilities() is assembled from constants, so this is the only
// place a reorder or a dropped entry becomes visible as a wire change.
func TestDaemonCapabilityHeaderValueSnapshot(t *testing.T) {
	if got := daemonClientCapabilities(); got != daemonCapabilitiesSnapshot {
		t.Errorf("daemonClientCapabilities() = %q, want %q\nChanging this value changes server-side gating for every daemon request and WS claim.", got, daemonCapabilitiesSnapshot)
	}
}

func daemonWireHeadersWant(contentType string) string {
	parts := []string{
		"Authorization: Bearer tok-1",
		"X-Client-Capabilities: " + daemonCapabilitiesSnapshot,
		"X-Client-OS: " + normalizeGOOS(runtime.GOOS),
		"X-Client-Platform: daemon",
		"X-Client-Version: 9.9.9",
	}
	if contentType != "" {
		parts = append(parts, "Content-Type: "+contentType)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func TestDaemonClientOutboundHeaderSnapshot(t *testing.T) {
	cases := []struct {
		name string
		want string
		call func(ctx context.Context, c *Client) error
	}{
		{
			name: "postJSON",
			want: daemonWireHeadersWant("application/json"),
			call: func(ctx context.Context, c *Client) error {
				return c.postJSON(ctx, "/api/daemon/test", map[string]any{}, nil)
			},
		},
		{
			// The skill-bundle download runs on a second http.Client with a
			// different timeout regime; it must still carry the same headers.
			// Any transport-level policy added later (redirect handling,
			// operator-configured extra headers) has to reach both clients.
			name: "postJSON via bundleClient",
			want: daemonWireHeadersWant("application/json"),
			call: func(ctx context.Context, c *Client) error {
				return c.postJSONVia(ctx, c.bundleClient, "/api/daemon/bundle", map[string]any{}, nil)
			},
		},
		{
			name: "getJSON",
			want: daemonWireHeadersWant(""),
			call: func(ctx context.Context, c *Client) error {
				return c.getJSON(ctx, "/api/daemon/test", nil)
			},
		},
		{
			// A task-scoped token replaces the client's PAT and nothing else.
			name: "getJSONWithToken",
			want: strings.Replace(daemonWireHeadersWant(""), "Bearer tok-1", "Bearer mat_task", 1),
			call: func(ctx context.Context, c *Client) error {
				return c.getJSONWithToken(ctx, "/api/daemon/test", "mat_task", nil)
			},
		},
		{
			name: "postJSONWithToken",
			want: strings.Replace(daemonWireHeadersWant("application/json"), "Bearer tok-1", "Bearer mat_task", 1),
			call: func(ctx context.Context, c *Client) error {
				return c.postJSONWithToken(ctx, "/api/daemon/test", "mat_task", map[string]any{}, nil)
			},
		},
		{
			// ListWorkspaces builds its request by hand instead of going
			// through getJSON, which is exactly the kind of copy that drifts.
			name: "ListWorkspaces",
			want: daemonWireHeadersWant(""),
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListWorkspaces(ctx)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				got = captureDaemonWireHeaders(r.Header)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			c.SetToken("tok-1")
			c.SetVersion("9.9.9")

			if err := tc.call(context.Background(), c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if requests != 1 {
				t.Fatalf("%s: server saw %d requests, want 1", tc.name, requests)
			}
			if got != tc.want {
				t.Errorf("%s outbound headers changed.\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
			}
		})
	}
}

// TestDaemonClientListWorkspacesSendsConditionalHeader pins the one
// request-specific header the daemon adds on top of the shared set: a cached
// ETag is replayed as If-None-Match. A shared transport must not overwrite
// per-request headers a caller already set.
func TestDaemonClientListWorkspacesSendsConditionalHeader(t *testing.T) {
	var second string
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("ETag", `"ws-etag-1"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		second = captureDaemonWireHeaders(r.Header)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok-1")
	c.SetVersion("9.9.9")

	for i := 0; i < 2; i++ {
		if _, err := c.ListWorkspaces(context.Background()); err != nil {
			t.Fatalf("ListWorkspaces #%d: %v", i+1, err)
		}
	}

	want := daemonWireHeadersWant("")
	want = strings.Join(append(strings.Split(want, "\n"), `If-None-Match: "ws-etag-1"`), "\n")
	wantLines := strings.Split(want, "\n")
	sort.Strings(wantLines)
	want = strings.Join(wantLines, "\n")

	if second != want {
		t.Errorf("conditional ListWorkspaces headers changed.\ngot:\n%s\nwant:\n%s", second, want)
	}
}

// TestTaskWakeupHandshakeHeaderSnapshot pins the wakeup WebSocket handshake
// header set and, by comparing it to the HTTP set, the invariant that the two
// paths advertise the same identity and capabilities. The WS path builds its
// headers in wakeup.go rather than through Client.setIdentityHeaders, so this
// is the drift the snapshot exists to catch.
func TestTaskWakeupHandshakeHeaderSnapshot(t *testing.T) {
	upgrader := websocket.Upgrader{}
	handshake := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case handshake <- captureDaemonWireHeaders(r.Header):
		default:
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately: the assertion is about the handshake, so the
		// connection only needs to exist long enough to be established.
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = conn.Close()
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		CLIVersion:     "9.9.9",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.client.SetToken("tok-1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = d.runTaskWakeupConnection(ctx, []string{"runtime-1"}, make(chan taskWakeup, 1), make(chan struct{}))

	select {
	case got := <-handshake:
		// The handshake carries no Content-Type (no body) and no
		// If-None-Match, so the expected set is the daemon's bodyless HTTP
		// set verbatim.
		if want := daemonWireHeadersWant(""); got != want {
			t.Errorf("wakeup WS handshake headers changed.\ngot:\n%s\nwant:\n%s\nThe WS handshake must advertise the same identity and capabilities as the HTTP control plane.", got, want)
		}
	default:
		t.Fatal("wakeup WS handshake never reached the server")
	}
}

// TestDaemonRequestErrorMatchesCLIShape documents, in executable form, that
// daemon.requestError and cli.HTTPError render the same message for the same
// failure — the duplication MUL-6638 proposes to collapse into one shared
// error type. Kept as a test rather than a comment so a change to either
// side's formatting is caught while the two types still coexist.
func TestDaemonRequestErrorMatchesCLIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("  already claimed\n"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.postJSON(context.Background(), "/api/daemon/test", map[string]any{}, nil)
	if err == nil {
		t.Fatal("postJSON: expected an error for a 409 response")
	}
	const want = "POST /api/daemon/test returned 409: already claimed"
	if err.Error() != want {
		t.Errorf("requestError.Error() = %q, want %q", err.Error(), want)
	}
}
