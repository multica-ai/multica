package taskfailure

import "regexp"

// Match only the adapter's complete numeric/boolean summary line; provider
// error text and HTTP statuses remain classification evidence. Keep the format
// in sync with agent.codebuddyFailureDiagnostic.
var codebuddyProtocolSummaryRe = regexp.MustCompile(`(?m)^\[codebuddy protocol: exit_code=-?[0-9]+ stdout_bytes=[0-9]+ stderr_bytes=[0-9]+ event_count=[0-9]+ invalid_event_count=[0-9]+ assistant_event_count=[0-9]+ tool_use_count=[0-9]+ saw_result=(true|false) result_is_error=(true|false) result_bytes=[0-9]+ last_assistant_bytes=[0-9]+ scanner_error=(true|false)\]$`)

// WithoutCodebuddyProtocolSummary removes adapter metadata before error text
// is used to classify a failure or decide whether its session is resumable.
func WithoutCodebuddyProtocolSummary(raw string) string {
	return codebuddyProtocolSummaryRe.ReplaceAllString(raw, "")
}
