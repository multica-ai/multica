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
	r.PublishWithID(scopeType, scopeID, "", message, ulid.Make().String())
}

func (r *ShardedStreamRelay) BroadcastToWorkspace(workspaceID string, message []byte) {
	r.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (r *ShardedStreamRelay) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	r.PublishWithID(ScopeUser, userID, exclude, message, ulid.Make().String())
}

func (r *ShardedStreamRelay) Broadcast(message []byte) {
	r.PublishWithID("global", "all", "", message, ulid.Make().String())
}

func (r *ShardedStreamRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) {
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
		return
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
}

func (r *ShardedStreamRelay) shardFor(scopeType, scopeID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(scopeType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(scopeID))
	return int(h.Sum32() % uint32(r.config.Shards))
}

func (r *ShardedStreamRelay) readShard(ctx context.Context, shard int) {
	stream := ShardedStreamKey(shard)
	lastID := "$"
	for {
		if ctx.Err() != nil || r.isStopping() {
			return
		}

		readCtx, cancel := context.WithTimeout(ctx, r.config.ReadBlock+time.Second)
		res, err := r.readRDB.XRead(readCtx, &redis.XReadArgs{
			Streams: []string{stream, lastID},
			Count:   r.config.ReadCount,
			Block:   r.config.ReadBlock,
		}).Result()
		cancel()

		if errors.Is(err, redis.Nil) || (err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))) {
			continue
		}
		if err != nil {
			M.RedisXReadErrors.Add(1)
			M.SetRedisLastError(err.Error())
			slog.Warn("realtime/sharded-redis: XREAD failed", "error", err, "shard", shard, "stream", stream)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		for _, s := range res {
			for _, msg := range s.Messages {
				lastID = msg.ID
				M.RedisXReadTotal.Add(1)
				r.deliverMessage(msg)
			}
		}
	}
}

func (r *ShardedStreamRelay) deliverMessage(msg redis.XMessage) {
	ev, ok := envelopeFromXMessage(msg)
	if !ok || ev.Scope == "" || ev.ScopeID == "" {
		return
	}
	deliverEnvelope(r.hub, ev)
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
