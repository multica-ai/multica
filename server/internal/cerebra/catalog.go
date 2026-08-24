package cerebra

import (
	"strings"
)

// ModelProfile contains metadata and capability classification for a discovered model.
type ModelProfile struct {
	ModelID string
	Tier    Tier
	Score   int
}

// ClassifyModelTier analyzes a model name/identifier and assigns its optimal complexity tier
// based on capability, latency, and parameter tier heuristics.
func ClassifyModelTier(modelID string) Tier {
	lower := strings.ToLower(modelID)

	// Heavy indicators: High-parameter, advanced reasoning, architecture-capable
	heavyKeywords := []string{
		"ultra", "opus", "pro", "pickle", "large", "max", "r1", "o1", "o3",
		"reasoning", "nemotron-3-ultra", "claude-3-opus", "gpt-4",
	}
	for _, kw := range heavyKeywords {
		if strings.Contains(lower, kw) {
			return TierHeavy
		}
	}

	// Standard indicators: Balanced coding, 30B+ parameter, debugging, sonnet
	standardKeywords := []string{
		"3.5", "nemotron-3.5", "sonnet", "coder", "instruct", "code", "standard",
	}
	for _, kw := range standardKeywords {
		if strings.Contains(lower, kw) {
			return TierStandard
		}
	}

	// Simple indicators: High-throughput, low-latency, lightweight
	simpleKeywords := []string{
		"mimo", "hy3", "flash", "haiku", "nano", "mini", "small", "spark", "preview",
	}
	for _, kw := range simpleKeywords {
		if strings.Contains(lower, kw) {
			return TierSimple
		}
	}

	// Default to Standard (balanced coding, debugging, refactoring)
	return TierStandard
}

// BuildTierMapFromCatalog scans a slice of discovered runtime models and dynamically selects
// the optimal model for each of the 3 tiers (Simple, Standard, Heavy).
func BuildTierMapFromCatalog(availableModels []string) TierMap {
	tierMap := make(TierMap)

	if len(availableModels) == 0 {
		return tierMap
	}

	var simpleCandidates []string
	var standardCandidates []string
	var heavyCandidates []string

	for _, model := range availableModels {
		if strings.TrimSpace(model) == "" {
			continue
		}
		tier := ClassifyModelTier(model)
		switch tier {
		case TierSimple:
			simpleCandidates = append(simpleCandidates, model)
		case TierStandard:
			standardCandidates = append(standardCandidates, model)
		case TierHeavy:
			heavyCandidates = append(heavyCandidates, model)
		}
	}

	// 1. Assign Simple Tier
	if len(simpleCandidates) > 0 {
		tierMap[TierSimple] = selectBestSimpleModel(simpleCandidates)
	} else if len(standardCandidates) > 0 {
		tierMap[TierSimple] = standardCandidates[0]
	} else if len(heavyCandidates) > 0 {
		tierMap[TierSimple] = heavyCandidates[0]
	}

	// 2. Assign Standard Tier
	if len(standardCandidates) > 0 {
		tierMap[TierStandard] = selectBestStandardModel(standardCandidates)
	} else if len(heavyCandidates) > 0 {
		tierMap[TierStandard] = heavyCandidates[0]
	} else if len(simpleCandidates) > 0 {
		tierMap[TierStandard] = simpleCandidates[0]
	}

	// 3. Assign Heavy Tier
	if len(heavyCandidates) > 0 {
		tierMap[TierHeavy] = selectBestHeavyModel(heavyCandidates)
	} else if len(standardCandidates) > 0 {
		tierMap[TierHeavy] = standardCandidates[0]
	} else if len(simpleCandidates) > 0 {
		tierMap[TierHeavy] = simpleCandidates[0]
	}

	return tierMap
}

func selectBestSimpleModel(candidates []string) string {
	// Simple tier: prefer lightweight, fast, conversational models
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "mimo") || strings.Contains(lower, "hy3") || strings.Contains(lower, "spark") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "haiku") || strings.Contains(lower, "mini") || strings.Contains(lower, "nano") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "flash") {
			return c
		}
	}
	return candidates[0]
}

func selectBestStandardModel(candidates []string) string {
	// Standard tier: prefer strong coding, debugging, and tool execution models
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "3.5") || strings.Contains(lower, "lightning") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "sonnet") || strings.Contains(lower, "coder") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "preview") {
			return c
		}
	}
	return candidates[0]
}

func selectBestHeavyModel(candidates []string) string {
	// Prefer ultra > opus > pickle > r1 > pro > first
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "ultra") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "opus") {
			return c
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "pickle") || strings.Contains(lower, "r1") {
			return c
		}
	}
	return candidates[0]
}
