package gitlab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// TokenDecrypter resolves the encrypted service-token bytes from a
// workspace_gitlab_connection row into the plaintext token that the GitLab
// REST client needs. The reconciler doesn't import the secrets package
// directly so that tests can pass a stub.
type TokenDecrypter func(ctx context.Context, encrypted []byte) (string, error)

// Reconciler polls GitLab every 5 minutes per connected workspace to catch
// changes that the webhook stream missed.
type Reconciler struct {
	queries *db.Queries
	client  *gitlabapi.Client
	decrypt TokenDecrypter
	// issueDeleter is optional. When non-nil, each tick also runs a deletion
	// sweep: full ListIssues(state=all) → diff against ListCachedGitlabIssues
	// → tear down cache rows with no GitLab counterpart. Project webhooks
	// don't fire on issue destruction (admin-scope system-hook only), so
	// without this sweep deleted issues linger in the cache indefinitely.
	issueDeleter IssueDeleter

	tickInterval       time.Duration
	overlapWindow      time.Duration
	staleWebhookWindow time.Duration
}

func NewReconciler(queries *db.Queries, client *gitlabapi.Client, decrypt TokenDecrypter) *Reconciler {
	return &Reconciler{
		queries:            queries,
		client:             client,
		decrypt:            decrypt,
		tickInterval:       5 * time.Minute,
		overlapWindow:      10 * time.Minute,
		staleWebhookWindow: 15 * time.Minute,
	}
}

// WithIssueDeleter installs an IssueDeleter so the reconciler can tear down
// cache rows for issues destroyed on GitLab. Optional; when omitted, the
// reconciler only does incremental upserts (pre-sweep behavior).
func (r *Reconciler) WithIssueDeleter(d IssueDeleter) *Reconciler {
	if d != nil {
		r.issueDeleter = d
	}
	return r
}

// Run blocks until ctx is cancelled, ticking every tickInterval.
func (r *Reconciler) Run(ctx context.Context) {
	tick := time.NewTicker(r.tickInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := r.tickOne(ctx); err != nil {
				slog.Error("reconciler tick", "error", err)
			}
		}
	}
}

// tickOne runs one pass over all connected workspaces.
func (r *Reconciler) tickOne(ctx context.Context) error {
	conns, err := r.queries.ListConnectedGitlabWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list connected: %w", err)
	}
	for _, conn := range conns {
		if err := r.reconcileOne(ctx, conn); err != nil {
			slog.Error("reconcile workspace",
				"workspace_id", conn.WorkspaceID,
				"error", err)
			continue
		}
	}
	return nil
}

func (r *Reconciler) reconcileOne(ctx context.Context, conn db.WorkspaceGitlabConnection) error {
	token, err := r.decrypt(ctx, conn.ServiceTokenEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Compute updated_after with overlap.
	since := time.Now().UTC().Add(-r.overlapWindow)
	if conn.LastSyncCursor.Valid && conn.LastSyncCursor.Time.Before(since) {
		since = conn.LastSyncCursor.Time.Add(-r.overlapWindow)
	}

	issues, err := r.client.ListIssues(ctx, token, conn.GitlabProjectID, gitlabapi.ListIssuesParams{
		State:        "all",
		UpdatedAfter: since.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	// Upsert each issue (skips happen via external_updated_at on UPDATE).
	maxSeen := time.Time{}
	if conn.LastSyncCursor.Valid {
		maxSeen = conn.LastSyncCursor.Time
	}
	agentMap, err := buildAgentSlugMap(ctx, r.queries, conn.WorkspaceID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}
	for _, issue := range issues {
		values := TranslateIssue(issue, &TranslateContext{AgentBySlug: agentMap})
		// Reconciler does not reverse-resolve creator/assignee — Phase 2b
		// webhook stream already keyed cache rows with those refs, and the
		// reconciler only backstops drift on issue fields the hook may have
		// missed. Creator type stays NULL for newly-reconciled rows.
		if _, err := r.queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(conn.WorkspaceID, conn.GitlabProjectID, issue, values, "", "")); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("upsert iid=%d: %w", issue.IID, err)
			}
			// Skipped (existing newer) — don't include in maxSeen advance below.
			continue
		}
		if t, err := time.Parse(time.RFC3339, issue.UpdatedAt); err == nil && t.After(maxSeen) {
			maxSeen = t
		}
	}

	// Advance the cursor.
	if !maxSeen.IsZero() {
		if err := r.queries.UpdateWorkspaceGitlabSyncCursor(ctx, db.UpdateWorkspaceGitlabSyncCursorParams{
			WorkspaceID:    conn.WorkspaceID,
			LastSyncCursor: pgtype.Timestamptz{Time: maxSeen, Valid: true},
		}); err != nil {
			return fmt.Errorf("update cursor: %w", err)
		}
	}

	// Stale-webhook detection. If we've never received a webhook, treat the
	// connection's created_at as the baseline — a connection older than the
	// stale window without any webhooks suggests delivery is broken.
	var lastSignal time.Time
	if conn.LastWebhookReceivedAt.Valid {
		lastSignal = conn.LastWebhookReceivedAt.Time
	} else if conn.CreatedAt.Valid {
		lastSignal = conn.CreatedAt.Time
	}
	if len(issues) > 0 && !lastSignal.IsZero() &&
		time.Since(lastSignal) > r.staleWebhookWindow {
		slog.Warn("reconciler picked up issues but webhook stream is silent",
			"workspace_id", conn.WorkspaceID,
			"last_signal_at", lastSignal,
			"reconciled_count", len(issues))
		if err := r.queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      conn.WorkspaceID,
			ConnectionStatus: "error",
			StatusMessage: pgtype.Text{
				String: "webhook deliveries appear delayed; reconciler is filling the gap",
				Valid:  true,
			},
		}); err != nil {
			slog.Error("update connection status to error after stale-webhook detection", "error", err)
		}
	}

	// Recovery: if we previously flipped to 'error' due to stale webhooks but
	// they've started flowing again, demote back to 'connected'.
	if conn.ConnectionStatus == "error" && conn.LastWebhookReceivedAt.Valid &&
		time.Since(conn.LastWebhookReceivedAt.Time) <= r.staleWebhookWindow {
		if err := r.queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      conn.WorkspaceID,
			ConnectionStatus: "connected",
			StatusMessage:    pgtype.Text{},
		}); err != nil {
			slog.Error("clear error status after webhook recovery", "error", err)
		}
	}

	// Deletion sweep: catch issues destroyed on GitLab (project webhooks
	// don't fire on destroy). Sweep failures are logged and swallowed — the
	// incremental upsert side of the tick has already succeeded, and we'd
	// rather skip this pass than fail a whole workspace's reconciliation.
	if r.issueDeleter != nil {
		if err := r.sweepDeletions(ctx, conn, token); err != nil {
			slog.Error("reconciler delete sweep",
				"workspace_id", conn.WorkspaceID, "error", err)
		}
	}
	return nil
}

