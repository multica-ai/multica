package companybraincensus

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	ParityPopulationApprovalCapability = "company_brain_parity_proof:populate"
	ParityPopulationApprovalBoundary   = "fir_3924_company_brain_parity_population"
	ParityPopulationApprovalIssueID    = "96fdd40e-36fb-4409-9a2e-6c6f1fb665ec"
)

// ParityPopulationApprovalResource binds a human approval to every immutable
// non-secret input in one parity population request. Authorization identity is
// excluded because it identifies the approval row itself.
func ParityPopulationApprovalResource(request ParityPopulationRequest) string {
	pins := struct {
		WorkspaceID                     string `json:"workspace_id"`
		FrozenCensusSHA256              string `json:"frozen_census_sha256"`
		CensusVersion                   int64  `json:"census_version"`
		CompanyBrainConnectionID        string `json:"company_brain_connection_id"`
		ExpectedEligibleAgentCount      int    `json:"eligible_agent_count"`
		ExpectedTargetPermissionsSHA256 string `json:"target_permissions_sha256"`
	}{
		WorkspaceID:                     request.WorkspaceID,
		FrozenCensusSHA256:              request.FrozenCensusSHA256,
		CensusVersion:                   request.CensusVersion,
		CompanyBrainConnectionID:        request.CompanyBrainConnectionID,
		ExpectedEligibleAgentCount:      request.ExpectedEligibleAgentCount,
		ExpectedTargetPermissionsSHA256: request.ExpectedTargetPermissionsSHA256,
	}
	return "workspace:" + request.WorkspaceID + ":company-brain-parity:" + canonicalHash(pins)
}

type parityPopulationApprovalQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// OwnerAdminParityPopulationAuthorizationGate consumes one exact, live,
// single-use approval decided by a workspace owner or admin.
type OwnerAdminParityPopulationAuthorizationGate struct {
	db parityPopulationApprovalQueryer
}

var _ ParityPopulationAuthorizationGate = (*OwnerAdminParityPopulationAuthorizationGate)(nil)

func NewOwnerAdminParityPopulationAuthorizationGate(
	db parityPopulationApprovalQueryer,
) *OwnerAdminParityPopulationAuthorizationGate {
	return &OwnerAdminParityPopulationAuthorizationGate{db: db}
}

func (g *OwnerAdminParityPopulationAuthorizationGate) AuthorizeOnce(
	ctx context.Context,
	request ParityPopulationRequest,
) error {
	if err := validateParityPopulationRequest(request); err != nil {
		return err
	}
	if g == nil || g.db == nil {
		return fmt.Errorf("parity population approval database is required")
	}

	var approvedBy string
	err := g.db.QueryRow(
		ctx,
		consumeParityPopulationApprovalSQL,
		request.AuthorizationID,
		request.WorkspaceID,
		ParityPopulationApprovalCapability,
		ParityPopulationApprovalResource(request),
		ParityPopulationApprovalIssueID,
		ParityPopulationApprovalBoundary,
		request.FrozenCensusSHA256,
		request.CensusVersion,
		request.CompanyBrainConnectionID,
		request.ExpectedEligibleAgentCount,
		request.ExpectedTargetPermissionsSHA256,
	).Scan(&approvedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"population refused: approval must be a live, unused, single-use " +
				"Company Brain parity approval decided by a workspace owner or admin",
		)
	}
	if err != nil {
		return fmt.Errorf("consume Company Brain parity population approval: %w", err)
	}
	return nil
}

const consumeParityPopulationApprovalSQL = `
	UPDATE cerebro_approval_request AS approval
	SET consumed_at = now(), updated_at = now()
	FROM member AS approver
	WHERE approval.id = $1
	  AND approval.workspace_id = $2
	  AND approval.agent_id IS NULL
	  AND approval.capability = $3
	  AND approval.resource = $4
	  AND approval.context ->> 'issue_id' = $5
	  AND approval.context ->> 'approval_boundary' = $6
	  AND approval.context ->> 'frozen_census_sha256' = $7
	  AND approval.context ->> 'census_version' = ($8::bigint)::text
	  AND approval.context ->> 'company_brain_connection_id' = $9
	  AND approval.context ->> 'eligible_agent_count' = ($10::integer)::text
	  AND approval.context ->> 'target_permissions_sha256' = $11
	  AND approval.status = 'approved'
	  AND approval.single_use = TRUE
	  AND approval.consumed_at IS NULL
	  AND (approval.expires_at IS NULL OR approval.expires_at > now())
	  AND approval.decided_by_id = approver.user_id
	  AND approver.workspace_id = approval.workspace_id
	  AND approver.role IN ('owner', 'admin')
	RETURNING approval.decided_by_id::text
`
