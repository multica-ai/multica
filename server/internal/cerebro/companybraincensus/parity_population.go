package companybraincensus

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// FrozenCensus identifies one immutable migration census and the logical
// Company Brain connection whose target permissions must be compared with it.
type FrozenCensus struct {
	Report                   Report
	Version                  int64
	CompanyBrainConnectionID string
	SnapshotSHA256           string
}

// FrozenCensusLoader returns migration evidence that has already been frozen.
// Implementations must not regenerate the census from live Connections.
type FrozenCensusLoader interface {
	LoadFrozenCensus(context.Context, string) (FrozenCensus, error)
}

// CurrentTargetPermissionLoader returns the exact current source-scoped
// permission evidence for the frozen census's logical Company Brain connection.
type CurrentTargetPermissionLoader interface {
	LoadCurrentTargetPermissions(context.Context, string, string, Report) ([]TargetPermission, error)
}

// ParityProofBatchWriter accepts one complete deterministic EvaluateParity
// batch. ParityProofWriter implements this boundary.
type ParityProofBatchWriter interface {
	Write(context.Context, string, []ParityEvaluation) error
}

var _ ParityProofBatchWriter = (*ParityProofWriter)(nil)

// ParityPopulationRequest pins every non-secret input needed for one
// authorised parity-proof population. The authorization gate owns one-shot
// consumption; the coordinator revalidates the pinned evidence before writing.
type ParityPopulationRequest struct {
	AuthorizationID                 string
	WorkspaceID                     string
	FrozenCensusSHA256              string
	CensusVersion                   int64
	CompanyBrainConnectionID        string
	ExpectedEligibleAgentCount      int
	ExpectedTargetPermissionsSHA256 string
}

// ParityPopulationAuthorizationGate atomically consumes one authorization
// bound to the complete population request. No production implementation is
// registered until the population slice is separately authorised.
type ParityPopulationAuthorizationGate interface {
	AuthorizeOnce(context.Context, ParityPopulationRequest) error
}

// ParityPopulationCoordinator joins the isolated parity evaluator and proof
// writer. It is intentionally not registered with any API, CLI, scheduler, or
// feature flag.
type ParityPopulationCoordinator struct {
	gate    ParityPopulationAuthorizationGate
	frozen  FrozenCensusLoader
	current CurrentTargetPermissionLoader
	writer  ParityProofBatchWriter
	now     func() time.Time
}

func NewParityPopulationCoordinator(
	gate ParityPopulationAuthorizationGate,
	frozen FrozenCensusLoader,
	current CurrentTargetPermissionLoader,
	writer ParityProofBatchWriter,
) *ParityPopulationCoordinator {
	return &ParityPopulationCoordinator{
		gate: gate, frozen: frozen, current: current, writer: writer, now: time.Now,
	}
}

// Populate consumes one authorization, loads the pinned frozen census and exact
// current target evidence, then writes one complete evaluator batch.
func (c *ParityPopulationCoordinator) Populate(
	ctx context.Context,
	request ParityPopulationRequest,
) error {
	if err := validateParityPopulationRequest(request); err != nil {
		return err
	}
	if c == nil || c.gate == nil {
		return fmt.Errorf("parity population authorization gate is required")
	}
	if c == nil || c.frozen == nil {
		return fmt.Errorf("frozen census loader is required")
	}
	if c.current == nil {
		return fmt.Errorf("current target permission loader is required")
	}
	if c.writer == nil {
		return fmt.Errorf("parity proof writer is required")
	}
	if c.now == nil {
		return fmt.Errorf("parity population clock is required")
	}
	if err := c.gate.AuthorizeOnce(ctx, request); err != nil {
		return fmt.Errorf("authorize Company Brain parity population: %w", err)
	}

	frozen, err := c.frozen.LoadFrozenCensus(ctx, request.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load frozen Company Brain census: %w", err)
	}
	if frozen.SnapshotSHA256 != request.FrozenCensusSHA256 {
		return fmt.Errorf("frozen census snapshot checksum changed")
	}
	if frozen.Version != request.CensusVersion {
		return fmt.Errorf("frozen census version changed")
	}
	if frozen.CompanyBrainConnectionID != request.CompanyBrainConnectionID {
		return fmt.Errorf("frozen census logical connection changed")
	}
	if len(frozen.Report.Actors) != request.ExpectedEligibleAgentCount {
		return fmt.Errorf("frozen census eligible agent count changed")
	}
	targetLoaderReport, err := cloneParityPopulationReport(frozen.Report)
	if err != nil {
		return err
	}

	targets, err := c.current.LoadCurrentTargetPermissions(
		ctx,
		request.WorkspaceID,
		frozen.CompanyBrainConnectionID,
		targetLoaderReport,
	)
	if err != nil {
		return fmt.Errorf("load current Company Brain target permissions: %w", err)
	}
	canonicalTargets, err := canonicalTargetPermissions(targets)
	if err != nil {
		return fmt.Errorf("canonicalize current Company Brain target permissions: %w", err)
	}
	targetSHA256 := canonicalHash(canonicalTargets)
	if targetSHA256 != request.ExpectedTargetPermissionsSHA256 {
		return fmt.Errorf("target permission evidence changed")
	}

	evaluations := EvaluateParity(
		frozen.Report,
		canonicalTargets,
		frozen.Version,
		frozen.CompanyBrainConnectionID,
		c.now(),
	)
	if len(evaluations) != request.ExpectedEligibleAgentCount {
		return fmt.Errorf("parity evaluation eligible agent count changed")
	}
	if err := c.writer.Write(ctx, request.WorkspaceID, evaluations); err != nil {
		return fmt.Errorf("write Company Brain parity proof batch: %w", err)
	}
	return nil
}

