package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

const directoryKeyPrefix = "mul:" + runtimePendingRedisHashTag + ":directories:"

func directoryKey(id string) string               { return directoryKeyPrefix + id }
func directoryPendingKey(runtimeID string) string { return directoryKeyPrefix + "pending:" + runtimeID }

var createDirectoryRequestScript = redis.NewScript(`
if redis.call('ZCARD',KEYS[2]) >= tonumber(ARGV[4]) then return 0 end
redis.call('SET',KEYS[1],ARGV[1],'EX',tonumber(ARGV[2]))
redis.call('ZADD',KEYS[2],ARGV[3],ARGV[5])
redis.call('EXPIRE',KEYS[2],tonumber(ARGV[2])*2)
return 1
`)
var compareDirectoryRequestScript = redis.NewScript(`
if redis.call('GET',KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET',KEYS[1],ARGV[2],'EX',tonumber(ARGV[3]))
return 1
`)

type RedisDirectoryListStore struct{ rdb redis.UniversalClient }

func NewRedisDirectoryListStore(rdb redis.UniversalClient) *RedisDirectoryListStore {
	return &RedisDirectoryListStore{rdb: rdb}
}
func (s *RedisDirectoryListStore) Create(ctx context.Context, runtimeID, daemonID, path string) (*RuntimeDirectoryListRequest, error) {
	req := newDirectoryRequest(runtimeID, daemonID, path)
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	created, err := createDirectoryRequestScript.Run(ctx, s.rdb, []string{directoryKey(req.ID), directoryPendingKey(runtimeID)}, raw, int(directoryRetention.Seconds()), req.CreatedAt.UnixNano(), directoryStoreLimit, req.ID).Int()
	if err != nil {
		return nil, fmt.Errorf("create directory request: %w", err)
	}
	if created == 0 {
		return nil, errors.New("directory request queue is full")
	}
	return req, nil
}
func (s *RedisDirectoryListStore) load(ctx context.Context, id string) (*RuntimeDirectoryListRequest, []byte, error) {
	raw, err := s.rdb.Get(ctx, directoryKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var req RuntimeDirectoryListRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, nil, err
	}
	return &req, raw, nil
}
func (s *RedisDirectoryListStore) compare(ctx context.Context, id string, before []byte, req *RuntimeDirectoryListRequest) (bool, error) {
	after, err := json.Marshal(req)
	if err != nil {
		return false, err
	}
	changed, err := compareDirectoryRequestScript.Run(ctx, s.rdb, []string{directoryKey(id)}, before, after, int(directoryRetention.Seconds())).Int()
	return changed == 1, err
}
func (s *RedisDirectoryListStore) Get(ctx context.Context, id string) (*RuntimeDirectoryListRequest, error) {
	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		req, raw, err := s.load(ctx, id)
		if err != nil || req == nil {
			return req, err
		}
		if !applyDirectoryTimeout(req, time.Now()) {
			return req, nil
		}
		changed, err := s.compare(ctx, id, raw, req)
		if err != nil {
			return nil, err
		}
		if changed {
			if err := s.rdb.ZRem(ctx, directoryPendingKey(req.RuntimeID), id).Err(); err != nil {
				return nil, err
			}
			return req, nil
		}
	}
	return nil, errors.New("directory request changed concurrently")
}
func (s *RedisDirectoryListStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	count, err := s.rdb.ZCard(ctx, directoryPendingKey(runtimeID)).Result()
	return count > 0, err
}
func (s *RedisDirectoryListStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeDirectoryListRequest, error) {
	pendingKey := directoryPendingKey(runtimeID)
	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil || len(ids) == 0 {
			return nil, err
		}
		id := ids[0]
		req, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || req.RuntimeID != runtimeID || req.Status != "pending" {
			if err := s.rdb.ZRem(ctx, pendingKey, id).Err(); err != nil {
				return nil, err
			}
			continue
		}
		req.Status = "running"
		req.UpdatedAt = time.Now()
		raw, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		claimed, err := claimPendingScript.Run(ctx, s.rdb, []string{pendingKey, directoryKey(id)}, id, raw, int(directoryRetention.Seconds())).Int()
		if err != nil {
			return nil, err
		}
		if claimed == 1 {
			return req, nil
		}
	}
	return nil, nil
}
func (s *RedisDirectoryListStore) Finish(ctx context.Context, id string, result *protocol.DirectoryListing, errMsg string) error {
	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		req, raw, err := s.load(ctx, id)
		if err != nil || req == nil {
			return err
		}
		if directoryRequestTerminal(req) {
			return nil
		}
		if !applyDirectoryTimeout(req, time.Now()) {
			finishDirectoryRequest(req, result, errMsg)
		}
		changed, err := s.compare(ctx, id, raw, req)
		if err != nil {
			return err
		}
		if changed {
			return nil
		}
	}
	return errors.New("directory request changed concurrently")
}
