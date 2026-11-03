// Package mobilelink exposes the account's existing libp2p device transport to
// iOS through gomobile. It runs no agent and accepts no inbound conversations.
package mobilelink

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multica-ai/multica/server/internal/daemon/mobilediag"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const deviceProtocol = "/multica/local-api/1"

// headerClientKind labels this client's requests on the computer's timing records.
const headerClientKind = "X-Multica-Client-Kind"

const (
	unregisterTimeout = 3 * time.Second
	diagnosticsPage   = 500
	diagnosticsFile   = ".timing.json"
)

type configuration struct {
	RegistryURL string `json:"registryUrl"`
	APIURL      string `json:"apiUrl"`
	Token       string `json:"token"`
	UserID      string `json:"userId"`
	StoragePath string `json:"storagePath"`
	DeviceName  string `json:"deviceName"`
}

// relayTransferLimit is the largest file moved while a device is only reachable
// through the relay.
const relayTransferLimit = 5 << 20

var errRelayLimit = errors.New("relay_transfer_limited")

// Client owns a foreground mobile device link. Close releases every socket and
// clears the registry credential together; callers replace it on account change.
type Client struct {
	cfg      configuration
	ctx      context.Context
	cancel   context.CancelFunc
	svc      *p2p.Service
	registry *registry
	server   *http.Server
	endpoint string
	token    string
	mu       sync.Mutex
	tunnels  map[peer.ID]*p2p.Tunnel
	once     sync.Once
	// ring holds this client's marks until the app drains them. It outlives a
	// stopped link through its file, since iOS stops the link while the app sleeps.
	ring  *timing.Ring
	marks *timing.Marks
	// logger writes to the phone's log, which other devices can read.
	logger *slog.Logger
	// logReads answers those reads; nil when the log could not open.
	logReads     *http.Server
	logReadsStop func()

	// peersRead closes after the first device list attempt has settled, so requests
	// made right after Start do not report an empty account while it is in flight.
	peersRead     chan struct{}
	peersReadOnce sync.Once
	peersKnown    atomic.Bool
}

// Start returns once the loopback endpoint is serving. Registry calls run in the
// background: a slow or failing registry must not block devices it already lists.
func Start(configJSON string) (*Client, error) {
	var cfg configuration
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, err
	}
	for _, raw := range []string{cfg.RegistryURL, cfg.APIURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return nil, fmt.Errorf("invalid_configuration: invalid service URL")
		}
	}
	if cfg.Token == "" || cfg.UserID == "" || !filepath.IsAbs(cfg.StoragePath) {
		return nil, fmt.Errorf("invalid_configuration: login and storage are required")
	}
	if err := os.MkdirAll(cfg.StoragePath, 0700); err != nil {
		return nil, err
	}
	// Partition identities by account: signing in as another user must never
	// register the old account's peer key under the new account.
	keyPath := filepath.Join(cfg.StoragePath, hex.EncodeToString([]byte(cfg.UserID))+".key")
	key, err := p2p.LoadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	publicKey, err := crypto.MarshalPublicKey(key.GetPublic())
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Partitioned by account like the key: another user must not report these.
	ring := timing.OpenRing(filepath.Join(cfg.StoragePath, hex.EncodeToString([]byte(cfg.UserID))+diagnosticsFile), 0)
	c := &Client{cfg: cfg, ctx: ctx, cancel: cancel, token: hex.EncodeToString(secret), endpoint: "http://" + ln.Addr().String(),
		tunnels: map[peer.ID]*p2p.Tunnel{}, peersRead: make(chan struct{}), ring: ring, marks: timing.NewMarks(ring, timing.SourceMobile), logger: slog.Default()}
	// Marks and logs only ever add to the phone's log, so a log that cannot open
	// costs its diagnostics and nothing else.
	logs, logsErr := openStore(cfg.StoragePath)
	if logsErr == nil {
		logs.SetAccount(cfg.UserID)
		c.marks = timing.NewMarks(logs.RetainMarks(ring), timing.SourceMobile)
		c.logger = logs.Logger()
	}
	c.registry = &registry{url: strings.TrimRight(cfg.RegistryURL, "/"), login: cfg.Token, http: &http.Client{Timeout: 30 * time.Second}}
	changed := make(chan struct{}, 1)
	c.svc, err = p2p.Start(ctx, p2p.Config{Key: key, Fetch: c.fetchPeers, Relay: c.registry.relay,
		RefreshInterval: 15 * time.Second, Marks: c.marks, Logger: c.logger, OnPeersRead: c.peersSettled, OnAddrsChanged: func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		}})
	if err != nil {
		_ = ln.Close()
		cancel()
		return nil, err
	}
	c.svc.OnDirect(c.moveToDirect)
	c.server = &http.Server{Handler: http.HandlerFunc(c.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = c.server.Serve(ln) }()
	if logsErr == nil {
		c.serveLogReads(logs)
	}
	go c.keepRegistered(publicKey, changed)
	return c, nil
}