// sweepDeletions diffs GitLab's current issue set against our cache and
// tears down rows with no counterpart upstream. Runs once per reconciler
// tick, gated by issueDeleter != nil.
//
// Safety model: we can't trust "list returned empty" as authoritative —
// a genuinely-empty project looks identical to a transient permissions
// glitch or scope mismatch. Instead, before deleting any cached row that's
// missing from the list, we do a targeted GET /projects/:id/issues/:iid.
// Only a confirmed 404 triggers the delete. Any other response (the issue
// still exists, or a transient error) skips this row for the current tick
// and leaves it for the next pass.
//
// Individual deletion failures are logged per-issue and don't abort the
// sweep — one tombstoned row shouldn't block others from being cleaned up.
func (r *Reconciler) sweepDeletions(ctx context.Context, conn db.WorkspaceGitlabConnection, token string) error {
	// Full project scan, no updated_after filter: we need the entire live set
	// to compute the diff. client.ListIssues paginates via per_page=100.
	live, err := r.client.ListIssues(ctx, token, conn.GitlabProjectID, gitlabapi.ListIssuesParams{State: "all"})
	if err != nil {
		return fmt.Errorf("list live issues: %w", err)
	}
	cached, err := r.queries.ListCachedGitlabIssues(ctx, conn.WorkspaceID)
	if err != nil {
		return fmt.Errorf("list cached issues: %w", err)
	}

	liveIIDs := make(map[int32]struct{}, len(live))
	for _, iss := range live {
		liveIIDs[int32(iss.IID)] = struct{}{}
	}

	for _, row := range cached {
		if !row.GitlabIid.Valid {
			continue
		}
		if _, exists := liveIIDs[row.GitlabIid.Int32]; exists {
			continue
		}

		// Row appears orphaned. Before deleting, do a per-issue check:
		// GitLab returns 404 for destroyed issues. Anything else (200 → the
		// list lied; 403 → token scope flake; 5xx → outage) means we skip
		// this pass and try again on the next tick.
		if _, err := r.client.GetIssue(ctx, token, conn.GitlabProjectID, int(row.GitlabIid.Int32)); err != nil {
			if !errors.Is(err, gitlabapi.ErrNotFound) {
				slog.Warn("reconciler delete sweep: per-issue check failed; skipping",
					"workspace_id", conn.WorkspaceID,
					"gitlab_iid", row.GitlabIid.Int32,
					"error", err)
				continue
			}
			// 404 confirmed — fall through to the delete below.
		} else {
			slog.Warn("reconciler delete sweep: list missed an issue that still exists; skipping",
				"workspace_id", conn.WorkspaceID,
				"gitlab_iid", row.GitlabIid.Int32)
			continue
		}

		if err := r.issueDeleter.CleanupAndDeleteIssue(ctx, row); err != nil {
			slog.Error("reconciler delete sweep: cleanup failed",
				"workspace_id", conn.WorkspaceID,
				"issue_id", uuidString(row.ID),
				"gitlab_iid", row.GitlabIid.Int32,
				"error", err)
			continue
		}
		slog.Info("reconciler delete sweep: cache row removed (no GitLab counterpart)",
			"workspace_id", conn.WorkspaceID,
			"issue_id", uuidString(row.ID),
			"gitlab_iid", row.GitlabIid.Int32)
	}
	return nil
}
