package realtime

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
)

const (
	defaultShardedRelayShards = 8
	// At the measured baseline of roughly 1 KiB per relay entry, 2000 entries
	// across each of eight shards estimates about 16 MiB instead of the former
	// ~800 MiB default. Operators can tune this from observed entry sizes.
	defaultShardedRelayStreamMaxLen = 2000
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
	ReplayGrace         time.Duration
	TrimHorizon         time.Duration
	StreamTTL           time.Duration
	TTLRefreshInterval  time.Duration
	MaintenanceInterval time.Duration
	StreamTTLEnabled    bool
}

// DefaultShardedStreamRelayConfig returns production-safe defaults: a small
// fixed number of blocking readers per pod, bounded stream retention, and
// batched reads.
func DefaultShardedStreamRelayConfig() ShardedStreamRelayConfig {
	retention := DefaultStreamRetentionConfig()
	return ShardedStreamRelayConfig{
		Shards:              defaultShardedRelayShards,
		StreamMaxLen:        retention.StreamMaxLen,
		ReadCount:           defaultShardedRelayReadCount,
		ReadBlock:           defaultShardedRelayReadBlock,
		ReplayGrace:         defaultShardedRelayReplayGrace,
		TrimHorizon:         retention.TrimHorizon,
		StreamTTL:           retention.StreamTTL,
		TTLRefreshInterval:  retention.TTLRefreshInterval,
		MaintenanceInterval: retention.MaintenanceInterval,
		StreamTTLEnabled:    retention.StreamTTLEnabled,
	}
}

// RetentionConfig returns the mode-independent stream retention settings.
func (c ShardedStreamRelayConfig) RetentionConfig() StreamRetentionConfig {
	c = c.withDefaults()
	return StreamRetentionConfig{
		StreamMaxLen:        c.StreamMaxLen,
		TrimHorizon:         c.TrimHorizon,
		StreamTTL:           c.StreamTTL,
		TTLRefreshInterval:  c.TTLRefreshInterval,
		MaintenanceInterval: c.MaintenanceInterval,
		StreamTTLEnabled:    c.StreamTTLEnabled,
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
	if c.TrimHorizon <= c.ReplayGrace {
		c.TrimHorizon = 2 * c.ReplayGrace
	}
	if c.StreamTTL < c.TrimHorizon {
		c.StreamTTL = c.TrimHorizon + c.ReplayGrace
	}
	if c.TTLRefreshInterval <= 0 || c.TTLRefreshInterval >= c.StreamTTL {
		c.TTLRefreshInterval = retentionSubinterval(c.StreamTTL, def.TTLRefreshInterval)
	}
	if c.MaintenanceInterval <= 0 || c.MaintenanceInterval >= c.StreamTTL {
		c.MaintenanceInterval = retentionSubinterval(c.StreamTTL, def.MaintenanceInterval)
	}
	return c
}

func retentionSubinterval(ttl, preferred time.Duration) time.Duration {
	if preferred > 0 && preferred < ttl {
		return preferred
	}
	interval := ttl / 3
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
}

// Normalized fills missing fields and repairs unsafe retention relationships.
func (c ShardedStreamRelayConfig) Normalized() ShardedStreamRelayConfig {
	return c.withDefaults()
}

// Validate checks the retention relationship that keeps trimming outside the
// replay window while allowing idle stream keys to expire safely.
func (c ShardedStreamRelayConfig) Validate() error {
	if c.ReplayGrace <= 0 {
		return errors.New("ReplayGrace must be positive")
	}
	if c.TrimHorizon <= c.ReplayGrace {
		return fmt.Errorf("TrimHorizon (%s) must be greater than ReplayGrace (%s)", c.TrimHorizon, c.ReplayGrace)
	}
	if c.StreamTTL < c.TrimHorizon {
		return fmt.Errorf("StreamTTL (%s) must be at least TrimHorizon (%s)", c.StreamTTL, c.TrimHorizon)
	}
	if c.TTLRefreshInterval <= 0 || c.TTLRefreshInterval >= c.StreamTTL {
		return fmt.Errorf("TTLRefreshInterval (%s) must be positive and less than StreamTTL (%s)", c.TTLRefreshInterval, c.StreamTTL)
	}
	if c.MaintenanceInterval <= 0 || c.MaintenanceInterval >= c.StreamTTL {
		return fmt.Errorf("MaintenanceInterval (%s) must be positive and less than StreamTTL (%s)", c.MaintenanceInterval, c.StreamTTL)
	}
	return nil
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
	now      func() time.Time
	ttl      *streamTTLRefresher

	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup

	streamSeen       []atomic.Bool
	streamGeneration []atomic.Uint64

	daemonRuntime DaemonRuntimeDeliverer
	wecomOutbound WecomOutboundDeliverer
}

func NewShardedStreamRelay(hub *Hub, writeRDB, readRDB *redis.Client, config ShardedStreamRelayConfig) *ShardedStreamRelay {
	if readRDB == nil {
		readRDB = writeRDB
	}
	config = config.withDefaults()
	return &ShardedStreamRelay{
		hub:              hub,
		writeRDB:         writeRDB,
		readRDB:          readRDB,
		nodeID:           ulid.Make().String(),
		config:           config,
		now:              time.Now,
		ttl:              newStreamTTLRefresher(config.StreamTTL, config.TTLRefreshInterval),
		streamSeen:       make([]atomic.Bool, config.Shards),
		streamGeneration: make([]atomic.Uint64, config.Shards),
	}
}

func (r *ShardedStreamRelay) NodeID() string { return r.nodeID }

func (r *ShardedStreamRelay) SetWecomOutboundDeliverer(d WecomOutboundDeliverer) {
	r.wecomOutbound = d
}

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

	r.wg.Add(2 + r.config.Shards)
	go func() {
		defer r.wg.Done()
		r.heartbeatLoop(ctx)
	}()
	go func() {
		defer r.wg.Done()
		r.retentionLoop(ctx)
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
	shard := r.shardFor(scopeType, scopeID)
	stream := ShardedStreamKey(shard)
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
	r.streamSeen[shard].Store(true)
	if r.config.StreamTTLEnabled {
		if err := r.ttl.refreshIfDue(ctx, r.writeRDB, stream); err != nil {
			r.recordRetentionError("PEXPIRE failed", err, "stream", stream)
		}
	}
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
	return streamMinID(r.now(), r.config.ReplayGrace)
}

func (r *ShardedStreamRelay) readShard(ctx context.Context, shard int) {
	stream := ShardedStreamKey(shard)
	// Start from a bounded lookback window, not "$", so that events
	// published while this pod was down are replayed. The grace window is
	// short enough that replay volume stays manageable, and downstream
	// consumers (daemon wakeups, client reconnects) are idempotent.
	lastID := r.replayStartID()
	generation := r.streamGeneration[shard].Load()
	for {
		if ctx.Err() != nil || r.isStopping() {
			return
		}
		if current := r.streamGeneration[shard].Load(); current != generation {
			lastID = r.replayStartID()
			generation = current
		}
		if !r.readShardOnce(ctx, shard, stream, &lastID) {
			return
		}
	}
}

func (r *ShardedStreamRelay) retentionLoop(ctx context.Context) {
	r.maintainStreams(ctx)
	ticker := time.NewTicker(r.config.MaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.maintainStreams(ctx)
		}
	}
}

