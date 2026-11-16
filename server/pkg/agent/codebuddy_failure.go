package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Optional diagnostic fields are decoded independently so a new error shape
// cannot make an otherwise valid terminal result disappear from the stream.
func codebuddyResultError(msg codebuddySDKMessage) string {
	var parts []string
	seen := make(map[string]bool)
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text != "" && !seen[text] {
			parts = append(parts, text)
			seen[text] = true
		}
	}
	add(msg.ResultText)
	var errors []json.RawMessage
	if json.Unmarshal(msg.Errors, &errors) == nil {
		for _, raw := range errors {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				add(text)
			}
		}
	}
	var infos []json.RawMessage
	if json.Unmarshal(msg.ErrorsInfo, &infos) == nil {
		for _, raw := range infos {
			var info struct {
				Status  json.RawMessage `json:"status"`
				Details json.RawMessage `json:"details"`
			}
			if json.Unmarshal(raw, &info) != nil {
				continue
			}
			var status int
			// Only the HTTP status dimension is an HTTP witness. The separate
			// business/SDK code can also be numeric (including 401 or 429).
			if json.Unmarshal(info.Status, &status) == nil && status >= 400 && status <= 599 {
				add(fmt.Sprintf("HTTP %d", status))
			}
			var details string
			if json.Unmarshal(info.Details, &details) == nil {
				add(details)
			}
		}
	}
	return strings.Join(parts, "; ")
}

type byteCountingReader struct {
	io.Reader
	bytes int64
}

func (r *byteCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += int64(n)
	return n, err
}

func codebuddyFailureDiagnostic(errMsg string, obs streamProtocolObservation, stdoutBytes, stderrBytes int64) string {
	const maxDetailBytes = 8192
	const truncated = " [truncated]"
	detail := sanitizeAgentDiagnostic(errMsg)
	if len(detail) > maxDetailBytes {
		detail = truncateUTF8(detail, maxDetailBytes-len(truncated)) + truncated
	}
	// Keep this numeric/boolean line in sync with taskfailure's filter. Counts
	// are diagnostic evidence and must never act as provider HTTP status codes.
	return fmt.Sprintf("%s\n[codebuddy protocol: exit_code=%d stdout_bytes=%d stderr_bytes=%d event_count=%d invalid_event_count=%d assistant_event_count=%d tool_use_count=%d saw_result=%t result_is_error=%t result_bytes=%d last_assistant_bytes=%d scanner_error=%t]",
		detail, obs.exitCode, stdoutBytes, stderrBytes, obs.eventCount, obs.invalidEventCount,
		obs.assistantEventCount, obs.toolUseCount, obs.sawResult, obs.resultIsError,
		obs.resultBytes, obs.lastAssistantBytes, obs.scannerError)
}
