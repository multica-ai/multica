package companybraincensus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	populationWorkspaceID     = "11111111-1111-4111-8111-111111111111"
	populationAuthorizationID = "22222222-2222-4222-8222-222222222222"
	populationConnectionID    = "33333333-3333-4333-8333-333333333333"
	populationSnapshotSHA256  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestParityPopulationBoundaryAuthorizesAndRevalidatesPinnedInputsBeforeWriting(t *testing.T) {
	report, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	targets := []TargetPermission{target}
	targetSHA256, err := TargetPermissionsSHA256(targets)
	if err != nil {
		t.Fatalf("hash target permissions: %v", err)
	}
	request := ParityPopulationRequest{
		AuthorizationID:                 populationAuthorizationID,
		WorkspaceID:                     populationWorkspaceID,
		FrozenCensusSHA256:              populationSnapshotSHA256,
		CensusVersion:                   12,
		CompanyBrainConnectionID:        populationConnectionID,
		ExpectedEligibleAgentCount:      1,
		ExpectedTargetPermissionsSHA256: targetSHA256,
	}

	var calls []string
	gate := parityPopulationAuthorizationGateFunc(func(
		_ context.Context,
		got ParityPopulationRequest,
	) error {
		calls = append(calls, "authorization")
		if !reflect.DeepEqual(got, request) {
			t.Fatalf("authorization request = %#v, want %#v", got, request)
		}
		return nil
	})
	frozen := frozenCensusLoaderFunc(func(
		_ context.Context,
		workspaceID string,
	) (FrozenCensus, error) {
		calls = append(calls, "frozen-census")
		return FrozenCensus{
			Report:                   report,
			Version:                  request.CensusVersion,
			CompanyBrainConnectionID: request.CompanyBrainConnectionID,
			SnapshotSHA256:           request.FrozenCensusSHA256,
		}, nil
	})
	current := currentTargetPermissionLoaderFunc(func(
		_ context.Context,
		_ string,
		_ string,
		_ Report,
	) ([]TargetPermission, error) {
		calls = append(calls, "current-target-permissions")
		return targets, nil
	})
	writer := parityProofBatchWriterFunc(func(
		_ context.Context,
		_ string,
		evaluations []ParityEvaluation,
	) error {
		calls = append(calls, "parity-proof-writer")
		if len(evaluations) != request.ExpectedEligibleAgentCount {
			t.Fatalf(
				"evaluation count = %d, want %d",
				len(evaluations),
				request.ExpectedEligibleAgentCount,
			)
		}
		return nil
	})

	coordinator := NewParityPopulationCoordinator(gate, frozen, current, writer)
	coordinator.now = func() time.Time {
		return time.Date(2026, 7, 30, 10, 45, 0, 0, time.UTC)
	}
	if err := coordinator.Populate(context.Background(), request); err != nil {
		t.Fatalf("populate parity proofs: %v", err)
	}

	wantCalls := []string{
		"authorization",
		"frozen-census",
		"current-target-permissions",
		"parity-proof-writer",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestParityPopulationBoundaryFailsClosedBeforeReadsWhenAuthorizationIsDenied(t *testing.T) {
	denied := errors.New("population authorization denied")
	var calls []string
	gate := parityPopulationAuthorizationGateFunc(func(
		context.Context,
		ParityPopulationRequest,
	) error {
		calls = append(calls, "authorization")
		return denied
	})
	frozen := frozenCensusLoaderFunc(func(
		context.Context,
		string,
	) (FrozenCensus, error) {
		calls = append(calls, "frozen-census")
		return FrozenCensus{}, nil
	})
	current := currentTargetPermissionLoaderFunc(func(
		context.Context,
		string,
		string,
		Report,
	) ([]TargetPermission, error) {
		calls = append(calls, "current-target-permissions")
		return nil, nil
	})
	writer := parityProofBatchWriterFunc(func(
		context.Context,
		string,
		[]ParityEvaluation,
	) error {
		calls = append(calls, "parity-proof-writer")
		return nil
	})

	coordinator := NewParityPopulationCoordinator(gate, frozen, current, writer)
	err := coordinator.Populate(context.Background(), validParityPopulationRequest(t))
	if err == nil || !strings.Contains(err.Error(), denied.Error()) {
		t.Fatalf("Populate() error = %v, want authorization denial", err)
	}
	if want := []string{"authorization"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestParityPopulationBoundaryRejectsInvalidRequestBeforeAuthorization(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ParityPopulationRequest)
		wantError string
	}{
		{
			name: "authorization identity",
			mutate: func(request *ParityPopulationRequest) {
				request.AuthorizationID = ""
			},
			wantError: "invalid parity population authorization identity",
		},
		{
			name: "workspace identity",
			mutate: func(request *ParityPopulationRequest) {
				request.WorkspaceID = ""
			},
			wantError: "invalid parity population workspace identity",
		},
		{
			name: "snapshot checksum",
			mutate: func(request *ParityPopulationRequest) {
				request.FrozenCensusSHA256 = "not-a-checksum"
			},
			wantError: "invalid frozen census snapshot checksum",
		},
		{
			name: "census version",
			mutate: func(request *ParityPopulationRequest) {
				request.CensusVersion = 0
			},
			wantError: "parity population census version must be positive",
		},
		{
			name: "logical connection identity",
			mutate: func(request *ParityPopulationRequest) {
				request.CompanyBrainConnectionID = ""
			},
			wantError: "invalid parity population logical connection identity",
		},
		{
			name: "eligible agent count",
			mutate: func(request *ParityPopulationRequest) {
				request.ExpectedEligibleAgentCount = 0
			},
			wantError: "expected eligible agent count must be positive",
		},
		{
			name: "target evidence checksum",
			mutate: func(request *ParityPopulationRequest) {
				request.ExpectedTargetPermissionsSHA256 = "not-a-checksum"
			},
			wantError: "invalid target permission evidence checksum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validParityPopulationRequest(t)
			test.mutate(&request)
			var authorized bool
			coordinator := NewParityPopulationCoordinator(
				parityPopulationAuthorizationGateFunc(func(
					context.Context,
					ParityPopulationRequest,
				) error {
					authorized = true
					return nil
				}),
				nil,
				nil,
				nil,
			)
			err := coordinator.Populate(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Populate() error = %v, want %q", err, test.wantError)
			}
			if authorized {
				t.Fatal("invalid request reached authorization gate")
			}
		})
	}
}

func TestParityPopulationBoundaryRejectsPinnedEvidenceDriftBeforeWriting(t *testing.T) {
	report, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID

	tests := []struct {
		name       string
		mutate     func(*FrozenCensus, *[]TargetPermission)
		wantError  string
		wantWriter bool
	}{
		{
			name: "snapshot checksum",
			mutate: func(frozen *FrozenCensus, _ *[]TargetPermission) {
				frozen.SnapshotSHA256 = strings.Repeat("b", 64)
			},
			wantError: "frozen census snapshot checksum changed",
		},
		{
			name: "census version",
			mutate: func(frozen *FrozenCensus, _ *[]TargetPermission) {
				frozen.Version++
			},
			wantError: "frozen census version changed",
		},
		{
			name: "logical connection",
			mutate: func(frozen *FrozenCensus, _ *[]TargetPermission) {
				frozen.CompanyBrainConnectionID = "44444444-4444-4444-8444-444444444444"
			},
			wantError: "frozen census logical connection changed",
		},
		{
			name: "eligible actor count",
			mutate: func(frozen *FrozenCensus, _ *[]TargetPermission) {
				frozen.Report.Actors = append(frozen.Report.Actors, frozen.Report.Actors[0])
			},
			wantError: "frozen census eligible agent count changed",
		},
		{
			name: "target permission evidence",
			mutate: func(_ *FrozenCensus, targets *[]TargetPermission) {
				(*targets)[0].AccessVersion++
			},
			wantError: "target permission evidence changed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validParityPopulationRequest(t)
			frozenEvidence := FrozenCensus{
				Report:                   report,
				Version:                  request.CensusVersion,
				CompanyBrainConnectionID: request.CompanyBrainConnectionID,
				SnapshotSHA256:           request.FrozenCensusSHA256,
			}
			targets := []TargetPermission{target}
			test.mutate(&frozenEvidence, &targets)

			var writerCalled bool
			coordinator := NewParityPopulationCoordinator(
				parityPopulationAuthorizationGateFunc(func(
					context.Context,
					ParityPopulationRequest,
				) error {
					return nil
				}),
				frozenCensusLoaderFunc(func(
					context.Context,
					string,
				) (FrozenCensus, error) {
					return frozenEvidence, nil
				}),
				currentTargetPermissionLoaderFunc(func(
					context.Context,
					string,
					string,
					Report,
				) ([]TargetPermission, error) {
					return targets, nil
				}),
				parityProofBatchWriterFunc(func(
					context.Context,
					string,
					[]ParityEvaluation,
				) error {
					writerCalled = true
					return nil
				}),
			)
			err := coordinator.Populate(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Populate() error = %v, want %q", err, test.wantError)
			}
			if writerCalled != test.wantWriter {
				t.Fatalf("writer called = %v, want %v", writerCalled, test.wantWriter)
			}
		})
	}
}

