package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/oklog/ulid/v2"
)

// MembershipChecker verifies a user belongs to a workspace.
type MembershipChecker interface {
	IsMember(ctx context.Context, userID, workspaceID string) bool
}

type TagWebSocketAccessGate interface {
	GrantSession(context.Context, tagaccess.SessionGrant) error
	Authorize(context.Context, tagaccess.AccessRequest) tagaccess.Decision
}

type TagWebSocketMirrorResolver interface {
	MulticaUserID(context.Context, string) (string, bool, error)
	VIBESUserID(context.Context, string) (string, bool, error)
	MulticaWorkspaceID(context.Context, string) (string, bool, error)
}

// SlugResolver translates a workspace slug to its UUID.
type SlugResolver func(ctx context.Context, slug string) (workspaceID string, err error)

// PATResolver resolves a Personal Access Token to a user ID.
type PATResolver interface {
	ResolveToken(ctx context.Context, token string) (userID string, ok bool)
}

// ScopeAuthorizer decides whether a connection (identified by userID +
// workspaceID) is allowed to subscribe to a given scope. Implementations
// typically perform a DB lookup on the underlying resource (task / chat
// session) and verify it belongs to workspaceID. Implementations should
// cache positive results to avoid hot-path DB load.
type ScopeAuthorizer interface {
	AuthorizeScope(ctx context.Context, userID, workspaceID, scopeType, scopeID string) (bool, error)
}

var allowedWSOrigins atomic.Value // holds []string
var trustedProxies atomic.Value   // holds []netip.Prefix

func init() {
	allowedWSOrigins.Store(loadAllowedOrigins())
	trustedProxies.Store(loadTrustedProxies())
}

func loadAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:5174",
		}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

// loadTrustedProxies reads the same MULTICA_TRUSTED_PROXIES env var the rest of
// the server uses (see cmd/server/router.go and handler.Config.TrustedProxies),
// parsing it as a comma-separated list of CIDR prefixes. Invalid entries are
// dropped with a warn-line rather than crashing. Empty input returns nil, which
// means "trust no proxy" — X-Forwarded-Host is then never honored. The router
// overrides this at startup via SetTrustedProxies so both share one config.
func loadTrustedProxies() []netip.Prefix {
	raw := strings.TrimSpace(os.Getenv("MULTICA_TRUSTED_PROXIES"))
	if raw == "" {
		return nil
	}
	var prefixes []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			slog.Warn("ws: ignoring invalid trusted proxy CIDR", "value", s, "error", err)
			continue
		}
		prefixes = append(prefixes, p)
	}
	return prefixes
}

// SetAllowedOrigins overrides the WebSocket origin whitelist.
func SetAllowedOrigins(origins []string) {
	allowedWSOrigins.Store(origins)
}

// SetTrustedProxies overrides the trusted proxy CIDR list. The server wires the
// shared MULTICA_TRUSTED_PROXIES value in here at startup.
func SetTrustedProxies(proxies []netip.Prefix) {
	trustedProxies.Store(proxies)
}

// isTrustedProxy reports whether the request's remote address falls within one
// of the configured trusted proxy CIDRs.
func isTrustedProxy(remoteAddr string) bool {
	proxies := trustedProxies.Load().([]netip.Prefix)
	if len(proxies) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(remoteHost(remoteAddr))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteHost extracts the host/IP from an http.Request.RemoteAddr, which is
// normally "host:port". It handles bracketed IPv6 ("[::1]:443") via
// net.SplitHostPort and falls back to the raw value (sans brackets) when no
// port is present.
func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return strings.Trim(remoteAddr, "[]")
}

// firstForwardedHost returns the first host from a (possibly comma-separated)
// X-Forwarded-Host header. Proxy chains append values left-to-right, so the
// first entry is the original client-facing host we compare against Origin.
func firstForwardedHost(h string) string {
	if i := strings.IndexByte(h, ','); i >= 0 {
		h = h[:i]
	}
	return strings.TrimSpace(h)
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Same-origin: native clients (mobile, CLI) have no real page host, so
	// their WebSocket library fills Origin with the connection target —
	// which equals the server's own Host. They authenticate via bearer
	// token, not auto-attached cookies, so CSRF (the attack the explicit
	// allowlist below defends against) does not apply. This matches the
	// gorilla/websocket default CheckOrigin behavior; the allowlist exists
	// in addition to support cross-origin browser clients (web/desktop).
	if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, r.Host) {
		return true
	}
	// Reverse-proxy support: when sitting behind a proxy the Host header
	// contains the internal address. X-Forwarded-Host carries the original
	// public host seen by the client, so we treat a matching origin as
	// same-origin in that case too. SECURITY: Only trust X-Forwarded-Host
	// if the request comes from a trusted proxy to prevent header spoofing.
	if fwdHost := firstForwardedHost(r.Header.Get("X-Forwarded-Host")); fwdHost != "" && isTrustedProxy(r.RemoteAddr) {
		if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, fwdHost) {
			return true
		}
	}
	origins := allowedWSOrigins.Load().([]string)
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	slog.Warn("ws: rejected origin", "origin", origin, "remote_addr", r.RemoteAddr)
	return false
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10

	// inboundReadLimit caps a single inbound message. Every frame a client
	// legitimately sends is tiny — the largest is the token auth frame, well
	// under 1 KiB — but gorilla buffers a whole message in memory before
	// handing it over, and a fragmented message keeps the read deadline alive
	// through interleaved pongs. Without a limit one connection can therefore
	// grow that buffer without bound and OOM the process. Matches the daemon
	// hub limit so both WebSocket surfaces answer this question the same way.
	inboundReadLimit = 64 * 1024
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

