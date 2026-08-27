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
	lower := strings.ToLower(strings.TrimSpace(modelID))
	if lower == "" {
		return TierStandard
	}

	// Strip provider prefix (e.g. "opencode/", "anthropic/", "openai/") for model name matching.
	baseName := lower
	if idx := strings.LastIndex(lower, "/"); idx != -1 {
		baseName = lower[idx+1:]
	}

	// 1. Simple indicators (e.g. "o1-mini", "gpt-4o-mini", "claude-3-5-haiku", "mimo-v2.5-free", "x-preview-f-free").
	// Note: "preview" alone is a release tag, not always a tier indicator (e.g. "o1-preview", "gemini-1.5-pro-preview").
	simpleKeywords := []string{
		"mimo", "hy3", "flash", "haiku", "nano", "mini", "small", "spark", "lite", "x-preview",
	}
	for _, kw := range simpleKeywords {
		if hasModelSegment(baseName, kw) {
			return TierSimple
		}
	}

	// 2. Heavy indicators: High-parameter, advanced reasoning, architecture-capable models.
	heavyKeywords := []string{
		"ultra", "opus", "pickle", "large", "max", "reasoning", "nemotron-3-ultra", "claude-3-opus",
		"r1", "o1", "o3", "pro",
	}
	for _, kw := range heavyKeywords {
		if hasModelSegment(baseName, kw) {
			return TierHeavy
		}
	}

	// 3. Standard indicators: Balanced coding, 30B+ parameter, debugging, sonnet, general instruct models.
	standardKeywords := []string{
		"sonnet", "coder", "instruct", "lightning", "standard", "code", "starcoder", "deepseek-coder",
		"gpt-4", "gpt-3.5", "nemotron-3.5", "3.5",
	}
	for _, kw := range standardKeywords {
		if hasModelSegment(baseName, kw) {
			return TierStandard
		}
	}

	// Default to Standard (balanced coding, debugging, refactoring)
	return TierStandard
}

// hasModelSegment checks whether keyword exists in name bounded by delimiters (-_./ : or start/end).
func hasModelSegment(name, keyword string) bool {
	if keyword == "" {
		return false
	}
	offset := 0
	for {
		idx := strings.Index(name[offset:], keyword)
		if idx == -1 {
			return false
		}
		absIdx := offset + idx
		end := absIdx + len(keyword)

		beforeOK := absIdx == 0 || isSegmentDelimiter(name[absIdx-1])
		afterOK := end == len(name) || isSegmentDelimiter(name[end])

		if beforeOK && afterOK {
			return true
		}
		offset = absIdx + 1
		if offset >= len(name) {
			return false
		}
	}
}

func isSegmentDelimiter(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == '/' || b == ':' || b == ' ' || b == '@'
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
