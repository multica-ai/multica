package daemon

import (
	"os"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// loadCodeArtsConfiguredModels reads OpenCode-compatible custom providers
// from CodeArts' user configuration. CodeArts can use these providers in its
// TUI even when its `models` command is blocked by the Huawei Cloud login
// preflight, so they are the daemon's non-network discovery fallback.
func loadCodeArtsConfiguredModels(home string) ([]agent.Model, error) {
	raw, err := os.ReadFile(codeArtsUserConfigPath(home))
	if os.IsNotExist(err) {
		return []agent.Model{}, nil
	}
	if err != nil {
		return nil, err
	}
	document, err := unmarshalRuntimeMcpConfig(raw, "jsonc")
	if err != nil {
		return nil, err
	}
	providers, ok := document["provider"].(map[string]any)
	if !ok {
		return []agent.Model{}, nil
	}

	providerNames := make([]string, 0, len(providers))
	for name := range providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	models := make([]agent.Model, 0)
	for _, rawProviderName := range providerNames {
		providerName := strings.TrimSpace(rawProviderName)
		providerConfig, ok := providers[rawProviderName].(map[string]any)
		if !ok || providerName == "" {
			continue
		}
		configuredModels, ok := providerConfig["models"].(map[string]any)
		if !ok {
			continue
		}
		modelNames := make([]string, 0, len(configuredModels))
		for name := range configuredModels {
			modelNames = append(modelNames, name)
		}
		sort.Strings(modelNames)
		for _, rawModelName := range modelNames {
			modelName := strings.TrimSpace(rawModelName)
			if modelName == "" {
				continue
			}
			label := modelName
			if modelConfig, ok := configuredModels[rawModelName].(map[string]any); ok {
				if configuredLabel, ok := modelConfig["name"].(string); ok && strings.TrimSpace(configuredLabel) != "" {
					label = strings.TrimSpace(configuredLabel)
				}
			}
			models = append(models, agent.Model{
				ID:       providerName + "/" + modelName,
				Label:    label,
				Provider: providerName,
			})
		}
	}
	return models, nil
}
