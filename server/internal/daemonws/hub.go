package daemonws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// ClientIdentity captures the already-authenticated daemon connection scope.
type ClientIdentity struct {
	DaemonID string
	UserID   string
	// WorkspaceID is the legacy single-workspace scope used by older callers
	// and daemon-token auth. New code should populate WorkspaceIDs from the
	// runtime rows authorized for this connection.
	WorkspaceID   string
	WorkspaceIDs  []string
	RuntimeIDs    []string
	ClientVersion string
	// Capabilities is the raw X-Client-Capabilities header captured at connect,
	// so RPC handlers can honor the same capability gating as the HTTP path.
	Capabilities string
}

// AuthorizedWorkspaceIDs returns the connection's workspace scope in stable
// order, preferring the multi-workspace field and falling back to WorkspaceID
// for older tests/callers.
func (i ClientIdentity) AuthorizedWorkspaceIDs() []string {
	seen := make(map[string]struct{}, len(i.WorkspaceIDs)+1)
	out := make([]string, 0, len(i.WorkspaceIDs)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range i.WorkspaceIDs {
		add(id)
	}
	if len(out) == 0 {
		add(i.WorkspaceID)
	}
	return out
}

func (i ClientIdentity) PrimaryWorkspaceID() string {
	ids := i.AuthorizedWorkspaceIDs()
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// AllowsWorkspace reports whether workspaceID is within the connection scope.
// An empty scope remains permissive for legacy unit tests that construct
// ClientIdentity directly without workspace data.
func (i ClientIdentity) AllowsWorkspace(workspaceID string) bool {
	ids := i.AuthorizedWorkspaceIDs()
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if id == workspaceID {
			return true
		}
	}
	return false
}

type client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	identity ClientIdentity
	runtimes map[string]struct{}

	// ctx is cancelled when the connection tears down, so async RPC handlers
	// stop instead of running against a dead socket. cancel is invoked from
	// readPump's defer.
	ctx    context.Context
	cancel context.CancelFunc

	// sendMu guards sendClosed so a late async send (e.g. an RPC response
	// goroutine) can never write to the closed send channel. teardown flips
	// sendClosed under sendMu before closing send.
	sendMu     sync.Mutex
	sendClosed bool

	dedupMu  sync.Mutex
	seenIDs  map[string]struct{}
	seenList []string

	// rpcSem bounds concurrent RPC handlers for this connection.
	rpcSem chan struct{}
}

// trySend delivers frame to the write pump without blocking and without ever
// writing to a closed channel (safe against concurrent teardown). Returns false
// when the buffer is full or the connection is closing.
func (c *client) trySend(frame []byte) bool {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return false
	}
	select {
	case c.send <- frame:
		return true
	default:
		return false
	}
}

const eventDedupCapacity = 128

// markSeen records eventID as already delivered to this client. Empty event IDs
// disable dedup and are always delivered.
func (c *client) markSeen(eventID string) bool {
	if eventID == "" {
		return true
	}
	c.dedupMu.Lock()
	defer c.dedupMu.Unlock()
	if c.seenIDs == nil {
		c.seenIDs = make(map[string]struct{}, eventDedupCapacity)
	}
	if _, ok := c.seenIDs[eventID]; ok {
		return false
	}
	c.seenIDs[eventID] = struct{}{}
	c.seenList = append(c.seenList, eventID)
	if len(c.seenList) > eventDedupCapacity {
		drop := c.seenList[0]
		c.seenList = c.seenList[1:]
		delete(c.seenIDs, drop)
	}
	return true
}

// HeartbeatHandler processes a daemon:heartbeat frame. It must verify that
// runtimeID is one of identity.RuntimeIDs (the connection's authenticated
// scope) and return the ack payload to send back. Returning an error skips
// the ack and is logged at debug level.
type HeartbeatHandler func(ctx context.Context, identity ClientIdentity, runtimeID string, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, error)

// RPCHandler processes a generic daemon:rpc_request (MUL-4257). It dispatches
// on method (e.g. "tasks.claim"), scoping work to identity (DaemonID +
// authenticated RuntimeIDs), and returns an HTTP-style status plus a response
// body OR an error. A returned error is surfaced to the daemon as a non-2xx
// RPC response so it can fall back to HTTP. The handler runs in its own
// goroutine, so it must not assume it owns the read pump.
type RPCHandler func(ctx context.Context, identity ClientIdentity, method string, body json.RawMessage) (status int, respBody json.RawMessage, err error)

