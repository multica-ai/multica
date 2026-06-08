// Package dettools implements Multica's deterministic tool plane: a set of
// typed, auditable, read-only tools exposed to coding agents over MCP (Model
// Context Protocol) via stdio. The MCP host and every tool handler are pure Go
// compiled into the multica binary, so the tool plane ships with the daemon and
// needs no separate runtime. The daemon spawns this server per task by pointing
// an agent's mcp_config at `multica mcp-tools serve`.
package dettools

import "fmt"

// Status values for a tool Result.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Stable error codes. These are part of the tool contract: agents and the
// daemon may branch on them, so values must not change once shipped.
const (
	CodeInvalidInput      = "INVALID_INPUT"
	CodeMissingDependency = "MISSING_DEPENDENCY"
	CodePolicyFailure     = "POLICY_FAILURE"
	CodeTimeout           = "TIMEOUT"
	CodeInternal          = "INTERNAL_ERROR"
)

// Artifact describes a file a tool produced for the agent or UI to consume.
type Artifact struct {
	Type string `json:"type"` // e.g. "json", "markdown"
	Path string `json:"path"` // path relative to the task working directory
}

// Result is the fixed envelope every deterministic tool returns. It is
// serialized as both the MCP text content and structuredContent of a tools/call
// response.
type Result struct {
	Status      string         `json:"status"`
	Summary     string         `json:"summary"`
	MachineData map[string]any `json:"machine_data,omitempty"`
	Artifacts   []Artifact     `json:"artifacts,omitempty"`
	Retryable   bool           `json:"retryable"`
	ErrorCode   string         `json:"error_code,omitempty"`
}

// OK builds a successful Result.
func OK(summary string, data map[string]any) Result {
	return Result{Status: StatusOK, Summary: summary, MachineData: data}
}

// Errf builds a failed Result with a stable error code. TIMEOUT and
// INTERNAL_ERROR are marked retryable; deterministic failures (INVALID_INPUT,
// MISSING_DEPENDENCY, POLICY_FAILURE) are not, because retrying the same input
// yields the same outcome.
func Errf(code, format string, args ...any) Result {
	return Result{
		Status:    StatusError,
		ErrorCode: code,
		Summary:   fmt.Sprintf(format, args...),
		Retryable: code == CodeTimeout || code == CodeInternal,
	}
}