func validateParityPopulationRequest(request ParityPopulationRequest) error {
	if _, err := util.ParseUUID(request.AuthorizationID); err != nil {
		return fmt.Errorf("invalid parity population authorization identity: %w", err)
	}
	if _, err := util.ParseUUID(request.WorkspaceID); err != nil {
		return fmt.Errorf("invalid parity population workspace identity: %w", err)
	}
	if !sha256Hex.MatchString(request.FrozenCensusSHA256) {
		return fmt.Errorf("invalid frozen census snapshot checksum")
	}
	if request.CensusVersion <= 0 {
		return fmt.Errorf("parity population census version must be positive")
	}
	if _, err := util.ParseUUID(request.CompanyBrainConnectionID); err != nil {
		return fmt.Errorf("invalid parity population logical connection identity: %w", err)
	}
	if request.ExpectedEligibleAgentCount <= 0 {
		return fmt.Errorf("expected eligible agent count must be positive")
	}
	if !sha256Hex.MatchString(request.ExpectedTargetPermissionsSHA256) {
		return fmt.Errorf("invalid target permission evidence checksum")
	}
	return nil
}

func cloneParityPopulationReport(report Report) (Report, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return Report{}, fmt.Errorf("copy frozen Company Brain census: %w", err)
	}
	var clone Report
	if err := json.Unmarshal(raw, &clone); err != nil {
		return Report{}, fmt.Errorf("copy frozen Company Brain census: %w", err)
	}
	return clone, nil
}

// TargetPermissionsSHA256 returns an order-independent checksum for the exact
// current non-secret target evidence that an authorization pins.
func TargetPermissionsSHA256(targets []TargetPermission) (string, error) {
	canonical, err := canonicalTargetPermissions(targets)
	if err != nil {
		return "", err
	}
	return canonicalHash(canonical), nil
}

func canonicalTargetPermissions(
	targets []TargetPermission,
) ([]TargetPermission, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("target permission evidence is empty")
	}
	canonical := make([]TargetPermission, 0, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.PermissionID) == "" ||
			strings.TrimSpace(target.CompanyBrainConnectionID) == "" ||
			strings.TrimSpace(target.AgentID) == "" ||
			target.AccessVersion <= 0 ||
			!sourceID.MatchString(target.WriteSource) {
			return nil, fmt.Errorf("target permission identity or version is invalid")
		}
		readSources, ok := canonicalStrings(
			target.AllowedReadSources,
			sourceID.MatchString,
		)
		if !ok || !containsString(readSources, target.WriteSource) {
			return nil, fmt.Errorf("target permission source evidence is invalid")
		}
		tools, ok := canonicalStrings(target.CanonicalToolCalls, validTool)
		if !ok || len(tools) == 0 {
			return nil, fmt.Errorf("target permission tool evidence is invalid")
		}
		approvals := append(
			[]ScopedToolDecision(nil),
			target.ApprovalOutcomes...,
		)
		seenApprovals := make(map[string]struct{}, len(approvals))
		approvedTools := make(map[string]struct{}, len(tools))
		for _, approval := range approvals {
			if !sourceID.MatchString(approval.Source) ||
				!validTool(approval.Tool) ||
				!validDecision(approval.Decision) ||
				!containsString(tools, approval.Tool) {
				return nil, fmt.Errorf("target permission approval evidence is invalid")
			}
			key := approval.Source + "\x00" + approval.Tool
			if _, duplicate := seenApprovals[key]; duplicate {
				return nil, fmt.Errorf("target permission approval evidence is duplicated")
			}
			seenApprovals[key] = struct{}{}
			approvedTools[approval.Tool] = struct{}{}
		}
		if len(approvals) == 0 || len(approvedTools) != len(tools) {
			return nil, fmt.Errorf("target permission approval evidence is incomplete")
		}
		sortDecisions(approvals)

		target.AllowedReadSources = readSources
		target.CanonicalToolCalls = tools
		target.ApprovalOutcomes = approvals
		canonical = append(canonical, target)
	}
	sort.Slice(canonical, func(i, j int) bool {
		left, right := canonical[i], canonical[j]
		if left.AgentID != right.AgentID {
			return left.AgentID < right.AgentID
		}
		if left.PermissionID != right.PermissionID {
			return left.PermissionID < right.PermissionID
		}
		return left.CompanyBrainConnectionID < right.CompanyBrainConnectionID
	})
	return canonical, nil
}
