package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/companybraincensus"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

type options struct {
	databaseURL    string
	workspaceID    string
	snapshotFile   string
	snapshotSHA256 string
	apply          bool
	approvalID     string
}

type approvalContext struct {
	IssueID                 string `json:"issue_id"`
	ApprovalBoundary        string `json:"approval_boundary"`
	FrozenCensusSHA256      string `json:"frozen_census_sha256"`
	CensusVersion           int64  `json:"census_version"`
	CompanyBrainConnection  string `json:"company_brain_connection_id"`
	EligibleAgentCount      int    `json:"eligible_agent_count"`
	TargetPermissionsSHA256 string `json:"target_permissions_sha256"`
}

type parityPopulationPlan struct {
	Mode       string                                    `json:"mode"`
	Inputs     companybraincensus.ParityPopulationInputs `json:"inputs"`
	Capability string                                    `json:"approval_capability"`
	Resource   string                                    `json:"approval_resource"`
	Context    approvalContext                           `json:"approval_context"`
}

type stateFingerprints struct {
	connections string
	permissions string
}

type parityPopulationResult struct {
	Mode                       string `json:"mode"`
	ExpectedEligibleAgentCount int    `json:"expected_eligible_agent_count"`
	ProofCount                 int    `json:"proof_count"`
	MatchedCount               int    `json:"matched_count"`
	DoNotMigrateCount          int    `json:"do_not_migrate_count"`
	UnresolvedCount            int    `json:"unresolved_count"`
	ConnectionsUnchanged       bool   `json:"connections_unchanged"`
	PermissionsUnchanged       bool   `json:"permissions_unchanged"`
	CutoverReady               bool   `json:"cutover_ready"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Company Brain parity proof:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("cerebro_company_brain_parity_proof", flag.ContinueOnError)
	var opts options
	flags.StringVar(
		&opts.databaseURL,
		"database-url",
		os.Getenv("DATABASE_URL"),
		"current database URL",
	)
	flags.StringVar(
		&opts.workspaceID,
		"workspace-id",
		"",
		"workspace UUID whose frozen census is being proved",
	)
	flags.StringVar(
		&opts.snapshotFile,
		"snapshot-file",
		"",
		"checksum-pinned frozen census JSON file",
	)
	flags.StringVar(
		&opts.snapshotSHA256,
		"snapshot-sha256",
		"",
		"SHA-256 of the exact frozen census JSON bytes",
	)
	flags.BoolVar(
		&opts.apply,
		"apply",
		false,
		"populate parity proof rows; default is read-only dry-run",
	)
	flags.StringVar(
		&opts.approvalID,
		"approval-id",
		"",
		"single-use Company Brain parity population approval UUID",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := validateOptions(opts); err != nil {
		return err
	}

	rawSnapshot, err := os.ReadFile(opts.snapshotFile)
	if err != nil {
		return fmt.Errorf("read frozen census snapshot: %w", err)
	}
	pool, err := pgxpool.New(ctx, opts.databaseURL)
	if err != nil {
		return fmt.Errorf("open current database: %w", err)
	}
	defer pool.Close()

	frozen := companybraincensus.NewFrozenCensusSnapshotLoader(
		rawSnapshot,
		opts.snapshotSHA256,
	)
	current := companybraincensus.NewDatabaseCurrentTargetPermissionLoader(
		pool,
		toolpolicy.NewStore(pool),
	)
	inputs, err := companybraincensus.NewParityPopulationPreparer(
		frozen,
		current,
	).Prepare(ctx, opts.workspaceID)
	if err != nil {
		return fmt.Errorf("prepare parity population: %w", err)
	}

	request := companybraincensus.ParityPopulationRequest{
		AuthorizationID:                 opts.approvalID,
		WorkspaceID:                     inputs.WorkspaceID,
		FrozenCensusSHA256:              inputs.FrozenCensusSHA256,
		CensusVersion:                   inputs.CensusVersion,
		CompanyBrainConnectionID:        inputs.CompanyBrainConnectionID,
		ExpectedEligibleAgentCount:      inputs.ExpectedEligibleAgentCount,
		ExpectedTargetPermissionsSHA256: inputs.ExpectedTargetPermissionsSHA256,
	}
	plan := buildPopulationPlan(inputs, request)
	if !opts.apply {
		return printJSON(plan)
	}

	before, err := loadStateFingerprints(ctx, pool, opts.workspaceID)
	if err != nil {
		return err
	}
	coordinator := companybraincensus.NewParityPopulationCoordinator(
		companybraincensus.NewOwnerAdminParityPopulationAuthorizationGate(pool),
		frozen,
		current,
		companybraincensus.NewParityProofWriter(pool),
	)
	if err := coordinator.Populate(ctx, request); err != nil {
		return err
	}
	after, err := loadStateFingerprints(ctx, pool, opts.workspaceID)
	if err != nil {
		return err
	}
	result, err := loadPopulationResult(ctx, pool, request)
	if err != nil {
		return err
	}
	result.ConnectionsUnchanged = before.connections == after.connections
	result.PermissionsUnchanged = before.permissions == after.permissions
	result.finalize()
	if !result.ConnectionsUnchanged || !result.PermissionsUnchanged {
		return errors.New(
			"population safety check failed: Connections or permissions changed",
		)
	}
	if result.ProofCount != result.ExpectedEligibleAgentCount {
		return fmt.Errorf(
			"population verification failed: wrote %d proofs for %d eligible agents",
			result.ProofCount,
			result.ExpectedEligibleAgentCount,
		)
	}
	return printJSON(result)
}

func validateOptions(opts options) error {
	if strings.TrimSpace(opts.databaseURL) == "" ||
		strings.TrimSpace(opts.workspaceID) == "" ||
		strings.TrimSpace(opts.snapshotFile) == "" ||
		strings.TrimSpace(opts.snapshotSHA256) == "" {
		return errors.New(
			"database-url, workspace-id, snapshot-file, and snapshot-sha256 are required",
		)
	}
	if _, err := util.ParseUUID(opts.workspaceID); err != nil {
		return fmt.Errorf("invalid workspace-id: %w", err)
	}
	if !sha256Hex.MatchString(opts.snapshotSHA256) {
		return errors.New("snapshot-sha256 must be 64 lowercase hexadecimal characters")
	}
	if opts.apply && strings.TrimSpace(opts.approvalID) == "" {
		return errors.New("apply requires approval-id")
	}
	if opts.apply {
		if _, err := util.ParseUUID(opts.approvalID); err != nil {
			return fmt.Errorf("invalid approval-id: %w", err)
		}
	}
	return nil
}

func buildPopulationPlan(
	inputs companybraincensus.ParityPopulationInputs,
	request companybraincensus.ParityPopulationRequest,
) parityPopulationPlan {
	return parityPopulationPlan{
		Mode:       "dry-run",
		Inputs:     inputs,
		Capability: companybraincensus.ParityPopulationApprovalCapability,
		Resource:   companybraincensus.ParityPopulationApprovalResource(request),
		Context: approvalContext{
			IssueID:                 companybraincensus.ParityPopulationApprovalIssueID,
			ApprovalBoundary:        companybraincensus.ParityPopulationApprovalBoundary,
			FrozenCensusSHA256:      inputs.FrozenCensusSHA256,
			CensusVersion:           inputs.CensusVersion,
			CompanyBrainConnection:  inputs.CompanyBrainConnectionID,
			EligibleAgentCount:      inputs.ExpectedEligibleAgentCount,
			TargetPermissionsSHA256: inputs.ExpectedTargetPermissionsSHA256,
		},
	}
}

func loadStateFingerprints(
	ctx context.Context,
	pool *pgxpool.Pool,
	workspaceID string,
) (stateFingerprints, error) {
	var fingerprints stateFingerprints
	err := pool.QueryRow(ctx, stateFingerprintsSQL, workspaceID).Scan(
		&fingerprints.connections,
		&fingerprints.permissions,
	)
	if err != nil {
		return stateFingerprints{}, fmt.Errorf(
			"fingerprint current Connections and permissions: %w",
			err,
		)
	}
	return fingerprints, nil
}

func loadPopulationResult(
	ctx context.Context,
	pool *pgxpool.Pool,
	request companybraincensus.ParityPopulationRequest,
) (parityPopulationResult, error) {
	result := parityPopulationResult{
		Mode:                       "apply",
		ExpectedEligibleAgentCount: request.ExpectedEligibleAgentCount,
	}
	err := pool.QueryRow(
		ctx,
		populationResultSQL,
		request.WorkspaceID,
		request.CompanyBrainConnectionID,
		request.CensusVersion,
	).Scan(
		&result.ProofCount,
		&result.MatchedCount,
		&result.DoNotMigrateCount,
	)
	if err != nil {
		return parityPopulationResult{}, fmt.Errorf(
			"verify populated Company Brain parity proof: %w",
			err,
		)
	}
	return result, nil
}

func (r *parityPopulationResult) finalize() {
	r.UnresolvedCount = r.ExpectedEligibleAgentCount -
		r.MatchedCount -
		r.DoNotMigrateCount
	if r.UnresolvedCount < 0 {
		r.UnresolvedCount = 0
	}
	r.CutoverReady = r.ConnectionsUnchanged &&
		r.PermissionsUnchanged &&
		r.ProofCount == r.ExpectedEligibleAgentCount &&
		r.MatchedCount+r.DoNotMigrateCount == r.ExpectedEligibleAgentCount
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

const stateFingerprintsSQL = `
	SELECT
		md5(
			COALESCE((
				SELECT string_agg(md5(to_jsonb(connection_row)::text), '' ORDER BY id)
				FROM workspace_connection AS connection_row
				WHERE workspace_id = $1
			), '')
			||
			COALESCE((
				SELECT string_agg(md5(to_jsonb(logical_row)::text), '' ORDER BY id)
				FROM cerebro_company_brain_connection AS logical_row
				WHERE workspace_id = $1
			), '')
		),
		md5(COALESCE((
			SELECT string_agg(md5(to_jsonb(policy_row)::text), '' ORDER BY id)
			FROM cerebro_tool_policy AS policy_row
			WHERE workspace_id = $1
		), ''))
`

const populationResultSQL = `
	SELECT
		COUNT(*)::integer,
		COUNT(*) FILTER (WHERE proof.status = 'matched')::integer,
		COUNT(*) FILTER (
			WHERE proof.status = 'blocked'
			  AND EXISTS (
				SELECT 1
				FROM cerebro_company_brain_migration_decision AS decision
				WHERE decision.workspace_id = proof.workspace_id
				  AND decision.company_brain_connection_id = proof.company_brain_connection_id
				  AND decision.agent_id = proof.agent_id
				  AND decision.census_version = proof.census_version
				  AND decision.outcome = 'do_not_migrate'
				  AND decision.status = 'resolved'
				  AND decision.saved_decision = 'do_not_migrate'
			  )
		)::integer
	FROM cerebro_company_brain_parity_proof AS proof
	WHERE proof.workspace_id = $1
	  AND proof.company_brain_connection_id = $2
	  AND proof.census_version = $3
`
