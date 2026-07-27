// Package agentroute implements the pure policy for choosing an execution
// topology and ranking validated agent/model configurations.
//
// The package deliberately has no database, network, clock, or dispatch
// dependencies. Callers gather live capacity and capability evidence, call
// Route, persist the decision if needed, and retain responsibility for
// exactly-once execution and fallback.
package agentroute

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	basisPointsMax = 10_000
	permilleMax    = 1_000

	defaultMaxParallel  = 3
	defaultMaxFallbacks = 2
	parallelAmbiguityBP = 6_000
	reviewAmbiguityBP   = 6_000
)

// ErrNoEligibleCandidate is returned with a populated Decision when every
// candidate fails a hard eligibility gate.
var ErrNoEligibleCandidate = errors.New("no eligible routing candidate")

// Risk is the consequence level of a workload.
type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

// Urgency controls whether emergency reserve may be consumed.
type Urgency string

const (
	UrgencyNormal    Urgency = "normal"
	UrgencyEmergency Urgency = "emergency"
)

// Topology describes how the selected candidates should collaborate.
type Topology string

const (
	TopologySolo                Topology = "solo"
	TopologySerial              Topology = "serial"
	TopologyBoundedParallel     Topology = "bounded_parallel"
	TopologyCrossProviderReview Topology = "cross_provider_review"
)

// AssignmentRole describes a candidate's responsibility in the selected
// topology. Only RoleLead is write-capable.
type AssignmentRole string

const (
	RoleLead     AssignmentRole = "lead"
	RoleExplorer AssignmentRole = "explorer"
	RoleCritic   AssignmentRole = "critic"
)

// AffinityStatus is the promotion state of a skill/model affinity record.
type AffinityStatus string

const (
	AffinityExperimental AffinityStatus = "experimental"
	AffinityPromoted     AffinityStatus = "promoted"
	AffinityRejected     AffinityStatus = "rejected"
)

// Workload contains policy-relevant task requirements. Lists are treated as
// sets and matched exactly after trimming whitespace.
type Workload struct {
	ID                       string
	Risk                     Risk
	Urgency                  Urgency
	Protected                bool
	RequiredSkills           []string
	RequiredTools            []string
	RequiredAuthority        []string
	HasDependencies          bool
	IndependentBranches      int
	AmbiguityBP              int
	MaxParallel              int
	AllowCrossProviderReview bool
}

// Candidate is one independently validated provider/model/thinking
// configuration. QualityBP and LatencyPenaltyBP use basis points. Forecast plan
// use is a permille share of the provider's current plan window.
type Candidate struct {
	ID                  string
	Provider            string
	Model               string
	Thinking            string
	Online              bool
	ProtectedApproved   bool
	SupportedSkills     []string
	SupportedTools      []string
	AuthorityScopes     []string
	QualityBP           int
	LatencyPenaltyBP    int
	ExpectedUsePermille int
}

// Capacity is a provider-plan snapshot. RemainingPermille is remaining plan
// capacity in the current window; ReservePermille is the emergency floor that
// ordinary work must leave untouched.
type Capacity struct {
	Provider          string
	Known             bool
	RemainingPermille int
	ReservePermille   int
}

// SkillAffinity adds a bounded score to candidates for one required skill.
// Only promoted records with a non-empty EvidenceRevision affect routing.
// Empty Model or Thinking fields match every configuration on that provider.
type SkillAffinity struct {
	Skill            string
	Provider         string
	Model            string
	Thinking         string
	Status           AffinityStatus
	ScoreBP          int
	EvidenceRevision string
}

// Request is the complete input to the pure routing policy.
type Request struct {
	Workload     Workload
	Candidates   []Candidate
	Capacities   []Capacity
	Affinities   []SkillAffinity
	MaxFallbacks int
}

// RejectionReason is a stable, machine-readable hard-gate result.
type RejectionReason string

