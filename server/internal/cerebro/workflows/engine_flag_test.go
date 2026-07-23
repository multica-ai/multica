package workflows

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestWorkflowsEngineFlagOn(t *testing.T) {
	tests := []struct {
		name string
		rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
		want bool
	}{
		{name: "no override defaults off", rows: nil, want: false},
		{
			name: "unrelated flags leave engine off",
			rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: "cerebro_workflows", Enabled: true}},
			want: false,
		},
		{
			name: "engine override on",
			rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: FlagWorkflowsEngine, Enabled: true}},
			want: true,
		},
		{
			name: "engine override off wins over default",
			rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: FlagWorkflowsEngine, Enabled: false}},
			want: false,
		},
		{
			name: "engine override found among others",
			rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{
				{FlagKey: "cerebro_evals", Enabled: false},
				{FlagKey: FlagWorkflowsEngine, Enabled: true},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workflowsEngineFlagOn(tt.rows); got != tt.want {
				t.Fatalf("workflowsEngineFlagOn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEngineEnabledForWorkspace_EnvMasterForcesOn(t *testing.T) {
	// enabled=true (CEREBRO_WORKFLOWS_ENABLED master switch) forces the engine
	// on for any workspace without touching the DB.
	s := &Service{enabled: true}
	if !s.engineEnabledForWorkspace(context.Background(), pgtype.UUID{}) {
		t.Fatal("env master-on must force the engine enabled")
	}
}

func TestEngineEnabledForWorkspace_NoQueriesIsOff(t *testing.T) {
	// enabled=false and no flag store: default off, never panics.
	s := &Service{}
	wsID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	if s.engineEnabledForWorkspace(context.Background(), wsID) {
		t.Fatal("engine must be off when the flag store is unavailable")
	}
}
