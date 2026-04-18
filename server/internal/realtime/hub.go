package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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

// SlugResolver translates a workspace slug to its UUID. Used by HandleWebSocket
// to accept slug-based identification from the frontend.
type SlugResolver func(ctx context.Context, slug string) (workspaceID string, err error)

// PATResolver resolves a Personal Access Token to a user ID.
// Returns the user ID and true if the token is valid, or ("", false) otherwise.
type PATResolver interface {
	ResolveToken(ctx context.Context, token string) (userID string, ok bool)
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

// SetAllowedOrigins overrides the WebSocket origin whitelist (called from router setup).
func SetAllowedOrigins(origins []string) {
	allowedWSOrigins.Store(origins)
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
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
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	// Connections that miss a pong within this window are considered dead and
	// are closed, freeing goroutines and channel memory.
	pongWait = 60 * time.Second

	// pingPeriod is how often the server sends a ping to keep the connection
	// alive through intermediate proxies and load balancers. Must be less than
	// pongWait so that a missing pong is detected before the next ping is due.
	pingPeriod = (pongWait * 9) / 10
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

// Client represents a single WebSocket connection with identity.
type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	userID      string
	workspaceID string
}

// Hub manages WebSocket connections organized by workspace rooms.
type Hub struct {
	rooms      map[string]map[*Client]bool // workspaceID -> clients
	broadcast  chan []byte                  // global broadcast (daemon events)
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub event loop.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			room := client.workspaceID
			if h.rooms[room] == nil {
				h.rooms[room] = make(map[*Client]bool)
			}
			h.rooms[room][client] = true
			total := 0
			for _, r := range h.rooms {
				total += len(r)
			}
			h.mu.Unlock()
			slog.Info("ws client connected", "workspace_id", room, "total_clients", total)

		case client := <-h.unregister:
			h.mu.Lock()
			room := client.workspaceID
			if clients, ok := h.rooms[room]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.send)
					if len(clients) == 0 {
						delete(h.rooms, room)
					}
				}
			}
			total := 0
			for _, r := range h.rooms {
				total += len(r)
			}
			h.mu.Unlock()
			slog.Info("ws client disconnected", "workspace_id", room, "total_clients", total)

		case message := <-h.broadcast:
			// Global broadcast for daemon events (no workspace filtering)
			h.mu.RLock()
			var slow []*Client
			for _, clients := range h.rooms {
				for client := range clients {
					select {
					case client.send <- message:
					default:
						slow = append(slow, client)
					}
				}
			}
			h.mu.RUnlock()
			if len(slow) > 0 {
				h.mu.Lock()
				for _, client := range slow {
					room := client.workspaceID
					if clients, ok := h.rooms[room]; ok {
						if _, exists := clients[client]; exists {
							delete(clients, client)
							close(client.send)
							if len(clients) == 0 {
								delete(h.rooms, room)
							}
						}
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// BroadcastToWorkspace sends a message only to clients in the given workspace.
func (h *Hub) BroadcastToWorkspace(workspaceID string, message []byte) {
	h.mu.RLock()
	clients := h.rooms[workspaceID]
	var slow []*Client
	for client := range clients {
		select {
		case client.send <- message:
		default:
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	// Remove slow clients under write lock
	if len(slow) > 0 {
		h.mu.Lock()
		for _, client := range slow {
			if room, ok := h.rooms[workspaceID]; ok {
				if _, exists := room[client]; exists {
					delete(room, client)
					close(client.send)
					if len(room) == 0 {
						delete(h.rooms, workspaceID)
					}
				}
			}
		}
		h.mu.Unlock()
	}
}

// SendToUser sends a message to all connections belonging to a specific user,
// regardless of which workspace room they are in. Connections in excludeWorkspace
// are skipped (they already receive the message via BroadcastToWorkspace).
func (h *Hub) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}

	h.mu.RLock()
	type target struct {
		client      *Client
		workspaceID string
	}
	var targets []target
	for wsID, clients := range h.rooms {
		if wsID == exclude {
			continue
		}
		for client := range clients {
			if client.userID == userID {
				targets = append(targets, target{client, wsID})
			}
		}
	}
	h.mu.RUnlock()

	var slow []target
	for _, t := range targets {
		select {
		case t.client.send <- message:
		default:
			slow = append(slow, t)
		}
	}

	// Remove slow clients under write lock (same pattern as BroadcastToWorkspace)
	if len(slow) > 0 {
		h.mu.Lock()
		for _, t := range slow {
			if room, ok := h.rooms[t.workspaceID]; ok {
				if _, exists := room[t.client]; exists {
					delete(room, t.client)
					close(t.client.send)
					if len(room) == 0 {
						delete(h.rooms, t.workspaceID)
					}
				}
			}
		}
		h.mu.Unlock()
	}
}

// Broadcast sends a message to all connected clients (used for daemon events).
func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
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
// Message format: {"type":"auth","payload":{"token":"..."}}
// Returns the token string or an error description.
func firstMessageAuth(conn *websocket.Conn) (string, string) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{}) // clear deadline for subsequent reads

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

// HandleWebSocket upgrades an HTTP connection to WebSocket with cookie or first-message auth.
// resolveSlug may be nil if slug-based identification is not needed (e.g. in tests).
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

	// Try cookie auth first (web clients).
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

	// Upgrade the connection. Clients without cookies (desktop) will authenticate
	// via the first WebSocket message, so we must upgrade before we have a token.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	// First-message auth for non-cookie clients (desktop, CLI).
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

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	// Require a pong within pongWait of each ping. The deadline is refreshed
	// every time a pong frame arrives, so a healthy connection stays open
	// indefinitely. A dead connection (no pong) is detected within pongWait.
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("websocket read error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			}
			break
		}
		// TODO: Route inbound messages to appropriate handlers
		slog.Debug("ws message received", "user_id", c.userID, "workspace_id", c.workspaceID)
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
				// Hub closed the channel (slow-client eviction or shutdown).
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