// scopeKey is the composite key used to look up a "room" of subscribers.
type scopeKey struct {
	Type string
	ID   string
}

func sk(t, id string) scopeKey { return scopeKey{Type: t, ID: id} }

// Client represents a single WebSocket connection with identity and the set
// of scopes it is currently subscribed to.
type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	userID      string
	workspaceID string
	metadata    ConnectionMetadata
	revoked     atomic.Bool
	sendMu      sync.Mutex
	sendClosed  bool
	writeMu     sync.Mutex
	frameMu     sync.Mutex

	// subscriptions is guarded by hub.mu. Tracks the scopes this client is
	// currently in. Used to clean up rooms on disconnect.
	subscriptions map[scopeKey]bool

	// lastSeenEventIDs is used by the dual-write broadcaster (and any
	// future deliverer) to dedup messages that arrived first via the local
	// fast path and are then re-played from Redis. Bounded LRU semantics
	// are not required because event IDs are ULIDs and we only keep the
	// last few.
	dedupMu  sync.Mutex
	seenIDs  map[string]struct{}
	seenList []string
}

const dedupCapacity = 128

// markSeen records eventID as already delivered to this client. Returns true
// if it was the first time we saw this id (caller should deliver), false if
// it's a duplicate (caller should drop).
func (c *Client) markSeen(eventID string) bool {
	if eventID == "" {
		return true
	}
	c.dedupMu.Lock()
	defer c.dedupMu.Unlock()
	if c.seenIDs == nil {
		c.seenIDs = make(map[string]struct{}, dedupCapacity)
	}
	if _, ok := c.seenIDs[eventID]; ok {
		return false
	}
	c.seenIDs[eventID] = struct{}{}
	c.seenList = append(c.seenList, eventID)
	if len(c.seenList) > dedupCapacity {
		drop := c.seenList[0]
		c.seenList = c.seenList[1:]
		delete(c.seenIDs, drop)
	}
	return true
}

// SubscriptionCallback fires when a scope's local subscriber count crosses
// 0↔1 boundaries. Used by the Redis relay to start/stop XREADGROUP loops on
// demand.
type SubscriptionCallback func(scopeType, scopeID string)

type registrationRequest struct {
	client   *Client
	accepted chan bool
}

type revocationLeaseFence struct {
	valid func() bool
}

// Hub manages WebSocket connections organized into scope-based rooms.
type Hub struct {
	rooms      map[scopeKey]map[*Client]bool
	clients    map[*Client]bool // every connected client (used by global Broadcast and snapshots)
	broadcast  chan []byte
	register   chan registrationRequest
	unregister chan *Client
	mu         sync.RWMutex
	authorize  sync.RWMutex
	instanceID string
	fences     revocationFences

	// revocationHealthy is installed with the cross-instance close broker.
	// Tag clients fail closed when this feed cannot be checked.
	revocationHealthy func(context.Context) bool
	revocationLease   atomic.Pointer[revocationLeaseFence]

	authorizer ScopeAuthorizer

	// Subscription lifecycle hooks. Both can be nil.
	onFirstSubscriber SubscriptionCallback
	onLastSubscriber  SubscriptionCallback
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[scopeKey]map[*Client]bool),
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan registrationRequest),
		unregister: make(chan *Client),
		instanceID: ulid.Make().String(),
		fences:     newRevocationFences(),
	}
}

// SetAuthorizer wires a ScopeAuthorizer into the hub. Safe to call before Run.
func (h *Hub) SetAuthorizer(a ScopeAuthorizer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authorizer = a
}

// SetSubscriptionCallbacks registers callbacks fired when a scope on this
// node transitions from 0→1 subscribers (onFirst) or 1→0 (onLast). The
// Redis relay uses these to start/stop a per-scope consumer loop.
func (h *Hub) SetSubscriptionCallbacks(onFirst, onLast SubscriptionCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onFirstSubscriber = onFirst
	h.onLastSubscriber = onLast
}

// Run starts the hub event loop.
func (h *Hub) Run() {
	for {
		select {
		case request := <-h.register:
			request.accepted <- h.acceptRegistration(request.client)

		case client := <-h.unregister:
			h.removeClient(client)

		case message := <-h.broadcast:
			h.fanoutAll(message, "")
		}
	}
}

func (h *Hub) registerClient(client *Client) bool {
	accepted := make(chan bool, 1)
	h.register <- registrationRequest{client: client, accepted: accepted}
	return <-accepted
}