// maxInFlightRPCPerClient bounds concurrent RPC handlers per connection so a
// single daemon cannot fan out unbounded goroutines / DB work over one socket.
const maxInFlightRPCPerClient = 8

// MessageKindRecorder is the optional metric hook called once per inbound
// daemon WebSocket frame. kind is the protocol message type with the
// "daemon:" prefix stripped (e.g. "heartbeat") or the literal "unknown" for
// types we don't model. A nil recorder is safely no-op'd.
type MessageKindRecorder interface {
	RecordDaemonWSMessageReceived(kind string)
}

// Hub keeps daemon WebSocket connections indexed by runtime ID. Messages are
// best-effort wakeup hints; the daemon still uses HTTP claim for correctness.
type Hub struct {
	upgrader websocket.Upgrader

	mu          sync.RWMutex
	clients     map[*client]bool
	byRuntime   map[string]map[*client]bool
	byWorkspace map[string]map[*client]bool
	byUser      map[string]map[*client]bool

	hbMu        sync.RWMutex
	onHeartbeat HeartbeatHandler

	rpcMu sync.RWMutex
	onRPC RPCHandler

	kindMu       sync.RWMutex
	kindRecorder MessageKindRecorder
}

func NewHub() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			// Daemon clients authenticate with Authorization headers before the
			// upgrade. Browsers cannot set those headers through the native WS API,
			// and DaemonAuth does not accept cookies, so cookie-based CSWSH does
			// not apply to this endpoint. Re-evaluate this if DaemonAuth ever
			// grows cookie fallback.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients:     make(map[*client]bool),
		byRuntime:   make(map[string]map[*client]bool),
		byWorkspace: make(map[string]map[*client]bool),
		byUser:      make(map[string]map[*client]bool),
	}
}

// SetHeartbeatHandler installs the callback used for daemon:heartbeat frames.
// Wiring is done after handler construction because the handler depends on
// DB queries that aren't available when the hub is built. A nil handler
// disables WS heartbeat processing — daemons fall back to HTTP heartbeat
// transparently because their fallback timer fires whenever no ack arrives.
func (h *Hub) SetHeartbeatHandler(fn HeartbeatHandler) {
	if h == nil {
		return
	}
	h.hbMu.Lock()
	h.onHeartbeat = fn
	h.hbMu.Unlock()
}

func (h *Hub) heartbeatHandler() HeartbeatHandler {
	h.hbMu.RLock()
	defer h.hbMu.RUnlock()
	return h.onHeartbeat
}

// SetRPCHandler installs the callback used for daemon:rpc_request frames
// (MUL-4257). Like SetHeartbeatHandler it is wired after handler construction.
// A nil handler disables WS RPC — daemons fall back to the HTTP claim endpoint.
func (h *Hub) SetRPCHandler(fn RPCHandler) {
	if h == nil {
		return
	}
	h.rpcMu.Lock()
	h.onRPC = fn
	h.rpcMu.Unlock()
}

func (h *Hub) rpcHandler() RPCHandler {
	h.rpcMu.RLock()
	defer h.rpcMu.RUnlock()
	return h.onRPC
}

// SetMessageKindRecorder installs an optional callback fired exactly once per
// inbound daemon WebSocket frame. Used by the metrics layer to count traffic
// by handler kind without hard-coupling the hub to any specific collector.
func (h *Hub) SetMessageKindRecorder(rec MessageKindRecorder) {
	if h == nil {
		return
	}
	h.kindMu.Lock()
	h.kindRecorder = rec
	h.kindMu.Unlock()
}

func (h *Hub) messageKindRecorder() MessageKindRecorder {
	if h == nil {
		return nil
	}
	h.kindMu.RLock()
	defer h.kindMu.RUnlock()
	return h.kindRecorder
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, identity ClientIdentity) {
	if len(identity.RuntimeIDs) == 0 && identity.UserID == "" {
		http.Error(w, `{"error":"runtime_ids or user identity required"}`, http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("daemon websocket upgrade failed", "error", err)
		return
	}

	runtimes := make(map[string]struct{}, len(identity.RuntimeIDs))
	for _, runtimeID := range identity.RuntimeIDs {
		if runtimeID != "" {
			runtimes[runtimeID] = struct{}{}
		}
	}
	c := &client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 16),
		identity: identity,
		runtimes: runtimes,
		rpcSem:   make(chan struct{}, maxInFlightRPCPerClient),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	h.register(c)

	go c.writePump()
	go c.readPump()
}

