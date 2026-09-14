package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
)

// Redis-backed InstallSessionStore — the multi-replica implementation.
//
// The record is split across two keys so the first-writer-wins contract
// falls out of Redis primitives instead of a read-modify-write:
//
//	mul:lark:install:<id>       immutable half, written once by Create
//	mul:lark:install:<id>:done  terminal half, written with SetNX
//
// SetNX is the whole concurrency story: the expiry deadline and a poll
// result race on the same key and the loser's write simply does not land,
// so whichever outcome the user was shown first is the one that sticks. A
// single-key CAS would need either a Lua script or an optimistic retry
// loop to say the same thing.
const (
	installSessionKeyPrefix = "mul:lark:install:"
	installSessionDoneSufix = ":done"
)

func installSessionKey(id string) string { return installSessionKeyPrefix + id }
func installSessionDoneKey(id string) string {
	return installSessionKeyPrefix + id + installSessionDoneSufix
}

type RedisInstallSessionStore struct {
	rdb redis.UniversalClient
}

func NewRedisInstallSessionStore(rdb redis.UniversalClient) *RedisInstallSessionStore {
	return &RedisInstallSessionStore{rdb: rdb}
}

// redisInstallSession is the immutable half: everything Create knows and
// nothing that changes afterwards. Short JSON keys because these records
// are written once per bind attempt and read every ~5s.
type redisInstallSession struct {
	WorkspaceID string    `json:"w"`
	InitiatorID string    `json:"i"`
	ExpiresAt   time.Time `json:"e"`
}

// redisInstallOutcome is the terminal half, written at most once.
type redisInstallOutcome struct {
	Status         string `json:"s"`
	InstallationID string `json:"n,omitempty"`
	ErrorReason    string `json:"r,omitempty"`
	ErrorMessage   string `json:"m,omitempty"`
}

func (s *RedisInstallSessionStore) Create(ctx context.Context, state InstallSessionState, ttl time.Duration) error {
	data, err := json.Marshal(redisInstallSession{
		WorkspaceID: uuidString(state.WorkspaceID),
		InitiatorID: uuidString(state.InitiatorID),
		ExpiresAt:   state.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("lark: marshal install session: %w", err)
	}
	if err := s.rdb.Set(ctx, installSessionKey(state.ID), data, ttl).Err(); err != nil {
		return fmt.Errorf("lark: persist install session: %w", err)
	}
	return nil
}

func (s *RedisInstallSessionStore) Get(ctx context.Context, workspaceID pgtype.UUID, id string) (InstallSessionState, error) {
	raw, err := s.rdb.Get(ctx, installSessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}
	if err != nil {
		// Surface the failure rather than degrading to "not found": a
		// Redis blip must not tell the dialog the session is gone, which
		// is a terminal state it cannot recover from.
		return InstallSessionState{}, fmt.Errorf("lark: load install session: %w", err)
	}
	var rec redisInstallSession
	if err := json.Unmarshal(raw, &rec); err != nil {
		return InstallSessionState{}, fmt.Errorf("lark: decode install session: %w", err)
	}
	if rec.WorkspaceID != uuidString(workspaceID) {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}

	state := InstallSessionState{
		ID:          id,
		WorkspaceID: workspaceID,
		Status:      RegistrationStatusPending,
		ExpiresAt:   rec.ExpiresAt,
	}
	if err := state.InitiatorID.Scan(rec.InitiatorID); err != nil {
		return InstallSessionState{}, fmt.Errorf("lark: decode install session initiator: %w", err)
	}

	doneRaw, err := s.rdb.Get(ctx, installSessionDoneKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return state, nil
	}
	if err != nil {
		return InstallSessionState{}, fmt.Errorf("lark: load install session outcome: %w", err)
	}
	var outcome redisInstallOutcome
	if err := json.Unmarshal(doneRaw, &outcome); err != nil {
		return InstallSessionState{}, fmt.Errorf("lark: decode install session outcome: %w", err)
	}
	state.Status = RegistrationSessionStatus(outcome.Status)
	state.ErrorReason = outcome.ErrorReason
	state.ErrorMessage = outcome.ErrorMessage
	if outcome.InstallationID != "" {
		if err := state.InstallationID.Scan(outcome.InstallationID); err != nil {
			return InstallSessionState{}, fmt.Errorf("lark: decode install session installation id: %w", err)
		}
	}
	return state, nil
}

func (s *RedisInstallSessionStore) MarkTerminal(ctx context.Context, id string, outcome InstallSessionOutcome, ttl time.Duration) error {
	installationID := ""
	if outcome.InstallationID.Valid {
		installationID = uuidString(outcome.InstallationID)
	}
	data, err := json.Marshal(redisInstallOutcome{
		Status:         string(outcome.Status),
		InstallationID: installationID,
		ErrorReason:    outcome.ErrorReason,
		ErrorMessage:   outcome.ErrorMessage,
	})
	if err != nil {
		return fmt.Errorf("lark: marshal install session outcome: %w", err)
	}
	// SetNX: the first terminal write wins, later ones are no-ops.
	won, err := s.rdb.SetNX(ctx, installSessionDoneKey(id), data, ttl).Result()
	if err != nil {
		return fmt.Errorf("lark: record install session outcome: %w", err)
	}
	if !won {
		return nil
	}
	// Extend the immutable half to match, so the dialog can still read the
	// full record during the terminal window. Best-effort: if the base key
	// already expired there is nothing to hold open, and Get would report
	// not-found either way.
	if err := s.rdb.Expire(ctx, installSessionKey(id), ttl).Err(); err != nil {
		return fmt.Errorf("lark: extend install session retention: %w", err)
	}
	return nil
}
