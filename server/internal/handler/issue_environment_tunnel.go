package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/envtunnel"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	tunnelDialTimeout          = 10 * time.Second
	maxPendingDialsPerRuntime  = 4
	environmentProxyIdleLease  = 2 * time.Minute
	environmentTunnelChunkSize = 32 << 10
)

var (
	errEnvironmentProxyQuota     = errors.New("environment relay quota exceeded")
	errEnvironmentTunnelLimit    = errors.New("environment tunnel limit reached")
	errEnvironmentSessionRevoked = errors.New("environment session revoked")
)

type EnvironmentProxyLimits struct {
	MaxBodyBytes     int64
	BytesPerSecond   int64
	WorkspaceTunnels int
	GlobalTunnels    int
	TunnelLifetime   time.Duration
}

func (l EnvironmentProxyLimits) withDefaults() EnvironmentProxyLimits {
	if l.MaxBodyBytes <= 0 {
		l.MaxBodyBytes = 32 << 20
	}
	if l.BytesPerSecond <= 0 {
		l.BytesPerSecond = 8 << 20
	}
	if l.WorkspaceTunnels <= 0 {
		l.WorkspaceTunnels = 32
	}
	if l.GlobalTunnels <= 0 {
		l.GlobalTunnels = 256
	}
	if l.TunnelLifetime <= 0 {
		l.TunnelLifetime = 30 * time.Minute
	}
	return l
}

type environmentProxyState struct {
	mu           sync.Mutex
	launchNonces map[string]int64
	revoked      map[environmentRevocationKey]int64
	leases       map[string]map[string]*environmentLease
	lastCleanup  time.Time
	pending      map[string]*pendingEnvironmentTunnel
	slots        map[string]*environmentTunnelSlots
	active       map[*trackedEnvironmentTunnel]environmentSessionClaims
	activeByWS   map[string]int
	activeGlobal int
}

type environmentRevocationKey struct {
	envID     string
	runtimeID string
}

type environmentLease struct {
	runtimeID string
	idleUntil time.Time
	requests  int
	limiter   environmentBandwidth
}

type pendingEnvironmentTunnel struct {
	target environmentSessionClaims
	secret string
	ctx    context.Context
	cancel context.CancelFunc
	conn   chan net.Conn
}

type environmentTunnelSlots struct {
	ch   chan struct{}
	refs int
}

func (s *environmentProxyState) init() {
	s.launchNonces = make(map[string]int64)
	s.revoked = make(map[environmentRevocationKey]int64)
	s.leases = make(map[string]map[string]*environmentLease)
	s.pending = make(map[string]*pendingEnvironmentTunnel)
	s.slots = make(map[string]*environmentTunnelSlots)
	s.active = make(map[*trackedEnvironmentTunnel]environmentSessionClaims)
	s.activeByWS = make(map[string]int)
}

func (s *environmentProxyState) addLaunch(nonce string, expiresAt int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.launchNonces) >= maxEnvironmentLaunches {
		now := time.Now().Unix()
		for key, expiry := range s.launchNonces {
			if expiry <= now {
				delete(s.launchNonces, key)
			}
		}
	}
	if len(s.launchNonces) >= maxEnvironmentLaunches {
		return false
	}
	s.launchNonces[nonce] = expiresAt
	return true
}

func (s *environmentProxyState) consumeLaunch(claims environmentLaunchClaims) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt, found := s.launchNonces[claims.Nonce]
	if found {
		delete(s.launchNonces, claims.Nonce)
	}
	return found && expiresAt == claims.ExpiresAt && s.validLocked(claims.environmentSessionClaims, time.Now())
}

func (s *environmentProxyState) validLocked(target environmentSessionClaims, now time.Time) bool {
	return target.ExpiresAt > now.Unix() &&
		target.IssuedAt > s.revoked[environmentRevocationKey{envID: target.EnvID}] &&
		target.IssuedAt > s.revoked[environmentRevocationKey{runtimeID: target.RuntimeID}]
}

func (s *environmentProxyState) sessionValid(target environmentSessionClaims, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validLocked(target, now)
}

func (s *environmentProxyState) cleanupLocked(now time.Time) {
	if now.Sub(s.lastCleanup) < time.Minute {
		return
	}
	s.lastCleanup = now
	for wsID, leases := range s.leases {
		pruneEnvironmentLeases(leases, now)
		if len(leases) == 0 {
			delete(s.leases, wsID)
		}
	}
	cutoff := now.Add(-environmentSessionTTL - environmentLaunchTTL).UnixNano()
	for key, revokedAt := range s.revoked {
		if revokedAt < cutoff {
			delete(s.revoked, key)
		}
	}
}

func pruneEnvironmentLeases(leases map[string]*environmentLease, now time.Time) {
	for id, lease := range leases {
		if lease.requests == 0 && !lease.idleUntil.After(now) {
			delete(leases, id)
		}
	}
}

func (p *environmentProxy) admitEnvironment(w http.ResponseWriter, target environmentSessionClaims, request bool) *environmentLease {
	lease, err := p.reserveEnvironment(target, request)
	switch {
	case err == errEnvironmentSessionRevoked:
		writeError(w, http.StatusGone, "environment session revoked")
	case err == errEnvironmentProxyQuota:
		writeEnvironmentProxyLimit(w, *target.Limit, target.Revision)
	}
	return lease
}