// NotifyTaskAvailable sends a best-effort wakeup to daemons watching runtimeID.
func (h *Hub) NotifyTaskAvailable(runtimeID, taskID string) {
	h.notifyTaskAvailable(runtimeID, taskID, "")
}

// NotifyRuntimeProfilesChanged asks connected daemons in workspaceID to pull
// runtime profiles after a create, update, disable, or delete.
func (h *Hub) NotifyRuntimeProfilesChanged(workspaceID, profileID string) {
	h.notifyRuntimeProfilesChanged(workspaceID, profileID, "")
}

// NotifyWorkspacesChanged asks every connected daemon authenticated as userID
// to reconcile its workspace membership set.
func (h *Hub) NotifyWorkspacesChanged(userID string) {
	h.notifyWorkspacesChanged(userID, "")
}

func (h *Hub) notifyTaskAvailable(runtimeID, taskID, eventID string) {
	if h == nil || runtimeID == "" {
		return
	}
	data, err := taskAvailableFrame(runtimeID, taskID)
	if err != nil {
		return
	}
	delivered, deduped := h.notifyFrame(runtimeID, data, eventID)
	if delivered {
		M.WakeupDeliveredHit.Add(1)
	} else if !deduped {
		M.WakeupDeliveredMiss.Add(1)
	}
}

func (h *Hub) notifyRuntimeProfilesChanged(workspaceID, profileID, eventID string) {
	if h == nil || workspaceID == "" {
		return
	}
	data, err := runtimeProfilesChangedFrame(workspaceID, profileID)
	if err != nil {
		return
	}
	h.notifyWorkspaceFrame(workspaceID, data, eventID)
}

func (h *Hub) notifyWorkspacesChanged(userID, eventID string) {
	if h == nil || userID == "" {
		return
	}
	data, err := workspacesChangedFrame()
	if err != nil {
		return
	}
	h.notifyUserFrame(userID, data, eventID)
}

func (h *Hub) DeliverDaemonRuntime(scopeID string, frame []byte, eventID string) {
	if h == nil {
		return
	}
	M.WakeupReceivedTotal.Add(1)
	var msg protocol.Message
	if err := json.Unmarshal(frame, &msg); err != nil {
		slog.Debug("daemon websocket relay: invalid frame", "error", err, "scope_id", scopeID, "event_id", eventID)
		M.WakeupDeliveredMiss.Add(1)
		return
	}
	switch msg.Type {
	case protocol.EventDaemonTaskAvailable:
		var payload protocol.TaskAvailablePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.RuntimeID == "" {
			slog.Debug("daemon websocket relay: invalid task_available payload", "error", err, "scope_id", scopeID, "event_id", eventID)
			M.WakeupDeliveredMiss.Add(1)
			return
		}
		delivered, deduped := h.notifyFrame(payload.RuntimeID, frame, eventID)
		if delivered {
			M.WakeupDeliveredHit.Add(1)
		} else if !deduped {
			M.WakeupDeliveredMiss.Add(1)
		}
	case protocol.EventDaemonRuntimeProfilesChanged:
		var payload protocol.RuntimeProfilesChangedPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.WorkspaceID == "" {
			slog.Debug("daemon websocket relay: invalid runtime_profiles_changed payload", "error", err, "scope_id", scopeID, "event_id", eventID)
			M.WakeupDeliveredMiss.Add(1)
			return
		}
		delivered, deduped := h.notifyWorkspaceFrame(payload.WorkspaceID, frame, eventID)
		if delivered {
			M.WakeupDeliveredHit.Add(1)
		} else if !deduped {
			M.WakeupDeliveredMiss.Add(1)
		}
	case protocol.EventDaemonWorkspacesChanged:
		delivered, deduped := h.notifyUserFrame(scopeID, frame, eventID)
		if delivered {
			M.WakeupDeliveredHit.Add(1)
		} else if !deduped {
			M.WakeupDeliveredMiss.Add(1)
		}
	default:
		M.WakeupDeliveredMiss.Add(1)
		return
	}
}

