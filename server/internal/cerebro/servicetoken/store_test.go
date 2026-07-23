package servicetoken

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type fakeWorkspaceFlagReader struct {
	rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	err  error
}

func (f fakeWorkspaceFlagReader) ListCerebroWorkspaceFeatureFlags(
	context.Context,
	pgtype.UUID,
) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	return f.rows, f.err
}

func TestCerebroStoreFeatureEnabledResolvesDefaultAndOverrides(t *testing.T) {
	lookupErr := errors.New("flag lookup failed")
	tests := []struct {
		name    string
		reader  workspaceFlagReader
		want    bool
		wantErr error
	}{
		{
			name:   "no override uses registry default on",
			reader: fakeWorkspaceFlagReader{},
			want:   true,
		},
		{
			name: "unrelated override still uses registry default on",
			reader: fakeWorkspaceFlagReader{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{
				{FlagKey: "cerebro_workpad", Enabled: false},
			}},
			want: true,
		},
		{
			name: "explicit off",
			reader: fakeWorkspaceFlagReader{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{
				{FlagKey: FlagKey, Enabled: false},
			}},
			want: false,
		},
		{
			name: "explicit on",
			reader: fakeWorkspaceFlagReader{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{
				{FlagKey: FlagKey, Enabled: true},
			}},
			want: true,
		},
		{
			name:    "query error fails closed",
			reader:  fakeWorkspaceFlagReader{err: lookupErr},
			want:    false,
			wantErr: lookupErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &cerebroStore{flags: tt.reader}
			got, err := store.FeatureEnabled(
				context.Background(),
				"11111111-1111-1111-1111-111111111111",
			)
			if got != tt.want {
				t.Fatalf("FeatureEnabled() = %v, want %v", got, tt.want)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FeatureEnabled() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
