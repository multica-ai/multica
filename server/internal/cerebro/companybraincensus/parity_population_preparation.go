package companybraincensus

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util"
)

// ParityPopulationInputs contains the non-secret evidence pins needed to
// request a later one-shot population authorization. It is not an
// authorization and cannot invoke population.
type ParityPopulationInputs struct {
	WorkspaceID                     string `json:"workspace_id"`
	FrozenCensusSHA256              string `json:"frozen_census_sha256"`
	CensusVersion                   int64  `json:"census_version"`
	CompanyBrainConnectionID        string `json:"company_brain_connection_id"`
	ExpectedEligibleAgentCount      int    `json:"eligible_agent_count"`
	ExpectedTargetPermissionsSHA256 string `json:"target_permissions_sha256"`
}

// ParityPopulationPreparer reads the frozen census and current target
// permissions without exposing an authorization gate or proof writer.
type ParityPopulationPreparer struct {
	frozen  FrozenCensusLoader
	current CurrentTargetPermissionLoader
}

func NewParityPopulationPreparer(
	frozen FrozenCensusLoader,
	current CurrentTargetPermissionLoader,
) *ParityPopulationPreparer {
	return &ParityPopulationPreparer{frozen: frozen, current: current}
}

// Prepare returns the exact immutable inputs that a separately authorized
// population request must pin.
func (p *ParityPopulationPreparer) Prepare(
	ctx context.Context,
	workspaceID string,
) (ParityPopulationInputs, error) {
	if err := ctx.Err(); err != nil {
		return ParityPopulationInputs{}, err
	}
	if _, err := util.ParseUUID(workspaceID); err != nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"invalid parity population workspace identity: %w",
			err,
		)
	}
	if p == nil || p.frozen == nil {
		return ParityPopulationInputs{}, fmt.Errorf("frozen census loader is required")
	}
	if p.current == nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"current target permission loader is required",
		)
	}

	frozen, err := p.frozen.LoadFrozenCensus(ctx, workspaceID)
	if err != nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"load frozen Company Brain census: %w",
			err,
		)
	}
	if !sha256Hex.MatchString(frozen.SnapshotSHA256) {
		return ParityPopulationInputs{}, fmt.Errorf(
			"invalid frozen census snapshot checksum",
		)
	}
	if frozen.Version <= 0 {
		return ParityPopulationInputs{}, fmt.Errorf(
			"parity population census version must be positive",
		)
	}
	if _, err := util.ParseUUID(frozen.CompanyBrainConnectionID); err != nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"invalid parity population logical connection identity: %w",
			err,
		)
	}
	if len(frozen.Report.Actors) == 0 {
		return ParityPopulationInputs{}, fmt.Errorf(
			"expected eligible agent count must be positive",
		)
	}

	targetLoaderReport, err := cloneParityPopulationReport(frozen.Report)
	if err != nil {
		return ParityPopulationInputs{}, err
	}
	targets, err := p.current.LoadCurrentTargetPermissions(
		ctx,
		workspaceID,
		frozen.CompanyBrainConnectionID,
		targetLoaderReport,
	)
	if err != nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"load current Company Brain target permissions: %w",
			err,
		)
	}
	targetSHA256, err := TargetPermissionsSHA256(targets)
	if err != nil {
		return ParityPopulationInputs{}, fmt.Errorf(
			"hash current Company Brain target permissions: %w",
			err,
		)
	}

	return ParityPopulationInputs{
		WorkspaceID:                     workspaceID,
		FrozenCensusSHA256:              frozen.SnapshotSHA256,
		CensusVersion:                   frozen.Version,
		CompanyBrainConnectionID:        frozen.CompanyBrainConnectionID,
		ExpectedEligibleAgentCount:      len(frozen.Report.Actors),
		ExpectedTargetPermissionsSHA256: targetSHA256,
	}, nil
}
