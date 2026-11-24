package taskfailure

import (
	"encoding/json"
	"strings"
)

// IsTraePromptStreamDisconnect identifies the provider failure whose replay needs
// a separate check of tool activity for the failed turn.
func IsTraePromptStreamDisconnect(raw string) bool {
	return classifyTraePromptStreamFailure(strings.ToLower(strings.TrimSpace(WithoutCodebuddyProtocolSummary(raw)))) == ReasonAgentProviderNetwork
}

// Trae wraps prompt errors in the same ACP internal error. Only discard that
// wrapper when every recorded reconnect points to an interrupted response.
func classifyTraePromptStreamFailure(lower string) Reason {
	const prefix = "traecli session/prompt failed: session/prompt: internal error (code=-32603, data="
	data, ok := strings.CutPrefix(lower, prefix)
	if !ok {
		return ""
	}
	data, ok = strings.CutSuffix(data, ")")
	if !ok {
		return ""
	}

	const emptyStream = "stream disconnected before completion: llm response empty stream"
	var payload struct {
		Message string          `json:"message"`
		Cause   json.RawMessage `json:"trae_error_info"`
		Retries []struct {
			Cause   json.RawMessage `json:"trae_error_info"`
			Details string          `json:"additional_details"`
		} `json:"trae_stream_errors"`
	}
	if json.Unmarshal([]byte(data), &payload) != nil || payload.Message != emptyStream || len(payload.Retries) == 0 {
		return ""
	}

	switch string(payload.Cause) {
	case "", "null", `"other"`:
	default:
		if !traeResponseDisconnected(payload.Cause) {
			return classifyAgentErrorText(lower)
		}
	}
	for _, retry := range payload.Retries {
		if !traeResponseDisconnected(retry.Cause) || (retry.Details != "" && retry.Details != emptyStream) {
			return classifyAgentErrorText(lower)
		}
	}
	// Keep the provider data so specific errors retain their normal precedence.
	return classifyAgentErrorText(payload.Message + " " + data)
}

func traeResponseDisconnected(cause json.RawMessage) bool {
	var info struct {
		Disconnect *struct {
			HTTPStatus int `json:"http_status_code"`
		} `json:"response_stream_disconnected"`
	}
	if json.Unmarshal(cause, &info) != nil || info.Disconnect == nil {
		return false
	}
	status := info.Disconnect.HTTPStatus
	return status == 0 || (status >= 200 && status < 300)
}
