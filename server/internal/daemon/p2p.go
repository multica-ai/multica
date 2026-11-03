package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/p2p"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const (
	// p2pKeyFileName lives in the profile directory: profiles on one machine
	// may be signed in to different accounts, so each is its own device.
	p2pKeyFileName        = "p2p.key"
	p2pReregisterInterval = 6 * time.Hour
)

var p2pRegisterRetrySchedule = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

const (
	deviceTimingFile  = "device-timing.jsonl"
	reportsTimingFile = "timing-reports.jsonl"
	// deviceTimingMaxBytes is less than a turn file's: state changes are rare and
	// proxied requests are not worth keeping for long.
	deviceTimingMaxBytes = 2 << 20
)

// openDeviceTiming opens this device's connection log, once. It is a separate file
// so a chatty link cannot rotate turn records away.
func (d *Daemon) openDeviceTiming() *timing.Log {
	d.deviceTimingOnce.Do(func() {
		dir := d.cfg.ProfileDir
		if dir == "" {
			profile, err := cli.ProfileDir(d.cfg.Profile)
			if err != nil {
				return
			}
			dir = profile
		}
		d.deviceTiming = timing.Open(filepath.Join(dir, "local-conversations", "diagnostics", deviceTimingFile), timing.Options{MaxBytes: deviceTimingMaxBytes})
	})
	return d.deviceTiming
}

// P2P returns the running device link service, or nil when p2p is disabled
// or has not started. Features register protocol handlers and open streams
// to the account's other devices through it.
func (d *Daemon) P2P() *p2p.Service { return d.p2p.Load() }

func (d *Daemon) startP2P(ctx context.Context) error {
	dir, err := cli.ProfileDir(d.cfg.Profile)
	if err != nil {
		return err
	}
	key, err := p2p.LoadOrCreateKey(filepath.Join(dir, p2pKeyFileName))
	if err != nil {
		return err
	}
	registry := newP2PRegistryClient(d.cfg.P2PServerURL, d.client.Token)
	// reregister is signalled when the advertised addresses change, so other
	// devices learn new ones (port mapping, relay circuit) without waiting
	// for the periodic registration.
	reregister := make(chan struct{}, 1)
	marks := timing.NewMarks(d.openDeviceTiming(), timing.SourceDaemon)
	svc, err := p2p.Start(ctx, p2p.Config{
		Key:             key,
		Fetch:           func(ctx context.Context) ([]p2p.Peer, error) { return fetchP2PPeers(ctx, registry) },
		Relay:           func(ctx context.Context) (*peer.AddrInfo, error) { return fetchP2PRelay(ctx, registry) },
		Port:            d.cfg.P2PPort,
		MDNSServiceName: p2p.DefaultMDNSServiceName,
		OnAddrsChanged: func() {
			select {
			case reregister <- struct{}{}:
			default:
			}
		},
		Logger: d.logger,
		Marks:  marks,
	})
	if err != nil {
		return err
	}
	d.p2p.Store(svc)
	if d.deviceLink != nil {
		d.deviceLink.attach(svc)
	}

	go func() {
		<-ctx.Done()
		d.p2p.Store(nil)
		if d.deviceLink != nil {
			d.deviceLink.detach(svc)
		}
		svc.Close()
	}()
	go d.p2pRegistrationLoop(ctx, registry, svc, key.GetPublic(), reregister, marks)
	networks := make(chan struct{}, 1)
	go d.p2pWatchLoop(ctx, registry, svc, networks)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		watchNetwork(ctx, ticker.C, networkFingerprint, func() {
			registry.networkChanged()
			svc.NetworkChanged()
			select {
			case networks <- struct{}{}:
			default:
			}
		})
	}()
	return nil
}

