package daemon

import (
	"encoding/json"
	"strings"
)

func resolveTaskModel(providerDefault string, runtimeConfig any) string {
	if model := runtimeConfigModel(runtimeConfig); model != "" {
		return model
	}
	return providerDefault
}

func runtimeConfigModel(runtimeConfig any) string {
	switch rc := runtimeConfig.(type) {
	case nil:
		return ""
	case map[string]any:
		return modelFromMap(rc)
	case map[string]string:
		return strings.TrimSpace(rc["model"])
	case json.RawMessage:
		return runtimeConfigModelFromJSON(rc)
	case []byte:
		return runtimeConfigModelFromJSON(rc)
	default:
		return ""
	}
}

func modelFromMap(runtimeConfig map[string]any) string {
	model, ok := runtimeConfig["model"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(model)
}

func runtimeConfigModelFromJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var runtimeConfig map[string]any
	if err := json.Unmarshal(raw, &runtimeConfig); err != nil {
		return ""
	}
	return modelFromMap(runtimeConfig)
}
