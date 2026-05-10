package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SingleUserEmail is the deterministic email used for the auto-created local
// user when MULTICA_SINGLE_USER=true. Pinning the value here (instead of
// reading it from another env var) keeps the user identity stable across
// restarts and forks of this codebase, so existing data keeps working when
// operators flip the flag on.
const SingleUserEmail = "local@multica.local"

// SingleUserName is the display name applied to the auto-created user.
const SingleUserName = "Local User"

// SingleUserMode reports whether MULTICA_SINGLE_USER is enabled. The check is
// case-insensitive and accepts the common truthy spellings ("true", "1",
// "yes", "on") so operators do not have to remember an exact form.
//
// Default is false (existing multi-user login flow). Read on every call so
// tests can flip the env var with t.Setenv without having to reload state.
func SingleUserMode() bool {
	v := strings.TrimSpace(os.Getenv("MULTICA_SINGLE_USER"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// singleUserCache caches the resolved single-user UUID inside the process so
// the auth middleware does not have to hit the database on every request once
// the user has been resolved or created. The cached value never expires —
// the local user exists for the lifetime of the deployment and its UUID is
// stable.
type singleUserCache struct {
	mu sync.RWMutex
	id string
}

var singleUserState singleUserCache

// EnsureSingleUser returns the UUID (string) of the auto-created local user,
// creating it on the first call if it does not exist. Safe to call
// concurrently — the resolved UUID is cached after the first successful
// lookup and subsequent callers re-use it without touching Postgres.
//
// queries must not be nil; the function returns an error when it is, which
// upstream callers (auth middleware) should treat the same as any other
// auth-resolution failure (HTTP 500-ish).
func EnsureSingleUser(ctx context.Context, queries *db.Queries) (string, error) {
	if queries == nil {
		return "", errors.New("single-user mode: nil queries")
	}

	singleUserState.mu.RLock()
	if id := singleUserState.id; id != "" {
		singleUserState.mu.RUnlock()
		return id, nil
	}
	singleUserState.mu.RUnlock()

	singleUserState.mu.Lock()
	defer singleUserState.mu.Unlock()
	// Re-check after grabbing the write lock — another goroutine may have
	// resolved the user while we were waiting.
	if id := singleUserState.id; id != "" {
		return id, nil
	}

	user, err := queries.GetUserByEmail(ctx, SingleUserEmail)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		created, cerr := queries.CreateUser(ctx, db.CreateUserParams{
			Name:  SingleUserName,
			Email: SingleUserEmail,
		})
		if cerr != nil {
			// Race: another node/process created the row between our
			// SELECT and INSERT (only possible across processes since we
			// hold the write lock locally). Fall back to a second SELECT.
			user, err = queries.GetUserByEmail(ctx, SingleUserEmail)
			if err != nil {
				return "", cerr
			}
		} else {
			user = created
		}
	}

	id := util.UUIDToString(user.ID)
	singleUserState.id = id
	return id, nil
}

// resetSingleUserCacheForTesting clears the cached UUID. Intended for tests
// that toggle MULTICA_SINGLE_USER and need a clean state. Not exported in the
// usual sense — it is exported (capitalised) only so the middleware test in a
// sibling package can call it. Production code must not invoke this.
func ResetSingleUserCacheForTesting() {
	singleUserState.mu.Lock()
	singleUserState.id = ""
	singleUserState.mu.Unlock()
}
