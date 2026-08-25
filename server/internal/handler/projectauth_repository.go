package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/projectauth"
)

// projectAuthRepository is the only Handler-side adapter for projectauth.
// Keeping SQL here means the permission package remains independent of sqlc
// generated code and upstream handler structure.
type projectAuthRepository struct{ db dbExecutor }

func newProjectAuthRepository(db dbExecutor) projectauth.Repository {
	if db == nil {
		return nil
	}
	return &projectAuthRepository{db: db}
}

func (r *projectAuthRepository) WorkspaceRole(ctx context.Context, workspaceID, userID string) (projectauth.WorkspaceRole, error) {
	var role string
	err := r.db.QueryRow(ctx, `SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).Scan(&role)
	return projectauth.WorkspaceRole(role), err
}

func (r *projectAuthRepository) ProjectRole(ctx context.Context, projectID, userID string) (projectauth.ProjectRole, error) {
	var role string
	err := r.db.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID).Scan(&role)
	return projectauth.ProjectRole(role), err
}

func (r *projectAuthRepository) ProjectWorkspace(ctx context.Context, projectID string) (string, error) {
	var workspaceID string
	err := r.db.QueryRow(ctx, `SELECT workspace_id::text FROM project WHERE id = $1`, projectID).Scan(&workspaceID)
	return workspaceID, err
}

func (r *projectAuthRepository) VisibleProjectIDs(ctx context.Context, workspaceID, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT p.id::text
		FROM project p
		WHERE p.workspace_id = $1
		  AND (EXISTS (
				SELECT 1 FROM member m
				WHERE m.workspace_id = p.workspace_id AND m.user_id = $2
				  AND m.role IN ('owner', 'admin')
			) OR EXISTS (
				SELECT 1 FROM project_members pm
				WHERE pm.project_id = p.id AND pm.user_id = $2
			))
		ORDER BY p.created_at DESC`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *projectAuthRepository) AddProjectMember(ctx context.Context, projectID, userID string, role projectauth.ProjectRole) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT $1, $2, $3
		WHERE EXISTS (
			SELECT 1 FROM project p
			JOIN member m ON m.workspace_id = p.workspace_id
			WHERE p.id = $1 AND m.user_id = $2
		)
		ON CONFLICT (project_id, user_id)
		DO UPDATE SET role = EXCLUDED.role, updated_at = now()`, projectID, userID, role)
	return err
}

func (r *projectAuthRepository) RemoveProjectMember(ctx context.Context, projectID, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
	return err
}

func (r *projectAuthRepository) ListProjectMembers(ctx context.Context, projectID string) ([]projectauth.ProjectMemberRecord, error) {
	rows, err := r.db.Query(ctx, `
		SELECT project_id::text, user_id::text, role
		FROM project_members WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []projectauth.ProjectMemberRecord
	for rows.Next() {
		var member projectauth.ProjectMemberRecord
		var role string
		if err := rows.Scan(&member.ProjectID, &member.UserID, &role); err != nil {
			return nil, err
		}
		member.Role = projectauth.ProjectRole(role)
		result = append(result, member)
	}
	return result, rows.Err()
}

func (r *projectAuthRepository) IssuePermission(ctx context.Context, issueID, projectID, userID string, permission projectauth.Permission) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM issue_permissions
			WHERE issue_id = $1 AND project_id = $2 AND user_id = $3 AND permission = $4
		)`, issueID, projectID, userID, permission).Scan(&exists)
	return exists, err
}

func (r *projectAuthRepository) GrantIssuePermission(ctx context.Context, issueID, projectID, userID, grantedBy string, permission projectauth.Permission) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO issue_permissions (issue_id, project_id, user_id, permission, granted_by)
		SELECT $1, $2, $3, $4, $5
		WHERE EXISTS (
			SELECT 1 FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN member m ON m.workspace_id = p.workspace_id AND m.user_id = $3
			WHERE i.id = $1 AND i.project_id = $2
		)
		ON CONFLICT (issue_id, user_id, permission)
		DO UPDATE SET project_id = EXCLUDED.project_id, granted_by = EXCLUDED.granted_by, updated_at = now()`, issueID, projectID, userID, permission, grantedBy)
	return err
}

func (r *projectAuthRepository) RevokeIssuePermission(ctx context.Context, issueID, projectID, userID string, permission projectauth.Permission) error {
	_, err := r.db.Exec(ctx, `DELETE FROM issue_permissions WHERE issue_id = $1 AND project_id = $2 AND user_id = $3 AND permission = $4`, issueID, projectID, userID, permission)
	return err
}

