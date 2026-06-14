package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/multica-ai/multica/server/internal/ail"
)

const (
	agentImprovementEvaluateName              = "agent_improvement_evaluate"
	agentImprovementDecisionReadyForCandidate = "ready_for_candidate"
	agentImprovementDecisionReadyForReview    = "ready_for_review"
	agentImprovementDecisionDefer             = "defer"
	agentImprovementDefaultMaxDecisions       = 5
	agentImprovementMaxDecisions              = 25
	agentImprovementMaxCandidates             = 100
	agentImprovementMaxSignatures             = 250
)

type agentImprovementEvaluateInput struct {
	CandidateDettools []ail.Stage3CandidateDettool `json:"candidate_dettools"`
	RepeatSignatures  []ail.Stage3Signature        `json:"repeat_signatures"`
	MaxDecisions      int                          `json:"max_decisions"`
}

type agentImprovementDecision struct {
	SuggestedName      string  `json:"suggested_name,omitempty"`
	SourceSignatureKey string  `json:"source_signature_key,omitempty"`
	Decision           string  `json:"decision"`
	Reason             string  `json:"reason"`
	SignalCount        int     `json:"signal_count"`
	UniqueTasks        int     `json:"unique_tasks"`
	UniqueAgents       int     `json:"unique_agents"`
	DeterminismGain    float64 `json:"expected_determinism_gain,omitempty"`
}

