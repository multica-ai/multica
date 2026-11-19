package agent

import "regexp"

// bedrockApplicationInferenceProfileRe matches a Bedrock application
// inference profile ARN. Unlike foundation-model ARNs and system-defined
// cross-region profiles (`us.anthropic.claude-*`), its resource id is an
// opaque random string: it names no model, so no pricing table can price it.
var bedrockApplicationInferenceProfileRe = regexp.MustCompile(
	`(?i)^arn:aws[a-z-]*:bedrock:[a-z0-9-]+:\d{12}:application-inference-profile/[a-z0-9]+$`,
)

func isBedrockApplicationInferenceProfileARN(model string) bool {
	return bedrockApplicationInferenceProfileRe.MatchString(model)
}

// resolveBedrockProfileUsage renames a Bedrock application inference profile
// key in a Claude run's usage to the model Bedrock actually served.
//
// When ANTHROPIC_MODEL is an application inference profile ARN, Claude Code
// keys the result's modelUsage by that ARN, while every assistant message
// carries the served model id (`claude-opus-5`) that Bedrock returns in the
// response body. Without this, each such run is recorded under the ARN and
// shows as unpriced on the dashboard.
//
// The rename happens only when it is unambiguous: exactly one profile key in
// the usage, and exactly one served model seen in the run. Any other shape is
// returned unchanged, so the run stays visibly unpriced rather than wrongly
// priced. The input map is not modified.
func resolveBedrockProfileUsage(usage map[string]TokenUsage, served map[string]struct{}) map[string]TokenUsage {
	profile := ""
	for model := range usage {
		if !isBedrockApplicationInferenceProfileARN(model) {
			continue
		}
		if profile != "" {
			return usage
		}
		profile = model
	}
	if profile == "" {
		return usage
	}

	target := ""
	for model := range served {
		// Claude Code labels locally generated messages "<synthetic>".
		if model == "" || model == "<synthetic>" || isBedrockApplicationInferenceProfileARN(model) {
			continue
		}
		if target != "" {
			return usage
		}
		target = model
	}
	if target == "" {
		return usage
	}

	resolved := make(map[string]TokenUsage, len(usage))
	for model, u := range usage {
		if model == profile {
			model = target
		}
		sum := resolved[model]
		sum.InputTokens += u.InputTokens
		sum.OutputTokens += u.OutputTokens
		sum.CacheReadTokens += u.CacheReadTokens
		sum.CacheWriteTokens += u.CacheWriteTokens
		sum.CostUSDTicks += u.CostUSDTicks
		resolved[model] = sum
	}
	return resolved
}
