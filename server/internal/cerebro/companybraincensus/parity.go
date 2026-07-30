package companybraincensus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ParityStatus is the fail-closed result of comparing one frozen legacy
// Company Brain census actor with one proposed target permission.
type ParityStatus string

const (
	ParityMatched ParityStatus = "matched"
	ParityBlocked ParityStatus = "blocked"
)

// ParityBlockerCode is stable evidence for why an actor cannot be cut over.
type ParityBlockerCode string

const (
	BlockerTargetPermissionMissing    ParityBlockerCode = "CB-TARGET-PERMISSION-MISSING"
	BlockerTargetPermissionAmbiguous  ParityBlockerCode = "CB-TARGET-PERMISSION-AMBIGUOUS"
	BlockerCensusEvidenceMissing      ParityBlockerCode = "CB-CENSUS-EVIDENCE-MISSING"
	BlockerCensusEvidenceAmbiguous    ParityBlockerCode = "CB-CENSUS-EVIDENCE-AMBIGUOUS"
	BlockerLegacyWriteSourceAmbiguous ParityBlockerCode = "CB-WRITE-DESTINATION-AMBIGUOUS"
	BlockerTargetEvidenceMissing      ParityBlockerCode = "CB-TARGET-EVIDENCE-MISSING"
	BlockerAccessMismatch             ParityBlockerCode = "CB-ACCESS-MISMATCH"
	BlockerApprovalMismatch           ParityBlockerCode = "CB-APPROVAL-MISMATCH"
	BlockerToolCountMismatch          ParityBlockerCode = "CB-TOOL-COUNT-MISMATCH"
	BlockerToolCallMismatch           ParityBlockerCode = "CB-TOOL-CALL-MISMATCH"
	BlockerWriteDestinationMismatch   ParityBlockerCode = "CB-WRITE-DESTINATION-MISMATCH"
)

// ScopedToolDecision is the canonical Allow/Ask/Deny outcome for one legacy
// Company Brain source and one bare MCP tool name.
type ScopedToolDecision struct {
	Source   string `json:"source"`
	Tool     string `json:"tool"`
	Decision string `json:"decision"`
}

// TargetPermission contains the complete non-secret target evidence needed to
// compare one agent with the frozen Migration census. It is deliberately
// separate from persistence so evaluating parity cannot mutate live rows.
type TargetPermission struct {
	PermissionID             string               `json:"permission_id"`
	CompanyBrainConnectionID string               `json:"company_brain_connection_id"`
	AgentID                  string               `json:"agent_id"`
	AccessVersion            int64                `json:"access_version"`
	AllowedReadSources       []string             `json:"allowed_read_sources"`
	WriteSource              string               `json:"write_source"`
	ApprovalOutcomes         []ScopedToolDecision `json:"approval_outcomes"`
	CanonicalToolCalls       []string             `json:"canonical_tool_calls"`
}

// ParityEvaluation mirrors the non-secret proof fields accepted by
// cerebro_company_brain_parity_proof. Missing target evidence remains blocked
// here and is not made persistable by inventing placeholder identities.
type ParityEvaluation struct {
	CompanyBrainConnectionID string            `json:"company_brain_connection_id"`
	TargetPermissionID       string            `json:"target_permission_id,omitempty"`
	AgentID                  string            `json:"agent_id"`
	CensusVersion            int64             `json:"census_version"`
	AccessVersion            int64             `json:"access_version,omitempty"`
	LegacyAccessSHA256       string            `json:"legacy_access_sha256,omitempty"`
	TargetAccessSHA256       string            `json:"target_access_sha256,omitempty"`
	LegacyApprovalSHA256     string            `json:"legacy_approval_sha256,omitempty"`
	TargetApprovalSHA256     string            `json:"target_approval_sha256,omitempty"`
	LegacyToolCallsSHA256    string            `json:"legacy_tool_calls_sha256,omitempty"`
	TargetToolCallsSHA256    string            `json:"target_tool_calls_sha256,omitempty"`
	LegacyToolCount          int               `json:"legacy_tool_count,omitempty"`
	TargetToolCount          int               `json:"target_tool_count,omitempty"`
	LegacyWriteSource        string            `json:"legacy_write_source,omitempty"`
	TargetWriteSource        string            `json:"target_write_source,omitempty"`
	Status                   ParityStatus      `json:"status"`
	BlockerCode              ParityBlockerCode `json:"blocker_code,omitempty"`
	EvidenceSHA256           string            `json:"evidence_sha256,omitempty"`
	EvidenceAt               time.Time         `json:"evidence_at"`
}

