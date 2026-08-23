package cerebra

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// RoutingResult is the output of a single Route() call.
type RoutingResult struct {
	RuntimeID    string
	Model        string
	Tier         Tier
	MatchedRule  string
	FallbackUsed bool
	LatencyMs    int64
	Status       string // "ok" | "fallback" | "error"
}

// TierMap maps Tier values to concrete model IDs for one runtime.
// A nil or missing entry means that tier is not configured.
type TierMap map[Tier]string

// RuntimeEntry is one candidate runtime with its tier map.
type RuntimeEntry struct {
	RuntimeID string
	TierMap   TierMap
}

// Router is the central request-level model router. It combines the classifier,
// policy evaluator, session affinity, and availability cache into a single
// Route() call that returns the selected model (or a safe fallback).
//
// INVARIANT: filter candidates BEFORE selection. Never roll selection and then
// discard bad picks — that is the production failure mode Router was designed to close.
type Router struct {
	classifier   Classifier
	policy       *Policy
	session      *SessionStore
	unavail      *UnavailabilityStore
	logger       *slog.Logger
	routingLogFn func(ctx context.Context, entry RoutingLogEntry) // async write hook
}

// NewRouter constructs a Router with the given dependencies.
func NewRouter(
	classifier Classifier,
	policy *Policy,
	session *SessionStore,
	unavail *UnavailabilityStore,
	logger *slog.Logger,
	logFn func(ctx context.Context, entry RoutingLogEntry),
) *Router {
	return &Router{
		classifier:   classifier,
		policy:       policy,
		session:      session,
		unavail:      unavail,
		logger:       logger,
		routingLogFn: logFn,
	}
}

