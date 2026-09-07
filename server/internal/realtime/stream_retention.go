package realtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRelayStreamTrimHorizon         = 10 * time.Minute
	defaultRelayStreamTTL                 = 15 * time.Minute
	defaultRelayStreamTTLRefreshInterval  = 30 * time.Second
	defaultRelayStreamMaintenanceInterval = time.Minute
)

// StreamRetentionConfig is shared by sharded and legacy relay modes so one
// set of operator controls has the same meaning during a dual-mode rollout.
// TTL is deliberately opt-in: deploy the compatible code with TTL disabled,
// then enable it only after every replica can refresh or remove expirations.
type StreamRetentionConfig struct {
	StreamMaxLen        int64
	TrimHorizon         time.Duration
	StreamTTL           time.Duration
	TTLRefreshInterval  time.Duration
	MaintenanceInterval time.Duration
	StreamTTLEnabled    bool
}

// DefaultStreamRetentionConfig returns safe cross-mode retention defaults.
func DefaultStreamRetentionConfig() StreamRetentionConfig {
	return StreamRetentionConfig{
		StreamMaxLen:        defaultShardedRelayStreamMaxLen,
		TrimHorizon:         defaultRelayStreamTrimHorizon,
		StreamTTL:           defaultRelayStreamTTL,
		TTLRefreshInterval:  defaultRelayStreamTTLRefreshInterval,
		MaintenanceInterval: defaultRelayStreamMaintenanceInterval,
		StreamTTLEnabled:    false,
	}
}

func (c StreamRetentionConfig) withDefaults() StreamRetentionConfig {
	def := DefaultStreamRetentionConfig()
	if c.StreamMaxLen <= 0 {
		c.StreamMaxLen = def.StreamMaxLen
	}
	if c.TrimHorizon <= 0 {
		c.TrimHorizon = def.TrimHorizon
	}
	if c.StreamTTL < c.TrimHorizon {
		c.StreamTTL = c.TrimHorizon + defaultShardedRelayReplayGrace
	}
	if c.TTLRefreshInterval <= 0 || c.TTLRefreshInterval >= c.StreamTTL {
		c.TTLRefreshInterval = retentionSubinterval(c.StreamTTL, def.TTLRefreshInterval)
	}
	if c.MaintenanceInterval <= 0 || c.MaintenanceInterval >= c.StreamTTL {
		c.MaintenanceInterval = retentionSubinterval(c.StreamTTL, def.MaintenanceInterval)
	}
	return c
}

// streamTTLRefresher limits PEXPIRE calls on the publish path while ensuring
// active stream keys remain eligible for volatile-* eviction policies. A
// maintenance pass repairs any TTL that was missed after a partial failure.
type streamTTLRefresher struct {
	ttl          time.Duration
	refreshEvery time.Duration
	now          func() time.Time

	mu          sync.Mutex
	lastRefresh map[string]time.Time
}

func newStreamTTLRefresher(ttl, refreshEvery time.Duration) *streamTTLRefresher {
	return &streamTTLRefresher{
		ttl:          ttl,
		refreshEvery: refreshEvery,
		now:          time.Now,
		lastRefresh:  make(map[string]time.Time),
	}
}

func (r *streamTTLRefresher) refreshIfDue(ctx context.Context, client *redis.Client, key string) error {
	now := r.now()
	if !r.claimRefresh(key, now) {
		return nil
	}

	ok, err := client.PExpire(ctx, key, r.ttl).Result()
	if err != nil {
		r.releaseRefresh(key, now)
		return err
	}
	if !ok {
		r.releaseRefresh(key, now)
		return fmt.Errorf("stream %q disappeared before TTL refresh", key)
	}
	return nil
}

// repairMissingTTL assigns a TTL only when a stream exists without one. It
// intentionally does not refresh a healthy TTL, so an idle stream can expire.
func (r *streamTTLRefresher) repairMissingTTL(ctx context.Context, client *redis.Client, key string) (time.Duration, error) {
	return r.reconcileTTL(ctx, client, key, true)
}

// reconcileTTL repairs a missing TTL when enabled and removes any persisted
// TTL when disabled. The disabled path is the compatibility and rollback
// phase: once all new replicas have observed it, old binaries can keep writing
// without an inherited expiry deleting an active stream.
func (r *streamTTLRefresher) reconcileTTL(ctx context.Context, client *redis.Client, key string, enabled bool) (time.Duration, error) {
	ttl, err := client.PTTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if !enabled {
		r.forget(key)
		if ttl == -2 || ttl == -1 {
			return ttl, nil
		}
		ok, err := client.Persist(ctx, key).Result()
		if err != nil {
			return ttl, err
		}
		if !ok {
			return -2, nil
		}
		return -1, nil
	}
	switch ttl {
	case -2: // key does not exist
		return ttl, nil
	case -1: // key exists without expiry
		ok, err := client.PExpire(ctx, key, r.ttl).Result()
		if err != nil {
			return ttl, err
		}
		if !ok {
			return -2, nil
		}
		r.mu.Lock()
		r.lastRefresh[key] = r.now()
		r.mu.Unlock()
		return r.ttl, nil
	default:
		return ttl, nil
	}
}

func (r *streamTTLRefresher) forgetStale(before time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, refreshedAt := range r.lastRefresh {
		if refreshedAt.Before(before) {
			delete(r.lastRefresh, key)
		}
	}
}

func (r *streamTTLRefresher) forget(key string) {
	r.mu.Lock()
	delete(r.lastRefresh, key)
	r.mu.Unlock()
}

func (r *streamTTLRefresher) claimRefresh(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.lastRefresh[key]; ok && now.Sub(last) < r.refreshEvery {
		return false
	}
	r.lastRefresh[key] = now
	return true
}

func (r *streamTTLRefresher) releaseRefresh(key string, claimedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.lastRefresh[key]; ok && current.Equal(claimedAt) {
		delete(r.lastRefresh, key)
	}
}

func streamMinID(now time.Time, horizon time.Duration) string {
	millis := now.Add(-horizon).UnixMilli()
	if millis < 0 {
		millis = 0
	}
	return fmt.Sprintf("%d-0", millis)
}

func redisTTLMillis(ttl time.Duration) int64 {
	if ttl == -2 || ttl == -1 {
		return int64(ttl)
	}
	return ttl.Milliseconds()
}

func redisInfoInt64(info, key string) (int64, bool) {
	prefix := key + ":"
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
		return value, err == nil
	}
	return 0, false
}
