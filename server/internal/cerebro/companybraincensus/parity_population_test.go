package companybraincensus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParityPopulationCoordinatorLoadsFrozenEvidenceAndWritesOneEvaluationBatch(t *testing.T) {
	report, target := parityFixture()
	now := time.Date(2026, 7, 30, 9, 45, 0, 0, time.UTC)
	const (
		workspaceID = "workspace-1"
		censusID    = int64(12)
		connection  = "logical-company-brain"
	)

	var calls []string
	frozen := frozenCensusLoaderFunc(func(_ context.Context, gotWorkspaceID string) (FrozenCensus, error) {
		calls = append(calls, "frozen-census")
		if gotWorkspaceID != workspaceID {
			t.Fatalf("frozen census workspace = %q, want %q", gotWorkspaceID, workspaceID)
		}
		return FrozenCensus{
			Report:                   report,
			Version:                  censusID,
			CompanyBrainConnectionID: connection,
		}, nil
	})
	current := currentTargetPermissionLoaderFunc(func(
		_ context.Context,
		gotWorkspaceID string,
		gotConnectionID string,
	) ([]TargetPermission, error) {
		calls = append(calls, "current-target-permissions")
		if gotWorkspaceID != workspaceID || gotConnectionID != connection {
			t.Fatalf(
				"target permission identity = (%q, %q), want (%q, %q)",
				gotWorkspaceID,
				gotConnectionID,
				workspaceID,
				connection,
			)
		}
		return []TargetPermission{target}, nil
	})

	var written [][]ParityEvaluation
	writer := parityProofBatchWriterFunc(func(
		_ context.Context,
		gotWorkspaceID string,
		evaluations []ParityEvaluation,
	) error {
		calls = append(calls, "parity-proof-writer")
		if gotWorkspaceID != workspaceID {
			t.Fatalf("writer workspace = %q, want %q", gotWorkspaceID, workspaceID)
		}
		written = append(written, append([]ParityEvaluation(nil), evaluations...))
		return nil
	})

	coordinator := NewParityPopulationCoordinator(frozen, current, writer)
	coordinator.now = func() time.Time { return now }
	if err := coordinator.Populate(context.Background(), workspaceID); err != nil {
		t.Fatalf("populate parity proofs: %v", err)
	}

	wantCalls := []string{
		"frozen-census",
		"current-target-permissions",
		"parity-proof-writer",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if len(written) != 1 {
		t.Fatalf("writer batches = %d, want exactly one", len(written))
	}
	want := EvaluateParity(report, []TargetPermission{target}, censusID, connection, now)
	if !reflect.DeepEqual(written[0], want) {
		t.Fatalf("written batch differs from EvaluateParity:\n got: %#v\nwant: %#v", written[0], want)
	}
}

func TestParityPopulationCoordinatorStopsAtTheFirstFailedBoundary(t *testing.T) {
	report, target := parityFixture()
	frozenErr := errors.New("frozen census unavailable")
	currentErr := errors.New("current target permissions unavailable")
	writerErr := errors.New("proof write failed")

	tests := []struct {
		name       string
		frozenErr  error
		currentErr error
		writerErr  error
		wantCalls  []string
		wantError  string
	}{
		{
			name:      "frozen census",
			frozenErr: frozenErr,
			wantCalls: []string{"frozen-census"},
			wantError: frozenErr.Error(),
		},
		{
			name:       "current target permissions",
			currentErr: currentErr,
			wantCalls:  []string{"frozen-census", "current-target-permissions"},
			wantError:  currentErr.Error(),
		},
		{
			name:      "parity proof writer",
			writerErr: writerErr,
			wantCalls: []string{"frozen-census", "current-target-permissions", "parity-proof-writer"},
			wantError: writerErr.Error(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			frozen := frozenCensusLoaderFunc(func(context.Context, string) (FrozenCensus, error) {
				calls = append(calls, "frozen-census")
				return FrozenCensus{
					Report:                   report,
					Version:                  12,
					CompanyBrainConnectionID: "logical-company-brain",
				}, test.frozenErr
			})
			current := currentTargetPermissionLoaderFunc(func(
				context.Context,
				string,
				string,
			) ([]TargetPermission, error) {
				calls = append(calls, "current-target-permissions")
				return []TargetPermission{target}, test.currentErr
			})
			writer := parityProofBatchWriterFunc(func(
				context.Context,
				string,
				[]ParityEvaluation,
			) error {
				calls = append(calls, "parity-proof-writer")
				return test.writerErr
			})

			coordinator := NewParityPopulationCoordinator(frozen, current, writer)
			coordinator.now = func() time.Time {
				return time.Date(2026, 7, 30, 9, 45, 0, 0, time.UTC)
			}
			err := coordinator.Populate(context.Background(), "workspace-1")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Populate() error = %v, want %q", err, test.wantError)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", calls, test.wantCalls)
			}
		})
	}
}

type frozenCensusLoaderFunc func(context.Context, string) (FrozenCensus, error)

func (f frozenCensusLoaderFunc) LoadFrozenCensus(
	ctx context.Context,
	workspaceID string,
) (FrozenCensus, error) {
	return f(ctx, workspaceID)
}

type currentTargetPermissionLoaderFunc func(
	context.Context,
	string,
	string,
) ([]TargetPermission, error)

func (f currentTargetPermissionLoaderFunc) LoadCurrentTargetPermissions(
	ctx context.Context,
	workspaceID string,
	companyBrainConnectionID string,
) ([]TargetPermission, error) {
	return f(ctx, workspaceID, companyBrainConnectionID)
}

type parityProofBatchWriterFunc func(context.Context, string, []ParityEvaluation) error

func (f parityProofBatchWriterFunc) Write(
	ctx context.Context,
	workspaceID string,
	evaluations []ParityEvaluation,
) error {
	return f(ctx, workspaceID, evaluations)
}
