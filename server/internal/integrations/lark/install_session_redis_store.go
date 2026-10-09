package lark

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
)

// Redis-backed InstallSessionStore — the multi-replica implementation.
//
// The whole session is ONE hash key, and both writes go through Lua.
// An earlier version split the record across a base key and a SetNX'd
// terminal key, which made the terminal status and the retention window
// two separately-failing round trips: a SetNX that landed followed by a
// failed EXPIRE left the status stored under the base key's original
// (much longer) TTL, and the retry could not repair it because SetNX then
// reported "someone else won". A session that finished early lost its
// terminal field first and read as `pending` again.
//
// Two properties remove that failure mode, and it is worth being precise
// about which does the work — Lua atomicity is NOT one of them. Redis
// does not roll back a script that fails partway through
// (https://redis.io/blog/you-dont-need-transaction-rollbacks-in-redis/),
// so "atomic" here buys isolation from other clients, not all-or-nothing
// recovery. What actually fixes it:
//
//  1. One key. Status and retention share a single expiry, so the
//     terminal outcome cannot outlive — or be outlived by — the record
//     that carries it.
//  2. A retry that re-asserts both. The terminal script keeps the first
//     status and re-runs EXPIRE unconditionally, so replaying it after a
//     lost or failed response converges on the right state instead of
//     reporting "someone else won" and leaving retention stale.
const installSessionKeyPrefix = "mul:lark:install:"

func installSessionKey(id string) string { return installSessionKeyPrefix + id }

// Hash fields. Short because these are written once per bind attempt and
// read every ~5s: w/i/e are the immutable half, s/n/r/m the terminal one.
const (
	installFieldWorkspace      = "w"
	installFieldInitiator      = "i"
	installFieldExpiresAt      = "e"
	installFieldStatus         = "s"
	installFieldInstallationID = "n"
	installFieldErrorReason    = "r"
	installFieldErrorMessage   = "m"
)

// installSessionCreateScript writes the immutable half and its retention
// in one step, so a failure can never leave a key with no TTL behind.
var installSessionCreateScript = redis.NewScript(`
redis.call('HSET', KEYS[1], 'w', ARGV[1], 'i', ARGV[2], 'e', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1
`)

// installSessionTerminalScript is idempotent AND self-repairing.
//
// The status is written only if no terminal status is present yet, which
// is the first-writer-wins guarantee: when the expiry deadline and a poll
// result race, the user keeps whichever outcome they were already shown.
// The EXPIRE runs UNCONDITIONALLY — including for the caller that lost —
// which is what makes a replay converge. A client that never saw the
// reply to a script the server DID run re-sends it, hits the "already
// terminal" branch, and still moves retention onto the terminal window
// rather than reporting success and leaving it stale.
//
// Returns 0 when the session is gone (expired out or never existed):
// there is nothing to record and nothing a retry could fix.
var installSessionTerminalScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return 0
end
if redis.call('HGET', KEYS[1], 's') == false then
    redis.call('HSET', KEYS[1], 's', ARGV[1], 'n', ARGV[2], 'r', ARGV[3], 'm', ARGV[4])
end
redis.call('EXPIRE', KEYS[1], ARGV[5])
return 1
`)

type RedisInstallSessionStore struct {
	rdb redis.UniversalClient
}

func NewRedisInstallSessionStore(rdb redis.UniversalClient) *RedisInstallSessionStore {
	return &RedisInstallSessionStore{rdb: rdb}
}

// ttlSeconds rounds up: a sub-second TTL must not floor to 0, which Redis
// reads as "no expiry" and would leak the key forever.
func ttlSeconds(ttl time.Duration) int {
	secs := int(ttl / time.Second)
	if ttl%time.Second != 0 {
		secs++
	}
	if secs < 1 {
		secs = 1
	}
	return secs
}

func (s *RedisInstallSessionStore) Create(ctx context.Context, state InstallSessionState, ttl time.Duration) error {
	err := installSessionCreateScript.Run(ctx, s.rdb,
		[]string{installSessionKey(state.ID)},
		uuidString(state.WorkspaceID),
		uuidString(state.InitiatorID),
		state.ExpiresAt.UTC().Format(time.RFC3339Nano),
		ttlSeconds(ttl),
	).Err()
	if err != nil {
		return fmt.Errorf("lark: persist install session: %w", err)
	}
	return nil
}

func (s *RedisInstallSessionStore) Get(ctx context.Context, workspaceID pgtype.UUID, id string) (InstallSessionState, error) {
	fields, err := s.rdb.HGetAll(ctx, installSessionKey(id)).Result()
	if err != nil {
		// Surface the failure rather than degrading to not-found: not-found
		// is terminal in the dialog, so a Redis blip reported that way would
		// permanently kill a live scan.
		return InstallSessionState{}, fmt.Errorf("lark: load install session: %w", err)
	}
	// HGETALL on a missing key returns an empty map, not an error.
	if len(fields) == 0 {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}
	if fields[installFieldWorkspace] != uuidString(workspaceID) {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}

	state := InstallSessionState{
		ID:          id,
		WorkspaceID: workspaceID,
		Status:      RegistrationStatusPending,
	}
	if err := state.InitiatorID.Scan(fields[installFieldInitiator]); err != nil {
		return InstallSessionState{}, fmt.Errorf("lark: decode install session initiator: %w", err)
	}
	if raw := fields[installFieldExpiresAt]; raw != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return InstallSessionState{}, fmt.Errorf("lark: decode install session expiry: %w", err)
		}
		state.ExpiresAt = expiresAt
	}
	if status := fields[installFieldStatus]; status != "" {
		state.Status = RegistrationSessionStatus(status)
		state.ErrorReason = fields[installFieldErrorReason]
		state.ErrorMessage = fields[installFieldErrorMessage]
		if raw := fields[installFieldInstallationID]; raw != "" {
			if err := state.InstallationID.Scan(raw); err != nil {
				return InstallSessionState{}, fmt.Errorf("lark: decode install session installation id: %w", err)
			}
		}
	}
	return state, nil
}

func (s *RedisInstallSessionStore) MarkTerminal(ctx context.Context, id string, outcome InstallSessionOutcome, ttl time.Duration) error {
	installationID := ""
	if outcome.InstallationID.Valid {
		installationID = uuidString(outcome.InstallationID)
	}
	applied, err := installSessionTerminalScript.Run(ctx, s.rdb,
		[]string{installSessionKey(id)},
		string(outcome.Status),
		installationID,
		outcome.ErrorReason,
		outcome.ErrorMessage,
		ttlSeconds(ttl),
	).Int64()
	if err != nil {
		return fmt.Errorf("lark: record install session outcome: %w", err)
	}
	if applied == 0 {
		// The session is gone; no retry can bring it back.
		return ErrInstallSessionNotFound
	}
	return nil
}

var _ InstallSessionStore = (*RedisInstallSessionStore)(nil)