// serveLogReads answers this account's computers for as long as the link runs,
// whatever the app has on screen.
func (c *Client) serveLogReads(logs *mobilediag.Store) {
	listener, stop := c.svc.Listen(mobilediag.Protocol)
	c.logReadsStop = stop
	c.logReads = &http.Server{Handler: logs.Handler(c.cfg.UserID), ReadHeaderTimeout: 10 * time.Second}
	go func(s *http.Server) { _ = s.Serve(listener) }(c.logReads)
}

// fetchPeers leaves other phones off the allowlist: they serve no conversations,
// and each relayed connection counts against the account's circuit limit.
func (c *Client) fetchPeers(ctx context.Context) ([]p2p.Peer, error) {
	peers, err := c.registry.peers(ctx)
	if err == nil {
		peers = slices.DeleteFunc(peers, func(p p2p.Peer) bool { return p.Profile == "mobile" && strings.HasPrefix(p.DaemonID, "ios-") })
	}
	return peers, err
}

// peersSettled runs once a read's list is installed, so a request woken here finds
// the devices it asks about. A read cut short by Close says nothing.
func (c *Client) peersSettled(err error) {
	if err == nil {
		c.peersKnown.Store(true)
	}
	if c.ctx.Err() == nil {
		c.peersReadOnce.Do(func() { close(c.peersRead) })
	}
}

// awaitPeers reports whether a device list has been read.
func (c *Client) awaitPeers(ctx context.Context) bool {
	select {
	case <-c.peersRead:
	case <-ctx.Done():
	}
	return c.peersKnown.Load()
}

// keepRegistered retries without retaining credentials from a stopped client.
func (c *Client) keepRegistered(publicKey []byte, changed <-chan struct{}) {
	tick := time.NewTicker(6 * time.Hour)
	defer tick.Stop()
	registered := false
	for {
		for {
			err := c.register(publicKey)
			if err == nil {
				break
			}
			c.logger.Warn("mobilelink: registration failed; retrying", "error", err)
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
		}
		if !registered {
			registered = true
			c.marks.Write(timing.Record{Stage: timing.StageLinkRegistered})
			// Devices that rejected this phone as unknown may list it now.
			_ = c.svc.PeersChanged(c.ctx)
		}
		select {
		case <-c.ctx.Done():
			return
		case <-changed:
		case <-tick.C:
		}
	}
}

// NetworkChanged rebuilds connections after the phone's network changes instead
// of waiting for the old paths to time out.
func (c *Client) NetworkChanged() {
	if c.ctx.Err() == nil {
		c.svc.NetworkChanged()
	}
}

// Backgrounded saves diagnostics, since iOS may end the process without notice.
// The link itself keeps running.
func (c *Client) Backgrounded() { c.ring.Save() }

// Foregrounded verifies connections when the app returns, since iOS may have
// suspended it. The link stays up in the background until iOS decides otherwise.
func (c *Client) Foregrounded() {
	if c.ctx.Err() == nil {
		c.svc.RecheckPaths()
	}
}

// UpdateToken replaces the login credential for later registry calls. Device
// connections do not depend on it, so they stay up.
func (c *Client) UpdateToken(token string) {
	if token != "" {
		c.registry.setLogin(token)
	}
}

func (c *Client) moveToDirect(id peer.ID) {
	c.mu.Lock()
	tunnel := c.tunnels[id]
	c.mu.Unlock()
	if tunnel != nil {
		tunnel.MoveToDirect()
	}
}

func (c *Client) register(publicKey []byte) error {
	return c.registry.call(c.ctx, "POST", "/v1/devices", map[string]any{
		"daemon_id": "ios-" + c.svc.ID().String(), "profile": "mobile", "public_key": base64.StdEncoding.EncodeToString(publicKey),
		"device_name": c.cfg.DeviceName, "addrs": c.svc.AdvertisedAddrs(),
	}, nil)
}

// Unregister takes this phone off the account's device list when its user signs out,
// so it stops being offered as a phone to read. Devices age out of the list anyway,
// so a failure here costs only that delay.
func (c *Client) Unregister() {
	ctx, cancel := context.WithTimeout(c.ctx, unregisterTimeout)
	defer cancel()
	query := url.Values{"daemon_id": {"ios-" + c.svc.ID().String()}, "profile": {"mobile"}}
	if err := c.registry.call(ctx, http.MethodDelete, "/v1/devices?"+query.Encode(), nil, nil); err != nil {
		c.logger.Warn("mobilelink: unregister failed", "error", err)
	}
}

// Endpoint returns a private loopback credential, never the Multica login token.
func (c *Client) Endpoint() string {
	data, _ := json.Marshal(map[string]string{"url": c.endpoint, "token": c.token})
	return string(data)
}

