package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// hermesProviderUnconfiguredError is the failure exactly as it reaches the
// daemon: Hermes' message wrapped in the ACP transport's framing, which is why
// the predicate matches on a substring rather than equality.
const hermesProviderUnconfiguredError = `hermes session/new failed: session/new: Internal error ` +
	`(code=-32603, data={"details":"No LLM provider configured. Run ` + "`hermes model`" +
	` to select a provider, or run ` + "`hermes setup`" + ` for first-time configuration."})`

func TestAnnotateHermesProviderUnconfigured(t *testing.T) {
	t.Parallel()

	t.Run("explains what hermes could not", func(t *testing.T) {
		t.Parallel()
		got := annotateHermesProviderUnconfigured(hermesProviderUnconfiguredError, "hermes", true)

		// The original text has to survive: it is what the runtime actually
		// said, and the hint is additive context, not a replacement.
		if !strings.Contains(got, "No LLM provider configured") {
			t.Errorf("the runtime's own message must be preserved, got: %s", got)
		}
		// The hint has to carry the insight Hermes' own remedy lacks — that a
		// different home was read — and point at where the path is recorded.
		for _, want := range []string{"HERMES_HOME", "hermes home resolved", "custom_env"} {
			if !strings.Contains(got, want) {
				t.Errorf("hint must mention %q, got: %s", want, got)
			}
		}
	})

	t.Run("leaves unrelated failures alone", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name         string
			errMsg       string
			provider     string
			overlayBuilt bool
		}{
			// A different provider's error can carry similar words; the
			// HERMES_HOME advice would be nonsense there.
			{"other provider", hermesProviderUnconfiguredError, "codex", true},
			// No overlay was built, so there is no seeding story to tell.
			{"no overlay", hermesProviderUnconfiguredError, "hermes", false},
			// An expired or rejected credential is a different failure: the
			// config WAS found. Widening into it would send users to the wrong
			// fix, which is the very thing this hint exists to stop.
			{"auth failure", "hermes session/prompt failed: 401 invalid api key", "hermes", true},
			{"empty", "", "hermes", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := annotateHermesProviderUnconfigured(tc.errMsg, tc.provider, tc.overlayBuilt); got != tc.errMsg {
					t.Errorf("error text must be untouched, got: %s", got)
				}
			})
		}
	})
}

// TestAnnotationCannotChangeMachineDecisions is the guard behind keeping the
// hint free of interpolated input.
//
// The annotated text does not stop at the daemon: it travels to the server as
// the task's error, is persisted in agent_task_queue.error, and is re-scanned
// there for the life of the row — by service.ResumeUnsafeFailure on the write
// path and by the ILIKE/regex guards in GetLastTaskSession /
// GetLastChatTaskSession on every later resume lookup. Those guards decide
// whether a session pointer survives.
//
// So anything embedded in this text gets a vote on session recovery. Paths are
// user-controlled (HERMES_HOME via the agent's custom_env, the overlay root via
// MULTICA_WORKSPACES_ROOT), and a home under /srv/400-invalid_request_error/ is
// a legal directory that would trip the poisoned-request guard on a failure
// that has nothing to do with it. A constant hint has no such vote.
func TestAnnotationCannotChangeMachineDecisions(t *testing.T) {
	t.Parallel()

	// Reasons paired with the error texts a real run reports them for.
	cases := []struct {
		name   string
		errMsg string
		reason string
	}{
		{"the failure this hint targets", hermesProviderUnconfiguredError, "agent_error.missing_config"},
		{"poisoned request", `API error 400: {"type":"invalid_request_error","message":"bad image"}`, "api_invalid_request"},
		{"auth method unresolved", "hermes session/resume failed: Could not resolve authentication method", "agent_error.unknown"},
		{"empty assistant message", `messages.2: assistant message content must not be empty`, "agent_error.unknown"},
		{"context overflow", "API Error: prompt is too long: 250000 tokens > 200000 maximum", "agent_error.context_overflow"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Annotate unconditionally — the gate is tested above; here we
			// want the hint attached to every shape to prove it is inert.
			annotated := tc.errMsg + hermesProviderUnconfiguredHint

			if got, want := taskfailure.Classify(annotated), taskfailure.Classify(tc.errMsg); got != want {
				t.Errorf("hint moved the failure reason: %q -> %q", want, got)
			}
			if got, want := service.ResumeUnsafeFailure(tc.reason, annotated),
				service.ResumeUnsafeFailure(tc.reason, tc.errMsg); got != want {
				t.Errorf("hint changed resume safety: %v -> %v", want, got)
			}
		})
	}
}
