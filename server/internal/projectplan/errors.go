package projectplan

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type ErrorKind string

const (
	ErrorDisabled           ErrorKind = "disabled"
	ErrorInvalid            ErrorKind = "invalid"
	ErrorNotFound           ErrorKind = "not_found"
	ErrorNotActive          ErrorKind = "not_active"
	ErrorActivePlanExists   ErrorKind = "active_plan_exists"
	ErrorVersionConflict    ErrorKind = "version_conflict"
	ErrorPositionConflict   ErrorKind = "position_conflict"
	ErrorIssueAlreadyLinked ErrorKind = "issue_already_linked"
	ErrorUnavailable        ErrorKind = "unavailable"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

func domainError(kind ErrorKind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

func translateWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return domainError(ErrorUnavailable, "project plan write failed", err)
	}

	switch pgErr.ConstraintName {
	case "project_plan_project_id_active_idx":
		return domainError(ErrorActivePlanExists, "this project already has an active plan", err)
	case "project_plan_project_version_key":
		return domainError(ErrorVersionConflict, "this project plan version already exists", err)
	case "project_plan_phase_plan_position_key":
		return domainError(ErrorPositionConflict, "another phase already uses this position", err)
	case "project_plan_part_phase_position_key":
		return domainError(ErrorPositionConflict, "another part already uses this position", err)
	case "project_plan_part_issue_plan_issue_key":
		return domainError(ErrorIssueAlreadyLinked, "this issue is already linked to a part in this plan", err)
	default:
		return domainError(ErrorUnavailable, "project plan write conflicted", err)
	}
}