func (p *environmentProxy) reserveEnvironment(target environmentSessionClaims, request bool) (*environmentLease, error) {
	s := &p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if !s.validLocked(target, now) {
		return nil, errEnvironmentSessionRevoked
	}
	s.cleanupLocked(now)
	if target.Limit != nil && *target.Limit == 0 {
		return nil, errEnvironmentProxyQuota
	}
	leases := s.leases[target.WorkspaceID]
	lease := leases[target.EnvID]
	if lease == nil || lease.requests == 0 && !lease.idleUntil.After(now) {
		pruneEnvironmentLeases(leases, now)
		if target.Limit != nil && len(leases) >= *target.Limit {
			return nil, errEnvironmentProxyQuota
		}
		if leases == nil {
			leases = make(map[string]*environmentLease)
			s.leases[target.WorkspaceID] = leases
		}
		lease = &environmentLease{
			runtimeID: target.RuntimeID,
			limiter: environmentBandwidth{
				rate:   float64(p.limits.BytesPerSecond),
				tokens: environmentTunnelChunkSize,
				last:   now,
			},
		}
		leases[target.EnvID] = lease
	}
	lease.idleUntil = now.Add(environmentProxyIdleLease)
	if request {
		lease.requests++
	}
	return lease, nil
}

func (s *environmentProxyState) releaseEnvironment(lease *environmentLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease.requests--
	lease.idleUntil = time.Now().Add(environmentProxyIdleLease)
}

func (h *Handler) revokeEnvironmentSession(id string) {
	if h.envProxy != nil {
		h.envProxy.state.revoke(environmentRevocationKey{envID: id})
	}
}

