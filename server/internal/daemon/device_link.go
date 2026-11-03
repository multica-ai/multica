package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/daemon/servicepreview"
)

// deviceLinkProtocol carries the local v2 API between this account's devices.
const deviceLinkProtocol = "/multica/local-api/1"

const deviceLinkFile = "device-link.json"

// relayTransferLimit is the largest file moved while two devices only
// reach each other through the relay.
const relayTransferLimit = 5 << 20

// deviceLink lets this account's other devices use this device's local
// conversations when the user turns it on, and forwards this device's
// Desktop to theirs.
type deviceLink struct {
	preview     *servicepreview.Manager
	previewStop func()
	path        string
	v2          *localhost.V2
	token       string
	logger      *slog.Logger

	mu      sync.Mutex
	enabled bool
	svc     *p2p.Service
	stop    func()
	server  *http.Server
	proxies map[peer.ID]*deviceProxy
}

func newDeviceLink(profileDir string, v2 *localhost.V2, token string, logger *slog.Logger) *deviceLink {
	l := &deviceLink{path: filepath.Join(profileDir, deviceLinkFile), v2: v2, token: token, logger: logger, proxies: map[peer.ID]*deviceProxy{}}
	if data, err := os.ReadFile(l.path); err == nil {
		var saved struct {
			Enabled bool `json:"enabled"`
		}
		if json.Unmarshal(data, &saved) == nil {
			l.enabled = saved.Enabled
		}
	}
	v2.SetRemoteOpen(l.open)
	l.preview = servicepreview.New(filepath.Join(profileDir, "service-preview.json"), l.open, v2.CheckPreviewConversation, logger)
	v2.SetServicePreview(l.preview)
	return l
}

// open reports whether other devices may use this one now.
func (l *deviceLink) open() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled && l.svc != nil
}

// attach starts serving other devices over svc while Device link is on.
func (l *deviceLink) attach(svc *p2p.Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.svc = svc
	svc.OnDirect(l.moveToDirect)
	l.apply()
}

// detach stops using svc, which is closing.
func (l *deviceLink) detach(svc *p2p.Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.svc != svc {
		return
	}
	l.svc = nil
	l.apply()
	for id, p := range l.proxies {
		p.tunnel.CloseIdleConnections()
		delete(l.proxies, id)
	}
}

// apply starts or stops the device-link listener to match the setting;
// l.mu is held.
func (l *deviceLink) apply() {
	want := l.enabled && l.svc != nil
	if want == (l.server != nil) {
		return
	}
	if !want {
		if l.previewStop != nil {
			l.previewStop()
			l.previewStop = nil
		}
		l.preview.Revoke()
		l.stop()
		_ = l.server.Close()
		l.server, l.stop = nil, nil
		return
	}
	svc := l.svc
	ln, stop := svc.Listen(deviceLinkProtocol)
	previewListener, previewStop := svc.Listen(servicepreview.Protocol)
	l.previewStop = previewStop
	go func() {
		for {
			conn, err := previewListener.Accept()
			if err != nil {
				return
			}
			id, ok := p2p.PeerOf(conn)
			if !ok {
				_ = conn.Close()
				continue
			}
			go l.preview.ServeStream(id.String(), conn)
		}
	}()
	l.stop = stop
	l.server = &http.Server{
		Handler:           http.HandlerFunc(l.v2.ServeRemote),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			id, ok := p2p.PeerOf(c)
			if !ok {
				return ctx
			}
			info, _ := svc.Peer(id)
			return localhost.WithRemotePeer(ctx, localhost.RemotePeer{ID: id.String(), DeviceName: info.DeviceName, Connection: p2p.StreamConnection(c)})
		},
	}
	go func(s *http.Server) {
		if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			l.logger.Warn("device link: server stopped", "error", err)
		}
	}(l.server)
}

func (l *deviceLink) setEnabled(enabled bool) error {
	data, _ := json.Marshal(map[string]bool{"enabled": enabled})
	if err := writeFileAtomic(l.path, data); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
	l.apply()
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".device-link-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// register mounts the loopback routes Desktop uses.
func (l *deviceLink) register(mux *http.ServeMux) {
	mux.HandleFunc("/local/service-preview", l.handlePreviewPolicy)
	mux.HandleFunc("/local/preview/tunnel", l.handlePreviewTunnel)
	mux.HandleFunc("/local/device-link", l.handleSetting)
	mux.HandleFunc("/local/devices", l.handleDevices)
	mux.HandleFunc("/local/devices/", l.handleForward)
}

func deviceLinkError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"kind": kind, "message": message}})
}

