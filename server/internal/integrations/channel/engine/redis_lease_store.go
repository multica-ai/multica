package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
)

const redisLeaseKeyPrefix = "multica:channel-lease:v1:"

var leaseNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const redisTryAcquireLeaseSource = `
local current = redis.call('GET', KEYS[1])
if (not current) or current == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
  return 1
end
return 0
`

const redisRenewLeaseSource = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`

const redisReleaseLeaseSource = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`

var redisTryAcquireLease = redis.NewScript(redisTryAcquireLeaseSource)
var redisRenewLease = redis.NewScript(redisRenewLeaseSource)
var redisReleaseLease = redis.NewScript(redisReleaseLeaseSource)

// RedisLeaseStore implements token-fenced channel leases in a dedicated Redis
// client/pool. Every mutation is a single Lua operation, so compare + expiry
// update/delete cannot be interleaved by another replica.
type RedisLeaseStore struct {
	client    *redis.Client
	namespace string
}

func NewRedisLeaseStore(client *redis.Client, namespace string) (*RedisLeaseStore, error) {
	if client == nil {
		return nil, errors.New("channel lease redis client is nil")
	}
	if !leaseNamespacePattern.MatchString(namespace) {
		return nil, errors.New("CHANNEL_WS_LEASE_NAMESPACE must match [A-Za-z0-9._-]+")
	}
	return &RedisLeaseStore{client: client, namespace: namespace}, nil
}

// Ready verifies connectivity and preloads every lease script. Redis-backed
// startup is fail-closed: callers must not start the Supervisor if this fails.
func (s *RedisLeaseStore) Ready(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	for _, item := range []struct {
		name   string
		script *redis.Script
	}{
		{name: "try_acquire", script: redisTryAcquireLease},
		{name: "renew", script: redisRenewLease},
		{name: "release", script: redisReleaseLease},
	} {
		if err := item.script.Load(ctx, s.client).Err(); err != nil {
			return fmt.Errorf("load %s script: %w", item.name, err)
		}
	}
	readyKey := redisLeaseKeyPrefix + s.namespace + ":__ready__:" + newNodeID()
	const readyToken = "readiness-check"
	for _, item := range []struct {
		name   string
		script *redis.Script
		args   []any
	}{
		{name: "try_acquire", script: redisTryAcquireLease, args: []any{readyToken, int64(5000)}},
		{name: "renew", script: redisRenewLease, args: []any{readyToken, int64(5000)}},
		{name: "release", script: redisReleaseLease, args: []any{readyToken}},
	} {
		result, err := item.script.Run(ctx, s.client, []string{readyKey}, item.args...).Int64()
		if err != nil {
			return fmt.Errorf("execute %s script: %w", item.name, err)
		}
		if result != 1 {
			return fmt.Errorf("execute %s script: unexpected result %d", item.name, result)
		}
	}
	return nil
}

func (s *RedisLeaseStore) ListHeldWSLeases(ctx context.Context, ids []pgtype.UUID) (map[string]struct{}, error) {
	held := make(map[string]struct{}, len(ids))
	if len(ids) == 0 {
		return held, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.key(id)
	}
	// RedisLeaseStore intentionally takes *redis.Client (standalone/sentinel),
	// not ClusterClient. A future cluster-mode implementation must group keys by
	// hash slot or pipeline individual GETs; cross-slot MGET is not valid.
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		if value != nil {
			held[uuidString(ids[i])] = struct{}{}
		}
	}
	return held, nil
}

func (s *RedisLeaseStore) TryAcquireWSLease(ctx context.Context, arg AcquireLeaseParams) error {
	return s.runLeaseScript(ctx, redisTryAcquireLease, arg)
}

func (s *RedisLeaseStore) RenewWSLease(ctx context.Context, arg AcquireLeaseParams) error {
	return s.runLeaseScript(ctx, redisRenewLease, arg)
}

func (s *RedisLeaseStore) ReleaseWSLease(ctx context.Context, arg ReleaseLeaseParams) error {
	_, err := redisReleaseLease.Run(ctx, s.client, []string{s.key(arg.ID)}, arg.Token).Int64()
	if err != nil {
		return err
	}
	// A stale token returns 0 and is an intentional fenced no-op.
	return nil
}

func (s *RedisLeaseStore) runLeaseScript(ctx context.Context, script *redis.Script, arg AcquireLeaseParams) error {
	ttlMillis := arg.TTL.Milliseconds()
	if ttlMillis <= 0 {
		return errors.New("channel lease TTL must be at least 1ms")
	}
	result, err := script.Run(ctx, s.client, []string{s.key(arg.ID)}, arg.Token, ttlMillis).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrLeaseNotAcquired
	}
	return nil
}

func (s *RedisLeaseStore) key(id pgtype.UUID) string {
	return redisLeaseKeyPrefix + s.namespace + ":" + uuidString(id)
}

var _ LeaseStore = (*RedisLeaseStore)(nil)