type parityEvidence struct {
	Access    []string
	Approvals []ScopedToolDecision
	Tools     []string
	Write     string
}

// EvaluateParity returns exactly one deterministic, fail-closed result for
// every eligible actor in the frozen report. It performs no database or
// Connection writes.
func EvaluateParity(
	report Report,
	targets []TargetPermission,
	censusVersion int64,
	companyBrainConnectionID string,
	now time.Time,
) []ParityEvaluation {
	targetsByAgent := make(map[string][]TargetPermission, len(targets))
	for _, target := range targets {
		targetsByAgent[target.AgentID] = append(targetsByAgent[target.AgentID], target)
	}

	actors := append([]actor(nil), report.Actors...)
	sort.Slice(actors, func(i, j int) bool {
		if actors[i].AgentID == actors[j].AgentID {
			return actors[i].Name < actors[j].Name
		}
		return actors[i].AgentID < actors[j].AgentID
	})

	results := make([]ParityEvaluation, 0, len(actors))
	actorCounts := make(map[string]int, len(actors))
	for _, actor := range actors {
		actorCounts[actor.AgentID]++
	}
	evaluatedActors := make(map[string]struct{}, len(actors))
	for _, actor := range actors {
		if _, evaluated := evaluatedActors[actor.AgentID]; evaluated {
			continue
		}
		evaluatedActors[actor.AgentID] = struct{}{}
		result := ParityEvaluation{
			CompanyBrainConnectionID: companyBrainConnectionID,
			AgentID:                  actor.AgentID,
			CensusVersion:            censusVersion,
			Status:                   ParityBlocked,
			EvidenceAt:               now.UTC(),
		}
		if actor.AgentID == "" || censusVersion <= 0 ||
			strings.TrimSpace(companyBrainConnectionID) == "" ||
			report.GeneratedAt.IsZero() ||
			report.GeneratedAt.After(now) {
			result.BlockerCode = BlockerCensusEvidenceMissing
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}
		if actorCounts[actor.AgentID] != 1 {
			result.BlockerCode = BlockerCensusEvidenceAmbiguous
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}

		legacy, blocker := legacyParityEvidence(actor)
		applyLegacyEvidence(&result, legacy)
		if blocker != "" {
			result.BlockerCode = blocker
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}

		candidates := targetsByAgent[actor.AgentID]
		if len(candidates) == 0 {
			result.BlockerCode = BlockerTargetPermissionMissing
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}
		if len(candidates) != 1 {
			result.BlockerCode = BlockerTargetPermissionAmbiguous
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}

		target := candidates[0]
		targetEvidence, ok := targetParityEvidence(
			target,
			actor.AgentID,
			companyBrainConnectionID,
		)
		if !ok {
			result.BlockerCode = BlockerTargetEvidenceMissing
			result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
			results = append(results, result)
			continue
		}
		result.TargetPermissionID = target.PermissionID
		result.AccessVersion = target.AccessVersion
		applyTargetEvidence(&result, targetEvidence)

		switch {
		case result.LegacyAccessSHA256 != result.TargetAccessSHA256:
			result.BlockerCode = BlockerAccessMismatch
		case result.LegacyApprovalSHA256 != result.TargetApprovalSHA256:
			result.BlockerCode = BlockerApprovalMismatch
		case result.LegacyToolCount != result.TargetToolCount:
			result.BlockerCode = BlockerToolCountMismatch
		case result.LegacyToolCallsSHA256 != result.TargetToolCallsSHA256:
			result.BlockerCode = BlockerToolCallMismatch
		case result.LegacyWriteSource != result.TargetWriteSource:
			result.BlockerCode = BlockerWriteDestinationMismatch
		default:
			result.Status = ParityMatched
		}
		result.EvidenceSHA256 = evaluationHash(report.GeneratedAt, result)
		results = append(results, result)
	}
	return results
}

