package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
)

// Stream / registry key naming. Centralised so tests can introspect.
func StreamKey(scopeType, scopeID string) string {
	return fmt.Sprintf("ws:scope:%s:%s:stream", scopeType, scopeID)
}
func NodesKey(scopeType, scopeID string) string {
	return fmt.Sprintf("ws:scope:%s:%s:nodes", scopeType, scopeID)
}
func HeartbeatKey(nodeID string) string {
	return fmt.Sprintf("ws:node:%s:heartbeat", nodeID)
}

const (
	streamMaxLen        int64 = 10000
	heartbeatTTL              = 90 * time.Second
	heartbeatPeriod           = 30 * time.Second
	consumerIdleGrace         = 10 * time.Minute
	consumerSweepPeriod       = 5 * time.Minute
)

// envelope is what we serialise into each XADD message. It is opaque to the
// hub: the relay decodes payload_json before fanning out.
type envelope struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	Scope       string `json:"scope"`
	ScopeID     string `json:"scope_id"`
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	CreatedAt   string `json:"created_at"`
	NodeID      string `json:"node_id"`
	PayloadJSON string `json:"payload_json"` // raw JSON of the original ws frame
}

func newEnvelope(nodeID, scopeType, scopeID, exclude string, frame []byte, id string) envelope {
	ev := envelope{
		EventID:     id,
		Scope:       scopeType,
		ScopeID:     scopeID,
		NodeID:      nodeID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		PayloadJSON: string(frame),
	}
	if exclude != "" {
		ev.WorkspaceID = exclude
	}
	if t, a := peekTypeActor(frame); t != "" {
		ev.EventType = t
		ev.ActorID = a
	}
	return ev
}

func envelopeRedisValues(ev envelope) map[string]any {
	return map[string]any{
		"event_id":     ev.EventID,
		"event_type":   ev.EventType,
		"scope":        ev.Scope,
		"scope_id":     ev.ScopeID,
		"workspace_id": ev.WorkspaceID,
		"actor_id":     ev.ActorID,
		"created_at":   ev.CreatedAt,
		"node_id":      ev.NodeID,
		"payload_json": ev.PayloadJSON,
	}
}

func envelopeFromXMessage(msg redis.XMessage) (envelope, bool) {
	ev := envelope{
		EventID:     redisString(msg.Values["event_id"]),
		EventType:   redisString(msg.Values["event_type"]),
		Scope:       redisString(msg.Values["scope"]),
		ScopeID:     redisString(msg.Values["scope_id"]),
		WorkspaceID: redisString(msg.Values["workspace_id"]),
		ActorID:     redisString(msg.Values["actor_id"]),
		CreatedAt:   redisString(msg.Values["created_at"]),
		NodeID:      redisString(msg.Values["node_id"]),
		PayloadJSON: redisString(msg.Values["payload_json"]),
	}
	return ev, ev.PayloadJSON != ""
}

func redisString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}

func deliverEnvelope(hub *Hub, ev envelope) {
	if ev.PayloadJSON == "" {
		return
	}
	frame := injectEventID([]byte(ev.PayloadJSON), ev.EventID)
	switch ev.Scope {
	case "global":
		hub.fanoutAllDedup(frame, "", ev.EventID)
	case ScopeUser:
		hub.fanoutUser(ev.ScopeID, frame, ev.WorkspaceID, ev.EventID)
	default:
		hub.BroadcastToScopeDedup(ev.Scope, ev.ScopeID, frame, ev.EventID)
	}
}

// RedisRelay is a Broadcaster implementation that writes every message to a
// per-scope Redis Stream and consumes streams for which there are local
// subscribers. Local fanout is delegated to the wrapped *Hub.
type RedisRelay struct {
	hub      *Hub
	writeRDB *redis.Client
	readRDB  *redis.Client
	nodeID   string

	mu        sync.Mutex
	consumers map[scopeKey]*scopeConsumer
	stopping  bool
	wg        sync.WaitGroup
}

type scopeConsumer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRedisRelay constructs a relay. The caller is responsible for invoking
// Start before producing messages.
func NewRedisRelay(hub *Hub, rdb *redis.Client) *RedisRelay {
	return NewRedisRelayWithClients(hub, rdb, rdb)
}

