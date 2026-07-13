package operatingsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidateStrategyInput(t *testing.T) {
	tests := []struct {
		name    string
		input   StrategyItemInput
		wantErr bool
	}{
		{name: "core value", input: StrategyItemInput{Kind: "core_value", Title: "Care deeply"}},
		{name: "horizon goal", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand", HorizonUnit: "year", HorizonCount: 3}},
		{name: "invalid kind", input: StrategyItemInput{Kind: "objective", Title: "Expand"}, wantErr: true},
		{name: "missing title", input: StrategyItemInput{Kind: "core_focus"}, wantErr: true},
		{name: "missing horizon", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand"}, wantErr: true},
		{name: "invalid horizon", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand", HorizonUnit: "quarter", HorizonCount: 1}, wantErr: true},
		{name: "horizon on core value", input: StrategyItemInput{Kind: "core_value", Title: "Care", HorizonUnit: "year", HorizonCount: 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStrategyInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateStrategyInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRockInput(t *testing.T) {
	valid := RockInput{
		ProjectID:      "550e8400-e29b-41d4-a716-446655440000",
		PeriodStart:    "2026-07-01",
		PeriodEnd:      "2026-09-30",
		Confidence:     70,
		ReportedHealth: "on_track",
	}
	tests := []struct {
		name   string
		mutate func(*RockInput)
	}{
		{name: "reversed dates", mutate: func(v *RockInput) { v.PeriodEnd = "2026-06-30" }},
		{name: "low confidence", mutate: func(v *RockInput) { v.Confidence = -1 }},
		{name: "high confidence", mutate: func(v *RockInput) { v.Confidence = 101 }},
		{name: "bad health", mutate: func(v *RockInput) { v.ReportedHealth = "green" }},
		{name: "bad project id", mutate: func(v *RockInput) { v.ProjectID = "not-a-uuid" }},
	}

	if err := ValidateRockInput(valid); err != nil {
		t.Fatalf("valid RockInput rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.mutate(&input)
			if err := ValidateRockInput(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizeTerminologyFallsBackPerField(t *testing.T) {
	got := NormalizeTerminology([]byte(`{"strategy":"Direction","rock":12,"rocks":""}`))
	if got.Strategy != "Direction" || got.Rock != "Rock" || got.Rocks != "Rocks" {
		t.Fatalf("unexpected terminology: %#v", got)
	}

	if got := NormalizeTerminology([]byte(`not-json`)); got != DefaultTerminology() {
		t.Fatalf("malformed JSON fallback = %#v", got)
	}
}

func TestDeriveHealthExplainsIssueContributors(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		total   int32
		done    int32
		blocked int32
		want    string
	}{
		{name: "blocked wins", total: 4, done: 1, blocked: 1, want: "off_track"},
		{name: "all done", total: 4, done: 4, want: "on_track"},
		{name: "work remains", total: 4, done: 1, want: "at_risk"},
		{name: "no issues", want: "at_risk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveHealth(tt.total, tt.done, tt.blocked, now)
			if got.State != tt.want || got.Reason == "" || got.CalculatedAt != now.Format(time.RFC3339) {
				t.Fatalf("DeriveHealth() = %#v", got)
			}
		})
	}
}

func TestEnsureProjectInWorkspaceRejectsCrossWorkspaceProject(t *testing.T) {
	projectID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	reader := fakeProjectReader{err: pgx.ErrNoRows}

	err := EnsureProjectInWorkspace(context.Background(), reader, projectID, workspaceID)
	if !errors.Is(err, ErrProjectNotInWorkspace) {
		t.Fatalf("error = %v, want ErrProjectNotInWorkspace", err)
	}
}

func TestDuplicateConnectionDetection(t *testing.T) {
	if !IsDuplicateConnection(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violation must be reported as duplicate connection")
	}
	if IsDuplicateConnection(errors.New("other")) {
		t.Fatal("ordinary error must not be reported as duplicate")
	}
}

type fakeProjectReader struct {
	project db.Project
	err     error
}

func (f fakeProjectReader) GetProjectInWorkspace(context.Context, db.GetProjectInWorkspaceParams) (db.Project, error) {
	return f.project, f.err
}