func (h *Hub) notifyFrame(runtimeID string, data []byte, eventID string) (delivered bool, deduped bool) {
	h.mu.RLock()
	clients := h.byRuntime[runtimeID]
	slow := make([]*client, 0)
	for c := range clients {
		if !c.markSeen(eventID) {
			deduped = true
			continue
		}
		select {
		case c.send <- data:
			delivered = true
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slow {
		h.unregister(c)
		c.conn.Close()
	}
	if len(slow) > 0 {
		M.SlowEvictionsTotal.Add(int64(len(slow)))
	}
	return delivered, deduped
}

func (h *Hub) notifyWorkspaceFrame(workspaceID string, data []byte, eventID string) (delivered bool, deduped bool) {
	h.mu.RLock()
	clients := h.byWorkspace[workspaceID]
	slow := make([]*client, 0)
	for c := range clients {
		if !c.markSeen(eventID) {
			deduped = true
			continue
		}
		select {
		case c.send <- data:
			delivered = true
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slow {
		h.unregister(c)
		c.conn.Close()
	}
	if len(slow) > 0 {
		M.SlowEvictionsTotal.Add(int64(len(slow)))
	}
	return delivered, deduped
}

func (h *Hub) notifyUserFrame(userID string, data []byte, eventID string) (delivered bool, deduped bool) {
	h.mu.RLock()
	clients := h.byUser[userID]
	slow := make([]*client, 0)
	for c := range clients {
		if !c.markSeen(eventID) {
			deduped = true
			continue
		}
		select {
		case c.send <- data:
			delivered = true
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slow {
		h.unregister(c)
		c.conn.Close()
	}
	if len(slow) > 0 {
		M.SlowEvictionsTotal.Add(int64(len(slow)))
	}
	return delivered, deduped
}

func taskAvailableFrame(runtimeID, taskID string) ([]byte, error) {
	return json.Marshal(protocol.Message{
		Type: protocol.EventDaemonTaskAvailable,
		Payload: mustMarshalRaw(protocol.TaskAvailablePayload{
			RuntimeID: runtimeID,
			TaskID:    taskID,
		}),
	})
}

func runtimeProfilesChangedFrame(workspaceID, profileID string) ([]byte, error) {
	return json.Marshal(protocol.Message{
		Type: protocol.EventDaemonRuntimeProfilesChanged,
		Payload: mustMarshalRaw(protocol.RuntimeProfilesChangedPayload{
			WorkspaceID:      workspaceID,
			RuntimeProfileID: profileID,
		}),
	})
}

func workspacesChangedFrame() ([]byte, error) {
	return json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonWorkspacesChanged,
		Payload: mustMarshalRaw(protocol.WorkspacesChangedPayload{}),
	})
}

func mustMarshalRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func (h *Hub) RuntimeConnectionCount(runtimeID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byRuntime[runtimeID])
}

func (h *Hub) WorkspaceConnectionCount(workspaceID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byWorkspace[workspaceID])
}

func (h *Hub) UserConnectionCount(userID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser[userID])
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	h.clients[c] = true
	for runtimeID := range c.runtimes {
		conns := h.byRuntime[runtimeID]
		if conns == nil {
			conns = make(map[*client]bool)
			h.byRuntime[runtimeID] = conns
		}
		conns[c] = true
	}
	workspaceIDs := c.identity.AuthorizedWorkspaceIDs()
	for _, workspaceID := range workspaceIDs {
		conns := h.byWorkspace[workspaceID]
		if conns == nil {
			conns = make(map[*client]bool)
			h.byWorkspace[workspaceID] = conns
		}
		conns[c] = true
	}
	if userID := c.identity.UserID; userID != "" {
		conns := h.byUser[userID]
		if conns == nil {
			conns = make(map[*client]bool)
			h.byUser[userID] = conns
		}
		conns[c] = true
	}
	total := len(h.clients)
	h.mu.Unlock()

	M.ConnectsTotal.Add(1)
	M.ActiveConnections.Add(1)
	slog.Info("daemon websocket connected",
		"daemon_id", c.identity.DaemonID,
		"user_id", c.identity.UserID,
		"workspace_id", c.identity.PrimaryWorkspaceID(),
		"workspace_ids", workspaceIDs,
		"runtimes", len(c.runtimes),
		"client_version", c.identity.ClientVersion,
		"total_clients", total,
	)
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	if !h.clients[c] {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	for runtimeID := range c.runtimes {
		if conns := h.byRuntime[runtimeID]; conns != nil {
			delete(conns, c)
			if len(conns) == 0 {
				delete(h.byRuntime, runtimeID)
			}
		}
	}
	workspaceIDs := c.identity.AuthorizedWorkspaceIDs()
	for _, workspaceID := range workspaceIDs {
		if conns := h.byWorkspace[workspaceID]; conns != nil {
			delete(conns, c)
			if len(conns) == 0 {
				delete(h.byWorkspace, workspaceID)
			}
		}
	}
	if userID := c.identity.UserID; userID != "" {
		if conns := h.byUser[userID]; conns != nil {
			delete(conns, c)
			if len(conns) == 0 {
				delete(h.byUser, userID)
			}
		}
	}
	c.sendMu.Lock()
	c.sendClosed = true
	close(c.send)
	c.sendMu.Unlock()
	total := len(h.clients)
	h.mu.Unlock()

	M.DisconnectsTotal.Add(1)
	M.ActiveConnections.Add(-1)
	slog.Info("daemon websocket disconnected",
		"daemon_id", c.identity.DaemonID,
		"user_id", c.identity.UserID,
		"workspace_id", c.identity.PrimaryWorkspaceID(),
		"workspace_ids", workspaceIDs,
		"runtimes", len(c.runtimes),
		"total_clients", total,
	)
}

func (c *client) readPump() {
	defer func() {
		// Cancel first so async RPC handlers stop before we close the send
		// channel, then unregister (which marks send closed).
		c.cancel()
		c.hub.unregister(c)
		c.conn.Close()
	}()

	// Read limit sized for daemon:rpc_request frames carrying a machine's full
	// runtime_id set (MUL-4257), well above the tiny heartbeat/wakeup frames.
	c.conn.SetReadLimit(64 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("daemon websocket read error", "error", err, "daemon_id", c.identity.DaemonID)
			}
			return
		}
		c.handleFrame(raw)
	}
}