// NewRedisRelayWithClients constructs a relay with separate Redis clients for
// writes and blocking reads. The read client is reserved for XREADGROUP BLOCK
// calls so long-polling stream consumers cannot exhaust the pool used by XADD,
// heartbeats, acks, and other request-path Redis operations.
func NewRedisRelayWithClients(hub *Hub, writeRDB, readRDB *redis.Client) *RedisRelay {
	if readRDB == nil {
		readRDB = writeRDB
	}
	return &RedisRelay{
		hub:       hub,
		writeRDB:  writeRDB,
		readRDB:   readRDB,
		nodeID:    ulid.Make().String(),
		consumers: make(map[scopeKey]*scopeConsumer),
	}
}

// NodeID returns this relay's randomly-assigned node identifier.
func (r *RedisRelay) NodeID() string { return r.nodeID }

// Wait blocks until all relay-owned goroutines have exited after the Start
// context is canceled.
func (r *RedisRelay) Wait() {
	r.wg.Wait()
}

// Stop prevents new scope consumers from being started and cancels any active
// consumers. The Start context still controls heartbeat and sweeper shutdown;
// callers should cancel it before calling Wait.
func (r *RedisRelay) Stop() {
	r.hub.SetSubscriptionCallbacks(nil, nil)

	r.mu.Lock()
	r.stopping = true
	for _, c := range r.consumers {
		c.cancel()
	}
	r.mu.Unlock()
}

// Start wires the hub→relay subscription callbacks, kicks off the heartbeat
// goroutine, and spins up consumers for any scopes the hub already knows
// about. ctx controls all background goroutines: cancelling it shuts the
// relay down.
func (r *RedisRelay) Start(ctx context.Context) {
	M.NodeID.Store(r.nodeID)
	if err := r.writeRDB.Ping(ctx).Err(); err != nil {
		slog.Error("realtime/redis: initial ping failed", "error", err)
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
	} else if r.readRDB != r.writeRDB {
		if err := r.readRDB.Ping(ctx).Err(); err != nil {
			slog.Error("realtime/redis: initial read-client ping failed", "error", err)
			M.RedisConnected.Store(false)
			M.SetRedisLastError(err.Error())
		} else {
			M.RedisConnected.Store(true)
		}
	} else {
		M.RedisConnected.Store(true)
	}

	r.hub.SetSubscriptionCallbacks(
		func(scopeType, scopeID string) { r.startConsumer(ctx, scopeType, scopeID) },
		func(scopeType, scopeID string) { r.stopConsumer(scopeType, scopeID) },
	)

	for _, key := range r.hub.LocalScopes() {
		r.startConsumer(ctx, key.Type, key.ID)
	}

	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		r.heartbeatLoop(ctx)
	}()
	go func() {
		defer r.wg.Done()
		r.consumerSweeper(ctx)
	}()
}

// BroadcastToScope publishes message into the scope's Redis stream. The
// envelope contains an event_id for client-side dedup. Local fanout happens
// when this node consumes its own write back through XREADGROUP — except in
// the dual-write configuration where the local hub is invoked directly.
func (r *RedisRelay) BroadcastToScope(scopeType, scopeID string, message []byte) {
	r.publish(scopeType, scopeID, "", message)
}

// BroadcastToWorkspace / SendToUser / Broadcast satisfy the back-compat
// portion of Broadcaster.
func (r *RedisRelay) BroadcastToWorkspace(workspaceID string, message []byte) {
	r.publish(ScopeWorkspace, workspaceID, "", message)
}
func (r *RedisRelay) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	r.publish(ScopeUser, userID, exclude, message)
}
func (r *RedisRelay) Broadcast(message []byte) {
	// Daemon broadcast — write to a special "global" stream so other nodes
	// can fan out to all clients regardless of subscriptions.
	r.publish("global", "all", "", message)
}

