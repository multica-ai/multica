package localhost

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// remoteServer serves v's device-link entry as if every connection came
// from the device peer.
func remoteServer(t *testing.T, v *V2, peer RemotePeer, open *atomic.Bool) *httptest.Server {
	t.Helper()
	v.SetRemoteOpen(open.Load)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(v.ServeRemote))
	srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context { return WithRemotePeer(ctx, peer) }
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func remoteHeaders(client string) http.Header {
	h := v2Headers("user-1")
	h.Del("Authorization")
	if client != "" {
		h.Set(headerClientID, client)
	}
	return h
}

func dialWS(t *testing.T, base string, h http.Header) *websocket.Conn {
	t.Helper()
	conn, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/local/v2/ws", h)
	if err != nil {
		t.Fatalf("dial = %v %v", err, res)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readUntil(t *testing.T, conn *websocket.Conn, match func(map[string]any) bool) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read: %v", err)
		}
		if match(msg) {
			return msg
		}
	}
}

func TestV2RemoteEntryNeedsDeviceLinkAndKeepsThisDevicesUpdates(t *testing.T) {
	t.Parallel()

	_, _, v := newV2Fixture(t)
	open := &atomic.Bool{}
	srv := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "MacBook"}, open)
	h := remoteHeaders("")
	if status, body := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, h); status != http.StatusServiceUnavailable || body["error"].(map[string]any)["kind"] != "device_link_off" {
		t.Fatalf("closed = %d %v", status, body)
	}
	open.Store(true)
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, h); status != http.StatusOK {
		t.Fatalf("open = %d", status)
	}
	for _, path := range []string{"/local/v2/drain", "/local/v2/undrain", "/local/v2/stop-providers", "/local/v2/ws-ticket"} {
		if status, _ := v2Do(t, srv, http.MethodPost, path, nil, h); status != http.StatusForbidden {
			t.Errorf("remote %s = %d", path, status)
		}
	}
	// The viewer's daemon admits these only from its own main process.
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/folder-projects", map[string]any{"path": t.TempDir()}, h); status != http.StatusOK && status != http.StatusCreated {
		t.Errorf("remote folder project = %d", status)
	}
}

func TestV2RefusesAnotherServersScope(t *testing.T) {
	t.Parallel()

	srv, _, v := newV2Fixture(t)
	v.SetServerOrigin("https://api.multica.ai/")
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, v2Headers("user-1")); status != http.StatusOK {
		t.Fatalf("same server = %d", status)
	}
	h := v2Headers("user-1")
	h.Set(headerAPIOrigin, "https://self-hosted.example")
	if status, body := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, h); status != http.StatusBadRequest || body["error"].(map[string]any)["kind"] != "api_url_mismatch" {
		t.Fatalf("other server = %d %v", status, body)
	}
}

func TestV2HostRequestsGoToTheClientThatActedAndWaitForItsReconnect(t *testing.T) {
	t.Parallel()

	local, svc, v := newV2Fixture(t)
	v.hosts.grace = 2 * time.Second
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "MacBook"}, open)
	scopeID, _ := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})

	remoteWS := dialWS(t, remote.URL, remoteHeaders("viewer"))
	if status, body := v2Do(t, remote, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, remoteHeaders("viewer")); status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, body)
	}
	if status, body := v2Do(t, remote, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "r1", "text": "hi"}, remoteHeaders("viewer")); status != http.StatusAccepted {
		t.Fatalf("send = %d %v", status, body)
	}
	// This device's Desktop connects later, which made it the preferred one before.
	localWS := dialWS(t, local.URL, func() http.Header { h := v2Headers("user-1"); h.Set(headerClientID, "host"); return h }())
	if err := localWS.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, localWS, func(m map[string]any) bool { return m["status"] == "subscribed" })

	result := make(chan string, 1)
	go func() {
		raw, err := v.hosts.request(context.Background(), scopeID, "conv-1", "settings.confirm", map[string]any{})
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- string(raw)
	}()
	first := readUntil(t, remoteWS, func(m map[string]any) bool { return m["type"] == "host.request" })
	// The viewer drops before answering and comes back with the same client.
	remoteWS.Close()
	again := dialWS(t, remote.URL, remoteHeaders("viewer"))
	resent := readUntil(t, again, func(m map[string]any) bool { return m["type"] == "host.request" })
	if resent["request_id"] != first["request_id"] {
		t.Fatalf("resent %v, want %v", resent["request_id"], first["request_id"])
	}
	if err := again.WriteJSON(map[string]any{"version": V2Version, "type": "host.reply", "request_id": resent["request_id"], "result": map[string]any{"approved": true}}); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != `{"approved":true}` {
		t.Fatalf("result = %s", got)
	}
}

