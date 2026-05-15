package issueguard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func NormalizeTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func DuplicateMessage(identifier, title, status string) string {
	return "Active duplicate issue exists: " + identifier + " " + title + " (status: " + status + "). Set allow_duplicate=true or use --allow-duplicate to create another."
}

type ActiveDuplicateError struct {
	ID         string
	Identifier string
	Title      string
	Status     string
}

func (e *ActiveDuplicateError) Error() string {
	return DuplicateMessage(e.Identifier, e.Title, e.Status)
}

func NewActiveDuplicateError(issue db.Issue, issuePrefix string) *ActiveDuplicateError {
	return &ActiveDuplicateError{
		ID:         util.UUIDToString(issue.ID),
		Identifier: fmt.Sprintf("%s-%d", issuePrefix, issue.Number),
		Title:      issue.Title,
		Status:     issue.Status,
	}
}

func LockAndFindActiveDuplicate(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	parentIssueID pgtype.UUID,
	title string,
	allowDuplicate bool,
) (db.Issue, bool, error) {
	normalizedTitle := NormalizeTitle(title)
	if normalizedTitle == "" {
		return db.Issue{}, false, nil
	}
	if err := q.LockIssueDuplicateKey(ctx, lockKey(workspaceID, projectID, parentIssueID, normalizedTitle)); err != nil {
		return db.Issue{}, false, err
	}
	if allowDuplicate {
		return db.Issue{}, false, nil
	}

	duplicate, err := q.FindActiveDuplicateIssue(ctx, db.FindActiveDuplicateIssueParams{
		WorkspaceID:     workspaceID,
		ProjectID:       projectID,
		ParentIssueID:   parentIssueID,
		NormalizedTitle: normalizedTitle,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, err
	}
	return duplicate, true, nil
}

func lockKey(workspaceID, projectID, parentIssueID pgtype.UUID, normalizedTitle string) string {
	return strings.Join([]string{
		"issue-active-duplicate",
		util.UUIDToString(workspaceID),
		util.UUIDToString(projectID),
		util.UUIDToString(parentIssueID),
		normalizedTitle,
	}, "|")
}
