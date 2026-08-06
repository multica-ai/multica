package workflows

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
	"github.com/multica-ai/multica/server/internal/cerebro/relatedcheck"
	"github.com/multica-ai/multica/server/internal/util"
)

// CheckRelatedActionType is the typed hook action that looks for an existing
// issue on the same matter (FIR-4183). Paired with the before.issue.assigned
// event it answers, at hand-over time, the question every agent would
// otherwise have to remember to ask itself.
const CheckRelatedActionType = "issue.check_related"

// ResultKeyBlocked is the key issue.check_related sets when it found a
// duplicate AND the policy asked for the hand-over to stop. IssueAssignGate
// reads it; nothing else in the engine does.
const ResultKeyBlocked = "blocked"

// relatedCheckRunner is the seam to the relatedcheck package. It is a variable
// so tests can run the action without a database or a gateway.
var relatedCheckRunner = relatedcheck.Check

// checkRelated runs the related-issue check for the event's issue, records
// every actionable match as a related link, and (by default) posts one comment
// the first time a link is made. Repeat runs on the same issue create no new
// links and therefore say nothing.
func (e *PostgresHookActionExecutor) checkRelated(ctx context.Context, workspaceID pgtype.UUID, policy HookPolicy, event HookEvent, config map[string]any) (map[string]any, error) {
	issueID := hookConfigString(config, "issue_id", event.IssueID)
	if !optionalHookUUID(issueID).Valid {
		return nil, errors.New("issue.check_related requires an issue")
	}

	res, err := relatedCheckRunner(ctx, e.db, &duplicatecheck.Judger{}, issueID, relatedcheck.Options{
		CandidateLimit: hookConfigInt(config, "candidate_limit", 0),
		MatchLimit:     hookConfigInt(config, "match_limit", 0),
		Link:           hookConfigBool(config, "link", true),
		UserLabel:      event.AgentID,
	})
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"candidates": res.Candidates,
		"matches":    len(res.Matches),
		"new_links":  res.NewLinks,
		"duplicate":  res.Duplicate != nil,
		"summary":    relatedcheck.Summary(res),
	}
	if res.Skipped != "" {
		out["skipped"] = res.Skipped
	}

	if hookConfigBool(config, "comment", true) {
		if body := relatedcheck.RenderComment(res); body != "" {
			authorType, authorID := "agent", optionalHookUUID(event.AgentID)
			if !authorID.Valid {
				authorType, authorID = policy.CreatedByType, optionalHookUUID(policy.CreatedByID)
			}
			var commentID pgtype.UUID
			err = e.db.QueryRow(ctx, `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
SELECT i.id, i.workspace_id, $3, $4, $5, 'comment' FROM issue i WHERE i.id=$1 AND i.workspace_id=$2
RETURNING id`, optionalHookUUID(issueID), workspaceID, authorType, authorID, body).Scan(&commentID)
			if err != nil {
				return nil, err
			}
			out["comment_id"] = util.UUIDToString(commentID)
		}
	}

	// Blocking is opt-in. Left off, the agent still starts — it just starts
	// with the duplicate already spelled out on the issue.
	out[ResultKeyBlocked] = res.Duplicate != nil && hookConfigBool(config, "block_on_duplicate", false)
	return out, nil
}

func hookConfigBool(config map[string]any, key string, fallback bool) bool {
	switch value := config[key].(type) {
	case bool:
		return value
	case string:
		switch value {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}