func TestV2HostRequestFailsWhenItsClientDoesNotReturn(t *testing.T) {
	t.Parallel()

	_, svc, v := newV2Fixture(t)
	v.hosts.grace = 100 * time.Millisecond
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b"}, open)
	scopeID, _ := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	ws := dialWS(t, remote.URL, remoteHeaders("viewer"))
	done := make(chan error, 1)
	go func() {
		_, err := v.hosts.request(context.Background(), scopeID, "conv-1", "side.control", map[string]any{})
		done <- err
	}()
	readUntil(t, ws, func(m map[string]any) bool { return m["type"] == "host.request" })
	ws.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("request succeeded without a reply")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request outlived the grace period")
	}
}

func TestV2PresenceTellsThisDeviceWhichConversationsAnotherHasOpen(t *testing.T) {
	t.Parallel()

	local, _, v := newV2Fixture(t)
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "MacBook"}, open)
	if status, _ := v2Do(t, local, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, v2Headers("user-1")); status != http.StatusCreated {
		t.Fatal("create")
	}
	localWS := dialWS(t, local.URL, v2Headers("user-1"))
	_ = localWS.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0})
	readUntil(t, localWS, func(m map[string]any) bool { return m["status"] == "subscribed" })

	remoteWS := dialWS(t, remote.URL, remoteHeaders("viewer"))
	_ = remoteWS.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe", "conversation_id": "conv-1", "after_seq": 0, "store_generation": v.svc.Generation()})
	in := readUntil(t, localWS, func(m map[string]any) bool { return m["type"] == "scope.presence" })
	if devices, _ := json.Marshal(in["devices"]); string(devices) != `[{"device_name":"MacBook","peer_id":"peer-b"}]` || in["conversation_id"] != "conv-1" {
		t.Fatalf("presence = %v", in)
	}
	schema := serverMessageSchema(t)
	if err := schema.Validate(in); err != nil {
		t.Fatalf("presence does not match the schema: %v", err)
	}
	// A Desktop that subscribes later learns it at once.
	late := dialWS(t, local.URL, v2Headers("user-1"))
	_ = late.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0})
	readUntil(t, late, func(m map[string]any) bool { return m["type"] == "scope.presence" })

	remoteWS.Close()
	out := readUntil(t, localWS, func(m map[string]any) bool { return m["type"] == "scope.presence" })
	if devices, _ := out["devices"].([]any); len(devices) != 0 {
		t.Fatalf("after close = %v", out)
	}
}

func TestV2ListsFoldersForRemotePickers(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	dir := t.TempDir()
	for _, name := range []string{"b-project", "A-project", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	status, body := v2Do(t, srv, http.MethodGet, "/local/v2/directories?path="+dir, nil, v2Headers("user-1"))
	if status != http.StatusOK || body["parent"] != filepath.Dir(dir) {
		t.Fatalf("list = %d %v", status, body)
	}
	got, _ := json.Marshal(body["entries"])
	want, _ := json.Marshal([]map[string]string{{"name": "A-project", "path": filepath.Join(dir, "A-project")}, {"name": "b-project", "path": filepath.Join(dir, "b-project")}})
	if string(got) != string(want) {
		t.Fatalf("entries = %s", got)
	}
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/directories?path=relative", nil, v2Headers("user-1")); status != http.StatusBadRequest {
		t.Fatalf("relative = %d", status)
	}
}

func TestV2PresenceKeepsARunAnotherDeviceStartedUntilItEnds(t *testing.T) {
	t.Parallel()

	local, _, v := newV2Fixture(t)
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "MacBook"}, open)
	if status, _ := v2Do(t, local, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, v2Headers("user-1")); status != http.StatusCreated {
		t.Fatal("create")
	}
	localWS := dialWS(t, local.URL, v2Headers("user-1"))
	_ = localWS.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0})
	readUntil(t, localWS, func(m map[string]any) bool { return m["status"] == "subscribed" })

	// The viewer sends without opening the conversation; the fixture's turn ends at once.
	if status, body := v2Do(t, remote, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "r1", "text": "hi"}, remoteHeaders("viewer")); status != http.StatusAccepted {
		t.Fatalf("send = %d %v", status, body)
	}
	started := readUntil(t, localWS, func(m map[string]any) bool { return m["type"] == "scope.presence" })
	if devices, _ := started["devices"].([]any); len(devices) != 1 {
		t.Fatalf("while running = %v", started)
	}
	ended := readUntil(t, localWS, func(m map[string]any) bool { return m["type"] == "scope.presence" })
	if devices, _ := ended["devices"].([]any); len(devices) != 0 {
		t.Fatalf("after the turn = %v", ended)
	}
}