func (r *RedisRelay) publish(scopeType, scopeID, exclude string, frame []byte) {
	ev := newEnvelope(r.nodeID, scopeType, scopeID, exclude, frame, ulid.Make().String())

	args := &redis.XAddArgs{
		Stream: StreamKey(scopeType, scopeID),
		MaxLen: streamMaxLen,
		Approx: true,
		Values: envelopeRedisValues(ev),
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.writeRDB.XAdd(ctx, args).Err(); err != nil {
		M.RedisXAddErrors.Add(1)
		M.SetRedisLastError(err.Error())
		slog.Warn("realtime/redis: XADD failed", "error", err, "scope", scopeType, "scope_id", scopeID)
		return
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
}

// startConsumer kicks off a single per-scope XREADGROUP loop if not already
// running.
func (r *RedisRelay) startConsumer(parent context.Context, scopeType, scopeID string) {
	key := sk(scopeType, scopeID)
	r.mu.Lock()
	if r.stopping || parent.Err() != nil {
		r.mu.Unlock()
		return
	}
	if _, exists := r.consumers[key]; exists {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	c := &scopeConsumer{cancel: cancel, done: make(chan struct{})}
	r.consumers[key] = c
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		r.runConsumer(ctx, c, scopeType, scopeID)
	}()
}

func (r *RedisRelay) stopConsumer(scopeType, scopeID string) {
	key := sk(scopeType, scopeID)
	r.mu.Lock()
	c, ok := r.consumers[key]
	if ok {
		delete(r.consumers, key)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	c.cancel()
}

func (r *RedisRelay) runConsumer(ctx context.Context, c *scopeConsumer, scopeType, scopeID string) {
	defer close(c.done)

	stream := StreamKey(scopeType, scopeID)
	group := "node:" + r.nodeID
	consumerName := r.nodeID

	// MKSTREAM ensures the stream exists. Ignore BUSYGROUP.
	createCtx, createCancel := context.WithTimeout(ctx, 2*time.Second)
	if err := r.writeRDB.XGroupCreateMkStream(createCtx, stream, group, "$").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		slog.Warn("realtime/redis: XGROUP CREATE failed", "error", err, "scope", scopeType, "scope_id", scopeID)
	}
	createCancel()

	// Register ourselves as a node interested in this scope.
	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	r.writeRDB.ZAdd(regCtx, NodesKey(scopeType, scopeID), redis.Z{Score: float64(time.Now().Add(heartbeatTTL).Unix()), Member: r.nodeID})
	regCancel()

	for {
		if ctx.Err() != nil {
			break
		}
		readCtx, readCancel := context.WithTimeout(ctx, 6*time.Second)
		res, err := r.readRDB.XReadGroup(readCtx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumerName,
			Streams:  []string{stream, ">"},
			Count:    32,
			Block:    5 * time.Second,
		}).Result()
		readCancel()
		if errors.Is(err, redis.Nil) || (err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))) {
			continue
		}
		if err != nil {
			M.RedisXReadErrors.Add(1)
			M.SetRedisLastError(err.Error())
			// Brief backoff to avoid busy-looping on a flapping connection.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, s := range res {
			for _, msg := range s.Messages {
				M.RedisXReadTotal.Add(1)
				r.deliverMessage(scopeType, scopeID, msg)
				ackCtx, ackCancel := context.WithTimeout(ctx, time.Second)
				if err := r.writeRDB.XAck(ackCtx, stream, group, msg.ID).Err(); err != nil {
					slog.Debug("realtime/redis: XACK failed", "error", err, "id", msg.ID)
				} else {
					M.RedisAckTotal.Add(1)
				}
				ackCancel()
			}
		}
	}

	// Best-effort consumer cleanup.
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
	r.writeRDB.XGroupDelConsumer(cleanCtx, stream, group, consumerName)
	cleanCancel()
}

func (r *RedisRelay) deliverMessage(scopeType, scopeID string, msg redis.XMessage) {
	ev, ok := envelopeFromXMessage(msg)
	if !ok {
		return
	}
	if ev.Scope == "" {
		ev.Scope = scopeType
	}
	if ev.ScopeID == "" {
		ev.ScopeID = scopeID
	}
	deliverEnvelope(r.hub, ev)
}

// fanoutUser is implemented in hub.go.

func (r *RedisRelay) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(heartbeatPeriod)
	defer t.Stop()
	for {
		r.heartbeatOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *RedisRelay) heartbeatOnce(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.writeRDB.Set(hbCtx, HeartbeatKey(r.nodeID), time.Now().UTC().Format(time.RFC3339Nano), heartbeatTTL).Err(); err != nil {
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
		return
	}
	M.RedisConnected.Store(true)
	expiry := float64(time.Now().Add(heartbeatTTL).Unix())
	for _, key := range r.hub.LocalScopes() {
		r.writeRDB.ZAdd(hbCtx, NodesKey(key.Type, key.ID), redis.Z{Score: expiry, Member: r.nodeID})
	}
}