func TestParityPopulationBoundaryIsolatesFrozenAndTargetEvidenceFromLoaderMutation(t *testing.T) {
	report, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	request := validParityPopulationRequest(t)

	var written []ParityEvaluation
	coordinator := NewParityPopulationCoordinator(
		parityPopulationAuthorizationGateFunc(func(
			context.Context,
			ParityPopulationRequest,
		) error {
			return nil
		}),
		frozenCensusLoaderFunc(func(
			context.Context,
			string,
		) (FrozenCensus, error) {
			return FrozenCensus{
				Report:                   report,
				Version:                  request.CensusVersion,
				CompanyBrainConnectionID: request.CompanyBrainConnectionID,
				SnapshotSHA256:           request.FrozenCensusSHA256,
			}, nil
		}),
		currentTargetPermissionLoaderFunc(func(
			_ context.Context,
			_ string,
			_ string,
			loaderReport Report,
		) ([]TargetPermission, error) {
			loaderReport.Actors[0].AgentID = "mutated-after-validation"
			loaderReport.Actors[0].Sources[0].Claim.WriteSource = "mutated"
			targets := []TargetPermission{target}
			return targets, nil
		}),
		parityProofBatchWriterFunc(func(
			_ context.Context,
			_ string,
			evaluations []ParityEvaluation,
		) error {
			written = append([]ParityEvaluation(nil), evaluations...)
			return nil
		}),
	)
	coordinator.now = func() time.Time {
		return time.Date(2026, 7, 30, 10, 45, 0, 0, time.UTC)
	}
	if err := coordinator.Populate(context.Background(), request); err != nil {
		t.Fatalf("populate parity proofs: %v", err)
	}
	if len(written) != 1 ||
		written[0].AgentID != report.Actors[0].AgentID ||
		written[0].Status != ParityMatched {
		t.Fatalf("written evaluations used mutated loader evidence: %#v", written)
	}
}