// p2pRegistrationLoop publishes the public key and current addresses with
// backoff, re-publishes well inside the registry's 24h device TTL so a live
// daemon never expires and a rename or registry cleanup heals, and
// re-publishes whenever the addresses change. After each successful
// registration the allowlist is refreshed, which also tells the daemon its
// own entry is live.
func (d *Daemon) p2pRegistrationLoop(ctx context.Context, registry *p2pRegistryClient, svc *p2p.Service, pub crypto.PubKey, reregister <-chan struct{}, marks *timing.Marks) {
	raw, err := crypto.MarshalPublicKey(pub)
	if err != nil {
		d.logger.Warn("p2p: encode public key", "error", err)
		return
	}
	publicKey := base64.StdEncoding.EncodeToString(raw)
	registered := false
	for {
		for attempt := 0; ; attempt++ {
			addrs := svc.AdvertisedAddrs()
			err := registry.register(ctx, d.cfg.DaemonID, d.cfg.Profile, publicKey, d.cfg.DeviceName, addrs)
			if err == nil {
				d.logger.Info("p2p: identity registered", "peer_id", svc.ID().String(), "addrs", addrs)
				if !registered {
					registered = true
					marks.Write(timing.Record{Stage: timing.StageLinkRegistered})
				}
				if err := svc.Refresh(ctx); err != nil && ctx.Err() == nil {
					d.logger.Warn("p2p: allowlist refresh failed", "error", err)
				}
				break
			}
			if ctx.Err() != nil {
				return
			}
			delay := p2pRegisterRetrySchedule[min(attempt, len(p2pRegisterRetrySchedule)-1)]
			registered = false
			d.logger.Warn("p2p: identity registration failed", "error", err, "retry_in", delay)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-reregister:
				timer.Stop()
				attempt = -1
			case <-timer.C:
			}
		}
		timer := time.NewTimer(p2pReregisterInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-reregister:
			timer.Stop()
		}
	}
}

// fetchP2PPeers turns the registry's device list into the allowlist. Peer IDs
// are derived from the keys locally rather than taken from the response.
func fetchP2PPeers(ctx context.Context, registry *p2pRegistryClient) ([]p2p.Peer, error) {
	peers, err := registry.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]p2p.Peer, 0, len(peers))
	for _, p := range peers {
		raw, err := base64.StdEncoding.DecodeString(p.PublicKey)
		if err != nil {
			continue
		}
		id, err := p2p.PeerIDFromPublicKey(raw)
		if err != nil {
			continue
		}
		var addrs []ma.Multiaddr
		for _, s := range p.Addrs {
			if a, err := ma.NewMultiaddr(s); err == nil {
				addrs = append(addrs, a)
			}
		}
		out = append(out, p2p.Peer{ID: id, DaemonID: p.DaemonID, Profile: p.Profile, DeviceName: p.DeviceName, RegisteredAddrs: addrs})
	}
	return out, nil
}

// fetchP2PRelay returns the registry's relay node, or nil when it has none.
func fetchP2PRelay(ctx context.Context, registry *p2pRegistryClient) (*peer.AddrInfo, error) {
	info, err := registry.relay(ctx)
	if err != nil || info == nil {
		return nil, err
	}
	id, err := peer.Decode(info.PeerID)
	if err != nil {
		return nil, fmt.Errorf("p2p registry: invalid relay peer id: %w", err)
	}
	out := &peer.AddrInfo{ID: id}
	for _, s := range info.Addrs {
		if a, err := ma.NewMultiaddr(s); err == nil {
			out.Addrs = append(out.Addrs, a)
		}
	}
	if len(out.Addrs) == 0 {
		return nil, fmt.Errorf("p2p registry: relay has no usable address")
	}
	return out, nil
}

// UnregisterP2PDevice removes this machine's device for profile from the p2p
// registry, so the account's other devices drop it right away instead of
// after the registry's 24h expiry. Sign-out calls it before discarding the
// login token; it is a no-op when no registry is configured.
func UnregisterP2PDevice(ctx context.Context, token, profile string) error {
	serverURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_DAEMON_P2P_SERVER_URL")), "/")
	if serverURL == "" || token == "" {
		return nil
	}
	daemonID := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_ID"))
	if daemonID == "" {
		id, err := EnsureDaemonID(profile)
		if err != nil {
			return err
		}
		daemonID = id
	}
	err := newP2PRegistryClient(serverURL, func() string { return token }).remove(ctx, daemonID, profile)
	if errors.Is(err, errP2PNotFound) {
		return nil
	}
	return err
}

// p2pStatusResponse is served on the local health port. It is read-only;
// streams can only be opened in-process through P2P().
type p2pStatusResponse struct {
	Enabled bool `json:"enabled"`
	*p2p.Status
}

func (d *Daemon) p2pStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := p2pStatusResponse{}
		if svc := d.P2P(); svc != nil {
			st := svc.Status()
			resp = p2pStatusResponse{Enabled: true, Status: &st}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