func agentImprovementEvaluateTool() Tool {
	return Tool{
		Name:        agentImprovementEvaluateName,
		Description: "Evaluate Stage 3 agent-improvement candidate dettools and repeat signatures into ready_for_candidate, ready_for_review, or defer decisions. Read-only and bounded.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "candidate_dettools": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "suggested_name": {"type": "string"},
          "source_signature_key": {"type": "string"},
          "expected_determinism_gain": {"type": "number"},
          "decision_hint": {"type": "string"}
        },
        "additionalProperties": false
      }
    },
    "repeat_signatures": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "key": {"type": "string"},
          "failure_reason": {"type": "string"},
          "error_signature": {"type": "string"},
          "loop_signature": {"type": "string"},
          "count": {"type": "integer"},
          "unique_tasks": {"type": "integer"},
          "unique_agents": {"type": "integer"},
          "first_seen": {"type": "string"},
          "last_seen": {"type": "string"},
          "example_task_id": {"type": "string"},
          "example_raw_ref": {"type": "string"}
        },
        "additionalProperties": false
      }
    },
    "max_decisions": {"type": "integer", "minimum": 1, "maximum": 25}
  },
  "additionalProperties": false
}`),
		Handler: agentImprovementEvaluateHandler,
	}
}

func agentImprovementEvaluateHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	_ = ctx
	_ = env

	var in agentImprovementEvaluateInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid %s input: %v", agentImprovementEvaluateName, err)
	}
	if len(in.CandidateDettools) > agentImprovementMaxCandidates {
		return Errf(CodeInvalidInput, "candidate_dettools has %d entries; maximum is %d", len(in.CandidateDettools), agentImprovementMaxCandidates)
	}
	if len(in.RepeatSignatures) > agentImprovementMaxSignatures {
		return Errf(CodeInvalidInput, "repeat_signatures has %d entries; maximum is %d", len(in.RepeatSignatures), agentImprovementMaxSignatures)
	}

	maxDecisions := normalizeAgentImprovementMaxDecisions(in.MaxDecisions)
	if maxDecisions == 0 {
		return Errf(CodeInvalidInput, "max_decisions must be between 1 and %d", agentImprovementMaxDecisions)
	}

	signatures := mapAgentImprovementSignatures(in.RepeatSignatures)
	decisions := make([]agentImprovementDecision, 0, len(in.CandidateDettools)+len(in.RepeatSignatures))
	for _, candidate := range in.CandidateDettools {
		decisions = append(decisions, evaluateAgentImprovementCandidate(candidate, signatures[candidate.SourceSignatureKey]))
	}

	if len(decisions) == 0 {
		for _, signature := range in.RepeatSignatures {
			decisions = append(decisions, deferAgentImprovementSignature(signature, "no Stage 3 candidate matched this signal"))
		}
	}
	sortAgentImprovementDecisions(decisions)
	recommended := countAgentImprovementDecisions(decisions, agentImprovementDecisionReadyForCandidate)
	decisions = capAgentImprovementDecisions(decisions, maxDecisions)

	data := map[string]any{
		"decisions":                   decisions,
		"recommended_candidate_count": recommended,
		"signal_count":                len(in.RepeatSignatures),
		"thresholds": map[string]any{
			"min_signature_count":   ail.MinSignatureCount,
			"min_unique_tasks":      ail.MinUniqueTasks,
			"default_max_decisions": agentImprovementDefaultMaxDecisions,
			"max_decisions":         agentImprovementMaxDecisions,
		},
	}
	return OK(fmt.Sprintf("evaluated %d candidate(s) against %d signal(s)", len(in.CandidateDettools), len(in.RepeatSignatures)), data)
}

func normalizeAgentImprovementMaxDecisions(value int) int {
	if value == 0 {
		return agentImprovementDefaultMaxDecisions
	}
	if value < 1 || value > agentImprovementMaxDecisions {
		return 0
	}
	return value
}

func mapAgentImprovementSignatures(signatures []ail.Stage3Signature) map[string]ail.Stage3Signature {
	out := make(map[string]ail.Stage3Signature, len(signatures))
	for _, signature := range signatures {
		if signature.Key != "" {
			out[signature.Key] = signature
		}
	}
	return out
}

func evaluateAgentImprovementCandidate(candidate ail.Stage3CandidateDettool, signature ail.Stage3Signature) agentImprovementDecision {
	if signature.Key == "" {
		return agentImprovementDecision{
			SuggestedName:      candidate.SuggestedName,
			SourceSignatureKey: candidate.SourceSignatureKey,
			Decision:           agentImprovementDecisionDefer,
			Reason:             "candidate does not match a repeat signature",
			DeterminismGain:    candidate.ExpectedDeterminismGain,
		}
	}

	decision := agentImprovementDecision{
		SuggestedName:      candidate.SuggestedName,
		SourceSignatureKey: candidate.SourceSignatureKey,
		Decision:           agentImprovementDecisionDefer,
		Reason:             "signal is below Stage 3 qualification thresholds",
		SignalCount:        signature.Count,
		UniqueTasks:        signature.UniqueTasks,
		UniqueAgents:       signature.UniqueAgents,
		DeterminismGain:    candidate.ExpectedDeterminismGain,
	}
	if signature.Count < ail.MinSignatureCount || signature.UniqueTasks < ail.MinUniqueTasks {
		return decision
	}
	if candidate.DecisionHint == agentImprovementDecisionReadyForCandidate || (signature.Count >= ail.MinSignatureCount*2 && signature.UniqueTasks > ail.MinUniqueTasks) {
		decision.Decision = agentImprovementDecisionReadyForCandidate
		decision.Reason = "high-volume repeat signal is structured enough for candidate scaffolding"
		return decision
	}
	decision.Decision = agentImprovementDecisionReadyForReview
	decision.Reason = "signal meets thresholds but needs human review before scaffolding"
	return decision
}

func deferAgentImprovementSignature(signature ail.Stage3Signature, reason string) agentImprovementDecision {
	return agentImprovementDecision{
		SourceSignatureKey: signature.Key,
		Decision:           agentImprovementDecisionDefer,
		Reason:             reason,
		SignalCount:        signature.Count,
		UniqueTasks:        signature.UniqueTasks,
		UniqueAgents:       signature.UniqueAgents,
	}
}

func sortAgentImprovementDecisions(decisions []agentImprovementDecision) {
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisionRank(decisions[i].Decision) != decisionRank(decisions[j].Decision) {
			return decisionRank(decisions[i].Decision) < decisionRank(decisions[j].Decision)
		}
		if decisions[i].SignalCount != decisions[j].SignalCount {
			return decisions[i].SignalCount > decisions[j].SignalCount
		}
		return decisions[i].SourceSignatureKey < decisions[j].SourceSignatureKey
	})
}

func decisionRank(decision string) int {
	switch decision {
	case agentImprovementDecisionReadyForCandidate:
		return 0
	case agentImprovementDecisionReadyForReview:
		return 1
	default:
		return 2
	}
}

func capAgentImprovementDecisions(decisions []agentImprovementDecision, maxDecisions int) []agentImprovementDecision {
	if len(decisions) <= maxDecisions {
		return decisions
	}
	return decisions[:maxDecisions]
}

func countAgentImprovementDecisions(decisions []agentImprovementDecision, target string) int {
	var count int
	for _, decision := range decisions {
		if decision.Decision == target {
			count++
		}
	}
	return count
}
