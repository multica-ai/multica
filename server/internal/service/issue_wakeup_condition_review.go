package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func isPRReviewEvent(event string) bool {
	return event == "reviewed" || event == "approved" || event == "changes_requested"
}

func isPRReviewCondition(raw []byte) bool {
	var c WakeupCondition
	return json.Unmarshal(raw, &c) == nil && c.Type == "pull_request" && isPRReviewEvent(c.Event)
}

// Reviews are submissions, not head-dependent states or a merge-approval gate.
// Keep seen IDs when a review disappears, so dismissals or relinking cannot
// replay a decision. Since also excludes history if the first fetch is delayed.
type prReviewState struct {
	Since time.Time `json:"since"`
	Seen  []string  `json:"seen,omitempty"`
}

func encodePRReviewState(state prReviewState) string {
	sort.Strings(state.Seen)
	raw, _ := json.Marshal(state)
	return string(raw)
}

func (s *IssueWakeupService) evaluatePRReviews(ctx context.Context, tx pgx.Tx, w db.IssueWakeup, event string) (bool, string, map[string]any, error) {
	state := prReviewState{Since: w.CreatedAt.Time}
	if w.ConditionState != "" {
		if err := json.Unmarshal([]byte(w.ConditionState), &state); err != nil {
			return false, "", nil, fmt.Errorf("read review condition state: %w", err)
		}
	}
	seen := make(map[string]bool, len(state.Seen))
	for _, key := range state.Seen {
		seen[key] = true
	}
	rows, err := tx.Query(ctx, `SELECT pr.id::text,pr.pr_number,pr.state,pr.review_snapshot,'github'
		FROM github_pull_request pr JOIN issue_pull_request ipr ON ipr.pull_request_id=pr.id
		WHERE ipr.issue_id=$1 AND pr.workspace_id=$2
		UNION ALL
		SELECT pr.id::text,pr.pr_number,pr.state,NULL::jsonb,pr.provider
		FROM vcs_pull_request pr JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id=pr.id
		WHERE ipr.issue_id=$1 AND pr.workspace_id=$2`, w.IssueID, w.WorkspaceID)
	if err != nil {
		return false, "", nil, err
	}
	defer rows.Close()
	linked, vcsLinked, missingSnapshot := 0, 0, false
	available := s.Tasks != nil && s.Tasks.PRSnapshotsEnabled
	var observations []map[string]any
	for rows.Next() {
		var id, lifecycle, provider string
		var number int32
		var raw []byte
		if err := rows.Scan(&id, &number, &lifecycle, &raw, &provider); err != nil {
			return false, "", nil, err
		}
		if provider != "github" {
			vcsLinked++
			continue
		}
		linked++
		if !available || (lifecycle != "open" && lifecycle != "draft") {
			continue
		}
		if len(raw) == 0 {
			missingSnapshot = true
			continue
		}
		var reviews []ghsnapshot.PRReview
		if err := json.Unmarshal(raw, &reviews); err != nil || reviews == nil {
			return false, "", nil, fmt.Errorf("invalid review snapshot for PR %s", id)
		}
		for _, review := range reviews {
			if review.ID <= 0 || !review.SubmittedAt.After(state.Since) ||
				(review.State != "APPROVED" && review.State != "CHANGES_REQUESTED") ||
				(event != "reviewed" && !strings.EqualFold(review.State, event)) {
				continue
			}
			key := fmt.Sprintf("%s:%d", id, review.ID)
			if seen[key] {
				continue
			}
			seen[key] = true
			state.Seen = append(state.Seen, key)
			observations = append(observations, map[string]any{
				"number": number, "review_id": review.ID, "review": strings.ToLower(review.State), "submitted_at": review.SubmittedAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return false, "", nil, err
	}
	if len(observations) == 0 {
		switch {
		case !available && (linked > 0 || vcsLinked == 0):
			return false, "", nil, errPRSnapshotsUnavailable
		case linked == 0 && vcsLinked == 0:
			return false, "", nil, errPRNotLinked
		case linked == 0:
			return false, "", nil, errPRReviewsUnsupported
		case missingSnapshot:
			return false, "", nil, errPRSnapshotPending
		}
	}
	return len(observations) > 0, encodePRReviewState(state), map[string]any{"pull_requests": observations}, nil
}