const (
	RejectInvalidCandidate   RejectionReason = "invalid_candidate"
	RejectOffline            RejectionReason = "runtime_offline"
	RejectProtectedRole      RejectionReason = "protected_role_not_approved"
	RejectMissingSkill       RejectionReason = "missing_required_skill"
	RejectMissingTool        RejectionReason = "missing_required_tool"
	RejectMissingAuthority   RejectionReason = "missing_required_authority"
	RejectForecastUnknown    RejectionReason = "forecast_usage_unknown"
	RejectCapacityUnknown    RejectionReason = "provider_capacity_unknown"
	RejectCapacityInvalid    RejectionReason = "provider_capacity_invalid"
	RejectReserveProtected   RejectionReason = "emergency_reserve_protected"
	RejectInsufficientBudget RejectionReason = "insufficient_remaining_capacity"
)

// Rejection explains why a candidate could not participate.
type Rejection struct {
	CandidateID string
	Reason      RejectionReason
	Detail      string
}

// RankedCandidate is an eligible candidate with the deterministic score and
// projected provider capacity after this one assignment.
type RankedCandidate struct {
	Candidate                  Candidate
	Score                      int
	ProjectedRemainingPermille int
	ProjectedHeadroomPermille  int
}

// Assignment is one participant in a selected topology. WriteCapable is true
// for exactly one assignment: the lead.
type Assignment struct {
	RankedCandidate
	Role         AssignmentRole
	WriteCapable bool
}

// Decision is the auditable output of Route.
type Decision struct {
	Topology    Topology
	Primary     RankedCandidate
	Assignments []Assignment
	Fallbacks   []RankedCandidate
	Rejections  []Rejection
}

// Route applies hard eligibility gates, ranks candidates, and chooses a bounded
// topology. Identical logical inputs always produce the same output regardless
// of candidate input order.
func Route(req Request) (Decision, error) {
	var decision Decision

	capacities, err := capacityIndex(req.Capacities)
	if err != nil {
		return decision, err
	}
	seenCandidateIDs := make(map[string]struct{}, len(req.Candidates))
	var ranked []RankedCandidate
	for _, candidate := range req.Candidates {
		id := strings.TrimSpace(candidate.ID)
		if _, exists := seenCandidateIDs[id]; id != "" && exists {
			return decision, fmt.Errorf("duplicate candidate id %q", id)
		}
		if id != "" {
			seenCandidateIDs[id] = struct{}{}
		}

		evaluated, rejection := evaluateCandidate(req.Workload, candidate, capacities, req.Affinities)
		if rejection != nil {
			decision.Rejections = append(decision.Rejections, *rejection)
			continue
		}
		ranked = append(ranked, evaluated)
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Candidate.ID < ranked[j].Candidate.ID
	})
	sort.Slice(decision.Rejections, func(i, j int) bool {
		if decision.Rejections[i].CandidateID != decision.Rejections[j].CandidateID {
			return decision.Rejections[i].CandidateID < decision.Rejections[j].CandidateID
		}
		return decision.Rejections[i].Reason < decision.Rejections[j].Reason
	})

	if len(ranked) == 0 {
		return decision, ErrNoEligibleCandidate
	}

	decision.Primary = ranked[0]
	decision.Topology, decision.Assignments = chooseTopology(req.Workload, ranked, capacities)
	decision.Fallbacks = chooseFallbacks(ranked[1:], ranked[0].Candidate.Provider, req.MaxFallbacks)
	return decision, nil
}

func capacityIndex(capacities []Capacity) (map[string]Capacity, error) {
	out := make(map[string]Capacity, len(capacities))
	for _, capacity := range capacities {
		provider := strings.TrimSpace(capacity.Provider)
		if provider == "" {
			continue
		}
		if _, exists := out[provider]; exists {
			return nil, fmt.Errorf("duplicate capacity snapshot for provider %q", provider)
		}
		capacity.Provider = provider
		out[provider] = capacity
	}
	return out, nil
}