func legacyParityEvidence(actor actor) (parityEvidence, ParityBlockerCode) {
	if len(actor.Sources) == 0 {
		return parityEvidence{}, BlockerCensusEvidenceMissing
	}

	readSources := make(map[string]struct{})
	writeSources := make(map[string]struct{})
	canonicalTools := make(map[string]struct{})
	sourceNames := make(map[string]struct{}, len(actor.Sources))
	approvals := make([]ScopedToolDecision, 0)
	var expectedTools []string

	for _, source := range actor.Sources {
		sourceName, ok := legacySourceName(source.ConnectionName)
		if !ok {
			return parityEvidence{}, BlockerCensusEvidenceMissing
		}
		if _, duplicate := sourceNames[sourceName]; duplicate {
			return parityEvidence{}, BlockerCensusEvidenceAmbiguous
		}
		sourceNames[sourceName] = struct{}{}

		tools, decisions, ok := canonicalSourceTools(sourceName, source.ToolAccess)
		if !ok {
			return parityEvidence{}, BlockerCensusEvidenceMissing
		}
		if expectedTools == nil {
			expectedTools = tools
		} else if !equalStrings(expectedTools, tools) {
			return parityEvidence{}, BlockerCensusEvidenceMissing
		}
		for _, tool := range tools {
			canonicalTools[tool] = struct{}{}
		}
		approvals = append(approvals, decisions...)

		switch {
		case source.Status == statusVerified && source.Claim != nil &&
			source.ErrorCode == "":
			if !validClaim(*source.Claim) {
				return parityEvidence{}, BlockerCensusEvidenceMissing
			}
			writeSources[source.Claim.WriteSource] = struct{}{}
			for _, readSource := range source.Claim.AllowedReadSources {
				readSources[readSource] = struct{}{}
			}
		case source.Status == statusUnverifiable && source.Claim == nil &&
			(source.ErrorCode == errorAccessDenied ||
				source.ErrorCode == errorApprovalRequired):
			// Deny and Ask are complete policy evidence. They deliberately
			// carry no identity claim and therefore grant no read/write source.
		default:
			return parityEvidence{}, BlockerCensusEvidenceMissing
		}
	}

	if len(readSources) == 0 || len(writeSources) == 0 {
		return parityEvidence{}, BlockerCensusEvidenceMissing
	}
	if len(writeSources) != 1 {
		return parityEvidence{}, BlockerLegacyWriteSourceAmbiguous
	}

	evidence := parityEvidence{
		Access:    sortedSet(readSources),
		Approvals: approvals,
		Tools:     sortedSet(canonicalTools),
	}
	for write := range writeSources {
		evidence.Write = write
	}
	sortDecisions(evidence.Approvals)
	return evidence, ""
}

func targetParityEvidence(
	target TargetPermission,
	agentID string,
	companyBrainConnectionID string,
) (parityEvidence, bool) {
	if strings.TrimSpace(target.PermissionID) == "" ||
		target.AgentID != agentID ||
		target.CompanyBrainConnectionID != companyBrainConnectionID ||
		target.AccessVersion <= 0 ||
		!sourceID.MatchString(target.WriteSource) ||
		len(target.AllowedReadSources) == 0 ||
		len(target.ApprovalOutcomes) == 0 ||
		len(target.CanonicalToolCalls) == 0 {
		return parityEvidence{}, false
	}

	access, ok := canonicalStrings(target.AllowedReadSources, sourceID.MatchString)
	if !ok || !containsString(access, target.WriteSource) {
		return parityEvidence{}, false
	}
	tools, ok := canonicalStrings(target.CanonicalToolCalls, validTool)
	if !ok {
		return parityEvidence{}, false
	}

	approvals := append([]ScopedToolDecision(nil), target.ApprovalOutcomes...)
	seenDecisions := make(map[string]struct{}, len(approvals))
	for _, decision := range approvals {
		if !sourceID.MatchString(decision.Source) ||
			!validTool(decision.Tool) ||
			!validDecision(decision.Decision) {
			return parityEvidence{}, false
		}
		key := decision.Source + "\x00" + decision.Tool
		if _, duplicate := seenDecisions[key]; duplicate {
			return parityEvidence{}, false
		}
		seenDecisions[key] = struct{}{}
	}
	sortDecisions(approvals)
	return parityEvidence{
		Access:    access,
		Approvals: approvals,
		Tools:     tools,
		Write:     target.WriteSource,
	}, true
}

