package agent

import (
	"bytes"
	"encoding/json"
)

// hasManagedMcpConfig preserves the API's three-state MCP semantics. Only SQL
// NULL / JSON null mean "inherit the runtime configuration"; any object,
// including an explicitly empty one, is a managed set and enables strict mode.
func hasManagedMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
