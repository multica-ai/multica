package realtime

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
)

const (
	defaultShardedRelayShards       = 8
	defaultShardedRelayStreamMaxLen = 100000
	defaultShardedRelayReadCount    = 128
	defaultShardedRelayReadBlock    = 5 * time.Second
	defaultShardedRelayReplayGrace  = 5 * time.Minute
)

// ShardedStreamKey returns the Redis Stream key used by a fixed relay shard.
func ShardedStreamKey(shard int) string {
	return fmt.Sprintf("ws:relay:shard:%d", shard)
}

// ShardedStreamRelayConfig controls the fixed-reader Redis Stream relay.
type ShardedStreamRelayConfig struct {
	Shards       int
	StreamMaxLen int64
	ReadCount    int64
	ReadBlock    time.Duration
	// ReplayGrace is the lookback window on startup: the shard reader starts
	// consuming from (now - ReplayGrace) rather than "$" so that any events
	// published while this pod was down are replayed. Events are bounded by
	// the stream's MAXLEN, and downstream consumers must be idempotent.
	ReplayGrace time.Duration
}

// DefaultShardedStreamRelayConfig returns production-safe defaults: a small
// fixed number of blocking readers per pod, bounded stream retention, and
// batched reads.
func DefaultShardedStreamRelayConfig() ShardedStreamRelayConfig {
	return ShardedStreamRelayConfig{
		Shards:       defaultShardedRelayShards,
		StreamMaxLen: defaultShardedRelayStreamMaxLen,
		ReadCount:    defaultShardedRelayReadCount,
		ReadBlock:    defaultShardedRelayReadBlock,
		ReplayGrace:  defaultShardedRelayReplayGrace,
	}
}

func (c ShardedStreamRelayConfig) withDefaults() ShardedStreamRelayConfig {
	def := DefaultShardedStreamRelayConfig()
	if c.Shards <= 0 {
		c.Shards = def.Shards
	}
	if c.StreamMaxLen <= 0 {
		c.StreamMaxLen = def.StreamMaxLen
	}
	if c.ReadCount <= 0 {
		c.ReadCount = def.ReadCount
	}
	if c.ReadBlock <= 0 {
		c.ReadBlock = def.ReadBlock
	}
	if c.ReplayGrace <= 0 {
		c.ReplayGrace = def.ReplayGrace
	}
	return c
}

// ShardedStreamRelay publishes all realtime events into a fixed set of Redis
// Streams. Every API node runs one XREAD BLOCK loop per shard and locally
// filters events by hub subscriptions. This keeps blocked Redis connections
// bounded by pod_count * shard_count instead of active_scope_count.
type ShardedStreamRelay struct {
	hub      *Hub
	writeRDB *redis.Client
	readRDB  *redis.Client
	nodeID   string
	config   ShardedStreamRelayConfig

	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup

	daemonRuntime DaemonRuntimeDeliverer
}

func NewShardedStreamRelay(hub *Hub, writeRDB, readRDB *redis.Client, config ShardedStreamRelayConfig) *ShardedStreamRelay {
	if readRDB == nil {
		readRDB = writeRDB
	}
	return &ShardedStreamRelay{
		hub:      hub,
		writeRDB: writeRDB,
		readRDB:  readRDB,
		nodeID:   ulid.Make().String(),
		config:   config.withDefaults(),
	}
}

func (r *ShardedStreamRelay) NodeID() string { return r.nodeID }

func (r *ShardedStreamRelay) SetDaemonRuntimeDeliverer(d DaemonRuntimeDeliverer) {
	r.daemonRuntime = d
}

func (r *ShardedStreamRelay) Start(ctx context.Context) {
	M.NodeID.Store(r.nodeID)
	if err := r.writeRDB.Ping(ctx).Err(); err != nil {
		slog.Error("realtime/sharded-redis: initial ping failed", "error", err)
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
	} else if r.readRDB != r.writeRDB {
		if err := r.readRDB.Ping(ctx).Err(); err != nil {
			slog.Error("realtime/sharded-redis: initial read-client ping failed", "error", err)
			M.RedisConnected.Store(false)
			M.SetRedisLastError(err.Error())
		} else {
			M.RedisConnected.Store(true)
		}
	} else {
		M.RedisConnected.Store(true)
	}

	r.wg.Add(1 + r.config.Shards)
	go func() {
		defer r.wg.Done()
		r.heartbeatLoop(ctx)
	}()
	for shard := 0; shard < r.config.Shards; shard++ {
		shard := shard
		go func() {
			defer r.wg.Done()
			r.readShard(ctx, shard)
		}()
	}
}

func (r *ShardedStreamRelay) Stop() {
	r.mu.Lock()
	r.stopping = true
	r.mu.Unlock()
}

