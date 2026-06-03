package identitysync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// Store is the persistence surface the reconciler needs. The production
// implementation (store.go) wraps the cerebro + upstream sqlc queries; tests
// supply a fake. Keeping the reconciler behind this seam lets the membership
// algorithm be unit-tested without a database.
type Store interface {
	// UpsertGroup find-or-creates the synced group for (workspace, googleEmail)
	// with the given display name and returns its id.
	UpsertGroup(ctx context.Context, workspaceID pgtype.UUID, googleEmail, name string) (pgtype.UUID, error)
	// ResolveWorkspaceUser maps a Google member email to a Multica user that is
	// a member of the workspace. found=false means "no Multica account" or "not
	// a member of this workspace" — the reconciler skips and logs, never errors.
	ResolveWorkspaceUser(ctx context.Context, workspaceID pgtype.UUID, email string) (userID pgtype.UUID, found bool, err error)
	// ListSyncedMemberIDs returns the user_ids in the group that the sync owns
	// (source = google_workspace). Manual members are invisible here.
	ListSyncedMemberIDs(ctx context.Context, groupID pgtype.UUID) ([]pgtype.UUID, error)
	// AddSyncedMember idempotently adds a synced member.
	AddSyncedMember(ctx context.Context, groupID, userID pgtype.UUID) error
	// RemoveSyncedMember removes a member, scoped to the synced source.
	RemoveSyncedMember(ctx context.Context, groupID, userID pgtype.UUID) error
}

// Reconciler applies a Google membership snapshot onto one workspace's
// cerebro_group tables.
type Reconciler struct {
	store  Store
	groups []SyncedGroup
}

// NewReconciler builds a Reconciler over the fixed FIR-2596 group list.
func NewReconciler(store Store) *Reconciler {
	return &Reconciler{store: store, groups: SyncedGroups}
}

// SyncResult is per-workspace observability for one reconcile pass.
type SyncResult struct {
	GroupsProcessed int
	MembersAdded    int
	MembersRemoved  int
	UsersSkipped    int // Google members with no matching Multica workspace user
}

// SyncWorkspace reconciles every synced group in one workspace against the
// supplied membership snapshot (group email -> member emails). The snapshot is
// fetched once per tick by the worker and shared across workspaces, since the
// data lake groups are firtal-wide.
func (r *Reconciler) SyncWorkspace(
	ctx context.Context,
	workspaceID pgtype.UUID,
	membership map[string][]string,
) (SyncResult, error) {
	var res SyncResult
	for _, g := range r.groups {
		groupID, err := r.store.UpsertGroup(ctx, workspaceID, g.Email, g.Name)
		if err != nil {
			return res, fmt.Errorf("upsert group %s: %w", g.Email, err)
		}
		res.GroupsProcessed++

		desired, skipped, err := r.resolveDesired(ctx, workspaceID, g.Email, membership)
		if err != nil {
			return res, err
		}
		res.UsersSkipped += skipped

		currentIDs, err := r.store.ListSyncedMemberIDs(ctx, groupID)
		if err != nil {
			return res, fmt.Errorf("list synced members for %s: %w", g.Email, err)
		}
		current := make(map[pgtype.UUID]struct{}, len(currentIDs))
		for _, id := range currentIDs {
			current[id] = struct{}{}
		}

		// Additions: desired - current.
		for id := range desired {
			if _, ok := current[id]; ok {
				continue
			}
			if err := r.store.AddSyncedMember(ctx, groupID, id); err != nil {
				return res, fmt.Errorf("add member to %s: %w", g.Email, err)
			}
			res.MembersAdded++
		}
		// Removals: current - desired (only synced members reach this map).
		for id := range current {
			if _, ok := desired[id]; ok {
				continue
			}
			if err := r.store.RemoveSyncedMember(ctx, groupID, id); err != nil {
				return res, fmt.Errorf("remove member from %s: %w", g.Email, err)
			}
			res.MembersRemoved++
		}
	}
	return res, nil
}

// resolveDesired turns the Google member emails for one group into the set of
// Multica user_ids that should be in the group, skipping (and counting) emails
// with no matching workspace user.
func (r *Reconciler) resolveDesired(
	ctx context.Context,
	workspaceID pgtype.UUID,
	groupEmail string,
	membership map[string][]string,
) (map[pgtype.UUID]struct{}, int, error) {
	desired := map[pgtype.UUID]struct{}{}
	skipped := 0
	for _, raw := range membership[strings.ToLower(strings.TrimSpace(groupEmail))] {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		userID, found, err := r.store.ResolveWorkspaceUser(ctx, workspaceID, email)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve user %s: %w", email, err)
		}
		if !found {
			skipped++
			slog.Info("gws group sync: member has no Multica workspace account, skipping",
				"workspace_id", uuidString(workspaceID),
				"group", groupEmail,
				"email", email)
			continue
		}
		desired[userID] = struct{}{}
	}
	return desired, skipped, nil
}