// Route selects the appropriate model for a task.
//
// Algorithm:
//  1. Classify prompt → tier + matched rule.
//  2. Load eligible candidates from runtimes' tier maps.
//  3. Remove unavailable candidates.
//  4. Remove policy-disallowed or over-budget candidates.
//  5. Reuse valid session pin if the same tier is requested.
//  6. Select from remaining (first available; extend with round-robin later).
//  7. Apply cross-runtime fallback / tier escalation if no candidate remains.
//  8. Write routing log asynchronously.
func (r *Router) Route(
	ctx context.Context,
	prompt string,
	meta TaskMeta,
	runtimes []RuntimeEntry,
	defaultModel string,
) RoutingResult {
	start := time.Now()

	tier, matchedRule, err := r.classifier.Score(ctx, prompt, meta)
	if err != nil {
		r.logger.Warn("cerebra: classifier error; using default model", "error", err)
		return r.fallback(ctx, defaultModel, matchedRule, start)
	}

	// Step 2–4: filter candidates.
	candidates := r.filterCandidates(ctx, runtimes, tier)

	// Step 5: session affinity — reuse pinned model if still valid and same tier.
	if pin := r.session.Get(ctx, meta.IssueID, meta.SessionID); pin != nil {
		if pin.Tier == tier && r.candidateExists(candidates, pin.RuntimeID, pin.Model) {
			r.session.Refresh(ctx, meta.IssueID, meta.SessionID)
			result := RoutingResult{
				RuntimeID:   pin.RuntimeID,
				Model:       pin.Model,
				Tier:        tier,
				MatchedRule: matchedRule + "+session_pin",
				LatencyMs:   time.Since(start).Milliseconds(),
				Status:      "ok",
			}
			r.writeLog(ctx, result, meta)
			return result
		}
	}

	// Step 6: select from filtered candidates.
	if len(candidates) > 0 {
		chosen := candidates[0] // deterministic: first candidate (extend to round-robin)
		r.session.Set(ctx, meta.IssueID, meta.SessionID, chosen.RuntimeID, chosen.Model, tier)
		result := RoutingResult{
			RuntimeID:   chosen.RuntimeID,
			Model:       chosen.Model,
			Tier:        tier,
			MatchedRule: matchedRule,
			LatencyMs:   time.Since(start).Milliseconds(),
			Status:      "ok",
		}
		r.writeLog(ctx, result, meta)
		return result
	}

	// Step 7: no candidates in target tier — try multi-level fallback before hard fallback.
	switch tier {
	case TierSimple:
		// Simple -> Standard -> Heavy
		if escalated := r.filterCandidates(ctx, runtimes, TierStandard); len(escalated) > 0 {
			result := RoutingResult{
				RuntimeID:    escalated[0].RuntimeID,
				Model:        escalated[0].Model,
				Tier:         TierStandard,
				MatchedRule:  matchedRule + "+escalated_to_standard",
				FallbackUsed: true,
				LatencyMs:    time.Since(start).Milliseconds(),
				Status:       "fallback",
			}
			r.writeLog(ctx, result, meta)
			return result
		}
		if escalated := r.filterCandidates(ctx, runtimes, TierHeavy); len(escalated) > 0 {
			result := RoutingResult{
				RuntimeID:    escalated[0].RuntimeID,
				Model:        escalated[0].Model,
				Tier:         TierHeavy,
				MatchedRule:  matchedRule + "+escalated_to_heavy",
				FallbackUsed: true,
				LatencyMs:    time.Since(start).Milliseconds(),
				Status:       "fallback",
			}
			r.writeLog(ctx, result, meta)
			return result
		}

	case TierStandard:
		// Standard -> Heavy -> Simple (if not MCP tool task)
		if escalated := r.filterCandidates(ctx, runtimes, TierHeavy); len(escalated) > 0 {
			result := RoutingResult{
				RuntimeID:    escalated[0].RuntimeID,
				Model:        escalated[0].Model,
				Tier:         TierHeavy,
				MatchedRule:  matchedRule + "+escalated_to_heavy",
				FallbackUsed: true,
				LatencyMs:    time.Since(start).Milliseconds(),
				Status:       "fallback",
			}
			r.writeLog(ctx, result, meta)
			return result
		}
		if !meta.WillUseMCPTools {
			if deescalated := r.filterCandidates(ctx, runtimes, TierSimple); len(deescalated) > 0 {
				result := RoutingResult{
					RuntimeID:    deescalated[0].RuntimeID,
					Model:        deescalated[0].Model,
					Tier:         TierSimple,
					MatchedRule:  matchedRule + "+fallback_to_simple",
					FallbackUsed: true,
					LatencyMs:    time.Since(start).Milliseconds(),
					Status:       "fallback",
				}
				r.writeLog(ctx, result, meta)
				return result
			}
		}

	case TierHeavy:
		// Heavy -> Standard -> Simple (if not MCP tool task)
		if deescalated := r.filterCandidates(ctx, runtimes, TierStandard); len(deescalated) > 0 {
			result := RoutingResult{
				RuntimeID:    deescalated[0].RuntimeID,
				Model:        deescalated[0].Model,
				Tier:         TierStandard,
				MatchedRule:  matchedRule + "+fallback_to_standard",
				FallbackUsed: true,
				LatencyMs:    time.Since(start).Milliseconds(),
				Status:       "fallback",
			}
			r.writeLog(ctx, result, meta)
			return result
		}
		if !meta.WillUseMCPTools {
			if deescalated := r.filterCandidates(ctx, runtimes, TierSimple); len(deescalated) > 0 {
				result := RoutingResult{
					RuntimeID:    deescalated[0].RuntimeID,
					Model:        deescalated[0].Model,
					Tier:         TierSimple,
					MatchedRule:  matchedRule + "+fallback_to_simple",
					FallbackUsed: true,
					LatencyMs:    time.Since(start).Milliseconds(),
					Status:       "fallback",
				}
				r.writeLog(ctx, result, meta)
				return result
			}
		}
	}

	// Hard fallback: use the pre-configured default model.
	r.logger.Warn("cerebra: no eligible candidates; using default model",
		"tier", tier, "matched_rule", matchedRule)
	return r.fallback(ctx, defaultModel, matchedRule, start)
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

type candidate struct {
	RuntimeID string
	Model     string
}

// filterCandidates applies the FILTER-BEFORE-SELECT invariant:
// unavailability → policy/budget → return clean pool.
func (r *Router) filterCandidates(ctx context.Context, runtimes []RuntimeEntry, tier Tier) []candidate {
	var out []candidate
	for _, rt := range runtimes {
		model, ok := rt.TierMap[tier]
		if !ok || model == "" {
			continue
		}
		if !r.unavail.IsAvailable(ctx, rt.RuntimeID, model) {
			continue
		}
		if !r.policy.Allow(rt.RuntimeID, model) {
			continue
		}
		out = append(out, candidate{RuntimeID: rt.RuntimeID, Model: model})
	}
	return out
}

func (r *Router) candidateExists(candidates []candidate, runtimeID, model string) bool {
	for _, c := range candidates {
		if c.RuntimeID == runtimeID && c.Model == model {
			return true
		}
	}
	return false
}

func (r *Router) fallback(_ context.Context, defaultModel, matchedRule string, start time.Time) RoutingResult {
	return RoutingResult{
		RuntimeID:    "",
		Model:        defaultModel,
		Tier:         TierSimple,
		MatchedRule:  matchedRule,
		FallbackUsed: true,
		LatencyMs:    time.Since(start).Milliseconds(),
		Status:       "fallback",
	}
}

func (r *Router) writeLog(ctx context.Context, result RoutingResult, meta TaskMeta) {
	if r.routingLogFn == nil {
		return
	}
	entry := RoutingLogEntry{
		TaskID:            meta.IssueID, // reuse until task-id is threaded through
		IssueID:           meta.IssueID,
		SessionID:         meta.SessionID,
		RuntimeID:         result.RuntimeID,
		ChosenModel:       result.Model,
		Tier:              string(result.Tier),
		MatchedRule:       result.MatchedRule,
		ToolChainExpected: meta.WillUseMCPTools,
		FallbackUsed:      result.FallbackUsed,
		LatencyMs:         int(result.LatencyMs),
		Status:            result.Status,
	}
	// Fire-and-forget so routing evidence never adds latency to task dispatch.
	go func() {
		if err := func() (retErr error) {
			defer func() {
				if rec := recover(); rec != nil {
					retErr = fmt.Errorf("routing log panic: %v", rec)
				}
			}()
			r.routingLogFn(ctx, entry)
			return nil
		}(); err != nil {
			r.logger.Warn("cerebra: routing log write failed", "error", err)
		}
	}()
}

// RoutingLogEntry is the record written to cerebra_routing_log.
type RoutingLogEntry struct {
	TaskID            string
	IssueID           string
	SessionID         string
	RuntimeID         string
	ChosenModel       string
	Tier              string
	MatchedRule       string
	ToolChainExpected bool
	FallbackUsed      bool
	LatencyMs         int
	Status            string

	// Optional
	PolicyReason      string
	CandidateCount    int
	ClassifierVersion string
	EstimatedCost     float64
	InputTokens       int
	OutputTokens      int
}

// ErrNoRoute is returned by integration tests when no model can be selected
// and no default is provided.
var ErrNoRoute = errors.New("cerebra: no eligible model candidate")