// acceptRegistration checks the authorization watermark and installs the
// client plus its implicit rooms under one lock. A close command therefore
// cannot finish its scan between an old authorization decision and registry
// insertion.
func (h *Hub) acceptRegistration(client *Client) bool {
	h.mu.Lock()
	if !h.fences.allows(client.metadata) || !client.authorizationLeaseValid() {
		client.revoked.Store(true)
		h.mu.Unlock()
		return false
	}
	h.clients[client] = true
	if client.subscriptions == nil {
		client.subscriptions = make(map[scopeKey]bool)
	}
	newRooms := make([]scopeKey, 0, 2)
	for _, key := range []scopeKey{sk(ScopeWorkspace, client.workspaceID), sk(ScopeUser, client.userID)} {
		if key.ID == "" {
			continue
		}
		room := h.rooms[key]
		if room == nil {
			room = make(map[*Client]bool)
			h.rooms[key] = room
			newRooms = append(newRooms, key)
		}
		room[client] = true
		client.subscriptions[key] = true
	}
	callback := h.onFirstSubscriber
	total := len(h.clients)
	h.mu.Unlock()

	M.ConnectsTotal.Add(1)
	M.ActiveConnections.Add(1)
	for _, key := range newRooms {
		M.IncRoom(key.Type)
		if callback != nil {
			callback(key.Type, key.ID)
		}
	}
	slog.Info("ws client connected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)
	return true
}

// removeClient drops a client from all rooms and the global set.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	if !h.clients[client] {
		h.mu.Unlock()
		return
	}
	delete(h.clients, client)
	subs := client.subscriptions
	client.subscriptions = nil
	emptied := make([]scopeKey, 0, len(subs))
	for key := range subs {
		if room, ok := h.rooms[key]; ok {
			delete(room, client)
			if len(room) == 0 {
				delete(h.rooms, key)
				emptied = append(emptied, key)
			}
		}
	}
	client.sendMu.Lock()
	client.sendClosed = true
	close(client.send)
	client.sendMu.Unlock()
	cb := h.onLastSubscriber
	total := len(h.clients)
	h.mu.Unlock()

	M.DisconnectsTotal.Add(1)
	M.ActiveConnections.Add(-1)
	if cb != nil {
		for _, key := range emptied {
			cb(key.Type, key.ID)
		}
	}
	for _, key := range emptied {
		M.DecRoom(key.Type)
	}
	slog.Info("ws client disconnected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)
}

// subscribe adds client to scope (scopeType, scopeID) and fires the
// onFirstSubscriber callback if the room transitioned from empty to non-empty.
// Returns true if the subscription was newly added.
func (h *Hub) subscribe(client *Client, scopeType, scopeID string) bool {
	if scopeType == "" || scopeID == "" {
		return false
	}
	key := sk(scopeType, scopeID)

	h.mu.Lock()
	if !h.clients[client] || client.revoked.Load() || !client.authorizationLeaseValid() {
		h.mu.Unlock()
		return false
	}
	if client.subscriptions == nil {
		client.subscriptions = map[scopeKey]bool{}
	}
	if client.subscriptions[key] {
		h.mu.Unlock()
		return false
	}
	client.subscriptions[key] = true
	room, ok := h.rooms[key]
	first := false
	if !ok {
		room = make(map[*Client]bool)
		h.rooms[key] = room
		first = true
	}
	room[client] = true
	cb := h.onFirstSubscriber
	h.mu.Unlock()

	M.SubscribesTotal(scopeType).Add(1)
	if first {
		M.IncRoom(scopeType)
		if cb != nil {
			cb(scopeType, scopeID)
		}
	}
	return true
}

// unsubscribe removes client from a scope room and fires onLastSubscriber if
// the room is now empty.
func (h *Hub) unsubscribe(client *Client, scopeType, scopeID string) bool {
	if scopeType == "" || scopeID == "" {
		return false
	}
	key := sk(scopeType, scopeID)

	h.mu.Lock()
	if !h.clients[client] {
		h.mu.Unlock()
		return false
	}
	if client.subscriptions == nil || !client.subscriptions[key] {
		h.mu.Unlock()
		return false
	}
	delete(client.subscriptions, key)
	emptied := false
	if room, ok := h.rooms[key]; ok {
		delete(room, client)
		if len(room) == 0 {
			delete(h.rooms, key)
			emptied = true
		}
	}
	cb := h.onLastSubscriber
	h.mu.Unlock()

	M.UnsubscribesTotal(scopeType).Add(1)
	if emptied {
		M.DecRoom(scopeType)
		if cb != nil {
			cb(scopeType, scopeID)
		}
	}
	return true
}

// HasLocalSubscribers reports whether at least one local client is subscribed
// to (scopeType, scopeID). Used by the Redis relay to decide whether to keep
// a per-scope consumer running.
func (h *Hub) HasLocalSubscribers(scopeType, scopeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.rooms[sk(scopeType, scopeID)]
	return ok
}

// LocalScopes returns the set of scopes currently active on this node.
// Snapshot only — callers must not assume thread-stability.
func (h *Hub) LocalScopes() []scopeKey {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]scopeKey, 0, len(h.rooms))
	for k := range h.rooms {
		out = append(out, k)
	}
	return out
}

// BroadcastToScope sends a message to every client subscribed to
// (scopeType, scopeID). Slow clients are evicted under write lock.
func (h *Hub) BroadcastToScope(scopeType, scopeID string, message []byte) {
	h.BroadcastToScopeDedup(scopeType, scopeID, message, "")
}

