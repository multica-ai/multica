package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

type linkAccount struct {
	mu    sync.Mutex
	peers []p2p.Peer
}

func (a *linkAccount) fetch(context.Context) ([]p2p.Peer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]p2p.Peer(nil), a.peers...), nil
}

type linkDevice struct {
	id    peer.ID
	p2p   *p2p.Service
	v2    *localhost.V2
	link  *deviceLink
	token string
	srv   *httptest.Server
}

func newLinkDevice(t *testing.T, acct *linkAccount, key crypto.PrivKey, token string) *linkDevice {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".desktop-user-id"), []byte("user-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := service.Open(context.Background(), service.Options{
		Path: filepath.Join(dir, "conversations.sqlite"),
		// Conversations are created but never run here.
		Claude: func(context.Context, sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
			return service.ClaudeSession{}, nil, errors.New("not run in this test")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	ps, err := p2p.Start(context.Background(), p2p.Config{Key: key, Fetch: acct.fetch, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, RefreshInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ps.Close() })
	if err := ps.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	v2 := localhost.NewV2(svc, token, dir)
	v2.SetServerOrigin("https://api.multica.ai")
	link := newDeviceLink(dir, v2, token, slog.Default())
	link.attach(ps)
	t.Cleanup(func() { link.detach(ps) })
	mux := http.NewServeMux()
	v2.Register(mux)
	link.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &linkDevice{id: ps.ID(), p2p: ps, v2: v2, link: link, token: token, srv: srv}
}

func newLinkKey(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peer.IDFromPrivateKey(key)
	return key, id
}

func (d *linkDevice) do(t *testing.T, bearer, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, d.srv.URL+path, reader)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Multica-API-Origin", "https://api.multica.ai")
	req.Header.Set("X-Multica-User-ID", "user-1")
	req.Header.Set("X-Workspace-ID", "ws-1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestDeviceLinkForwardsDesktopToAnotherDevice(t *testing.T) {
	keyA, idA := newLinkKey(t)
	keyB, idB := newLinkKey(t)
	acct := &linkAccount{peers: []p2p.Peer{{ID: idA, DeviceName: "MacBook"}, {ID: idB, DeviceName: "Mac Studio"}}}
	viewer := newLinkDevice(t, acct, keyA, "token-a")
	host := newLinkDevice(t, acct, keyB, "token-b")
	renderer := localhost.RendererToken(viewer.token)
	base := "/local/devices/" + idB.String() + "/local/v2"

	var addrs []ma.Multiaddr
	for _, a := range host.p2p.Status().ListenAddrs {
		addrs = append(addrs, ma.StringCast(a))
	}
	viewer.p2p.AddAddrs(peer.AddrInfo{ID: idB, Addrs: addrs})
	if err := host.link.setEnabled(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, body := viewer.do(t, renderer, http.MethodGet, "/local/devices", nil)
		if devices, _ := body["devices"].([]any); len(devices) == 1 {
			if d := devices[0].(map[string]any); d["peer_id"] != idB.String() || d["device_name"] != "Mac Studio" || d["connection"] != "direct" {
				t.Fatalf("device = %v", d)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host never listed: %v", body)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if status, body := viewer.do(t, renderer, http.MethodPost, base+"/sessions", map[string]any{"id": "conv-1", "provider": "claude"}); status != http.StatusCreated {
		t.Fatalf("create on host = %d %v", status, body)
	}
	if status, body := host.do(t, host.token, http.MethodGet, "/local/v2/sessions", nil); status != http.StatusOK || len(body["summaries"].([]any)) != 1 {
		t.Fatalf("host's own list = %d %v", status, body)
	}
	// Only the viewer's main process may widen what the host reads.
	if status, _ := viewer.do(t, renderer, http.MethodPost, base+"/folder-projects", map[string]any{"path": t.TempDir()}); status != http.StatusForbidden {
		t.Fatalf("renderer folder project = %d", status)
	}
	if status, body := viewer.do(t, viewer.token, http.MethodPost, base+"/folder-projects", map[string]any{"path": "/"}); status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("main folder project = %d %v", status, body)
	}
	// The host's drain is its own.
	if status, _ := viewer.do(t, viewer.token, http.MethodPost, base+"/drain", nil); status != http.StatusForbidden {
		t.Fatalf("drain through the link = %d", status)
	}

	_, ticket := viewer.do(t, renderer, http.MethodPost, base+"/ws-ticket", nil)
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.srv.URL, "http")+base+"/ws?ticket="+ticket["ticket"].(string), http.Header{"Origin": {"file://"}})
	if err != nil {
		t.Fatalf("ws = %v %v", err, res)
	}
	defer ws.Close()
	_ = ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe_scope", "after_scope_seq": 0})
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]any
	if err := ws.ReadJSON(&msg); err != nil || msg["status"] != "subscribed" {
		t.Fatalf("scope over the link = %v %v", msg, err)
	}

	if err := host.link.setEnabled(false); err != nil {
		t.Fatal(err)
	}
	// Identify tells the viewer the protocol is gone shortly after.
	deadline = time.Now().Add(5 * time.Second)
	for {
		status, body := viewer.do(t, renderer, http.MethodGet, base+"/sessions", nil)
		if status == http.StatusServiceUnavailable && body["error"].(map[string]any)["kind"] == "device_link_off" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after turning off = %d %v", status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, body := viewer.do(t, renderer, http.MethodGet, "/local/devices", nil); len(body["devices"].([]any)) != 0 {
		t.Fatalf("devices after turning off = %v", body)
	}
	if status, body := viewer.do(t, renderer, http.MethodGet, "/local/devices/"+idA.String()+"x/local/v2/sessions", nil); status != http.StatusNotFound {
		t.Fatalf("unknown device = %d %v", status, body)
	}
}