// consumerSweeper periodically drops stale ZSET entries (nodes whose TTL
// expired). Best-effort: we only sweep the scopes this node currently has
// local subscribers for, since they're the only ones we can reason about
// without scanning all keys.
func (r *RedisRelay) consumerSweeper(ctx context.Context) {
	t := time.NewTicker(consumerSweepPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := float64(time.Now().Unix())
		for _, key := range r.hub.LocalScopes() {
			swCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			r.writeRDB.ZRemRangeByScore(swCtx, NodesKey(key.Type, key.ID), "-inf", fmt.Sprintf("%f", now))
			cancel()
		}
	}
}

// peekTypeActor parses the WS frame just enough to lift event_type / actor_id
// for the envelope. Failures yield empty strings — the envelope still works.
func peekTypeActor(frame []byte) (string, string) {
	var probe struct {
		Type    string `json:"type"`
		ActorID string `json:"actor_id"`
	}
	_ = json.Unmarshal(frame, &probe)
	return probe.Type, probe.ActorID
}

// injectEventID inserts the event_id field into an existing JSON object frame
// without re-encoding the payload. The frame must be a JSON object.
func injectEventID(frame []byte, eventID string) []byte {
	if eventID == "" || len(frame) == 0 || frame[0] != '{' {
		return frame
	}
	// Decode-encode round-trip is simplest and avoids edge cases with
	// trailing whitespace / nested escapes. A few extra allocations per
	// message are fine relative to the network cost.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(frame, &obj); err != nil {
		return frame
	}
	if _, exists := obj["event_id"]; exists {
		return frame
	}
	idJSON, _ := json.Marshal(eventID)
	obj["event_id"] = idJSON
	out, err := json.Marshal(obj)
	if err != nil {
		return frame
	}
	return out
}

// DualWriteBroadcaster delivers each message both to the local hub (immediate
// fanout) AND to the Redis relay (cross-node fanout). It dedups via
// Client.markSeen so the same client doesn't see the same event twice when
// the Redis relay loops the message back.
type DualWriteBroadcaster struct {
	local *Hub
	relay RelayPublisher
}

// RelayPublisher is implemented by Redis relay backends that can publish a
// caller-supplied event id for local/Redis loopback deduplication.
type RelayPublisher interface {
	PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error
}

func NewDualWriteBroadcaster(local *Hub, relay RelayPublisher) *DualWriteBroadcaster {
	return newDualWriteBroadcaster(local, relay)
}

func newDualWriteBroadcaster(local *Hub, relay RelayPublisher) *DualWriteBroadcaster {
	return &DualWriteBroadcaster{local: local, relay: relay}
}

func (d *DualWriteBroadcaster) BroadcastToScope(scopeType, scopeID string, message []byte) {
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	// Local fast path: BroadcastToScopeDedup marks each client as having
	// seen `id`, so the Redis loopback for the same id will be ignored.
	d.local.BroadcastToScopeDedup(scopeType, scopeID, frame, id)
	_ = d.relay.PublishWithID(scopeType, scopeID, "", message, id)
}

func (d *DualWriteBroadcaster) BroadcastToWorkspace(workspaceID string, message []byte) {
	d.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (d *DualWriteBroadcaster) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	d.local.fanoutUser(userID, frame, exclude, id)
	_ = d.relay.PublishWithID(ScopeUser, userID, exclude, message, id)
}

func (d *DualWriteBroadcaster) Broadcast(message []byte) {
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	d.local.fanoutAllDedup(frame, "", id)
	_ = d.relay.PublishWithID("global", "all", "", message, id)
}

// PublishWithID is like publish but uses a caller-supplied event id so the
// dual-write path can dedup.
func (r *RedisRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	ev := newEnvelope(r.nodeID, scopeType, scopeID, exclude, frame, id)
	args := &redis.XAddArgs{
		Stream: StreamKey(scopeType, scopeID),
		MaxLen: streamMaxLen,
		Approx: true,
		Values: envelopeRedisValues(ev),
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.writeRDB.XAdd(ctx, args).Err(); err != nil {
		M.RedisXAddErrors.Add(1)
		M.SetRedisLastError(err.Error())
		slog.Warn("realtime/redis: XADD failed", "error", err, "scope", scopeType, "scope_id", scopeID)
		return err
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
	return nil
}

var _ Broadcaster = (*RedisRelay)(nil)
var _ Broadcaster = (*DualWriteBroadcaster)(nil)
var _ RelayPublisher = (*RedisRelay)(nil)