func (s *environmentProxyState) revoke(key environmentRevocationKey) {
	matches := func(envID, runtimeID string) bool {
		return key.envID != "" && envID == key.envID || key.runtimeID != "" && runtimeID == key.runtimeID
	}
	s.mu.Lock()
	now := time.Now()
	s.cleanupLocked(now)
	s.revoked[key] = now.UnixNano()
	for wsID, leases := range s.leases {
		for id, lease := range leases {
			if matches(id, lease.runtimeID) {
				delete(leases, id)
			}
		}
		if len(leases) == 0 {
			delete(s.leases, wsID)
		}
	}
	for _, pending := range s.pending {
		if matches(pending.target.EnvID, pending.target.RuntimeID) {
			pending.cancel()
		}
	}
	var connections []*trackedEnvironmentTunnel
	for conn, target := range s.active {
		if matches(target.EnvID, target.RuntimeID) {
			connections = append(connections, conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range connections {
		conn.Close()
	}
}

func (s *environmentProxyState) reserveTunnel(target environmentSessionClaims, limits EnvironmentProxyLimits) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeGlobal >= limits.GlobalTunnels || s.activeByWS[target.WorkspaceID] >= limits.WorkspaceTunnels {
		return false
	}
	s.activeGlobal++
	s.activeByWS[target.WorkspaceID]++
	return true
}

func (s *environmentProxyState) releaseTunnel(target environmentSessionClaims, conn *trackedEnvironmentTunnel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conn != nil {
		conn.timer.Stop()
		delete(s.active, conn)
	}
	s.activeGlobal--
	s.activeByWS[target.WorkspaceID]--
	if s.activeByWS[target.WorkspaceID] == 0 {
		delete(s.activeByWS, target.WorkspaceID)
	}
}

func (s *environmentProxyState) runtimeSlots(runtimeID string) (*environmentTunnelSlots, func()) {
	s.mu.Lock()
	slots := s.slots[runtimeID]
	if slots == nil {
		slots = &environmentTunnelSlots{ch: make(chan struct{}, maxPendingDialsPerRuntime)}
		s.slots[runtimeID] = slots
	}
	slots.refs++
	s.mu.Unlock()
	return slots, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		slots.refs--
		if slots.refs == 0 {
			delete(s.slots, runtimeID)
		}
	}
}

func (h *Handler) dialEnvironmentTunnel(ctx context.Context, _, _ string) (net.Conn, error) {
	p := h.envProxy
	s := &p.state
	target, ok := ctx.Value(environmentTunnelContextKey{}).(environmentSessionClaims)
	if !ok {
		return nil, errors.New("missing environment tunnel target")
	}
	if !s.reserveTunnel(target, p.limits) {
		// Idle pooled tunnels must not exhaust admission for active requests.
		p.transport.CloseIdleConnections()
		if !s.reserveTunnel(target, p.limits) {
			return nil, errEnvironmentTunnelLimit
		}
	}
	connected := false
	defer func() {
		if !connected {
			s.releaseTunnel(target, nil)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, tunnelDialTimeout)
	defer cancel()
	slots, releaseSlots := s.runtimeSlots(target.RuntimeID)
	defer releaseSlots()
	select {
	case slots.ch <- struct{}{}:
		defer func() { <-slots.ch }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	streamID := uuid.NewString()
	pending := &pendingEnvironmentTunnel{
		target: target,
		secret: uuid.NewString(),
		ctx:    ctx,
		cancel: cancel,
		conn:   make(chan net.Conn),
	}
	s.mu.Lock()
	if !s.validLocked(target, time.Now()) {
		s.mu.Unlock()
		return nil, errEnvironmentSessionRevoked
	}
	s.pending[streamID] = pending
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, streamID)
		s.mu.Unlock()
	}()

	// The local hub already checked the recipient's runtime binding when
	// registering its authenticated WebSocket. Keep the rendezvous secret
	// on that connection and out of relay messages and HTTP URL logs.
	kind := protocol.PendingWorkKindEnvironmentTunnelPrefix + streamID + "." + pending.secret
	if p.notifyTunnel == nil || !p.notifyTunnel(target.RuntimeID, kind) {
		return nil, context.DeadlineExceeded
	}
	select {
	case conn := <-pending.conn:
		s.mu.Lock()
		lease := s.leases[target.WorkspaceID][target.EnvID]
		if lease == nil || !s.validLocked(target, time.Now()) {
			s.mu.Unlock()
			conn.Close()
			return nil, errEnvironmentSessionRevoked
		}
		lifetime, cancelLifetime := context.WithCancel(context.Background())
		tunnel := &trackedEnvironmentTunnel{
			Conn:    conn,
			state:   s,
			target:  target,
			limiter: &lease.limiter,
			ctx:     lifetime,
			cancel:  cancelLifetime,
		}
		tunnel.timer = time.AfterFunc(p.limits.TunnelLifetime, func() { tunnel.Close() })
		s.active[tunnel] = target
		s.mu.Unlock()
		connected = true
		return tunnel, nil
	case <-ctx.Done():
		if !s.sessionValid(target, time.Now()) {
			return nil, errEnvironmentSessionRevoked
		}
		return nil, ctx.Err()
	}
}

// ConnectEnvironmentTunnel consumes a short-lived rendezvous credential
// delivered to the daemon over its authenticated, runtime-scoped socket.
func (h *Handler) ConnectEnvironmentTunnel(w http.ResponseWriter, r *http.Request) {
	p := h.envProxy
	if p == nil {
		writeError(w, http.StatusNotFound, "environment tunnel not found")
		return
	}
	streamID := chi.URLParam(r, "streamId")
	runtimeID := chi.URLParam(r, "runtimeId")
	s := &p.state
	s.mu.Lock()
	pending := s.pending[streamID]
	if pending != nil && pending.target.RuntimeID == runtimeID && pending.ctx.Err() == nil &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+pending.secret)) == 1 {
		delete(s.pending, streamID)
	} else {
		pending = nil
	}
	s.mu.Unlock()
	if pending == nil {
		writeError(w, http.StatusNotFound, "environment tunnel not found")
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: tunnelDialTimeout,
		Error: func(w http.ResponseWriter, _ *http.Request, status int, _ error) {
			writeError(w, status, "environment tunnel upgrade failed")
		},
	}
	ws, err := upgrader.Upgrade(w, r, http.Header{
		"X-Env-Tunnel-Key":  {pending.target.LocalKey},
		"X-Env-Tunnel-Port": {strconv.Itoa(int(pending.target.Port))},
	})
	if err != nil {
		pending.cancel()
		return
	}
	conn := envtunnel.NewConn(ws)
	select {
	case pending.conn <- conn:
	case <-pending.ctx.Done():
		conn.Close()
	}
}

type trackedEnvironmentTunnel struct {
	net.Conn
	state   *environmentProxyState
	target  environmentSessionClaims
	limiter *environmentBandwidth
	ctx     context.Context
	cancel  context.CancelFunc
	timer   *time.Timer
	once    sync.Once
}

func (c *trackedEnvironmentTunnel) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b[:min(len(b), environmentTunnelChunkSize)])
	if n > 0 {
		if waitErr := c.limiter.wait(c.ctx, n); waitErr != nil {
			return 0, waitErr
		}
	}
	return n, err
}

func (c *trackedEnvironmentTunnel) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		chunk := b[written:min(written+environmentTunnelChunkSize, len(b))]
		if err := c.limiter.wait(c.ctx, len(chunk)); err != nil {
			return written, err
		}
		n, err := c.Conn.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (c *trackedEnvironmentTunnel) Close() error {
	var err error
	c.once.Do(func() {
		c.cancel()
		err = c.Conn.Close()
		c.state.releaseTunnel(c.target, c)
	})
	return err
}

// environmentBandwidth is shared by all tunnels for one environment.
type environmentBandwidth struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	last   time.Time
}

func (l *environmentBandwidth) wait(ctx context.Context, n int) error {
	for ctx.Err() == nil {
		l.mu.Lock()
		now := time.Now()
		l.tokens = min(environmentTunnelChunkSize, l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now
		deficit := float64(n) - l.tokens
		if deficit <= 0 {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		timer := time.NewTimer(time.Duration(deficit / l.rate * float64(time.Second)))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		}
	}
	return net.ErrClosed
}
