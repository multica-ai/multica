package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed ModelCatalogCache so every API replica shares one warm model
// catalog per runtime. Without it each node would keep its own map and a user
// bouncing between replicas behind a load balancer would still pay the full
// daemon round trip on most opens (same multi-node reasoning as
// RedisModelListStore).
//
// Key layout — one plain string key per runtime, TTL = serve window, so
// expiry is Redis's job and there is nothing to sweep:
//
//	mul:runtime_model_catalog:<runtime_id> → JSON-encoded ModelCatalogSnapshot
//
// No hash tag: every operation here is single-key, so cluster slot co-location
// buys nothing (unlike the Lua-scripted pending queues).
const modelCatalogKeyPrefix = "mul:runtime_model_catalog:"

func modelCatalogKey(runtimeID string) string { return modelCatalogKeyPrefix + runtimeID }

// RedisModelCatalogCache is the multi-node implementation of ModelCatalogCache.
type RedisModelCatalogCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRedisModelCatalogCache(rdb *redis.Client) *RedisModelCatalogCache {
	return &RedisModelCatalogCache{rdb: rdb, ttl: modelCatalogServeWindow}
}

func (c *RedisModelCatalogCache) Get(ctx context.Context, runtimeID string) (*ModelCatalogSnapshot, error) {
	if runtimeID == "" {
		return nil, nil
	}
	raw, err := c.rdb.Get(ctx, modelCatalogKey(runtimeID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model catalog: %w", err)
	}
	var snapshot ModelCatalogSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		// A snapshot we cannot decode is indistinguishable from a cold cache;
		// drop it so the next Put replaces the bad value.
		c.rdb.Del(ctx, modelCatalogKey(runtimeID))
		return nil, fmt.Errorf("decode model catalog: %w", err)
	}
	// Guard against a snapshot whose TTL outlived the serve window (TTL is set
	// from the same constant, but a config change or a manual write could
	// diverge) — the age check is the contract, not the TTL.
	if snapshot.StoredAt.IsZero() || time.Since(snapshot.StoredAt) > c.ttl {
		return nil, nil
	}
	return &snapshot, nil
}

func (c *RedisModelCatalogCache) Put(ctx context.Context, runtimeID string, models []ModelEntry, unavailable []UnavailableModelEntry, supported bool) error {
	// fallback=false: ReportModelListResult refuses to Put a fallback catalog
	// at all, so anything reaching a cache backend is a real discovery result.
	if runtimeID == "" || !cacheableModelCatalog(models, supported, false) {
		return nil
	}
	snapshot := ModelCatalogSnapshot{
		RuntimeID:         runtimeID,
		Models:            models,
		UnavailableModels: unavailable,
		Supported:         supported,
		StoredAt:          time.Now(),
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal model catalog: %w", err)
	}
	if err := c.rdb.Set(ctx, modelCatalogKey(runtimeID), data, c.ttl).Err(); err != nil {
		return fmt.Errorf("persist model catalog: %w", err)
	}
	return nil
}

func (c *RedisModelCatalogCache) Invalidate(ctx context.Context, runtimeID string) error {
	if runtimeID == "" {
		return nil
	}
	if err := c.rdb.Del(ctx, modelCatalogKey(runtimeID)).Err(); err != nil {
		return fmt.Errorf("invalidate model catalog: %w", err)
	}
	return nil
}