// BroadcastToScopeDedup is the same as BroadcastToScope but skips delivery
// to clients that have already seen eventID (used by the Redis relay to
// deduplicate the local fast path of DualWriteBroadcaster).
func (h *Hub) BroadcastToScopeDedup(scopeType, scopeID string, message []byte, eventID string) {
	if scopeType == "" || scopeID == "" {
		return
	}
	key := sk(scopeType, scopeID)

	h.mu.RLock()
	clients := h.rooms[key]
	var slow []*Client
	var sent int64
	for client := range clients {
		if !client.markSeen(eventID) {
			continue
		}
		if client.trySend(message) {
			sent++
		} else if !client.revoked.Load() {
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

// fanoutAll delivers message to every connected client. If excludeWorkspace
// is non-empty, clients whose workspaceID matches are skipped (used by the
// member:added dedup semantics carried over from SendToUser). eventID is the
// dedup key (empty disables dedup).
func (h *Hub) fanoutAll(message []byte, excludeWorkspace string) {
	h.fanoutAllDedup(message, excludeWorkspace, "")
}

func (h *Hub) fanoutAllDedup(message []byte, excludeWorkspace, eventID string) {
	h.mu.RLock()
	var slow []*Client
	var sent int64
	for client := range h.clients {
		if excludeWorkspace != "" && client.workspaceID == excludeWorkspace {
			continue
		}
		if !client.markSeen(eventID) {
			continue
		}
		if client.trySend(message) {
			sent++
		} else if !client.revoked.Load() {
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

// BroadcastToWorkspace is a back-compat shortcut.
func (h *Hub) BroadcastToWorkspace(workspaceID string, message []byte) {
	h.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

// SendToUser delivers a message to every connection belonging to userID,
// skipping any connections whose workspaceID matches excludeWorkspace.
func (h *Hub) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	h.fanoutUser(userID, message, exclude, "")
}

// Broadcast sends a message to every connected client (daemon events).
func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

// fanoutUser delivers a message to all clients in the user scope, optionally
// excluding clients in excludeWorkspace and deduping against eventID.
func (h *Hub) fanoutUser(userID string, message []byte, excludeWorkspace, eventID string) {
	key := sk(ScopeUser, userID)
	h.mu.RLock()
	clients := h.rooms[key]
	var slow []*Client
	var sent int64
	for client := range clients {
		if excludeWorkspace != "" && client.workspaceID == excludeWorkspace {
			continue
		}
		if !client.markSeen(eventID) {
			continue
		}
		if client.trySend(message) {
			sent++
		} else if !client.revoked.Load() {
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()
	if sent > 0 {
		M.MessagesSentTotal.Add(sent)
	}
	if len(slow) > 0 {
		h.evictSlow(slow)
	}
}

// evictSlow removes clients whose send channel was full. Mirrors the
// pre-phase-1 behavior: closes the send channel, decrements counters, fires
// onLastSubscriber for any rooms drained as a side effect.
func (h *Hub) evictSlow(slow []*Client) {
	M.MessagesDroppedTotal.Add(int64(len(slow)))
	M.SlowEvictionsTotal.Add(int64(len(slow)))

	h.mu.Lock()
	evicted := 0
	type emptied struct {
		Type, ID string
	}
	var drainedRooms []emptied
	for _, c := range slow {
		if !h.clients[c] {
			continue
		}
		delete(h.clients, c)
		for key := range c.subscriptions {
			if room, ok := h.rooms[key]; ok {
				delete(room, c)
				if len(room) == 0 {
					delete(h.rooms, key)
					drainedRooms = append(drainedRooms, emptied{key.Type, key.ID})
				}
			}
		}
		c.subscriptions = nil
		c.sendMu.Lock()
		c.sendClosed = true
		close(c.send)
		c.sendMu.Unlock()
		evicted++
	}
	cb := h.onLastSubscriber
	h.mu.Unlock()

	if evicted > 0 {
		M.ActiveConnections.Add(int64(-evicted))
		M.DisconnectsTotal.Add(int64(evicted))
	}
	for _, r := range drainedRooms {
		M.DecRoom(r.Type)
	}
	if cb != nil {
		for _, r := range drainedRooms {
			cb(r.Type, r.ID)
		}
	}
}

// Snapshot returns a JSON-friendly summary of the hub state.
func (h *Hub) Snapshot() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rooms := map[string]int{}
	for key := range h.rooms {
		rooms[key.Type]++
	}
	return map[string]any{
		"connections": len(h.clients),
		"rooms":       rooms,
	}
}

// authenticateToken validates a JWT or PAT string and returns the user ID.
func authenticateToken(tokenStr string, pr PATResolver, ctx context.Context) (string, string) {
	if strings.HasPrefix(tokenStr, "mul_") {
		if pr == nil {
			return "", `{"error":"invalid token"}`
		}
		uid, ok := pr.ResolveToken(ctx, tokenStr)
		if !ok {
			return "", `{"error":"invalid token"}`
		}
		if auth.IsTemporarilyDisabledUserID(uid) {
			return "", `{"error":"account disabled"}`
		}
		return uid, ""
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return "", `{"error":"invalid token"}`
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", `{"error":"invalid claims"}`
	}

	uid, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(uid) == "" {
		return "", `{"error":"invalid claims"}`
	}
	email, _ := claims["email"].(string)
	if auth.IsTemporarilyDisabledUser(uid, email) {
		return "", `{"error":"account disabled"}`
	}
	return uid, ""
}

// firstMessageAuth reads the first WebSocket message expecting an auth payload.
// A non-empty errMsg is for the caller to write back before closing the
// connection. closed=true means the connection is already torn down and the
// caller must return without writing anything further.
func firstMessageAuth(conn *websocket.Conn) (token, errMsg string, closed bool) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	_, raw, err := conn.ReadMessage()
	if err != nil {
		if errors.Is(err, websocket.ErrReadLimit) {
			// gorilla has already replied CloseMessageTooBig (1009), so an
			// auth_error frame here would be data sent after a close frame.
			// Counted separately to keep the breach out of ordinary churn.
			M.InboundTooLargeTotal.Add(1)
			slog.Warn("ws: pre-auth frame exceeded read limit", "limit_bytes", inboundReadLimit)
			conn.Close()
			return "", "", true
		}
		return "", `{"error":"auth timeout or read error"}`, false
	}

	var msg struct {
		Type    string `json:"type"`
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "auth" || msg.Payload.Token == "" {
		return "", `{"error":"expected auth message as first frame"}`, false
	}

	return msg.Payload.Token, "", false
}

type wsMessageWriter interface {
	WriteMessage(messageType int, data []byte) error
}

func writeWSAuthFrame(conn wsMessageWriter, payload []byte, frame string, attrs ...any) bool {
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		logAttrs := append([]any{"frame", frame, "error", err}, attrs...)
		slog.Warn("ws: failed to send auth frame", logAttrs...)
		return false
	}
	return true
}

func writeWSAuthErrorAndClose(conn *websocket.Conn, payload []byte, attrs ...any) {
	writeWSAuthFrame(conn, payload, "auth_error", attrs...)
	conn.Close()
}

// HandleWebSocket upgrades an HTTP connection to WebSocket with cookie or
// first-message auth.
func HandleWebSocket(hub *Hub, mc MembershipChecker, pr PATResolver, resolveSlug SlugResolver, w http.ResponseWriter, r *http.Request) {
	handleWebSocket(hub, mc, pr, resolveSlug, nil, w, r)
}

// HandleWebSocketWithTagMirrorGuard preserves the native/service admission
// path while rejecting mirrored VIBES identities that must use #299 instead.
func HandleWebSocketWithTagMirrorGuard(hub *Hub, mc MembershipChecker, pr PATResolver, resolveSlug SlugResolver, mirrors TagWebSocketMirrorResolver, w http.ResponseWriter, r *http.Request) {
	handleWebSocket(hub, mc, pr, resolveSlug, mirrors, w, r)
}

// HandleTagGatewayWebSocket is the browser-only Tag upgrade. Identity is
// accepted exclusively from a one-use, five-second VIBES Gateway assertion;
// cookies, PATs, URL identity parameters, Host, and Origin never establish it.
func HandleTagGatewayWebSocket(hub *Hub, gate TagWebSocketAccessGate, verifier *tagaccess.HTTPAssertionVerifier, replay tagaccess.HTTPAssertionReplayStore, mirrors TagWebSocketMirrorResolver, w http.ResponseWriter, r *http.Request) {
	if hub == nil || gate == nil || verifier == nil || replay == nil || mirrors == nil {
		http.Error(w, `{"error":"Tag WebSocket authentication unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	assertion, err := verifier.VerifyRequest(r)
	if err != nil {
		http.Error(w, `{"error":"Tag Gateway assertion rejected"}`, http.StatusUnauthorized)
		return
	}
	multicaUserID, found, err := mirrors.MulticaUserID(r.Context(), assertion.UserID)
	if err != nil {
		http.Error(w, `{"error":"Tag WebSocket authentication unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if !found {
		http.Error(w, `{"error":"Tag access denied"}`, http.StatusForbidden)
		return
	}
	multicaWorkspaceID, found, err := mirrors.MulticaWorkspaceID(r.Context(), assertion.WorkspaceID)
	if err != nil {
		http.Error(w, `{"error":"Tag WebSocket authentication unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if !found {
		http.Error(w, `{"error":"Tag access denied"}`, http.StatusForbidden)
		return
	}
	consumed, err := replay.Consume(r.Context(), tagaccess.HTTPAssertionReplay{
		Issuer: assertion.Issuer, Audience: assertion.Audience, RequestID: assertion.RequestID,
		Nonce: assertion.Nonce, ExpiresAt: time.UnixMilli(assertion.ExpiresAt),
	})
	if err != nil {
		http.Error(w, `{"error":"Tag WebSocket authentication unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if !consumed {
		http.Error(w, `{"error":"Tag Gateway assertion rejected"}`, http.StatusUnauthorized)
		return
	}
	tagSessionID := tagaccess.BrowserTagSessionID(assertion.UserID, assertion.SessionID)
	expiresAt := time.UnixMilli(assertion.ExpiresAt)
	if err := gate.GrantSession(r.Context(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: assertion.SessionID,
		VIBESUserID: assertion.UserID, WorkspaceID: assertion.WorkspaceID,
		AccountEpoch: assertion.AccountEpoch, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		MembershipGeneration: assertion.MembershipGeneration, AuthorityVersion: assertion.AuthorityVersion,
		SessionExpiresAt: expiresAt, GrantExpiresAt: expiresAt, Continuous: true,
	}); err != nil {
		if errors.Is(err, tagaccess.ErrGrantDenied) || errors.Is(err, tagaccess.ErrInvalidGrant) {
			http.Error(w, `{"error":"Tag access denied"}`, http.StatusForbidden)
		} else {
			http.Error(w, `{"error":"Tag WebSocket authentication unavailable"}`, http.StatusServiceUnavailable)
		}
		return
	}
	request := tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: assertion.SessionID,
		VIBESUserID: assertion.UserID, WorkspaceID: assertion.WorkspaceID,
		AccountEpoch: assertion.AccountEpoch, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		AuthorityVersion: assertion.AuthorityVersion, MembershipGeneration: assertion.MembershipGeneration,
	}
	if !hub.revocationFeedHealthy(r.Context()) {
		http.Error(w, `{"error":"authorization feed unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	decision := authorizeTagWebSocketAccess(r.Context(), gate, request)
	if !decision.Allowed {
		http.Error(w, `{"error":"Tag access denied"}`, http.StatusForbidden)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("Tag Gateway websocket upgrade failed", "error", err)
		return
	}
	conn.SetReadLimit(inboundReadLimit)

	// Serialize the last authority read through registry insertion with the
	// same lock used by targeted close dispatch. A revoke can therefore happen
	// entirely before this socket registers or entirely after it is visible.
	hub.authorize.RLock()
	decision = authorizeTagWebSocketAccess(context.Background(), gate, request)
	if !decision.Allowed || !hub.authorizationLeaseValidLocked() {
		hub.authorize.RUnlock()
		_ = conn.Close()
		return
	}
	metadata := ConnectionMetadata{
		ConnectionID: ulid.Make().String(), InstanceID: hub.InstanceID(), VIBESUserID: assertion.UserID,
		WorkspaceID: assertion.WorkspaceID, TagSessionID: tagSessionID, VIBESSessionID: assertion.SessionID,
		AccountEpoch: assertion.AccountEpoch, SessionWorkspaceGeneration: assertion.SessionWorkspaceGeneration,
		MembershipGeneration: assertion.MembershipGeneration, AuthorityVersion: assertion.AuthorityVersion,
	}
	client := &Client{
		hub: hub, conn: conn, send: make(chan []byte, 256), userID: multicaUserID,
		workspaceID: multicaWorkspaceID, metadata: metadata,
	}
	if !hub.registerClient(client) {
		hub.authorize.RUnlock()
		_ = conn.Close()
		return
	}
	hub.authorize.RUnlock()

	slog.Info("Tag Gateway websocket connected", "user_id", multicaUserID, "workspace_id", multicaWorkspaceID)
	go client.writePump()
	go client.readPump()
	go client.watchWebSocketAccess(gate, request)
}

func handleWebSocket(hub *Hub, mc MembershipChecker, pr PATResolver, resolveSlug SlugResolver, mirrors TagWebSocketMirrorResolver, w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		if slug := r.URL.Query().Get("workspace_slug"); slug != "" && resolveSlug != nil {
			resolved, err := resolveSlug(r.Context(), slug)
			if err != nil {
				http.Error(w, `{"error":"workspace not found"}`, http.StatusNotFound)
				return
			}
			workspaceID = resolved
		}
	}
	if workspaceID == "" {
		http.Error(w, `{"error":"workspace_id or workspace_slug required"}`, http.StatusBadRequest)
		return
	}
	var userID string
	needsAuthAck := false
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		uid, errMsg := authenticateToken(cookie.Value, pr, r.Context())
		if errMsg != "" {
			status := http.StatusUnauthorized
			if errMsg == `{"error":"account disabled"}` {
				status = http.StatusForbidden
			}
			http.Error(w, errMsg, status)
			return
		}
		if mirrored, unavailable := mirroredTagIdentity(r.Context(), mirrors, uid); mirrored || unavailable {
			status := http.StatusUnauthorized
			message := `{"error":"VIBES Gateway assertion required"}`
			if unavailable {
				status = http.StatusServiceUnavailable
				message = `{"error":"Tag authority unavailable"}`
			}
			http.Error(w, message, status)
			return
		}
		if mc == nil || !mc.IsMember(r.Context(), uid, workspaceID) {
			http.Error(w, `{"error":"not a member of this workspace"}`, http.StatusForbidden)
			return
		}
		userID = uid
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	// Bound inbound messages here rather than in readPump: the token auth
	// path below reads its first frame before the caller is authenticated, so
	// a limit installed any later leaves that read unbounded.
	conn.SetReadLimit(inboundReadLimit)

	if userID == "" {
		tokenStr, errMsg, closed := firstMessageAuth(conn)
		if closed {
			return
		}
		if errMsg != "" {
			writeWSAuthErrorAndClose(conn, []byte(errMsg), "workspace_id", workspaceID)
			return
		}
		uid, errMsg := authenticateToken(tokenStr, pr, r.Context())
		if errMsg != "" {
			writeWSAuthErrorAndClose(conn, []byte(errMsg), "workspace_id", workspaceID)
			return
		}
		if mirrored, unavailable := mirroredTagIdentity(r.Context(), mirrors, uid); mirrored || unavailable {
			message := `{"error":"VIBES Gateway assertion required"}`
			if unavailable {
				message = `{"error":"Tag authority unavailable"}`
			}
			writeWSAuthErrorAndClose(conn, []byte(message), "workspace_id", workspaceID, "user_id", uid)
			return
		}
		if mc == nil || !mc.IsMember(r.Context(), uid, workspaceID) {
			writeWSAuthErrorAndClose(
				conn,
				[]byte(`{"error":"not a member of this workspace"}`),
				"workspace_id", workspaceID,
				"user_id", uid,
			)
			return
		}
		userID = uid

		needsAuthAck = true
	}

	// Capture client metadata from query params (browsers cannot set custom
	// headers on WebSocket upgrades, so the WSClient passes them via the URL).
	// Logged with every connect so the same observability dimensions exist
	// for WS as for HTTP.
	clientPlatform := r.URL.Query().Get("client_platform")
	clientVersion := r.URL.Query().Get("client_version")
	clientOS := r.URL.Query().Get("client_os")
	slog.Info("websocket connected",
		"user_id", userID,
		"workspace_id", workspaceID,
		"client_platform", clientPlatform,
		"client_version", clientVersion,
		"client_os", clientOS,
	)

	instanceID := hub.InstanceID()
	metadata := ConnectionMetadata{
		ConnectionID: ulid.Make().String(), InstanceID: instanceID,
	}
	client := &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, 256),
		userID:      userID,
		workspaceID: workspaceID,
		metadata:    metadata,
	}
	if !hub.registerClient(client) {
		_ = conn.Close()
		return
	}
	if needsAuthAck {
		client.writeMu.Lock()
		if client.revoked.Load() {
			client.writeMu.Unlock()
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(client.writeTimeout()))
		acknowledged := writeWSAuthFrame(
			conn, []byte(`{"type":"auth_ack"}`), "auth_ack", "workspace_id", workspaceID, "user_id", userID,
		)
		client.writeMu.Unlock()
		if !acknowledged {
			hub.removeClient(client)
			_ = conn.Close()
			return
		}
	}

	go client.writePump()
	go client.readPump()
}

func mirroredTagIdentity(ctx context.Context, mirrors TagWebSocketMirrorResolver, multicaUserID string) (mirrored, unavailable bool) {
	if mirrors == nil {
		return false, false
	}
	_, mirrored, err := mirrors.VIBESUserID(ctx, multicaUserID)
	return mirrored, err != nil
}

func authorizeTagWebSocketAccess(parent context.Context, gate TagWebSocketAccessGate, request tagaccess.AccessRequest) tagaccess.Decision {
	ctx, cancel := context.WithTimeout(parent, AuthorizationWatchdogTarget/2)
	defer cancel()
	return gate.Authorize(ctx, request)
}

const (
	accessWatchdogPeriod       = 250 * time.Millisecond
	accessWatchdogQueryTimeout = AuthorizationWatchdogTarget - accessWatchdogPeriod
)

func (c *Client) watchWebSocketAccess(gate TagWebSocketAccessGate, request tagaccess.AccessRequest) {
	ticker := time.NewTicker(accessWatchdogPeriod)
	defer ticker.Stop()
	for range ticker.C {
		if c.revoked.Load() || !c.hub.hasClient(c) {
			return
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), accessWatchdogQueryTimeout)
		if !c.hub.revocationFeedHealthy(checkCtx) {
			cancel()
			c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
			return
		}
		decision := gate.Authorize(checkCtx, request)
		cancel()
		if !decision.Allowed || decision.AuthorityVersion != c.metadata.AuthorityVersion {
			c.hub.revokeClient(c, CloseCodeAuthorizationChanged, "fresh_authorization_required")
			return
		}
	}
}

func (h *Hub) hasClient(client *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[client]
}

func (h *Hub) revocationFeedHealthy(ctx context.Context) bool {
	h.mu.RLock()
	healthy := h.revocationHealthy
	h.mu.RUnlock()
	return healthy == nil || healthy(ctx)
}

func (h *Hub) authorizationLeaseValid() bool {
	lease := h.revocationLease.Load()
	return lease == nil || lease.valid()
}

func (h *Hub) authorizationLeaseValidLocked() bool {
	return h.authorizationLeaseValid()
}

func (c *Client) requiresAuthorizationFeed() bool {
	return c != nil && c.metadata.VIBESSessionID != ""
}

func (c *Client) authorizationLeaseValid() bool {
	return c == nil || !c.requiresAuthorizationFeed() || c.hub == nil || c.hub.authorizationLeaseValid()
}

func (h *Hub) revokeClient(client *Client, code int, reason string) bool {
	if client == nil || !client.revoked.CompareAndSwap(false, true) {
		return false
	}
	client.frameMu.Lock()
	client.frameMu.Unlock()
	h.removeClient(client)
	client.closeTransport(code, reason)
	return true
}

// inboundFrame describes the subset of inbound JSON messages the server
// understands today.
type inboundFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type subPayload struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		if !c.revoked.Load() {
			c.conn.Close()
		}
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			switch {
			case errors.Is(err, websocket.ErrReadLimit):
				// Counted separately so an over-limit close stays visible
				// instead of blending into ordinary connection churn.
				M.InboundTooLargeTotal.Add(1)
				slog.Warn("ws: inbound frame exceeded read limit",
					"limit_bytes", inboundReadLimit,
					"user_id", c.userID,
					"workspace_id", c.workspaceID,
				)
			case websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure):
				slog.Debug("websocket read error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			}
			break
		}
		if !c.authorizationLeaseValid() {
			c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
			break
		}
		if !c.processInboundFrame(raw) {
			break
		}
	}
}

func (c *Client) processInboundFrame(raw []byte) bool {
	c.frameMu.Lock()
	if c.revoked.Load() || !c.authorizationLeaseValid() {
		c.frameMu.Unlock()
		c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
		return false
	}
	c.handleFrame(raw)
	c.frameMu.Unlock()
	return true
}

func (c *Client) handleFrame(raw []byte) {
	if c.revoked.Load() {
		return
	}
	var f inboundFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		slog.Debug("ws inbound: invalid json", "error", err, "user_id", c.userID)
		return
	}
	switch f.Type {
	case "subscribe", "unsubscribe":
		var p subPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil || p.Scope == "" || p.ID == "" {
			c.sendJSON(map[string]any{
				"type": f.Type + "_error",
				"payload": map[string]string{
					"scope": p.Scope,
					"id":    p.ID,
					"error": "invalid payload",
				},
			})
			return
		}
		if f.Type == "subscribe" {
			c.handleSubscribe(p.Scope, p.ID)
		} else {
			c.handleUnsubscribe(p.Scope, p.ID)
		}
	case "ping":
		c.sendJSON(map[string]string{"type": "pong"})
	default:
		// Unknown frame — ignore silently for forward compat.
		slog.Debug("ws inbound: unknown frame", "type", f.Type, "user_id", c.userID)
	}
}

func (c *Client) handleSubscribe(scope, id string) {
	switch scope {
	case ScopeWorkspace, ScopeUser:
		// Implicit scopes — only allowed if it matches the connection identity.
		if (scope == ScopeWorkspace && id != c.workspaceID) || (scope == ScopeUser && id != c.userID) {
			M.SubscribeDeniedTotal(scope).Add(1)
			c.sendJSON(map[string]any{
				"type": "subscribe_error",
				"payload": map[string]string{
					"scope": scope,
					"id":    id,
					"error": "forbidden",
				},
			})
			return
		}
		// Already auto-subscribed at connect time; reply ack idempotently.
		c.hub.subscribe(c, scope, id)
	case ScopeTask, ScopeChat:
		auth := c.hub.authorizer
		if auth != nil {
			authorizeCtx, cancel := context.WithTimeout(context.Background(), AuthorizationWatchdogTarget/2)
			ok, err := auth.AuthorizeScope(authorizeCtx, c.userID, c.workspaceID, scope, id)
			cancel()
			if err != nil || !ok {
				M.SubscribeDeniedTotal(scope).Add(1)
				reason := "forbidden"
				if err != nil {
					reason = "lookup_failed"
				}
				c.sendJSON(map[string]any{
					"type": "subscribe_error",
					"payload": map[string]string{
						"scope": scope,
						"id":    id,
						"error": reason,
					},
				})
				return
			}
		}
		if c.revoked.Load() || !c.authorizationLeaseValid() {
			return
		}
		c.hub.subscribe(c, scope, id)
	default:
		M.SubscribeDeniedTotal(scope).Add(1)
		c.sendJSON(map[string]any{
			"type": "subscribe_error",
			"payload": map[string]string{
				"scope": scope,
				"id":    id,
				"error": "unknown_scope",
			},
		})
		return
	}
	c.sendJSON(map[string]any{
		"type":    "subscribe_ack",
		"payload": map[string]string{"scope": scope, "id": id},
	})
}

func (c *Client) handleUnsubscribe(scope, id string) {
	c.hub.unsubscribe(c, scope, id)
	c.sendJSON(map[string]any{
		"type":    "unsubscribe_ack",
		"payload": map[string]string{"scope": scope, "id": id},
	})
}

// sendJSON best-effort encodes v and pushes it to the client's send channel.
// Drops the message if the channel is full (the writePump will be evicted by
// the next BroadcastToScope cycle).
func (c *Client) sendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = c.trySend(data)
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if !c.revoked.Load() {
			c.conn.Close()
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !c.authorizationLeaseValid() {
				c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
				return
			}
			if c.revoked.Load() {
				return
			}
			if !ok {
				c.writeMu.Lock()
				if !c.revoked.Load() {
					_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				}
				c.writeMu.Unlock()
				return
			}
			c.writeMu.Lock()
			if c.revoked.Load() || !c.authorizationLeaseValid() {
				c.writeMu.Unlock()
				c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout()))
			err := c.conn.WriteMessage(websocket.TextMessage, message)
			c.writeMu.Unlock()
			if err != nil {
				slog.Warn("websocket write error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
				return
			}
		case <-ticker.C:
			if !c.authorizationLeaseValid() {
				c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
				return
			}
			if c.revoked.Load() {
				return
			}
			c.writeMu.Lock()
			if c.revoked.Load() || !c.authorizationLeaseValid() {
				c.writeMu.Unlock()
				c.hub.revokeClient(c, CloseCodeAuthorizationFeedLost, "authorization_feed_unavailable")
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout()))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