func evaluateCandidate(
	workload Workload,
	candidate Candidate,
	capacities map[string]Capacity,
	affinities []SkillAffinity,
) (RankedCandidate, *Rejection) {
	candidate.ID = strings.TrimSpace(candidate.ID)
	candidate.Provider = strings.TrimSpace(candidate.Provider)
	candidate.Model = strings.TrimSpace(candidate.Model)
	candidate.Thinking = strings.TrimSpace(candidate.Thinking)
	reject := func(reason RejectionReason, detail string) (RankedCandidate, *Rejection) {
		return RankedCandidate{}, &Rejection{CandidateID: candidate.ID, Reason: reason, Detail: detail}
	}

	if candidate.ID == "" || candidate.Provider == "" || candidate.Model == "" ||
		candidate.QualityBP <= 0 || candidate.QualityBP > basisPointsMax ||
		candidate.LatencyPenaltyBP < 0 || candidate.LatencyPenaltyBP > basisPointsMax {
		return reject(RejectInvalidCandidate, "identity or measured score is missing or out of range")
	}
	if !candidate.Online {
		return reject(RejectOffline, "runtime did not report online")
	}
	if workload.Protected && !candidate.ProtectedApproved {
		return reject(RejectProtectedRole, "candidate is not approved for protected workloads")
	}
	if missing := firstMissing(workload.RequiredSkills, candidate.SupportedSkills); missing != "" {
		return reject(RejectMissingSkill, missing)
	}
	if missing := firstMissing(workload.RequiredTools, candidate.SupportedTools); missing != "" {
		return reject(RejectMissingTool, missing)
	}
	if missing := firstMissing(workload.RequiredAuthority, candidate.AuthorityScopes); missing != "" {
		return reject(RejectMissingAuthority, missing)
	}
	if candidate.ExpectedUsePermille <= 0 || candidate.ExpectedUsePermille > permilleMax {
		return reject(RejectForecastUnknown, "forecast plan use must be in 1..1000")
	}

	capacity, exists := capacities[candidate.Provider]
	if !exists || !capacity.Known {
		return reject(RejectCapacityUnknown, "provider did not report a current plan snapshot")
	}
	if capacity.RemainingPermille < 0 || capacity.RemainingPermille > permilleMax ||
		capacity.ReservePermille < 0 || capacity.ReservePermille > permilleMax ||
		capacity.ReservePermille > capacity.RemainingPermille {
		return reject(RejectCapacityInvalid, "remaining or reserve capacity is out of range")
	}

	available := capacity.RemainingPermille
	if workload.Urgency != UrgencyEmergency {
		available -= capacity.ReservePermille
		if candidate.ExpectedUsePermille > available {
			return reject(RejectReserveProtected, "forecast use would cross the emergency reserve")
		}
	} else if candidate.ExpectedUsePermille > available {
		return reject(RejectInsufficientBudget, "forecast use exceeds reported remaining capacity")
	}

	projectedRemaining := capacity.RemainingPermille - candidate.ExpectedUsePermille
	projectedHeadroom := projectedRemaining
	if workload.Urgency != UrgencyEmergency {
		projectedHeadroom -= capacity.ReservePermille
	}
	if projectedHeadroom < 0 {
		projectedHeadroom = 0
	}

	// Quality dominates, capacity headroom breaks otherwise-close choices,
	// latency discourages expensive configurations, and independently promoted
	// skill affinity may adjust—but never bypass—hard gates.
	score := candidate.QualityBP*4 +
		projectedHeadroom*basisPointsMax/permilleMax*2 -
		candidate.LatencyPenaltyBP +
		affinityScore(workload.RequiredSkills, candidate, affinities)

	return RankedCandidate{
		Candidate:                  candidate,
		Score:                      score,
		ProjectedRemainingPermille: projectedRemaining,
		ProjectedHeadroomPermille:  projectedHeadroom,
	}, nil
}

