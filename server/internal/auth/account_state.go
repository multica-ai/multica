package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrSessionRevoked is returned for a session token minted before the
// account's sessions were ended (see AccountState.TokenVersion).
var ErrSessionRevoked = errors.New("session revoked")

// AccountState is what auth needs to know about a user beyond a valid
// credential (MUL-10296).
type AccountState struct {
	// Suspended blocks every credential the user holds.
	Suspended bool `json:"s"`
	// TokenVersion is the session generation. A session token carrying an
	// older one was ended by an incident response and is refused.
	TokenVersion int32 `json:"v"`
}

// AccountStateLoader reads the current state from the database. A user that
// does not exist is not an error: it reads as the zero state.
type AccountStateLoader func(ctx context.Context, userID string) (AccountState, error)

const accountStatePrefix = "mul:auth:account:v1:"

// AccountStateCacheTTL bounds how long a cached state can outlive a write
// that failed to reach Redis. A successful write replaces the entry at once.
const AccountStateCacheTTL = time.Minute

// AccountStates answers AccountState for every authenticated request.
//
// With Redis it caches each state. A miss fills the entry with SETNX and a
// suspension overwrites it with SET, so a request that read the database
// before the suspension committed can never write its stale answer over the
// new one: either its fill lands first and is overwritten, or it finds the
// key taken. Without Redis every call reads the database.
//
// A nil *AccountStates allows everyone; it exists so tests and callers that
// have no database stay unchanged.
type AccountStates struct {
	rdb  redis.UniversalClient
	load AccountStateLoader
}

func NewAccountStates(rdb redis.UniversalClient, load AccountStateLoader) *AccountStates {
	if load == nil {
		return nil
	}
	return &AccountStates{rdb: rdb, load: load}
}

func accountStateKey(userID string) string { return accountStatePrefix + userID }

// Get returns the user's state. An error means the state is unknown, and the
// caller must refuse the request rather than guess.
func (a *AccountStates) Get(ctx context.Context, userID string) (AccountState, error) {
	if a == nil || userID == "" {
		return AccountState{}, nil
	}
	if a.rdb != nil {
		raw, err := a.rdb.Get(ctx, accountStateKey(userID)).Bytes()
		if err == nil {
			var state AccountState
			if json.Unmarshal(raw, &state) == nil {
				return state, nil
			}
		} else if !errors.Is(err, redis.Nil) {
			slog.Warn("account_state: get failed; falling back to DB", "error", err)
		}
	}
	state, err := a.load(ctx, userID)
	if err != nil {
		return AccountState{}, err
	}
	if a.rdb != nil {
		if raw, merr := json.Marshal(state); merr == nil {
			if err := a.rdb.SetNX(ctx, accountStateKey(userID), raw, AccountStateCacheTTL).Err(); err != nil {
				slog.Warn("account_state: fill failed", "error", err)
			}
		}
	}
	return state, nil
}

// Put records a state the caller has just committed, so every node sees it
// on its next request. Call it after the transaction commits.
func (a *AccountStates) Put(ctx context.Context, userID string, state AccountState) {
	if a == nil || a.rdb == nil || userID == "" {
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := a.rdb.Set(ctx, accountStateKey(userID), raw, AccountStateCacheTTL).Err(); err != nil {
		// The entry may still hold the old state; drop it so the next read
		// goes to the database, and let the TTL cover a failed delete too.
		slog.Warn("account_state: set failed; deleting", "error", err)
		_ = a.rdb.Del(ctx, accountStateKey(userID)).Err()
	}
}

// SessionTokenVersion reads the session generation from a session token's
// claims. Tokens minted before MUL-10296 carry none and read as 0.
func SessionTokenVersion(claims map[string]any) int32 {
	switch v := claims[TokenVersionClaim].(type) {
	case float64:
		return int32(v)
	case json.Number:
		n, _ := v.Int64()
		return int32(n)
	}
	return 0
}

// CheckSession reports why a session token of this generation must be refused,
// or nil.
func (s AccountState) CheckSession(tokenVersion int32) error {
	if s.Suspended {
		return ErrTemporarilyDisabledUser
	}
	if tokenVersion < s.TokenVersion {
		return ErrSessionRevoked
	}
	return nil
}
