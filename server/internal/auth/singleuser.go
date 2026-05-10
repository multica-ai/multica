package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// Defaults for the auto-created workspace that BootstrapSingleUser creates
// when single-user mode starts up against an empty database. The slug is
// short, memorable, and not in reserved_slugs.json. The user can rename
// any of these from the workspace settings page once they're inside the
// app — these values exist purely to skip the onboarding wizard, not to
// be permanent.
const (
	SingleUserWorkspaceName = "Local"
	SingleUserWorkspaceSlug = "local"
	SingleUserIssuePrefix   = "LOC"
)

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

// BootstrapSingleUser is called once at server startup. When single-user
// mode is on, it ensures the local user has at least one workspace so the
// frontend skips the onboarding wizard entirely on first load.
//
// What it does, in order:
//
//  1. If MULTICA_SINGLE_USER is off, no-op.
//  2. Resolve (or create) the single local user via EnsureSingleUser.
//  3. If that user already has any workspace, stop — onboarding has
//     already happened, either through this function on a previous run
//     or through the normal handler.CreateWorkspace flow.
//  4. Otherwise open a single transaction and run the same three writes
//     that handler.CreateWorkspace runs for new signups: insert the
//     workspace, insert the owner member row, mark the user onboarded.
//     COALESCE in MarkUserOnboarded keeps it idempotent on re-runs.
//
// It is safe to call this on every server start. The check at step 3
// makes subsequent invocations no-ops, and step 4 is wrapped in a
// transaction so a half-finished bootstrap never leaves the DB in a
// partial state.
//
// pool must not be nil when single-user mode is on. Returning an error
// from this function should be treated as fatal at startup — without
// the local user / default workspace, the rest of single-user mode
// can't function correctly.
func BootstrapSingleUser(ctx context.Context, pool *pgxpool.Pool) error {
	if !SingleUserMode() {
		return nil
	}
	if pool == nil {
		return errors.New("single-user bootstrap: nil pool")
	}

	queries := db.New(pool)

	userIDStr, err := EnsureSingleUser(ctx, queries)
	if err != nil {
		return fmt.Errorf("single-user bootstrap: ensure user: %w", err)
	}
	userID, err := util.ParseUUID(userIDStr)
	if err != nil {
		return fmt.Errorf("single-user bootstrap: parse user uuid %q: %w", userIDStr, err)
	}

	workspaces, err := queries.ListWorkspaces(ctx, userID)
	if err != nil {
		return fmt.Errorf("single-user bootstrap: list workspaces: %w", err)
	}
	if len(workspaces) > 0 {
		// Already bootstrapped (this run or any previous one). Nothing to do.
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("single-user bootstrap: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	ws, err := qtx.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        SingleUserWorkspaceName,
		Slug:        SingleUserWorkspaceSlug,
		IssuePrefix: SingleUserIssuePrefix,
	})
	if err != nil {
		return fmt.Errorf("single-user bootstrap: create workspace: %w", err)
	}

	if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      userID,
		Role:        "owner",
	}); err != nil {
		return fmt.Errorf("single-user bootstrap: create member: %w", err)
	}

	if _, err := qtx.MarkUserOnboarded(ctx, userID); err != nil {
		return fmt.Errorf("single-user bootstrap: mark onboarded: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("single-user bootstrap: commit: %w", err)
	}

	slog.Info("single-user mode: bootstrapped default workspace",
		"slug", ws.Slug,
		"user_email", SingleUserEmail)
	return nil
}
