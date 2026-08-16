package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxAgentAllowedTools     = 128
	maxAgentAllowedToolBytes = 256
)

// parseAllowedTools validates the provider-native tool patterns stored in the
// agent JSONB column. A nil or JSON null value means unrestricted; an empty
// array is an explicit deny-all configuration.
func parseAllowedTools(raw json.RawMessage) ([]string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}

	var tools []string
	if err := json.Unmarshal(trimmed, &tools); err != nil {
		return nil, false, fmt.Errorf("allowed_tools must be an array of strings: %w", err)
	}
	if len(tools) > maxAgentAllowedTools {
		return nil, false, fmt.Errorf("allowed_tools must contain at most %d entries", maxAgentAllowedTools)
	}

	seen := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			return nil, false, fmt.Errorf("allowed_tools[%d] must not be empty", i)
		}
		if len(tool) > maxAgentAllowedToolBytes {
			return nil, false, fmt.Errorf("allowed_tools[%d] must be %d bytes or fewer", i, maxAgentAllowedToolBytes)
		}
		if _, exists := seen[tool]; exists {
			return nil, false, fmt.Errorf("allowed_tools contains duplicate entry %q", tool)
		}
		seen[tool] = struct{}{}
		tools[i] = tool
	}

	return tools, true, nil
}
