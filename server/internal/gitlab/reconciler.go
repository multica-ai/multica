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
		if _, err := r.queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(conn.WorkspaceID, conn.GitlabProjectID, issue, values)); err != nil {
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

	// Stale-webhook detection.
	if len(issues) > 0 && conn.LastWebhookReceivedAt.Valid &&
		time.Since(conn.LastWebhookReceivedAt.Time) > r.staleWebhookWindow {
		slog.Warn("reconciler picked up issues but webhook stream is silent",
			"workspace_id", conn.WorkspaceID,
			"last_webhook_received_at", conn.LastWebhookReceivedAt.Time,
			"reconciled_count", len(issues))
		_ = r.queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      conn.WorkspaceID,
			ConnectionStatus: "error",
			StatusMessage: pgtype.Text{
				String: "webhook deliveries appear delayed; reconciler is filling the gap",
				Valid:  true,
			},
		})
	}
	return nil
}
