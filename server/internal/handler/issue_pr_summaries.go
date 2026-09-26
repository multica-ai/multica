package handler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// IssuePullRequestSummary contains only the fields needed by issue surfaces.
// The detail sidebar continues to use the full PR endpoint.
type IssuePullRequestSummary struct {
	Provider          string  `json:"provider"`
	Number            int32   `json:"number"`
	Title             string  `json:"title"`
	State             string  `json:"state"`
	HTMLURL           string  `json:"html_url"`
	ChecksRollup      *string `json:"checks_rollup,omitempty"`
	ChecksConclusion  *string `json:"checks_conclusion,omitempty"`
	SnapshotAvailable *bool   `json:"snapshot_available,omitempty"`
}

// linkedPullRequestsByIssue reads both provider stores for the page in one
// workspace-scoped query. Neither the database nor GitHub is queried per row.
func (h *Handler) linkedPullRequestsByIssue(ctx context.Context, workspaceID pgtype.UUID, issueIDs []pgtype.UUID) (map[string][]IssuePullRequestSummary, error) {
	out := make(map[string][]IssuePullRequestSummary)
	if len(issueIDs) == 0 {
		return out, nil
	}
	const query = `
WITH vcs_page_prs AS (
    SELECT DISTINCT pr.id, pr.connection_id, pr.head_sha
    FROM issue_vcs_pull_request ipr
    JOIN vcs_pull_request pr ON pr.id = ipr.pull_request_id
    WHERE ipr.issue_id = ANY($1::uuid[]) AND pr.workspace_id = $2
), vcs_checks AS (
    SELECT page_pr.id,
           CASE WHEN bool_or(cs.state = 'failed') THEN 'failed'
                WHEN bool_or(cs.state = 'pending') THEN 'pending'
                WHEN count(cs.context) > 0 THEN 'passed'
                ELSE NULL END AS conclusion
    FROM vcs_page_prs page_pr
    LEFT JOIN vcs_commit_status cs
      ON cs.connection_id = page_pr.connection_id
     AND cs.sha = page_pr.head_sha AND page_pr.head_sha <> ''
    GROUP BY page_pr.id
)
SELECT issue_id, provider, pr_number, title, state, html_url,
       checks_rollup, checks_conclusion, snapshot_available
FROM (
    SELECT ipr.issue_id, 'github'::text AS provider, pr.pr_number,
           pr.title, pr.state, pr.html_url,
           CASE WHEN pr.snapshot_head_sha = pr.head_sha
                     AND pr.snapshot_head_sha <> ''
                     AND pr.snapshot_fetched_at IS NOT NULL
                THEN lower(pr.checks_rollup_state) ELSE NULL END AS checks_rollup,
           NULL::text AS checks_conclusion,
           (pr.snapshot_head_sha = pr.head_sha
             AND pr.snapshot_head_sha <> ''
             AND pr.snapshot_fetched_at IS NOT NULL) AS snapshot_available,
           pr.pr_created_at
    FROM issue_pull_request ipr
    JOIN github_pull_request pr ON pr.id = ipr.pull_request_id
    WHERE ipr.issue_id = ANY($1::uuid[]) AND pr.workspace_id = $2
    UNION ALL
    SELECT ipr.issue_id, pr.provider, pr.pr_number,
           pr.title, pr.state, pr.html_url,
           NULL::text AS checks_rollup,
           checks.conclusion AS checks_conclusion,
           NULL::boolean AS snapshot_available,
           pr.pr_created_at
    FROM issue_vcs_pull_request ipr
    JOIN vcs_pull_request pr ON pr.id = ipr.pull_request_id
    LEFT JOIN vcs_checks checks ON checks.id = pr.id
    WHERE ipr.issue_id = ANY($1::uuid[]) AND pr.workspace_id = $2
) linked
ORDER BY pr_created_at DESC`
	rows, err := h.DB.Query(ctx, query, issueIDs, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query linked pull requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var issueID pgtype.UUID
		var summary IssuePullRequestSummary
		var snapshotAvailable *bool
		if err := rows.Scan(&issueID, &summary.Provider, &summary.Number,
			&summary.Title, &summary.State, &summary.HTMLURL,
			&summary.ChecksRollup, &summary.ChecksConclusion, &snapshotAvailable); err != nil {
			return nil, fmt.Errorf("scan linked pull request: %w", err)
		}
		if summary.Provider == "github" {
			available := h.PRRefresh.Enabled() && snapshotAvailable != nil && *snapshotAvailable
			summary.SnapshotAvailable = &available
			if !available {
				summary.ChecksRollup = nil
			}
		}
		key := uuidToString(issueID)
		out[key] = append(out[key], summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read linked pull requests: %w", err)
	}
	return out, nil
}
