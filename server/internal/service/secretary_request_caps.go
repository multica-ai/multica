package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxRequestTreeWalk bounds the walk up a parent chain. Trees deeper than this are
// not produced by the Secretary; the bound only protects against a corrupt cycle.
const maxRequestTreeWalk = 16

// CheckSecretaryRequestCaps enforces the request caps (spec 13.7.1) on an issue
// about to be created:
//   - when the Secretary creates a top-level issue in the Secretary desk, it is a
//     request and counts against the open-request cap;
//   - when the Secretary creates a child of a request, the child is an addressee
//     and counts against the addressee cap;
//   - for any creator, an issue under a request's tree may not sit deeper than
//     MaxDelegationDepth hops from the request (Secretary to chief to leader is
//     two). Issues outside a request tree are never limited here.
//
// isSecretary is true when the creator is the workspace's Secretary agent.
func CheckSecretaryRequestCaps(ctx context.Context, q *db.Queries, workspaceID, projectID, parentID pgtype.UUID, isSecretary bool) error {
	desk, err := findSecretaryDesk(ctx, q, workspaceID)
	if err != nil {
		return fmt.Errorf("find secretary desk: %w", err)
	}
	if !desk.Valid {
		return nil
	}
	if !parentID.Valid {
		if isSecretary && projectID == desk {
			open, err := q.CountOpenSecretaryRequests(ctx, db.CountOpenSecretaryRequestsParams{
				WorkspaceID: workspaceID, ProjectID: desk,
			})
			if err != nil {
				return fmt.Errorf("count open requests: %w", err)
			}
			return CheckGuardrail(GuardrailOpenRequests, int(open)+1)
		}
		return nil
	}

	parent, depth, found, err := requestTreePosition(ctx, q, workspaceID, parentID, desk)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if isSecretary && depth == 0 {
		children, err := q.ListChildIssues(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("count addressees: %w", err)
		}
		if err := CheckGuardrail(GuardrailAddressees, len(children)+1); err != nil {
			return err
		}
	}
	// The new issue sits one hop below the parent.
	return CheckGuardrail(GuardrailDelegationDepth, depth+1)
}

// requestTreePosition walks up from parentID to its root. found is false when the
// root is not a request in the Secretary desk. depth is the parent's hop count
// from that root: 0 for the request itself, 1 for its children, and so on.
func requestTreePosition(ctx context.Context, q *db.Queries, workspaceID, parentID, desk pgtype.UUID) (db.Issue, int, bool, error) {
	cur, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: parentID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, 0, false, nil
	}
	if err != nil {
		return db.Issue{}, 0, false, fmt.Errorf("load parent issue: %w", err)
	}
	parent := cur
	// Each step up is one hop, so depth ends as the parent's distance from the root.
	depth := 0
	for i := 0; cur.ParentIssueID.Valid; i++ {
		if i >= maxRequestTreeWalk {
			return db.Issue{}, 0, false, nil
		}
		next, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: cur.ParentIssueID, WorkspaceID: workspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return db.Issue{}, 0, false, fmt.Errorf("walk parent chain: %w", err)
		}
		cur = next
		depth++
	}
	if cur.ProjectID != desk {
		return db.Issue{}, 0, false, nil
	}
	return parent, depth, true, nil
}