func (r *ShardedStreamRelay) maintainStreams(ctx context.Context) {
	maintCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	minID := streamMinID(r.now(), r.config.TrimHorizon)
	withoutTTL := int64(0)

	for shard := 0; shard < r.config.Shards; shard++ {
		if maintCtx.Err() != nil {
			return
		}
		stream := ShardedStreamKey(shard)
		exists, err := r.writeRDB.Exists(maintCtx, stream).Result()
		if err != nil {
			r.recordRetentionError("EXISTS failed", err, "stream", stream)
			continue
		}
		if exists == 0 {
			r.updateStreamPresence(shard, false)
			M.ObserveRedisStream(stream, 0, 0, -2)
			continue
		}
		r.updateStreamPresence(shard, true)

		trimmed, err := r.writeRDB.XTrimMinID(maintCtx, stream, minID).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			r.recordRetentionError("XTRIM MINID failed", err, "stream", stream, "min_id", minID)
		} else if trimmed > 0 {
			M.RedisRelayStreamTrimmedTotal.Add(trimmed)
		}

		ttl, err := r.ttl.reconcileTTL(maintCtx, r.writeRDB, stream, r.config.StreamTTLEnabled)
		if r.config.StreamTTLEnabled && ttl == -1 {
			withoutTTL++
		}
		if err != nil {
			r.recordRetentionError("stream TTL repair failed", err, "stream", stream)
		}

		length, err := r.writeRDB.XLen(maintCtx, stream).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			r.recordRetentionError("XLEN failed", err, "stream", stream)
			continue
		}
		memoryBytes, err := r.writeRDB.MemoryUsage(maintCtx, stream).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			r.recordRetentionError("MEMORY USAGE failed", err, "stream", stream)
			memoryBytes = 0
		}
		M.ObserveRedisStream(stream, length, memoryBytes, redisTTLMillis(ttl))
	}
	M.SetRedisStreamsWithoutTTL("sharded", withoutTTL)
	r.observeRedisServer(maintCtx)
}

func (r *ShardedStreamRelay) updateStreamPresence(shard int, exists bool) {
	if exists {
		r.streamSeen[shard].Store(true)
		return
	}
	if r.streamSeen[shard].Swap(false) {
		r.streamGeneration[shard].Add(1)
		r.ttl.forget(ShardedStreamKey(shard))
		M.RedisRelayStreamMissingTotal.Add(1)
	}
}

func (r *ShardedStreamRelay) observeRedisServer(ctx context.Context) {
	memoryInfo, err := r.writeRDB.Info(ctx, "memory").Result()
	if err == nil {
		if used, ok := redisInfoInt64(memoryInfo, "used_memory"); ok {
			M.RedisUsedMemoryBytes.Store(used)
		}
		if max, ok := redisInfoInt64(memoryInfo, "maxmemory"); ok {
			M.RedisMaxMemoryBytes.Store(max)
		}
	}
	statsInfo, err := r.writeRDB.Info(ctx, "stats").Result()
	if err == nil {
		if evicted, ok := redisInfoInt64(statsInfo, "evicted_keys"); ok {
			M.RedisEvictedKeys.Store(evicted)
		}
	}
}

func (r *ShardedStreamRelay) recordRetentionError(message string, err error, attrs ...any) {
	M.RedisRelayRetentionErrors.Add(1)
	M.SetRedisLastError(err.Error())
	attrs = append([]any{"error", err}, attrs...)
	slog.Warn("realtime/sharded-redis: "+message, attrs...)
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
	deliverEnvelope(r.hub, r.daemonRuntime, r.wecomOutbound, ev)
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
