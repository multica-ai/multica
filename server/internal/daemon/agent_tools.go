package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	maxDaemonAllowedTools     = 128
	maxDaemonAllowedToolBytes = 256
)

func decodeAllowedTools(raw json.RawMessage) ([]string, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false, nil
	}

	var tools []string
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, false, fmt.Errorf("allowed_tools must be an array of strings: %w", err)
	}
	if len(tools) > maxDaemonAllowedTools {
		return nil, false, fmt.Errorf("allowed_tools may contain at most %d entries", maxDaemonAllowedTools)
	}

	seen := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		if len(tool) == 0 || len(tool) > maxDaemonAllowedToolBytes {
			return nil, false, fmt.Errorf("allowed_tools[%d] must be between 1 and %d bytes", i, maxDaemonAllowedToolBytes)
		}
		if _, ok := seen[tool]; ok {
			return nil, false, fmt.Errorf("allowed_tools[%d] duplicates %q", i, tool)
		}
		seen[tool] = struct{}{}
	}

	return tools, true, nil
}