func TestTargetPermissionsSHA256IsOrderInvariantAndRejectsInvalidEvidence(t *testing.T) {
	_, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	reordered := target
	reordered.AllowedReadSources = []string{"shared", "commercial"}
	reordered.CanonicalToolCalls = []string{"search", "add_fact"}
	reordered.ApprovalOutcomes = append(
		[]ScopedToolDecision(nil),
		target.ApprovalOutcomes...,
	)
	for left, right := 0, len(reordered.ApprovalOutcomes)-1; left < right; left, right = left+1, right-1 {
		reordered.ApprovalOutcomes[left], reordered.ApprovalOutcomes[right] =
			reordered.ApprovalOutcomes[right], reordered.ApprovalOutcomes[left]
	}

	first, err := TargetPermissionsSHA256([]TargetPermission{target})
	if err != nil {
		t.Fatalf("hash target permissions: %v", err)
	}
	second, err := TargetPermissionsSHA256([]TargetPermission{reordered})
	if err != nil {
		t.Fatalf("hash reordered target permissions: %v", err)
	}
	if first != second {
		t.Fatalf("order changed hash: %q != %q", first, second)
	}

	invalid := target
	invalid.AllowedReadSources = []string{"shared", "shared"}
	if _, err := TargetPermissionsSHA256([]TargetPermission{invalid}); err == nil {
		t.Fatal("duplicate target evidence accepted")
	}
}

func validParityPopulationRequest(t *testing.T) ParityPopulationRequest {
	t.Helper()
	_, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	targetSHA256, err := TargetPermissionsSHA256([]TargetPermission{target})
	if err != nil {
		t.Fatalf("hash target permissions: %v", err)
	}
	return ParityPopulationRequest{
		AuthorizationID:                 populationAuthorizationID,
		WorkspaceID:                     populationWorkspaceID,
		FrozenCensusSHA256:              populationSnapshotSHA256,
		CensusVersion:                   12,
		CompanyBrainConnectionID:        populationConnectionID,
		ExpectedEligibleAgentCount:      1,
		ExpectedTargetPermissionsSHA256: targetSHA256,
	}
}

type parityPopulationAuthorizationGateFunc func(
	context.Context,
	ParityPopulationRequest,
) error

func (f parityPopulationAuthorizationGateFunc) AuthorizeOnce(
	ctx context.Context,
	request ParityPopulationRequest,
) error {
	return f(ctx, request)
}
