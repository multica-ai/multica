package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// assistantTurn is the classified outcome of one assistant stream event.
//
// The stream-json backends parse different wire shapes but share one question
// per event: may a fallback answer captured earlier in the run survive it?
type assistantTurn struct {
	text     string
	toolUses int
	// understood is false when the event's message body failed to parse, or
	// carried a content block type the backend does not handle. In both cases
	// the backend cannot tell whether the model delivered its answer inside
	// that event, so an earlier fallback must not outlive it.
	understood bool
}

// resolveFallback returns the empty-result fallback after this turn, given the
// fallback held before it. It is the single place the three stream-json
// backends encode which turns may leave an answer behind.
func (t assistantTurn) resolveFallback(prior string) string {
	switch {
	case t.toolUses > 0 || !t.understood:
		// A turn that invokes a tool is intermediate even when it also
		// contains narration. An unreadable turn tells us nothing at all —
		// the model may have answered in a shape we failed to parse. Neither
		// may leave a stale answer behind for the terminal result to adopt:
		// that is how pre-tool narration would reach a user as the final
		// answer (#6006), which the fail-closed contract below exists to stop.
		return ""
	case t.text != "":
		return t.text
	default:
		// Understood, text-less and tool-less — a thinking-only turn. It
		// carries neither an answer nor an intermediacy signal, so it must
		// leave an answer the model already delivered untouched.
		return prior
	}
}

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
	// terminalReasonError, when non-empty, is a failure the backend read out of
	// a STRUCTURED field on the terminal result event.
	//
	// It is not a claim that is_error missed the failure — on every frame
	// captured so far the two fire together. It is a claim about which one
	// NAMES the failure: Claude Code sets is_error from whether the last
	// message it rendered was an API error, and terminal_reason from why the
	// turn ended, so only the latter identifies the condition without relying
	// on the CLI's prose (GH #6402). Backends that read no such field leave
	// this empty and keep the pre-existing contract.
	terminalReasonError string
}

// finalizeStreamResult applies the shared fail-closed terminal contract used by
// Claude Code and CodeBuddy. A clean process exit is not proof that the
// stream-json protocol completed: success requires a result event. Failed runs
// always return an empty output so upstream issue/chat fallbacks can never
// mistake a partial transcript for a final answer.
//
// A SUCCESSFUL run with no deliverable text returns an empty output for the same
// reason. Empty is the platform's signal for "this turn produced no final text",
// and every delivery surface already owns a decision keyed on it: an issue task
// skips the synthesized fallback comment, a direct chat writes a localized
// no_response outcome, and a Slack/Lark channel drops the turn rather than push
// a placeholder outward. Returning prose here instead makes the output non-empty
// and silently defeats all three at once — that is how an English sentinel
// reached a Lark thread (GH #6462). Which words a user sees, and in which
// language, belongs to each delivery surface; the stream parser only reports
// whether text exists.
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
	switch {
	case state.terminalReasonError != "":
		// A recognised structured terminal reason wins outright — including
		// over is_error, which is the ordering that matters in practice.
		//
		// On the shape actually captured from Claude Code 2.1.220/2.1.221, a
		// context-exhausted turn arrives as is_error:true AND
		// terminal_reason:prompt_too_long together. Letting is_error go first
		// makes the structured branch dead code on the only frame we have, and
		// hands the failure whatever prose the CLI happened to put in `result`:
		// today that string classifies correctly by luck, but an empty or
		// reworded `result` degrades to "returned an error result without
		// details" → agent_error.unknown, which no resume blacklist covers, so
		// the saturated session stays pinned and the stall returns (GH #6402).
		//
		// Ordering it first costs nothing: the branch only fires for a reason
		// the backend positively recognised, and the message it builds carries
		// the CLI's own `result` text along as detail, so nothing is lost for
		// diagnosis.
		status = "failed"
		errMsg = state.terminalReasonError
	case state.resultIsError:
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
	return status, state.lastAssistantText, ""
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
	// unhandledEventTypeCount / unhandledEventTypes / unhandledSubtypeCount
	// report stream events the parser did not turn into messages. They belong on
	// this line rather than only in a separate warning so they can be read
	// together with toolUseCount and invalidEventCount.
	//
	// They are evidence, not a verdict. A non-zero count means the stream
	// carried events we do not handle and is the starting point for identifying
	// a protocol change; a zero count means only that none were observed at the
	// top level, and does not establish that the agent used no tools — a CLI can
	// execute tools without serializing the updates at all, and a new shape can
	// be nested inside a type we already recognize. Set by providers that track
	// them; zero elsewhere.
	unhandledEventTypeCount int
	unhandledEventTypes     string
	unhandledSubtypeCount   int
	// unreadableAssistantCount reports assistant events whose message body did
	// not parse, or that carried a content block type the backend does not
	// handle. Unlike unhandledEventTypeCount (a TOP-LEVEL event type we skip),
	// this counts events we accepted and then could not fully read.
	//
	// Non-zero means the run dropped its empty-result fallback defensively, so
	// a task that ended with no final text may have had an answer we could not
	// see. It is the first number to check when a known-good agent starts
	// returning empty outputs after a CLI upgrade.
	unreadableAssistantCount int
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
		"unhandled_event_type_count", obs.unhandledEventTypeCount,
		"unhandled_event_types", obs.unhandledEventTypes,
		"unhandled_subtype_count", obs.unhandledSubtypeCount,
		"unreadable_assistant_count", obs.unreadableAssistantCount,
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
