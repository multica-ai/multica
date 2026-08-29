package agent

import "strings"

// apiProtocol identifies the wire protocol used by an HTTP provider model.
// OpenCode Zen and OpenCode Go expose different protocols per model, so the
// route must be resolved from the provider and model rather than inferred
// from the provider name alone.
type apiProtocol string

const (
	apiProtocolChatCompletions   apiProtocol = "chat-completions"
	apiProtocolResponses         apiProtocol = "responses"
	apiProtocolAnthropicMessages apiProtocol = "anthropic-messages"
)

// providerModelAPIProtocol returns the route supported by a provider/model
// pair. OpenCode's models endpoint intentionally does not make this route a
// stable part of the catalog response, so this resolver follows the provider
// catalog's documented model families and fails closed for native Google
// models until a Gemini adapter exists.
func providerModelAPIProtocol(provider, model string) (apiProtocol, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "", false
	}
	switch provider {
	case "opencode-zen", "opencode-go":
		if strings.HasPrefix(model, "gemini-") {
			return "", false
		}
		if strings.HasPrefix(model, "claude-") || strings.HasPrefix(model, "qwen3") {
			return apiProtocolAnthropicMessages, true
		}
		if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "grok-") || strings.HasPrefix(model, "muse-spark") {
			return apiProtocolResponses, true
		}
		return apiProtocolChatCompletions, true
	default:
		return apiProtocolChatCompletions, true
	}
}
