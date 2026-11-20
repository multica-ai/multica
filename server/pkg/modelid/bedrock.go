package modelid

import "regexp"

// Bedrock Anthropic ids may follow routing prefixes such as `custom:bedrock/`.
var bedrockAnthropicIDRe = regexp.MustCompile(`^(?:[a-z][a-z0-9_-]*[/:])*(?:(?:us|eu|apac|global)\.)?anthropic\.(claude-)`)

// Preserve a single context tag while removing the Bedrock version.
var bedrockVersionSuffixRe = regexp.MustCompile(`-v\d+:\d+(\[[^\]]+\])?$`)

// NormalizeBedrock maps a lowercase Bedrock Anthropic model ID to its bare
// model ID. Other IDs are returned as supplied. Keep this in step with
// stripBedrock in packages/views/runtimes/utils.ts for pricing consistency.
func NormalizeBedrock(model string) string {
	if !bedrockAnthropicIDRe.MatchString(model) {
		return model
	}
	model = bedrockAnthropicIDRe.ReplaceAllString(model, "${1}")
	return bedrockVersionSuffixRe.ReplaceAllString(model, "${1}")
}