func (r *ShardedStreamRelay) Wait() {
	r.wg.Wait()
}

func (r *ShardedStreamRelay) BroadcastToScope(scopeType, scopeID string, message []byte) {
	_ = r.PublishWithID(scopeType, scopeID, "", message, ulid.Make().String())
}

func (r *ShardedStreamRelay) BroadcastToWorkspace(workspaceID string, message []byte) {
	r.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (r *ShardedStreamRelay) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	_ = r.PublishWithID(ScopeUser, userID, exclude, message, ulid.Make().String())
}

func (r *ShardedStreamRelay) Broadcast(message []byte) {
	_ = r.PublishWithID("global", "all", "", message, ulid.Make().String())
}

func (r *ShardedStreamRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	ev := newEnvelope(r.nodeID, scopeType, scopeID, exclude, frame, id)
	stream := ShardedStreamKey(r.shardFor(scopeType, scopeID))
	args := &redis.XAddArgs{
		Stream: stream,
		MaxLen: r.config.StreamMaxLen,
		Approx: true,
		Values: envelopeRedisValues(ev),
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.writeRDB.XAdd(ctx, args).Err(); err != nil {
		M.RedisXAddErrors.Add(1)
		M.SetRedisLastError(err.Error())
		slog.Warn("realtime/sharded-redis: XADD failed", "error", err, "scope", scopeType, "scope_id", scopeID, "stream", stream)
		return err
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
	return nil
}

func (r *ShardedStreamRelay) shardFor(scopeType, scopeID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(scopeType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(scopeID))
	return int(h.Sum32() % uint32(r.config.Shards))
}

// replayStartID returns a Redis stream ID anchored to (now - ReplayGrace) so
// that a freshly started shard reader replays only the recent grace window
// rather than the entire retained stream. The "-0" suffix matches any
// sequence number at that millisecond.
func (r *ShardedStreamRelay) replayStartID() string {
	ms := time.Now().Add(-r.config.ReplayGrace).UnixMilli()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%d-0", ms)
}

func (r *ShardedStreamRelay) readShard(ctx context.Context, shard int) {
	stream := ShardedStreamKey(shard)
	// Start from a bounded lookback window, not "$", so that events
	// published while this pod was down are replayed. The grace window is
	// short enough that replay volume stays manageable, and downstream
	// consumers (daemon wakeups, client reconnects) are idempotent.
	lastID := r.replayStartID()
	for {
		if ctx.Err() != nil || r.isStopping() {
			return
		}
		if !r.readShardOnce(ctx, shard, stream, &lastID) {
			return
		}
	}
}

// readShardOnce performs a single XREAD iteration for one shard. It returns
// true when the caller should continue reading, false when the context is
// done and the loop should exit. lastID is advanced past any messages read.
func (r *ShardedStreamRelay) readShardOnce(ctx context.Context, shard int, stream string, lastID *string) bool {
	readCtx, cancel := context.WithTimeout(ctx, r.config.ReadBlock+time.Second)
	res, err := r.readRDB.XRead(readCtx, &redis.XReadArgs{
		Streams: []string{stream, *lastID},
		Count:   r.config.ReadCount,
		Block:   r.config.ReadBlock,
	}).Result()
	cancel()

	if errors.Is(err, redis.Nil) || (err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))) {
		return true
	}
	if err != nil {
		M.RedisXReadErrors.Add(1)
		M.SetRedisLastError(err.Error())
		slog.Warn("realtime/sharded-redis: XREAD failed", "error", err, "shard", shard, "stream", stream)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
		return true
	}

	for _, s := range res {
		for _, msg := range s.Messages {
			*lastID = msg.ID
			M.RedisXReadTotal.Add(1)
			r.deliverMessage(msg)
		}
	}
	return true
}

func (r *ShardedStreamRelay) deliverMessage(msg redis.XMessage) {
	ev, ok := envelopeFromXMessage(msg)
	if !ok || ev.Scope == "" || ev.ScopeID == "" {
		return
	}
	deliverEnvelope(r.hub, r.daemonRuntime, ev)
}

func (r *ShardedStreamRelay) heartbeatLoop(ctx context.Context) {
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

func (r *ShardedStreamRelay) heartbeatOnce(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.writeRDB.Set(hbCtx, HeartbeatKey(r.nodeID), time.Now().UTC().Format(time.RFC3339Nano), heartbeatTTL).Err(); err != nil {
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
		return
	}
	M.RedisConnected.Store(true)
}

func (r *ShardedStreamRelay) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

var _ Broadcaster = (*ShardedStreamRelay)(nil)
var _ RelayPublisher = (*ShardedStreamRelay)(nil)