func TestV2LocalHostRequestsNeverReachAnotherDevice(t *testing.T) {
	t.Parallel()

	srv, svc, v := newV2Fixture(t)
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "MacBook"}, open)
	scopeID, err := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	viewer := dialWS(t, remote.URL, remoteHeaders("viewer"))
	if err := viewer.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, viewer, func(m map[string]any) bool { return m["status"] == "subscribed" })
	// The remote viewer acted last, so ordinary host requests would go to it.
	v.actedOn("conv-1", caller{kind: callerRemote, clientKey: "peer-b/viewer"})

	if _, err := v.hosts.requestLocal(context.Background(), scopeID, "conv-1", "developer_logs.export", map[string]any{}); !errors.Is(err, service.ErrLocalHostUnavailable) {
		t.Fatalf("no local Desktop = %v", err)
	}

	local := dialWS(t, srv.URL, v2Headers("user-1"))
	if err := local.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe_scope", "after_scope_seq": 0}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, local, func(m map[string]any) bool { return m["status"] == "subscribed" })
	result := make(chan string, 1)
	go func() {
		raw, err := v.hosts.requestLocal(context.Background(), scopeID, "conv-1", "developer_logs.export", map[string]any{})
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- string(raw)
	}()
	request := readUntil(t, local, func(m map[string]any) bool { return m["type"] == "host.request" })
	if err := local.WriteJSON(map[string]any{"version": V2Version, "type": "host.reply", "request_id": request["request_id"], "result": map[string]any{"path": "/tmp/logs.json"}}); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != `{"path":"/tmp/logs.json"}` {
		t.Fatalf("result = %s", got)
	}
}

func TestV2RecordsWhoAskedForATurn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, desktopUserIDFile), []byte("user-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	timingPath := filepath.Join(dir, "daemon-timing.jsonl")
	svc, err := service.Open(context.Background(), service.Options{TimingPath: timingPath, Path: filepath.Join(dir, "c.sqlite"), Claude: func(context.Context, sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
		return service.ClaudeSession{Fields: map[string]any{"executable": "/bin/claude"}}, func(_ context.Context, h claude.Handler) (service.ClaudeAdapter, error) {
			return &autoAdapter{handler: h, done: make(chan struct{})}, nil
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	mux := http.NewServeMux()
	v := NewV2(svc, v2Token, dir)
	v.Register(mux)
	local := httptest.NewServer(mux)
	t.Cleanup(local.Close)
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-b", DeviceName: "Phone"}, open)

	if status, body := v2Do(t, local, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, v2Headers("user-1")); status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, body)
	}
	send := func(srv *httptest.Server, header http.Header, requestID string) {
		t.Helper()
		if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": requestID, "text": "hi"}, header); status != http.StatusAccepted {
			t.Fatalf("send %s = %d %v", requestID, status, body)
		}
	}
	ws := dialWS(t, local.URL, v2Headers("user-1"))
	if err := ws.WriteJSON(map[string]any{"version": V2Version, "type": "subscribe", "conversation_id": "conv-1", "after_seq": 0, "store_generation": svc.Generation()}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, func(m map[string]any) bool { return m["status"] == "subscribed" })
	send(local, v2Headers("user-1"), "from-desktop")
	eventuallyV2(t, func() bool {
		data, _ := os.ReadFile(timingPath)
		return strings.Contains(string(data), `"turn.finished"`) && strings.Contains(string(data), `"ws.first_output_write"`)
	})
	phone := remoteHeaders("phone")
	phone.Set(headerClientKind, "mobile")
	send(remote, phone, "from-phone")
	unlabelled := remoteHeaders("other")
	unlabelled.Set(headerClientKind, "toaster")
	eventuallyV2(t, func() bool {
		data, _ := os.ReadFile(timingPath)
		return strings.Count(string(data), `"turn.finished"`) == 2
	})
	send(remote, unlabelled, "from-other")
	eventuallyV2(t, func() bool {
		data, _ := os.ReadFile(timingPath)
		return strings.Count(string(data), `"turn.finished"`) == 3
	})

	data, err := os.ReadFile(timingPath)
	if err != nil {
		t.Fatal(err)
	}
	received := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r["stage"] == "daemon.request_received" {
			received[r["requestId"].(string)] = r
		}
	}
	for id, want := range map[string][3]string{
		"from-desktop": {"local", "desktop", "loopback"},
		"from-phone":   {"p2p", "mobile", "unknown"},
		"from-other":   {"p2p", "unknown", "unknown"},
	} {
		r := received[id]
		if r["origin"] != want[0] || r["clientKind"] != want[1] || r["connection"] != want[2] {
			t.Errorf("%s = %v, want %v", id, r, want)
		}
	}
}

func TestV2AdmitLabelsForwardedRequestsAsDesktop(t *testing.T) {
	t.Parallel()

	_, _, v := newV2Fixture(t)
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/local/v2/sessions", nil)
	r.Header.Set("Authorization", "Bearer "+v2Token)
	r.Header.Set(headerClientKind, "mobile")
	if !v.Admit(httptest.NewRecorder(), r) {
		t.Fatal("not admitted")
	}
	if got := r.Header.Get(headerClientKind); got != timing.ClientDesktop {
		t.Fatalf("client kind = %q", got)
	}
}
