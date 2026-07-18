package issueguard

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ParentHasIncompleteDescendantsCode = "parent_has_incomplete_descendants"
	ParentMustBeReopenedCode           = "parent_must_be_reopened"
)

// ParentStateConflict is the transport-safe view of the database constraint.
// It carries only the affected parent ID and, when a parent completion was
// denied, the aggregate incomplete count; no descendant title or detail can
// leak through this error path.
type ParentStateConflict struct {
	Code                      string
	ParentIssueID             string
	IncompleteDescendantCount *int
}

func ParentStateConflictFrom(err error) (ParentStateConflict, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
		return ParentStateConflict{}, false
	}
	if pgErr.Message != ParentHasIncompleteDescendantsCode && pgErr.Message != ParentMustBeReopenedCode {
		return ParentStateConflict{}, false
	}

	conflict := ParentStateConflict{Code: pgErr.Message}
	for _, part := range strings.Split(pgErr.Detail, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "parent_issue_id":
			conflict.ParentIssueID = value
		case "incomplete_descendant_count":
			if count, err := strconv.Atoi(value); err == nil {
				conflict.IncompleteDescendantCount = &count
			}
		}
	}
	return conflict, conflict.ParentIssueID != ""
}

func ParentStateConflictMessage(code string) string {
	switch code {
	case ParentHasIncompleteDescendantsCode:
		return "parent issue has incomplete descendants"
	case ParentMustBeReopenedCode:
		return "parent issue must be reopened before activating a child issue"
	default:
		return "parent issue state conflict"
	}
}
