package projectplan

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTranslateWriteErrorClassifiesRequiredUniqueIndexes(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       ErrorKind
	}{
		{name: "active plan", constraint: "project_plan_project_id_active_idx", want: ErrorActivePlanExists},
		{name: "project version", constraint: "project_plan_project_version_key", want: ErrorVersionConflict},
		{name: "phase position", constraint: "project_plan_phase_plan_position_key", want: ErrorPositionConflict},
		{name: "part position", constraint: "project_plan_part_phase_position_key", want: ErrorPositionConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := translateWriteError(&pgconn.PgError{Code: "23505", ConstraintName: test.constraint})
			if got := planErrorKind(t, err); got != test.want {
				t.Fatalf("error kind = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTranslateWriteErrorHidesDriverDetails(t *testing.T) {
	err := translateWriteError(&pgconn.PgError{
		Code: "23505", ConstraintName: "project_plan_project_id_active_idx",
		Detail: "duplicate key value violates unique constraint",
	})
	if err.Error() != "this project already has an active plan" {
		t.Fatalf("public error = %q", err)
	}
}
