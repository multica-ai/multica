package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is a pgx-direct data layer for the github_installation and
// github_issue_binding tables. It does not go through sqlc (the new tables
// are integration-local), mirroring lark/hub_pgx.go.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a pgx pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Installation is one workspace↔project board binding.
type Installation struct {
	ID            string
	WorkspaceID   string
	OrgLogin      string
	ProjectNumber int
	ProjectNodeID string
}

// Binding maps a GitHub ProjectV2 item to a Multica issue.
type Binding struct {
	ID                string
	InstallationID    string
	WorkspaceID       string
	MulticaIssueID    string
	GitHubItemID      string
	GitHubContentID   string
	GitHubRepo        string
	GitHubIssueNumber int
	RemoteStatus      string
	LastPushedStatus  string
	RemoteHash        string
}

// EnsureInstallation upserts the single installation row for a workspace
// and returns its id. field_map is the JSON-encoded ProjectSchema cache.
func (s *Store) EnsureInstallation(ctx context.Context, wsID, org string, number int, projectNodeID string, fieldMapJSON []byte) (Installation, error) {
	const q = `
INSERT INTO github_installation (workspace_id, org_login, project_number, project_node_id, field_map)
VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
ON CONFLICT (workspace_id) DO UPDATE
   SET org_login       = EXCLUDED.org_login,
       project_number  = EXCLUDED.project_number,
       project_node_id = EXCLUDED.project_node_id,
       field_map       = EXCLUDED.field_map,
       status          = 'active',
       updated_at      = now()
RETURNING id::text, workspace_id::text, org_login, project_number, project_node_id`
	var inst Installation
	row := s.pool.QueryRow(ctx, q, wsID, org, number, projectNodeID, string(fieldMapJSON))
	if err := row.Scan(&inst.ID, &inst.WorkspaceID, &inst.OrgLogin, &inst.ProjectNumber, &inst.ProjectNodeID); err != nil {
		return Installation{}, fmt.Errorf("ensure installation: %w", err)
	}
	return inst, nil
}

// MarkSynced stamps last_synced_at (and clears/sets last_error).
func (s *Store) MarkSynced(ctx context.Context, installationID, errMsg string) error {
	const q = `
UPDATE github_installation
   SET last_synced_at = now(),
       last_error     = NULLIF($2, ''),
       status         = CASE WHEN $2 = '' THEN 'active' ELSE 'error' END,
       updated_at     = now()
 WHERE id = $1::uuid`
	_, err := s.pool.Exec(ctx, q, installationID, errMsg)
	return err
}

// GetBindingByItem looks up a binding by GitHub item node id.
func (s *Store) GetBindingByItem(ctx context.Context, installationID, itemID string) (*Binding, error) {
	return s.scanBinding(ctx, `
SELECT id::text, installation_id::text, workspace_id::text, multica_issue_id::text,
       github_item_id, COALESCE(github_content_id,''), COALESCE(github_repo,''),
       COALESCE(github_issue_number,0), COALESCE(remote_status,''),
       COALESCE(last_pushed_status,''), COALESCE(remote_hash,'')
  FROM github_issue_binding
 WHERE installation_id = $1::uuid AND github_item_id = $2`, installationID, itemID)
}

// GetBindingByIssue looks up a binding by Multica issue id.
func (s *Store) GetBindingByIssue(ctx context.Context, installationID, issueID string) (*Binding, error) {
	return s.scanBinding(ctx, `
SELECT id::text, installation_id::text, workspace_id::text, multica_issue_id::text,
       github_item_id, COALESCE(github_content_id,''), COALESCE(github_repo,''),
       COALESCE(github_issue_number,0), COALESCE(remote_status,''),
       COALESCE(last_pushed_status,''), COALESCE(remote_hash,'')
  FROM github_issue_binding
 WHERE installation_id = $1::uuid AND multica_issue_id = $2::uuid`, installationID, issueID)
}

func (s *Store) scanBinding(ctx context.Context, q string, args ...any) (*Binding, error) {
	var b Binding
	row := s.pool.QueryRow(ctx, q, args...)
	err := row.Scan(&b.ID, &b.InstallationID, &b.WorkspaceID, &b.MulticaIssueID,
		&b.GitHubItemID, &b.GitHubContentID, &b.GitHubRepo, &b.GitHubIssueNumber,
		&b.RemoteStatus, &b.LastPushedStatus, &b.RemoteHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertBinding inserts or updates a binding keyed on (installation, item).
func (s *Store) UpsertBinding(ctx context.Context, b Binding) error {
	const q = `
INSERT INTO github_issue_binding
  (installation_id, workspace_id, multica_issue_id, github_item_id, github_content_id,
   github_repo, github_issue_number, remote_status, last_pushed_status, remote_hash, synced_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, NULLIF($5,''), NULLIF($6,''),
        NULLIF($7,0), NULLIF($8,''), NULLIF($9,''), NULLIF($10,''), now())
ON CONFLICT (installation_id, github_item_id) DO UPDATE
   SET multica_issue_id  = EXCLUDED.multica_issue_id,
       github_content_id = EXCLUDED.github_content_id,
       github_repo       = EXCLUDED.github_repo,
       github_issue_number = EXCLUDED.github_issue_number,
       remote_status     = EXCLUDED.remote_status,
       last_pushed_status = EXCLUDED.last_pushed_status,
       remote_hash       = EXCLUDED.remote_hash,
       synced_at         = now()`
	_, err := s.pool.Exec(ctx, q,
		b.InstallationID, b.WorkspaceID, b.MulticaIssueID, b.GitHubItemID, b.GitHubContentID,
		b.GitHubRepo, b.GitHubIssueNumber, b.RemoteStatus, b.LastPushedStatus, b.RemoteHash)
	return err
}

// SetLastPushedStatus records the status we just pushed to GitHub so the
// inbound poller can recognize the echo and skip it (loop guard).
func (s *Store) SetLastPushedStatus(ctx context.Context, installationID, issueID, status string) error {
	const q = `
UPDATE github_issue_binding
   SET last_pushed_status = $3, remote_status = $3, synced_at = now()
 WHERE installation_id = $1::uuid AND multica_issue_id = $2::uuid`
	_, err := s.pool.Exec(ctx, q, installationID, issueID, status)
	return err
}

// SetIssueParent sets parent_issue_id directly. We avoid Queries.UpdateIssue
// here because its UPDATE assigns assignee/date/parent columns directly
// (not via COALESCE), so a partial param set would null out unrelated
// fields. A targeted UPDATE is the safe way to link a parent.
func (s *Store) SetIssueParent(ctx context.Context, issueID, parentIssueID, wsID string) error {
	const q = `
UPDATE issue SET parent_issue_id = $2::uuid, updated_at = now()
 WHERE id = $1::uuid AND workspace_id = $3::uuid
   AND ($2::uuid IS DISTINCT FROM parent_issue_id)`
	_, err := s.pool.Exec(ctx, q, issueID, parentIssueID, wsID)
	return err
}

// AnyMemberUserID returns a member user id for the workspace (the import
// creator). The first member is the workspace owner.
func (s *Store) AnyMemberUserID(ctx context.Context, wsID string) (string, error) {
	const q = `SELECT user_id::text FROM member WHERE workspace_id = $1::uuid ORDER BY created_at LIMIT 1`
	var uid string
	if err := s.pool.QueryRow(ctx, q, wsID).Scan(&uid); err != nil {
		return "", fmt.Errorf("resolve workspace member: %w", err)
	}
	return uid, nil
}
