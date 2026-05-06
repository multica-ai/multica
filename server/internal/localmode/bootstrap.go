package localmode

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	LocalUserEmail     = "local@multica.local"
	LocalUserName      = "Local User"
	LocalWorkspaceName = "Local"
	LocalWorkspaceSlug = "local"
	LocalIssuePrefix   = "LOC"
)

type Bootstrapper struct {
	pool *pgxpool.Pool
}

type BootstrapResult struct {
	User      db.User
	Workspace db.Workspace
	Member    db.Member
}

func NewBootstrapper(pool *pgxpool.Pool) *Bootstrapper {
	return &Bootstrapper{pool: pool}
}

func (b *Bootstrapper) EnsureLocal(ctx context.Context) (BootstrapResult, error) {
	if b == nil || b.pool == nil {
		return BootstrapResult{}, errors.New("localmode: bootstrapper requires a database pool")
	}

	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("begin local bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	q := db.New(tx)

	user, err := ensureLocalUser(ctx, q)
	if err != nil {
		return BootstrapResult{}, err
	}

	workspace, err := ensureLocalWorkspace(ctx, q)
	if err != nil {
		return BootstrapResult{}, err
	}

	member, err := ensureLocalOwnerMember(ctx, q, workspace.ID, user.ID)
	if err != nil {
		return BootstrapResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("commit local bootstrap transaction: %w", err)
	}

	return BootstrapResult{
		User:      user,
		Workspace: workspace,
		Member:    member,
	}, nil
}

func ensureLocalUser(ctx context.Context, q *db.Queries) (db.User, error) {
	user, err := q.GetLocalUserByEmail(ctx, LocalUserEmail)
	if err == nil {
		if user.OnboardedAt.Valid && user.StarterContentState.Valid {
			return user, nil
		}
		user, err = q.MarkLocalUserOnboarded(ctx, user.ID)
		if err != nil {
			return db.User{}, fmt.Errorf("mark local user onboarded: %w", err)
		}
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, fmt.Errorf("get local user: %w", err)
	}

	user, err = q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		Name:      LocalUserName,
		Email:     LocalUserEmail,
		AvatarUrl: pgtype.Text{},
	})
	if err != nil {
		return db.User{}, fmt.Errorf("create local user: %w", err)
	}
	return user, nil
}

func ensureLocalWorkspace(ctx context.Context, q *db.Queries) (db.Workspace, error) {
	workspace, err := q.GetLocalWorkspaceBySlug(ctx, LocalWorkspaceSlug)
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Workspace{}, fmt.Errorf("get local workspace: %w", err)
	}

	workspace, err = q.CreateLocalWorkspace(ctx, db.CreateLocalWorkspaceParams{
		Name:        LocalWorkspaceName,
		Slug:        LocalWorkspaceSlug,
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: LocalIssuePrefix,
	})
	if err != nil {
		return db.Workspace{}, fmt.Errorf("create local workspace: %w", err)
	}
	return workspace, nil
}

func ensureLocalOwnerMember(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID) (db.Member, error) {
	member, err := q.GetLocalOwnerMember(ctx, db.GetLocalOwnerMemberParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err == nil {
		return member, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Member{}, fmt.Errorf("get local owner member: %w", err)
	}

	member, err = q.CreateLocalOwnerMember(ctx, db.CreateLocalOwnerMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return db.Member{}, fmt.Errorf("create local owner member: %w", err)
	}
	return member, nil
}