// The host must read the connection from the stream the request arrived on, which
// only the device-link server's ConnContext can see.
func TestDeviceLinkRecordsTheStreamsConnectionOnTheHost(t *testing.T) {
	keyA, idA := newLinkKey(t)
	keyB, idB := newLinkKey(t)
	acct := &linkAccount{peers: []p2p.Peer{{ID: idA, DeviceName: "MacBook"}, {ID: idB, DeviceName: "Mac Studio"}}}
	viewer := newLinkDevice(t, acct, keyA, "token-a")
	host := newLinkDevice(t, acct, keyB, "token-b")
	path := filepath.Join(t.TempDir(), "device-timing.jsonl")
	log := timing.Open(path, timing.Options{})
	t.Cleanup(log.Close)
	host.v2.SetTiming(timing.NewMarks(log, timing.SourceDaemon), nil)
	var addrs []ma.Multiaddr
	for _, a := range host.p2p.Status().ListenAddrs {
		addrs = append(addrs, ma.StringCast(a))
	}
	viewer.p2p.AddAddrs(peer.AddrInfo{ID: idB, Addrs: addrs})
	if err := host.link.setEnabled(true); err != nil {
		t.Fatal(err)
	}
	renderer := localhost.RendererToken(viewer.token)
	base := "/local/devices/" + idB.String() + "/local/v2"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if status, _ := viewer.do(t, renderer, http.MethodGet, base+"/sessions", nil); status == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never answered")
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Flush()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var record timing.Record
		if json.Unmarshal(line, &record) == nil && record.Stage == timing.StageProxyRequest {
			found = true
			if record.Connection != timing.ConnectionDirect || record.Peer != idA.String() || record.Role != timing.RoleExecutor || record.ClientKind != timing.ClientDesktop {
				t.Fatalf("record = %+v", record)
			}
		}
	}
	if !found {
		t.Fatalf("no proxy.request in %s", raw)
	}
	// Reporting is for a phone: this device's Desktop cannot forward a report.
	if status, _ := viewer.do(t, renderer, http.MethodPost, base+timing.ReportsPath, map[string]any{"batch_id": "b", "records": []any{}}); status != http.StatusForbidden {
		t.Fatalf("forwarded report = %d", status)
	}
}
