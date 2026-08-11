package handler

const (
	runtimeModeLocal = "local"
	runtimeModeCloud = "cloud"
)

type runtimeProviderDefinition struct {
	RuntimeMode string
}

// runtimeProviderRegistry is the server-owned classification of built-in
// providers. Unknown providers fail safely to local; daemon payloads never
// grant themselves cloud placement eligibility.
var runtimeProviderRegistry = map[string]runtimeProviderDefinition{
	"platform-agent-cli": {RuntimeMode: runtimeModeCloud},
}

func runtimeProviderDefinitionFor(rawProvider string) runtimeProviderDefinition {
	provider := normalizeProvider(rawProvider)
	if definition, ok := runtimeProviderRegistry[provider]; ok {
		return definition
	}
	return runtimeProviderDefinition{RuntimeMode: runtimeModeLocal}
}
