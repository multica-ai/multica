package companybraincensus

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestParityPopulationPreparerReturnsExactReadOnlyAuthorizationInputs(t *testing.T) {
	report, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	wantTargetSHA256, err := TargetPermissionsSHA256([]TargetPermission{target})
	if err != nil {
		t.Fatalf("hash target permission fixture: %v", err)
	}

	var calls []string
	frozen := frozenCensusLoaderFunc(func(
		_ context.Context,
		workspaceID string,
	) (FrozenCensus, error) {
		calls = append(calls, "frozen-census")
		if workspaceID != populationWorkspaceID {
			t.Fatalf("workspace = %q, want %q", workspaceID, populationWorkspaceID)
		}
		return FrozenCensus{
			Report:                   report,
			Version:                  12,
			CompanyBrainConnectionID: populationConnectionID,
			SnapshotSHA256:           populationSnapshotSHA256,
		}, nil
	})
	current := currentTargetPermissionLoaderFunc(func(
		_ context.Context,
		workspaceID string,
		connectionID string,
		gotReport Report,
	) ([]TargetPermission, error) {
		calls = append(calls, "current-target-permissions")
		if workspaceID != populationWorkspaceID ||
			connectionID != populationConnectionID ||
			!reflect.DeepEqual(gotReport, report) {
			t.Fatalf(
				"target loader inputs = (%q, %q, %#v), want frozen evidence",
				workspaceID,
				connectionID,
				gotReport,
			)
		}
		return []TargetPermission{target}, nil
	})

	got, err := NewParityPopulationPreparer(frozen, current).Prepare(
		context.Background(),
		populationWorkspaceID,
	)
	if err != nil {
		t.Fatalf("prepare parity population inputs: %v", err)
	}
	want := ParityPopulationInputs{
		WorkspaceID:                     populationWorkspaceID,
		FrozenCensusSHA256:              populationSnapshotSHA256,
		CensusVersion:                   12,
		CompanyBrainConnectionID:        populationConnectionID,
		ExpectedEligibleAgentCount:      len(report.Actors),
		ExpectedTargetPermissionsSHA256: wantTargetSHA256,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prepared inputs = %#v, want %#v", got, want)
	}
	if wantCalls := []string{
		"frozen-census",
		"current-target-permissions",
	}; !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestParityPopulationPreparerStopsWhenRealTargetPermissionRowsDoNotExist(t *testing.T) {
	report, _ := parityFixture()
	var calls []string
	frozen := frozenCensusLoaderFunc(func(
		context.Context,
		string,
	) (FrozenCensus, error) {
		calls = append(calls, "frozen-census")
		return FrozenCensus{
			Report:                   report,
			Version:                  12,
			CompanyBrainConnectionID: populationConnectionID,
			SnapshotSHA256:           populationSnapshotSHA256,
		}, nil
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

	_, err := NewParityPopulationPreparer(frozen, current).Prepare(
		context.Background(),
		populationWorkspaceID,
	)
	if err == nil || !strings.Contains(err.Error(), "target permission evidence is empty") {
		t.Fatalf("Prepare() error = %v, want missing target permission boundary", err)
	}
	if wantCalls := []string{
		"frozen-census",
		"current-target-permissions",
	}; !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestParityPopulationPreparerRejectsInvalidTargetPermissionEvidence(t *testing.T) {
	report, target := parityFixture()
	target.CompanyBrainConnectionID = populationConnectionID
	target.AccessVersion = 0
	frozen := frozenCensusLoaderFunc(func(
		context.Context,
		string,
	) (FrozenCensus, error) {
		return FrozenCensus{
			Report:                   report,
			Version:                  12,
			CompanyBrainConnectionID: populationConnectionID,
			SnapshotSHA256:           populationSnapshotSHA256,
		}, nil
	})
	current := currentTargetPermissionLoaderFunc(func(
		context.Context,
		string,
		string,
		Report,
	) ([]TargetPermission, error) {
		return []TargetPermission{target}, nil
	})

	_, err := NewParityPopulationPreparer(frozen, current).Prepare(
		context.Background(),
		populationWorkspaceID,
	)
	if err == nil || !strings.Contains(err.Error(), "identity or version is invalid") {
		t.Fatalf("Prepare() error = %v, want invalid target evidence boundary", err)
	}
}
