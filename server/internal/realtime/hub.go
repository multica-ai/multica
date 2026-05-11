package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
)

// MembershipChecker verifies a user belongs to a workspace.
type MembershipChecker interface {
	IsMember(ctx context.Context, userID, workspaceID string) bool
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

func init() {
	allowedWSOrigins.Store(loadAllowedOrigins())
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

// SetAllowedOrigins overrides the WebSocket origin whitelist.
func SetAllowedOrigins(origins []string) {
	allowedWSOrigins.Store(origins)
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
	origins := allowedWSOrigins.Load().([]string)
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	slog.Warn("ws: rejected origin", "origin", origin)
	return false
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
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

// Hub manages WebSocket connections organized into scope-based rooms.
type Hub struct {
	rooms      map[scopeKey]map[*Client]bool
	clients    map[*Client]bool // every connected client (used by global Broadcast and snapshots)
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex

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
		register:   make(chan *Client),
		unregister: make(chan *Client),
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
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			total := len(h.clients)
			h.mu.Unlock()
			M.ConnectsTotal.Add(1)
			M.ActiveConnections.Add(1)
			// Auto-subscribe to the workspace and user scopes.
			h.subscribe(client, ScopeWorkspace, client.workspaceID)
			if client.userID != "" {
				h.subscribe(client, ScopeUser, client.userID)
			}
			slog.Info("ws client connected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)

		case client := <-h.unregister:
			h.removeClient(client)

		case message := <-h.broadcast:
			h.fanoutAll(message, "")
		}
	}
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
	close(client.send)
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
	if !h.clients[client] {
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
		select {
		case client.send <- message:
			sent++
		default:
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
		select {
		case client.send <- message:
			sent++
		default:
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
		select {
		case client.send <- message:
			sent++
		default:
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
		close(c.send)
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
	return uid, ""
}

// firstMessageAuth reads the first WebSocket message expecting an auth payload.
func firstMessageAuth(conn *websocket.Conn) (string, string) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	_, raw, err := conn.ReadMessage()
	if err != nil {
		return "", `{"error":"auth timeout or read error"}`
	}

	var msg struct {
		Type    string `json:"type"`
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "auth" || msg.Payload.Token == "" {
		return "", `{"error":"expected auth message as first frame"}`
	}

	return msg.Payload.Token, ""
}

// HandleWebSocket upgrades an HTTP connection to WebSocket with cookie or
// first-message auth.
func HandleWebSocket(hub *Hub, mc MembershipChecker, pr PATResolver, resolveSlug SlugResolver, w http.ResponseWriter, r *http.Request) {
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
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		uid, errMsg := authenticateToken(cookie.Value, pr, r.Context())
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusUnauthorized)
			return
		}
		if !mc.IsMember(r.Context(), uid, workspaceID) {
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

	if userID == "" {
		tokenStr, errMsg := firstMessageAuth(conn)
		if errMsg != "" {
			conn.WriteMessage(websocket.TextMessage, []byte(errMsg))
			conn.Close()
			return
		}
		uid, errMsg := authenticateToken(tokenStr, pr, r.Context())
		if errMsg != "" {
			conn.WriteMessage(websocket.TextMessage, []byte(errMsg))
			conn.Close()
			return
		}
		if !mc.IsMember(r.Context(), uid, workspaceID) {
			conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"not a member of this workspace"}`))
			conn.Close()
			return
		}
		userID = uid

		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth_ack"}`))
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

	client := &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, 256),
		userID:      userID,
		workspaceID: workspaceID,
	}
	hub.register <- client

	go client.writePump()
	go client.readPump()
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
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("websocket read error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			}
			break
		}
		c.handleFrame(raw)
	}
}

func (c *Client) handleFrame(raw []byte) {
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
			ok, err := auth.AuthorizeScope(context.Background(), c.userID, c.workspaceID, scope, id)
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
	select {
	case c.send <- data:
	default:
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Warn("websocket write error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
