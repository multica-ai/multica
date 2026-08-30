package cerebra

import (
	"regexp"
	"strconv"
	"strings"
)

var paramSizeRegex = regexp.MustCompile(`(?i)(?:^|[-_.:/@ ])(\d+(?:\.\d+)?)\s*([bmk])(?:$|[-_.:/@ ])`)

// extractParamSizeInBillions parses parameter size from model names (e.g. "qwen2.5:0.5b" -> 0.5, "llama3.3:70b" -> 70, "smollm:135m" -> 0.135).
func extractParamSizeInBillions(name string) (float64, bool) {
	matches := paramSizeRegex.FindStringSubmatch(name)
	if len(matches) < 3 {
		return 0, false
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}
	unit := strings.ToLower(matches[2])
	switch unit {
	case "m":
		return val / 1000.0, true
	case "k":
		return val / 1000000.0, true
	case "b":
		return val, true
	default:
		return val, true
	}
}

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

	// Strip provider prefix (e.g. "opencode/", "anthropic/", "openai/", "ollama/") for model name matching.
	baseName := lower
	if idx := strings.LastIndex(lower, "/"); idx != -1 {
		baseName = lower[idx+1:]
	}

	// 1. Explicit Simple flagship tags (e.g. "o1-mini", "gpt-4o-mini", "claude-3-5-haiku", "mimo-v2.5-free", "gemini-1.5-flash").
	simpleKeywords := []string{
		"mimo", "hy3", "flash", "haiku", "nano", "mini", "small", "spark", "lite", "x-preview", "tiny",
	}
	for _, kw := range simpleKeywords {
		if hasModelSegment(baseName, kw) {
			return TierSimple
		}
	}

	// 2. Explicit Heavy reasoning/flagship tags (e.g. "o1", "o3", "r1", "opus", "nemotron-3-ultra", "pickle").
	heavyKeywords := []string{
		"ultra", "opus", "pickle", "large", "max", "reasoning", "nemotron-3-ultra", "claude-3-opus",
		"r1", "o1", "o3", "pro",
	}
	for _, kw := range heavyKeywords {
		if hasModelSegment(baseName, kw) {
			return TierHeavy
		}
	}

	// 3. Dynamic Parameter-Size Auto-Detection for ANY newly downloaded model:
	if size, ok := extractParamSizeInBillions(baseName); ok {
		if size <= 4.0 {
			// <= 4B parameters (e.g. 0.5B, 1B, 1.5B, 2B, 3B, 3.8B) -> Simple Tier
			return TierSimple
		} else if size >= 30.0 {
			// >= 30B parameters (e.g. 32B, 70B, 72B, 110B, 405B) -> Heavy Tier
			return TierHeavy
		} else {
			// 4B to 30B parameters (e.g. 7B, 8B, 9B, 12B, 14B, 27B) -> Standard Tier
			return TierStandard
		}
	}

	// 4. Standard indicators: Balanced coding, debugging, sonnet, general instruct models.
	standardKeywords := []string{
		"sonnet", "coder", "instruct", "lightning", "standard", "code", "starcoder", "deepseek-coder",
		"gpt-4", "gpt-3.5", "nemotron-3.5", "3.5", "qwen", "mistral", "gemma", "llama",
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

func filterPreferredPool(candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}
	// 1. Separate direct authenticated / local providers from public aggregator proxies (openrouter/ and public free endpoints)
	var directCandidates []string
	var proxyCandidates []string
	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "openrouter/") || strings.Contains(lower, ":free") || strings.Contains(lower, "-free") || strings.Contains(lower, "/free") {
			proxyCandidates = append(proxyCandidates, c)
		} else {
			directCandidates = append(directCandidates, c)
		}
	}

	if len(directCandidates) > 0 {
		return directCandidates
	}
	return proxyCandidates
}

func selectBestSimpleModel(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	searchPool := filterPreferredPool(candidates)

	// 1. Prefer smallest tool-capable parameter model (e.g. 0.5B < 1B < 3B) with local Ollama priority
	var bestCandidate string
	minSize := 999999.0
	for _, c := range searchPool {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "smollm") {
			continue // smollm does not support tool calling
		}
		if size, ok := extractParamSizeInBillions(c); ok {
			effectiveSize := size
			if strings.HasPrefix(strings.ToLower(c), "ollama/") {
				effectiveSize -= 0.1 // Local priority
			}
			if effectiveSize < minSize {
				minSize = effectiveSize
				bestCandidate = c
			}
		}
	}
	if bestCandidate != "" {
		return bestCandidate
	}

	// 2. Prefer fast lightweight models (mini, nano, mimo, haiku, lite, flash)
	simpleTags := []string{"mini", "nano", "mimo", "haiku", "lite", "flash", "small", "tiny"}
	for _, c := range searchPool {
		lower := strings.ToLower(c)
		for _, tag := range simpleTags {
			if strings.Contains(lower, tag) {
				return c
			}
		}
	}
	return searchPool[0]
}

func selectBestStandardModel(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	searchPool := filterPreferredPool(candidates)

	// 1. Prefer mid-sized coding parameter models (7B - 16B)
	for _, c := range searchPool {
		if size, ok := extractParamSizeInBillions(c); ok && size >= 7.0 && size <= 16.0 {
			return c
		}
	}

	// 2. Prefer balanced coding / instruct model indicators
	standardTags := []string{"coder", "code", "sonnet", "flash", "instruct", "lightning", "standard"}
	for _, c := range searchPool {
		lower := strings.ToLower(c)
		for _, tag := range standardTags {
			if strings.Contains(lower, tag) {
				return c
			}
		}
	}

	// 3. Fall back to local Ollama models if present
	for _, c := range searchPool {
		if strings.HasPrefix(strings.ToLower(c), "ollama/") {
			return c
		}
	}

	return searchPool[0]
}

func selectBestHeavyModel(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	searchPool := filterPreferredPool(candidates)

	// 1. Prefer frontier reasoning tags (pro, r1, ultra, opus, o1, o3, max, large, reasoning)
	heavyTags := []string{"pro", "r1", "ultra", "opus", "o1", "o3", "max", "large", "reasoning"}
	for _, c := range searchPool {
		lower := strings.ToLower(c)
		for _, tag := range heavyTags {
			if strings.Contains(lower, tag) {
				return c
			}
		}
	}

	// 2. Prefer large parameter models (>= 30B, e.g. 31B, 32B, 70B, 405B)
	for _, c := range searchPool {
		if size, ok := extractParamSizeInBillions(c); ok && size >= 30.0 {
			return c
		}
	}

	return searchPool[0]
}


