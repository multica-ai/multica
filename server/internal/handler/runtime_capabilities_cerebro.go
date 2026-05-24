package handler

// CEREBRO-PATCH(runtime-capability-normalize): keep live runtime capability views complete even when daemon snapshots are stale.

import (
	"encoding/json"
	"sort"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

func normalizedRuntimeCapabilities(provider string, rawCapabilities, rawToolsConfig []byte) map[string]any {
	out := cerebrocapabilities.LegacyProviderMap(provider)

	var reported map[string]any
	if len(rawCapabilities) > 0 && json.Unmarshal(rawCapabilities, &reported) == nil {
		for key, value := range reported {
			if shouldKeepReportedCapabilityValue(out, key, value) {
				out[key] = value
			}
		}
	}

	if servers := mcpServerNamesFromToolsConfig(rawToolsConfig); len(servers) > 0 {
		out["mcp_servers"] = servers
	}
	ensureCapabilityStringSlice(out, "providers", provider)
	ensureCapabilityStringSlice(out, "tools", "")
	ensureCapabilityStringSlice(out, "mcp_servers", "")
	return out
}

func shouldKeepReportedCapabilityValue(base map[string]any, key string, value any) bool {
	if len(anyStringSlice(value)) > 0 {
		return true
	}
	if len(anyStringSlice(base[key])) > 0 {
		return false
	}
	if _, ok := value.([]any); ok {
		return false
	}
	if _, ok := value.([]string); ok {
		return false
	}
	return value != nil
}

func ensureCapabilityStringSlice(caps map[string]any, key, fallback string) {
	if len(anyStringSlice(caps[key])) > 0 {
		return
	}
	if fallback != "" {
		caps[key] = []string{fallback}
		return
	}
	caps[key] = []string{}
}

func mcpServerNamesFromToolsConfig(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.MCPServers) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func anyStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