func (l *deviceLink) handleSetting(w http.ResponseWriter, r *http.Request) {
	if !localhost.AuthorizeDesktop(r, l.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.Enabled == nil {
			deviceLinkError(w, http.StatusBadRequest, "invalid_request", "enabled is required")
			return
		}
		if err := l.setEnabled(*body.Enabled); err != nil {
			deviceLinkError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	l.mu.Lock()
	state := map[string]bool{"available": l.svc != nil, "enabled": l.enabled}
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// handleDevices lists the account's devices that are connected and share
// their conversations.
func (l *deviceLink) handleDevices(w http.ResponseWriter, r *http.Request) {
	if !localhost.AuthorizeDesktop(r, l.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	devices := []map[string]string{}
	l.mu.Lock()
	svc := l.svc
	l.mu.Unlock()
	if svc != nil {
		for _, p := range svc.Peers() {
			if p.Connected && svc.SupportsProtocol(p.ID, deviceLinkProtocol) {
				devices = append(devices, map[string]string{"peer_id": p.ID.String(), "device_name": p.DeviceName, "connection": p.Connection})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"devices": devices})
}

// handleForward sends /local/devices/{peer}/local/v2/... to that device.
// This device's entry authenticates Desktop first; the other device only
// sees this device's identity and the scope.
func (l *deviceLink) handleForward(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/local/devices/")
	raw, inner, _ := strings.Cut(rest, "/")
	id, err := peer.Decode(raw)
	if err != nil || !strings.HasPrefix("/"+inner, "/local/v2/") {
		deviceLinkError(w, http.StatusNotFound, "device_unknown", "Unknown device")
		return
	}
	r.URL.Path = "/" + inner
	r.URL.RawPath = ""
	// Tickets are issued here and spent on the forwarded WebSocket.
	if r.URL.Path == "/local/v2/ws-ticket" {
		l.v2.ServeHTTP(w, r)
		return
	}
	if !l.v2.Admit(w, r) {
		return
	}
	l.mu.Lock()
	svc := l.svc
	var p *deviceProxy
	if svc != nil {
		if _, known := svc.Peer(id); known {
			p = l.proxies[id]
			if p == nil {
				p = newDeviceProxy(svc, id)
				l.proxies[id] = p
			}
		}
	}
	l.mu.Unlock()
	if p == nil {
		deviceLinkError(w, http.StatusNotFound, "device_unknown", "This device is not signed in to your account")
		return
	}
	if r.URL.Path == "/local/v2/link-events" && r.Method == http.MethodGet {
		serveDeviceLinkEvents(w, r, svc, id)
		return
	}
	if svc.Connection(id) == p2p.ConnectionRelayed && r.ContentLength > relayTransferLimit {
		deviceLinkError(w, http.StatusRequestEntityTooLarge, "relay_transfer_limited", "Files over 5 MB cannot move while the devices are connected through the relay")
		return
	}
	p.proxy.ServeHTTP(w, r)
}

// moveToDirect closes idle and relayed WebSocket streams to a device once a
// direct connection exists, so Desktop reconnects over it.
func (l *deviceLink) moveToDirect(id peer.ID) {
	l.mu.Lock()
	p := l.proxies[id]
	l.mu.Unlock()
	if p != nil {
		p.tunnel.MoveToDirect()
	}
}

type deviceProxy struct {
	proxy  *httputil.ReverseProxy
	tunnel *p2p.Tunnel
}

var errRelayLimit = errors.New("relay transfer limited")

func newDeviceProxy(svc *p2p.Service, id peer.ID) *deviceProxy {
	p := &deviceProxy{tunnel: svc.NewTunnel(id, deviceLinkProtocol, &http.Transport{
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 8,
	})}
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = "http", "device-link", "device-link"
		},
		Transport:     p.tunnel,
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			if res.ContentLength > relayTransferLimit && svc.Connection(id) == p2p.ConnectionRelayed {
				return errRelayLimit
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			switch {
			case errors.Is(err, errRelayLimit):
				deviceLinkError(w, http.StatusRequestEntityTooLarge, "relay_transfer_limited", "Files over 5 MB cannot move while the devices are connected through the relay")
			case errors.Is(err, p2p.ErrNotAllowed):
				deviceLinkError(w, http.StatusNotFound, "device_unknown", "This device is not signed in to your account")
			// A device that turned Device link off removes the protocol and
			// resets the streams it was serving.
			case strings.Contains(err.Error(), "protocols not supported") || !svc.SupportsProtocol(id, deviceLinkProtocol):
				deviceLinkError(w, http.StatusServiceUnavailable, "device_link_off", "This device does not share its conversations")
			default:
				deviceLinkError(w, http.StatusBadGateway, "device_unreachable", fmt.Sprintf("Cannot reach the device: %v", err))
			}
		},
	}
	return p
}