// Close also closes hijacked WebSockets, which http.Server.Close does not own.
func (c *Client) Close() {
	c.once.Do(func() {
		c.cancel()
		_ = c.server.Close()
		if c.logReads != nil {
			c.logReadsStop()
			_ = c.logReads.Close()
		}
		c.mu.Lock()
		tunnels := make([]*p2p.Tunnel, 0, len(c.tunnels))
		for _, tunnel := range c.tunnels {
			tunnels = append(tunnels, tunnel)
		}
		c.mu.Unlock()
		for _, tunnel := range tunnels {
			tunnel.Close()
		}
		_ = c.svc.Close()
		c.ring.Save()
	})
}

func (c *Client) serve(w http.ResponseWriter, r *http.Request) {
	// Native fetch and WebSocket can both supply headers. Unlike the desktop
	// browser entry no ticket or login token is placed in a URL.
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "peer_id": c.svc.ID().String()})
		return
	}
	if r.Method == "GET" && r.URL.Path == "/devices" {
		if !c.awaitPeers(r.Context()) {
			writeError(w, "registry_unavailable", http.StatusServiceUnavailable)
			return
		}
		devices := []map[string]any{}
		for _, p := range c.svc.Peers() {
			devices = append(devices, map[string]any{"peer_id": p.ID.String(), "device_name": p.DeviceName,
				"connected": p.Connected, "available": p.Connected && c.svc.SupportsProtocol(p.ID, deviceProtocol), "connection": p.Connection})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": devices})
		return
	}
	if r.Method == "GET" && r.URL.Path == "/diagnostics" {
		c.serveDiagnostics(w, r)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/diagnostics/ack" {
		c.acknowledgeDiagnostics(w, r)
		return
	}
	id, inner, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/devices/"), "/")
	peerID, err := peer.Decode(id)
	if !strings.HasPrefix(r.URL.Path, "/devices/") || !ok || err != nil || !strings.HasPrefix(inner, "local/v2/") || strings.Contains(inner, "..") {
		http.NotFound(w, r)
		return
	}
	c.awaitPeers(r.Context())
	if _, known := c.svc.Peer(peerID); !known {
		writeError(w, "device_unknown", http.StatusNotFound)
		return
	}
	if r.Header.Get("X-Workspace-ID") == "" {
		writeError(w, "invalid_request", http.StatusBadRequest)
		return
	}
	// The native client owns account/API identity. Never trust a caller to
	// replace it using a scope header or arbitrary proxy target.
	r.Header.Set("X-Multica-User-ID", c.cfg.UserID)
	r.Header.Set("X-Multica-API-Origin", c.cfg.APIURL)
	r.Header.Set(headerClientKind, timing.ClientMobile)
	r.Header.Del("Authorization")
	r.URL.Path, r.URL.RawPath = "/"+inner, ""
	c.mu.Lock()
	tunnel := c.tunnels[peerID]
	if tunnel == nil {
		// The header timeout must outlast a send, which the app waits on for up
		// to two minutes while the computer prepares its runner.
		tunnel = c.svc.NewTunnel(peerID, deviceProtocol, &http.Transport{IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 130 * time.Second})
		c.tunnels[peerID] = tunnel
	}
	c.mu.Unlock()
	if c.svc.Connection(peerID) == p2p.ConnectionRelayed && r.ContentLength > relayTransferLimit {
		writeError(w, errRelayLimit.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.Out.URL.Scheme, p.Out.URL.Host, p.Out.Host = "http", "device-link", "device-link"
	}, Transport: tunnel, ModifyResponse: func(response *http.Response) error {
		if response.ContentLength > relayTransferLimit && c.svc.Connection(peerID) == p2p.ConnectionRelayed {
			return errRelayLimit
		}
		return nil
	},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, errRelayLimit) {
				writeError(w, errRelayLimit.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			c.logger.Warn("mobilelink: device request failed", "peer_id", peerID.String(), "path", r.URL.Path, "error", err)
			writeError(w, "device_unreachable", http.StatusBadGateway)
		}}
	proxy.ServeHTTP(w, r)
}

// serveDiagnostics gives the app this client's marks after its cursor. It sits
// behind the loopback credential like every other route; nothing here leaves the
// phone except by the app reporting it.
func (c *Client) serveDiagnostics(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	records, next, dropped := c.ring.After(after, diagnosticsPage)
	if records == nil {
		records = []timing.Record{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"records": records, "next": next, "dropped": dropped})
}

// acknowledgeDiagnostics hears how far the app has reported, so a new process's app
// does not report it all again.
func (c *Client) acknowledgeDiagnostics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Next    int64 `json:"next"`
		Dropped int64 `json:"dropped"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.Next < 0 || body.Dropped < 0 {
		writeError(w, "invalid_request", http.StatusBadRequest)
		return
	}
	c.ring.Acknowledge(body.Next, body.Dropped)
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, kind string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"kind": kind, "message": kind}})
}
