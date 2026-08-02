package taskmandate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type enforcementFlagReader struct {
	rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	err  error
}

func (r enforcementFlagReader) ListCerebroWorkspaceFeatureFlags(
	context.Context,
	pgtype.UUID,
) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	return r.rows, r.err
}

func TestEnforcementEnabledDefaultsOffAndOnlyTurnsOnExplicitly(t *testing.T) {
	t.Parallel()
	workspaceID := validUUID()
	tests := []struct {
		name   string
		reader enforcementFlagReader
		want   bool
	}{
		{name: "missing override defaults off"},
		{name: "read failure defaults off", reader: enforcementFlagReader{err: errors.New("database unavailable")}},
		{name: "explicit off", reader: enforcementFlagReader{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: EnforcementFlagKey, Enabled: false}}}},
		{name: "explicit on", reader: enforcementFlagReader{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: EnforcementFlagKey, Enabled: true}}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := EnforcementEnabled(context.Background(), tt.reader, workspaceID); got != tt.want {
				t.Fatalf("EnforcementEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
