package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	mcpProbeKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":mcp_probe:req:"
	mcpProbePendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":mcp_probe:pending:"
	mcpProbeRedisPopMaxRetries = 5
)

func mcpProbeKey(id string) string               { return mcpProbeKeyPrefix + id }
func mcpProbePendingKey(runtimeID string) string { return mcpProbePendingPrefix + runtimeID }

// RedisMcpProbeStore stores in-flight MCP probes so every API node agrees.
type RedisMcpProbeStore struct {
	rdb *redis.Client
}

func NewRedisMcpProbeStore(rdb *redis.Client) *RedisMcpProbeStore {
	return &RedisMcpProbeStore{rdb: rdb}
}

type redisMcpProbeEnvelope struct {
	Public       *McpProbeRequest `json:"r"`
	RunStartedAt *time.Time       `json:"s,omitempty"`
}

func (s *RedisMcpProbeStore) marshalRequest(req *McpProbeRequest) ([]byte, error) {
	data, err := json.Marshal(redisMcpProbeEnvelope{Public: req, RunStartedAt: req.RunStartedAt})
	if err != nil {
		return nil, fmt.Errorf("marshal mcp probe request: %w", err)
	}
	return data, nil
}

func (s *RedisMcpProbeStore) unmarshalRequest(raw []byte) (*McpProbeRequest, error) {
	var env redisMcpProbeEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode mcp probe request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode mcp probe request: missing payload")
	}
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

func (s *RedisMcpProbeStore) persistRequest(ctx context.Context, req *McpProbeRequest) error {
	data, err := s.marshalRequest(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, mcpProbeKey(req.ID), data, mcpProbeStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist mcp probe request: %w", err)
	}
	return nil
}

func (s *RedisMcpProbeStore) loadRequest(ctx context.Context, id string) (*McpProbeRequest, error) {
	raw, err := s.rdb.Get(ctx, mcpProbeKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mcp probe request: %w", err)
	}
	req, err := s.unmarshalRequest(raw)
	if err != nil {
		return nil, err
	}
	if applyMcpProbeTimeout(req, time.Now()) {
		if err := s.persistRequest(ctx, req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, mcpProbePendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisMcpProbeStore) Create(ctx context.Context, in McpProbeCreate) (*McpProbeRequest, error) {
	now := time.Now()
	req := &McpProbeRequest{
		ID:          randomID(),
		WorkspaceID: in.WorkspaceID,
		ServerID:    in.ServerID,
		RuntimeID:   in.RuntimeID,
		RuntimeName: in.RuntimeName,
		Status:      McpProbePending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}
	requestKey := mcpProbeKey(req.ID)
	pendingKey := mcpProbePendingKey(in.RuntimeID)
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, requestKey, data, mcpProbeStoreRetention)
	pipe.ZAdd(ctx, pendingKey, redis.Z{Score: float64(now.UnixNano()), Member: req.ID})
	pipe.Expire(ctx, pendingKey, mcpProbeStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.rdb.Del(ctx, requestKey).Err()
		_ = s.rdb.ZRem(ctx, pendingKey, req.ID).Err()
		return nil, fmt.Errorf("persist mcp probe request: %w", err)
	}
	return req, nil
}

func (s *RedisMcpProbeStore) Get(ctx context.Context, id string) (*McpProbeRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisMcpProbeStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, mcpProbePendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisMcpProbeStore) PopPending(ctx context.Context, runtimeID string) (*McpProbeRequest, error) {
	pendingKey := mcpProbePendingKey(runtimeID)
	for attempt := 0; attempt < mcpProbeRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]
		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || req.Status != McpProbePending {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		now := time.Now()
		req.Status = McpProbeRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}
		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, mcpProbeKey(id)},
			id, data, int(mcpProbeStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisMcpProbeStore) Complete(ctx context.Context, id string, tools []string, elapsedMs int64) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = McpProbeCompleted
	req.Tools = append([]string(nil), tools...)
	req.ElapsedMs = elapsedMs
	req.ErrorCode = ""
	req.Error = ""
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}

func (s *RedisMcpProbeStore) Fail(ctx context.Context, id string, code, errMsg string, elapsedMs int64) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = McpProbeFailed
	req.ErrorCode = sanitizeMcpProbeErrorCode(code)
	req.Error = sanitizeMcpProbeError(errMsg)
	req.ElapsedMs = elapsedMs
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}
