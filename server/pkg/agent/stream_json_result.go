package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

const emptySuccessfulStreamResult = "The agent completed without a final response."

// streamTerminalState keeps the user-facing final answer separate from the
// streamed assistant turns. Assistant messages are still emitted through the
// Session.Messages channel for live progress/transcript storage, but only a
// terminal result (or the last complete assistant message after an explicitly
// successful empty result) may become Result.Output.
type streamTerminalState struct {
	lastAssistantText string
	finalResultText   string
	sawResult         bool
	resultIsError     bool
	scanErr           error
}

// finalizeStreamResult applies the shared fail-closed terminal contract used by
// Claude Code and CodeBuddy. A clean process exit is not proof that the
// stream-json protocol completed: success requires a result event. Failed runs
// always return an empty output so upstream issue/chat fallbacks can never
// mistake a partial transcript for a final answer.
func finalizeStreamResult(
	provider string,
	timeout time.Duration,
	runErr error,
	writeErr error,
	exitErr error,
	sessionID string,
	state streamTerminalState,
	completionGuardError string,
) (status, output, errMsg string) {
	status = "completed"
	if state.resultIsError {
		status = "failed"
		errMsg = state.finalResultText
		if errMsg == "" {
			errMsg = provider + " returned an error result without details"
		}
	}

	switch {
	case status == "completed" && errors.Is(runErr, context.DeadlineExceeded):
		status = "timeout"
		errMsg = fmt.Sprintf("%s timed out after %s", provider, timeout)
	case status == "completed" && errors.Is(runErr, context.Canceled):
		status = "aborted"
		errMsg = "execution cancelled"
	case state.scanErr != nil && status == "completed":
		status = "failed"
		errMsg = fmt.Sprintf("%s stdout read error: %v", provider, state.scanErr)
	case writeErr != nil && status == "completed" && sessionID == "":
		status = "failed"
		errMsg = fmt.Sprintf("write %s input: %v", provider, writeErr)
	case exitErr != nil && status == "completed":
		status = "failed"
		errMsg = fmt.Sprintf("%s exited with error: %v", provider, exitErr)
	case !state.sawResult && status == "completed":
		status = "failed"
		errMsg = provider + " stream ended without terminal result"
	}

	if status == "completed" && completionGuardError != "" {
		status = "failed"
		errMsg = completionGuardError
	}

	if status != "completed" {
		return status, "", errMsg
	}
	if state.finalResultText != "" {
		return status, state.finalResultText, ""
	}
	if state.lastAssistantText != "" {
		return status, state.lastAssistantText, ""
	}
	return status, emptySuccessfulStreamResult, ""
}

type streamProtocolObservation struct {
	provider                   string
	cliVersion                 string
	model                      string
	exitCode                   int
	eventCount                 int
	invalidEventCount          int
	assistantEventCount        int
	toolUseCount               int
	sawResult                  bool
	resultIsError              bool
	resultBytes                int
	lastAssistantBytes         int
	scannerError               bool
	lastEventType              string
	anthropicBaseURLConfigured bool
}

// logStreamProtocolObservation records only protocol metadata. It deliberately
// excludes assistant/result text, tool input/output, the configured base URL,
// and environment values so diagnosing a missing terminal event cannot leak the
// task transcript or provider credentials into daemon logs.
func logStreamProtocolObservation(logger *slog.Logger, obs streamProtocolObservation) {
	logger.Info("agent stream protocol summary",
		"provider", obs.provider,
		"cli_version", obs.cliVersion,
		"model", obs.model,
		"exit_code", obs.exitCode,
		"event_count", obs.eventCount,
		"invalid_event_count", obs.invalidEventCount,
		"assistant_event_count", obs.assistantEventCount,
		"tool_use_count", obs.toolUseCount,
		"saw_result", obs.sawResult,
		"result_is_error", obs.resultIsError,
		"result_bytes", obs.resultBytes,
		"last_assistant_bytes", obs.lastAssistantBytes,
		"scanner_error", obs.scannerError,
		"last_event_type", obs.lastEventType,
		"anthropic_base_url_configured", obs.anthropicBaseURLConfigured,
	)
}

func streamProcessExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