func (r *projectAuthRepository) ListIssuePermissions(ctx context.Context, issueID, projectID string) ([]projectauth.IssuePermissionRecord, error) {
	rows, err := r.db.Query(ctx, `
		SELECT issue_id::text, project_id::text, user_id::text, permission, granted_by::text
		FROM issue_permissions WHERE issue_id = $1 AND project_id = $2 ORDER BY created_at, user_id, permission`, issueID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []projectauth.IssuePermissionRecord
	for rows.Next() {
		var record projectauth.IssuePermissionRecord
		var permission string
		if err := rows.Scan(&record.IssueID, &record.ProjectID, &record.UserID, &permission, &record.GrantedBy); err != nil {
			return nil, err
		}
		record.Permission = projectauth.Permission(permission)
		result = append(result, record)
	}
	return result, rows.Err()
}

func (r *projectAuthRepository) IssueProject(ctx context.Context, issueID string) (string, error) {
	var projectID pgtype.Text
	err := r.db.QueryRow(ctx, `
		SELECT project_id::text
		FROM issue
		WHERE id = $1 AND project_id IS NOT NULL`, issueID).Scan(&projectID)
	if err != nil || !projectID.Valid {
		return "", err
	}
	return projectID.String, nil
}

// 2026-08-24 coder(lq): Keep report SQL in the Handler adapter so the
// projectauth package does not depend on sqlc or the upstream schema layer.
// Each UNION branch preserves the authorization source for audit/reporting.
func (r *projectAuthRepository) ListPermissionReport(ctx context.Context, filter projectauth.PermissionReportFilter) (projectauth.PermissionReportResult, error) {
	rows, err := r.db.Query(ctx, `
		WITH permission_map(project_role, permission) AS (
			VALUES
				('owner', 'project.view'), ('owner', 'project.edit'),
				('owner', 'project.issue.create'), ('owner', 'project.issue.manage'),
				('owner', 'project.agent.use'), ('owner', 'project.member.manage'),
				('owner', 'project.settings.manage'),
				('manager', 'project.view'), ('manager', 'project.edit'),
				('manager', 'project.issue.create'), ('manager', 'project.issue.manage'),
				('manager', 'project.agent.use'),
				('member', 'project.view'), ('member', 'project.issue.create'),
				('member', 'project.agent.use'),
				('viewer', 'project.view')
		), report_rows AS (
			SELECT 'project'::text AS scope, p.id::text AS project_id, p.title AS project_title,
				NULL::text AS issue_id, NULL::text AS issue_title,
				m.user_id::text AS user_id, u.name AS user_name, u.email AS user_email,
				m.role AS workspace_role, NULL::text AS project_role,
				pm.permission AS permission, 'workspace_role'::text AS source,
				NULL::text AS granted_by
			FROM project p
			JOIN member m ON m.workspace_id = p.workspace_id
			JOIN "user" u ON u.id = m.user_id
			CROSS JOIN (VALUES ('project.view'), ('project.edit'), ('project.issue.create'),
				('project.issue.manage'), ('project.agent.use'), ('project.member.manage'),
				('project.settings.manage')) AS pm(permission)
			WHERE m.role IN ('owner', 'admin')

			UNION ALL

			SELECT 'project', p.id::text, p.title, NULL, NULL,
				pm.user_id::text, u.name, u.email, m.role, pm.role,
				permission_map.permission, 'project_role', NULL
			FROM project_members pm
			JOIN project p ON p.id = pm.project_id
			JOIN "user" u ON u.id = pm.user_id
			LEFT JOIN member m ON m.workspace_id = p.workspace_id AND m.user_id = pm.user_id
			JOIN permission_map ON permission_map.project_role = pm.role

			UNION ALL

			SELECT 'issue', p.id::text, p.title, i.id::text, i.title,
				ip.user_id::text, u.name, u.email, m.role, pm.role,
				ip.permission, 'issue_grant', ip.granted_by::text
			FROM issue_permissions ip
			JOIN issue i ON i.id = ip.issue_id AND i.project_id = ip.project_id
			JOIN project p ON p.id = ip.project_id AND p.workspace_id = i.workspace_id
			JOIN "user" u ON u.id = ip.user_id
			LEFT JOIN member m ON m.workspace_id = p.workspace_id AND m.user_id = ip.user_id
			LEFT JOIN project_members pm ON pm.project_id = ip.project_id AND pm.user_id = ip.user_id
		)
		SELECT scope, project_id, project_title, issue_id, issue_title,
			user_id, user_name, user_email, workspace_role, project_role,
			permission, source, granted_by, COUNT(*) OVER() AS total_count
		FROM report_rows
		WHERE project_id IN (SELECT id::text FROM project WHERE workspace_id = $1)
		  AND ($2 = '' OR project_id = $2)
		  AND ($3 = '' OR issue_id = $3)
		  AND ($4 = '' OR user_id = $4)
		  AND ($5 = '' OR workspace_role = $5 OR project_role = $5)
		  AND ($6 = '' OR permission = $6)
		  AND ($7 = 'all' OR scope = $7)
		ORDER BY project_title, project_id, issue_title NULLS FIRST, user_name, permission, source
		LIMIT $8 OFFSET $9`,
		filter.WorkspaceID, filter.ProjectID, filter.IssueID, filter.UserID,
		filter.Role, string(filter.Permission), filter.Scope, filter.Limit, filter.Offset)
	if err != nil {
		return projectauth.PermissionReportResult{}, err
	}
	defer rows.Close()

	result := projectauth.PermissionReportResult{Rows: make([]projectauth.PermissionReportRow, 0)}
	for rows.Next() {
		var row projectauth.PermissionReportRow
		var issueID, issueTitle, projectRole, grantedBy pgtype.Text
		var workspaceRole, permission, source pgtype.Text
		var total int64
		if err := rows.Scan(&row.Scope, &row.ProjectID, &row.ProjectTitle, &issueID, &issueTitle,
			&row.UserID, &row.UserName, &row.UserEmail, &workspaceRole, &projectRole,
			&permission, &source, &grantedBy, &total); err != nil {
			return projectauth.PermissionReportResult{}, err
		}
		if workspaceRole.Valid {
			row.WorkspaceRole = projectauth.WorkspaceRole(workspaceRole.String)
		}
		if permission.Valid {
			row.Permission = projectauth.Permission(permission.String)
		}
		if source.Valid {
			row.Source = source.String
		}
		if issueID.Valid {
			row.IssueID = issueID.String
		}
		if issueTitle.Valid {
			row.IssueTitle = issueTitle.String
		}
		if projectRole.Valid {
			row.ProjectRole = projectauth.ProjectRole(projectRole.String)
		}
		if grantedBy.Valid {
			row.GrantedBy = grantedBy.String
		}
		result.Rows = append(result.Rows, row)
		result.Total = total
	}
	return result, rows.Err()
}