func (c *client) handleFrame(raw []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Debug("daemon websocket invalid frame", "error", err, "daemon_id", c.identity.DaemonID)
		if rec := c.hub.messageKindRecorder(); rec != nil {
			rec.RecordDaemonWSMessageReceived("invalid")
		}
		return
	}
	kind := strings.TrimPrefix(msg.Type, "daemon:")
	if kind == "" {
		kind = "unknown"
	}
	if rec := c.hub.messageKindRecorder(); rec != nil {
		rec.RecordDaemonWSMessageReceived(kind)
	}
	switch msg.Type {
	case protocol.EventDaemonHeartbeat:
		c.handleHeartbeatFrame(msg.Payload)
	case protocol.EventDaemonRPCRequest:
		c.handleRPCFrame(msg.Payload)
	default:
		// Unknown app messages are intentionally ignored for forward
		// compatibility with future daemon → server message types.
	}
}

// handleRPCFrame processes a generic daemon:rpc_request (MUL-4257): it runs the
// registered RPC handler in its own goroutine (so a DB-bound claim does not
// stall the read pump or the next heartbeat) and writes back a
// daemon:rpc_response echoing the request id. A missing handler or a full
// in-flight slot yields a non-2xx response so the daemon falls back to HTTP.
func (c *client) handleRPCFrame(raw json.RawMessage) {
	var req protocol.RPCRequestPayload
	if err := json.Unmarshal(raw, &req); err != nil {
		slog.Debug("daemon websocket rpc invalid payload", "error", err, "daemon_id", c.identity.DaemonID)
		return
	}
	if req.RequestID == "" {
		slog.Debug("daemon websocket rpc missing request_id", "daemon_id", c.identity.DaemonID)
		return
	}
	handler := c.hub.rpcHandler()
	if handler == nil {
		c.sendRPCResponse(req.RequestID, http.StatusServiceUnavailable, nil, "rpc handler unavailable")
		return
	}
	// Bound concurrent handlers; if saturated, tell the daemon to fall back
	// rather than queueing unbounded work on one socket.
	select {
	case c.rpcSem <- struct{}{}:
	default:
		c.sendRPCResponse(req.RequestID, http.StatusTooManyRequests, nil, "too many in-flight rpc requests")
		return
	}
	go func() {
		defer func() { <-c.rpcSem }()
		// Bound server-side execution by the caller's requested budget (in
		// addition to the connection ctx), so a slow RPC is cancelled — and its
		// work rolled back — rather than committing after the daemon has already
		// timed out and fallen back to HTTP (MUL-4257). The daemon waits a grace
		// period beyond this budget, so a claim that DID commit before the
		// deadline still reports back in time.
		hctx := c.ctx
		if req.TimeoutMs > 0 {
			var cancel context.CancelFunc
			hctx, cancel = context.WithTimeout(c.ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
			defer cancel()
		}
		status, body, err := handler(hctx, c.identity, req.Method, req.Body)
		if err != nil {
			if status < 400 {
				status = http.StatusInternalServerError
			}
			c.sendRPCResponse(req.RequestID, status, nil, err.Error())
			return
		}
		c.sendRPCResponse(req.RequestID, status, body, "")
	}()
}

func (c *client) sendRPCResponse(requestID string, status int, body json.RawMessage, errMsg string) {
	frame, err := json.Marshal(protocol.Message{
		Type: protocol.EventDaemonRPCResponse,
		Payload: mustMarshalRaw(protocol.RPCResponsePayload{
			RequestID: requestID,
			Status:    status,
			Body:      body,
			Error:     errMsg,
		}),
	})
	if err != nil {
		slog.Debug("daemon websocket rpc response marshal failed", "error", err)
		return
	}
	if !c.trySend(frame) {
		// Send buffer full or connection closing — drop the response; the
		// daemon's per-request timeout fires and it falls back to HTTP.
		slog.Debug("daemon websocket rpc response dropped",
			"daemon_id", c.identity.DaemonID, "request_id", requestID)
	}
}

// handleHeartbeatFrame processes an inbound daemon:heartbeat from the daemon,
// invokes the hub's handler, and writes back a daemon:heartbeat_ack.
func (c *client) handleHeartbeatFrame(raw json.RawMessage) {
	handler := c.hub.heartbeatHandler()
	if handler == nil {
		// Server doesn't have a heartbeat handler wired — daemon will time
		// out waiting for an ack and fall back to HTTP heartbeat.
		return
	}

	var payload protocol.DaemonHeartbeatRequestPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		slog.Debug("daemon websocket heartbeat invalid payload", "error", err, "daemon_id", c.identity.DaemonID)
		return
	}
	if payload.RuntimeID == "" {
		slog.Debug("daemon websocket heartbeat missing runtime_id", "daemon_id", c.identity.DaemonID)
		return
	}
	if _, ok := c.runtimes[payload.RuntimeID]; !ok {
		// The connection authenticated for a fixed runtime set; reject any
		// heartbeat for a runtime the client did not register for.
		slog.Warn("daemon websocket heartbeat for unauthorized runtime",
			"daemon_id", c.identity.DaemonID,
			"runtime_id", payload.RuntimeID)
		return
	}

	// Intentionally do NOT wrap this ctx with WithTimeout. The handler
	// reaches LocalSkill{List,Import}Store.PopPending, whose Redis Lua
	// claim script has side effects (ZREM + SET-running) that cannot be
	// safely un-run if the client cancels mid-script — the same invariant
	// that keeps the HTTP heartbeat from putting a per-call timeout on
	// PopPending. The natural bound is the read pump's lifetime (the conn
	// closes if the daemon goes away) plus Redis's own server-side limits.
	ack, err := handler(context.Background(), c.identity, payload.RuntimeID, payload.SupportsBatchImport)
	if err != nil {
		slog.Warn("daemon websocket heartbeat handler failed",
			"error", err,
			"daemon_id", c.identity.DaemonID,
			"runtime_id", payload.RuntimeID)
		return
	}
	if ack == nil {
		return
	}
	frame, err := json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonHeartbeatAck,
		Payload: mustMarshalRaw(ack),
	})
	if err != nil {
		slog.Debug("daemon websocket heartbeat ack marshal failed", "error", err)
		return
	}
	if !c.trySend(frame) {
		// Send buffer full or connection closing — drop; HTTP heartbeat resumes.
		slog.Debug("daemon websocket heartbeat ack dropped",
			"daemon_id", c.identity.DaemonID,
			"runtime_id", payload.RuntimeID)
	}
}

func (c *client) writePump() {
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
				slog.Debug("daemon websocket write error", "error", err, "daemon_id", c.identity.DaemonID)
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