func firstMissing(required, supported []string) string {
	have := make(map[string]struct{}, len(supported))
	for _, value := range supported {
		if value = strings.TrimSpace(value); value != "" {
			have[value] = struct{}{}
		}
	}
	for _, value := range required {
		if value = strings.TrimSpace(value); value != "" {
			if _, ok := have[value]; !ok {
				return value
			}
		}
	}
	return ""
}

func affinityScore(requiredSkills []string, candidate Candidate, affinities []SkillAffinity) int {
	required := make(map[string]struct{}, len(requiredSkills))
	for _, skill := range requiredSkills {
		if skill = strings.TrimSpace(skill); skill != "" {
			required[skill] = struct{}{}
		}
	}
	score := 0
	for _, affinity := range affinities {
		if affinity.Status != AffinityPromoted || strings.TrimSpace(affinity.EvidenceRevision) == "" {
			continue
		}
		if _, ok := required[strings.TrimSpace(affinity.Skill)]; !ok {
			continue
		}
		if strings.TrimSpace(affinity.Provider) != candidate.Provider {
			continue
		}
		if model := strings.TrimSpace(affinity.Model); model != "" && model != candidate.Model {
			continue
		}
		if thinking := strings.TrimSpace(affinity.Thinking); thinking != "" && thinking != candidate.Thinking {
			continue
		}
		if affinity.ScoreBP < -basisPointsMax || affinity.ScoreBP > basisPointsMax {
			continue
		}
		score += affinity.ScoreBP
	}
	return score
}

func chooseTopology(
	workload Workload,
	ranked []RankedCandidate,
	capacities map[string]Capacity,
) (Topology, []Assignment) {
	lead := Assignment{RankedCandidate: ranked[0], Role: RoleLead, WriteCapable: true}
	if workload.HasDependencies {
		return TopologySerial, []Assignment{lead}
	}

	if riskAtLeastHigh(workload.Risk) &&
		workload.AmbiguityBP >= reviewAmbiguityBP &&
		workload.AllowCrossProviderReview {
		for _, candidate := range ranked[1:] {
			if candidate.Candidate.Provider != lead.Candidate.Provider {
				return TopologyCrossProviderReview, []Assignment{
					lead,
					{RankedCandidate: candidate, Role: RoleCritic, WriteCapable: false},
				}
			}
		}
	}

	if workload.IndependentBranches >= 2 && workload.AmbiguityBP >= parallelAmbiguityBP {
		limit := workload.MaxParallel
		if limit <= 0 {
			limit = defaultMaxParallel
		}
		if limit > workload.IndependentBranches {
			limit = workload.IndependentBranches
		}
		assignments := []Assignment{lead}
		used := map[string]int{
			lead.Candidate.Provider: lead.Candidate.ExpectedUsePermille,
		}
		for _, candidate := range ranked[1:] {
			if len(assignments) >= limit {
				break
			}
			capacity := capacities[candidate.Candidate.Provider]
			available := capacity.RemainingPermille
			if workload.Urgency != UrgencyEmergency {
				available -= capacity.ReservePermille
			}
			projectedUse := used[candidate.Candidate.Provider] + candidate.Candidate.ExpectedUsePermille
			if projectedUse > available {
				continue
			}
			used[candidate.Candidate.Provider] = projectedUse
			assignments = append(assignments, Assignment{
				RankedCandidate: candidate,
				Role:            RoleExplorer,
				WriteCapable:    false,
			})
		}
		if len(assignments) >= 2 {
			return TopologyBoundedParallel, assignments
		}
	}

	return TopologySolo, []Assignment{lead}
}

func riskAtLeastHigh(risk Risk) bool {
	return risk == RiskHigh || risk == RiskCritical
}

func chooseFallbacks(ranked []RankedCandidate, primaryProvider string, limit int) []RankedCandidate {
	if limit <= 0 {
		limit = defaultMaxFallbacks
	}
	var out []RankedCandidate
	for _, candidate := range ranked {
		if candidate.Candidate.Provider == primaryProvider {
			continue
		}
		out = append(out, candidate)
		if len(out) >= limit {
			break
		}
	}
	return out
}