func applyLegacyEvidence(result *ParityEvaluation, evidence parityEvidence) {
	if len(evidence.Access) > 0 {
		result.LegacyAccessSHA256 = canonicalHash(evidence.Access)
	}
	if len(evidence.Approvals) > 0 {
		result.LegacyApprovalSHA256 = canonicalHash(evidence.Approvals)
	}
	if len(evidence.Tools) > 0 {
		result.LegacyToolCallsSHA256 = canonicalHash(evidence.Tools)
		result.LegacyToolCount = len(evidence.Tools)
	}
	result.LegacyWriteSource = evidence.Write
}

func applyTargetEvidence(result *ParityEvaluation, evidence parityEvidence) {
	result.TargetAccessSHA256 = canonicalHash(evidence.Access)
	result.TargetApprovalSHA256 = canonicalHash(evidence.Approvals)
	result.TargetToolCallsSHA256 = canonicalHash(evidence.Tools)
	result.TargetToolCount = len(evidence.Tools)
	result.TargetWriteSource = evidence.Write
}

func canonicalSourceTools(
	source string,
	access []toolAccess,
) ([]string, []ScopedToolDecision, bool) {
	if len(access) == 0 {
		return nil, nil, false
	}
	tools := make([]string, 0, len(access))
	decisions := make([]ScopedToolDecision, 0, len(access))
	seen := make(map[string]struct{}, len(access))
	for _, item := range access {
		if !validTool(item.Tool) || !validDecision(item.Decision) {
			return nil, nil, false
		}
		if _, duplicate := seen[item.Tool]; duplicate {
			return nil, nil, false
		}
		seen[item.Tool] = struct{}{}
		tools = append(tools, item.Tool)
		decisions = append(decisions, ScopedToolDecision{
			Source: source, Tool: item.Tool, Decision: item.Decision,
		})
	}
	sort.Strings(tools)
	sortDecisions(decisions)
	return tools, decisions, true
}

func legacySourceName(connectionName string) (string, bool) {
	if connectionName == "company-brain" {
		return "multica", true
	}
	const prefix = "company-brain-"
	if !strings.HasPrefix(connectionName, prefix) {
		return "", false
	}
	source := strings.TrimPrefix(connectionName, prefix)
	return source, sourceID.MatchString(source)
}

func validClaim(claim Claim) bool {
	if !sourceID.MatchString(claim.WriteSource) ||
		len(claim.AllowedReadSources) == 0 {
		return false
	}
	for _, source := range claim.AllowedReadSources {
		if !sourceID.MatchString(source) {
			return false
		}
	}
	return containsString(claim.AllowedReadSources, claim.WriteSource)
}

func validTool(tool string) bool {
	return tool != "" && strings.TrimSpace(tool) == tool
}

func validDecision(decision string) bool {
	return decision == "allow" || decision == "ask" || decision == "deny"
}

func canonicalStrings(
	values []string,
	valid func(string) bool,
) ([]string, bool) {
	out := append([]string(nil), values...)
	for _, value := range out {
		if !valid(value) {
			return nil, false
		}
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, false
		}
	}
	return out, true
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortDecisions(decisions []ScopedToolDecision) {
	sort.Slice(decisions, func(i, j int) bool {
		left := decisions[i]
		right := decisions[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Tool != right.Tool {
			return left.Tool < right.Tool
		}
		return left.Decision < right.Decision
	})
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func canonicalHash(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func evaluationHash(generatedAt time.Time, result ParityEvaluation) string {
	payload := struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Result      ParityEvaluation `json:"result"`
	}{
		GeneratedAt: generatedAt.UTC(),
		Result:      result,
	}
	payload.Result.EvidenceSHA256 = ""
	return canonicalHash(payload)
}
