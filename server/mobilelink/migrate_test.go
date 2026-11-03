package mobilelink

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/p2pserver"
)

// migrationFixture is a phone and a desktop that can only reach each other
// through a real relay until the test registers a direct address.
type migrationFixture struct {
	client  *Client
	desktop *p2p.Service
	base    string
	release chan struct{}

	mu    sync.Mutex
	addrs []string
}

func (f *migrationFixture) setAddrs(addrs []string) {
	f.mu.Lock()
	f.addrs = addrs
	f.mu.Unlock()
}

func filterAddrs(addrs []string, circuit bool) []string {
	var out []string
	for _, a := range addrs {
		if strings.Contains(a, "/p2p-circuit") == circuit {
			out = append(out, a)
		}
	}
	return out
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func generateKey(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, id
}

func startMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	const userID = "account-a"
	f := &migrationFixture{release: make(chan struct{})}
	storage := t.TempDir()
	phoneKey, err := p2p.LoadOrCreateKey(filepath.Join(storage, hex.EncodeToString([]byte(userID))+".key"))
	if err != nil {
		t.Fatal(err)
	}
	phoneID, err := peer.IDFromPrivateKey(phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	desktopKey, desktopID := generateKey(t)
	relayKey, _ := generateKey(t)

	store, err := p2pserver.OpenStore(filepath.Join(t.TempDir(), "p2p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	relay, err := p2pserver.StartRelay(p2pserver.RelayConfig{Key: relayKey, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relay.Close() })
	publicKey := func(id peer.ID) string {
		pub, err := id.ExtractPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := crypto.MarshalPublicKey(pub)
		return base64.StdEncoding.EncodeToString(raw)
	}
	for i, id := range []peer.ID{desktopID, phoneID} {
		if _, _, err := store.Upsert(context.Background(), p2pserver.Device{UserID: "user-1", DaemonID: "d" + strconv.Itoa(i), PeerID: id.String(), PublicKey: publicKey(id)}, time.Now(), time.Time{}); err != nil {
			t.Fatal(err)
		}
	}

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"session_token": "session", "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/v1/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"relay": map[string]any{"peer_id": relay.Info().PeerID, "addrs": relay.Info().Addrs}})
		case r.URL.Path == "/v1/devices" && r.Method == http.MethodGet:
			f.mu.Lock()
			addrs := f.addrs
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"devices": []map[string]any{{
				"public_key": publicKey(desktopID), "device_name": "Test Mac", "daemon_id": "desktop", "profile": "default", "addrs": addrs,
			}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(registryServer.Close)

	f.desktop, err = p2p.Start(context.Background(), p2p.Config{
		Key:             desktopKey,
		ListenAddrs:     []string{"/ip4/127.0.0.1/tcp/0"},
		RefreshInterval: time.Hour,
		Fetch:           func(context.Context) ([]p2p.Peer, error) { return []p2p.Peer{{ID: phoneID}}, nil },
		Relay: func(context.Context) (*peer.AddrInfo, error) {
			info := relay.Info()
			id, err := peer.Decode(info.PeerID)
			if err != nil {
				return nil, err
			}
			out := &peer.AddrInfo{ID: id}
			for _, raw := range info.Addrs {
				a, err := ma.NewMultiaddr(raw)
				if err != nil {
					return nil, err
				}
				out.Addrs = append(out.Addrs, a)
			}
			return out, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.desktop.Close() })
	listener, stop := f.desktop.Listen(deviceProtocol)
	t.Cleanup(stop)
	type relayedKey struct{}
	server := &http.Server{
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, relayedKey{}, strconv.FormatBool(p2p.Relayed(c)))
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			relayed, _ := r.Context().Value(relayedKey{}).(string)
			w.Header().Set("X-Stream-Relayed", relayed)
			w.Header().Set("X-Seen-Client-Kind", r.Header.Get(headerClientKind))
			switch r.URL.Path {
			case "/local/v2/ws":
				ws, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, http.Header{"X-Stream-Relayed": {relayed}})
				if err != nil {
					return
				}
				defer ws.Close()
				for {
					kind, data, err := ws.ReadMessage()
					if err != nil {
						return
					}
					if ws.WriteMessage(kind, data) != nil {
						return
					}
				}
			case "/local/v2/slow":
				_, _ = io.WriteString(w, "first")
				w.(http.Flusher).Flush()
				<-f.release
				_, _ = io.WriteString(w, "-last")
			case "/local/v2/large":
				w.Header().Set("Content-Length", strconv.Itoa(6<<20))
				w.WriteHeader(http.StatusOK)
			default:
				_, _ = io.WriteString(w, "ok")
			}
		}),
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	eventually(t, 15*time.Second, "desktop relay address", func() bool { return len(filterAddrs(f.desktop.AdvertisedAddrs(), true)) > 0 })
	f.setAddrs(filterAddrs(f.desktop.AdvertisedAddrs(), true))

	config, _ := json.Marshal(configuration{RegistryURL: registryServer.URL, APIURL: "https://api.example.test", Token: "login", UserID: userID, StoragePath: storage, DeviceName: "Test iPhone"})
	f.client, err = Start(string(config))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.client.Close)
	f.base = f.client.endpoint + "/devices/" + desktopID.String() + "/local/v2"
	eventually(t, 30*time.Second, "phone to connect through the relay", func() bool { return f.client.svc.Connection(desktopID) == p2p.ConnectionRelayed })
	return f
}

func (f *migrationFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.client.token)
	req.Header.Set("X-Workspace-ID", "workspace-a")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (f *migrationFixture) dialWS(t *testing.T) (*websocket.Conn, string) {
	t.Helper()
	headers := http.Header{"Authorization": {"Bearer " + f.client.token}, "X-Workspace-ID": {"workspace-a"}}
	ws, res, err := websocket.DefaultDialer.Dial("ws"+f.base[4:]+"/ws", headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws, res.Header.Get("X-Stream-Relayed")
}

func TestRelayedWebSocketMovesToDirectWhileRequestsFinish(t *testing.T) {
	f := startMigrationFixture(t)

	idle := f.get(t, "/hello")
	_, _ = io.Copy(io.Discard, idle.Body)
	idle.Body.Close()
	if got := idle.Header.Get("X-Stream-Relayed"); got != "true" {
		t.Fatalf("first request relayed = %q, want true", got)
	}
	slow := f.get(t, "/slow")
	defer slow.Body.Close()
	first := make([]byte, len("first"))
	if _, err := io.ReadFull(slow.Body, first); err != nil {
		t.Fatalf("in-flight request: %v", err)
	}
	ws, relayed := f.dialWS(t)
	if relayed != "true" {
		t.Fatalf("WebSocket relayed = %q, want true", relayed)
	}

	f.setAddrs(append(filterAddrs(f.desktop.AdvertisedAddrs(), true), filterAddrs(f.desktop.Status().ListenAddrs, false)...))
	if err := f.client.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, 15*time.Second, "direct connection", func() bool { return f.client.svc.Connection(f.desktop.ID()) == p2p.ConnectionDirect })

	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("expected the relayed WebSocket to close")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("relayed WebSocket stayed open after a direct connection appeared")
	}

	close(f.release)
	rest, err := io.ReadAll(slow.Body)
	if err != nil || string(rest) != "-last" {
		t.Fatalf("in-flight request after migration: %q %v", rest, err)
	}

	moved, relayed := f.dialWS(t)
	if relayed != "false" {
		t.Fatalf("reconnected WebSocket relayed = %q, want false", relayed)
	}
	if err := moved.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := moved.ReadMessage(); err != nil || string(data) != "ping" {
		t.Fatalf("direct WebSocket echo: %q %v", data, err)
	}
	after := f.get(t, "/hello")
	after.Body.Close()
	if got := after.Header.Get("X-Stream-Relayed"); got != "false" {
		t.Fatalf("request after migration relayed = %q, want false", got)
	}
}

func TestRelayLimitedResponseKeepsItsErrorKind(t *testing.T) {
	f := startMigrationFixture(t)
	res := f.get(t, "/large")
	defer res.Body.Close()
	var body struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusRequestEntityTooLarge || body.Error.Kind != "relay_transfer_limited" {
		t.Fatalf("status %d kind %q, want 413 relay_transfer_limited", res.StatusCode, body.Error.Kind)
	}
}
