package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// codexRetiredCompactionError is the failure exactly as GH #8000 reported it.
const codexRetiredCompactionError = `Error running remote compact task: unexpected status 404 Not Found: ` +
	`{"detail":"Not Found"}, url: https://chatgpt.com/backend-api/codex/responses/compact, ` +
	`cf-ray: a35507973eee3d57-SJC, request id: 9eed88ee-822d-446e-9b73-df49d56fe7e0`

func TestAnnotateCodexRetiredCompaction(t *testing.T) {
	t.Parallel()

	t.Run("explains what codex could not", func(t *testing.T) {
		t.Parallel()
		got := annotateCodexRetiredCompaction(codexRetiredCompactionError, "codex")

		// The original text has to survive: it is what the runtime actually
		// said, and the hint is additive context, not a replacement.
		if !strings.Contains(got, "Error running remote compact task") {
			t.Errorf("the runtime's own message must be preserved, got: %s", got)
		}
		// The hint has to name what Codex's own error never mentions: the
		// setting, and every place it can be turned off. Being a constant, it
		// cannot know which source applied — so naming only the config file
		// would send anyone who set it in launch arguments to edit a line that
		// is not there, the same dead end this hint exists to prevent.
		for _, want := range []string{
			"remote_compaction_v2",
			"config.toml",
			"--disable remote_compaction_v2",
			"-c features.remote_compaction_v2=false",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("hint must mention %q, got: %s", want, got)
			}
		}
		// Having sent people to look at Multica-side launch arguments, the
		// hint must not then tell them nothing on the Multica side needs
		// changing. The only target it can safely exclude is the generated
		// per-task copy, which is rebuilt from the shared config every run.
		if strings.Contains(strings.ToLower(got), "nothing needs changing") {
			t.Errorf("hint contradicts its own instruction to check launch arguments, got: %s", got)
		}
		if !strings.Contains(got, "per-task codex config") {
			t.Errorf("hint must name the copy that is regenerated, got: %s", got)
		}
	})

	t.Run("leaves unrelated failures alone", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name     string
			errMsg   string
			provider string
		}{
			// Only the codex backend can emit this text; a lookalike from
			// another runtime must not be sent to edit ~/.codex.
			{"other provider", codexRetiredCompactionError, "claude"},
			// Compaction failures on the v2 route. They carry the same
			// "Error running remote compact task" prefix and can carry the
			// same status, so firing here would tell a user whose setting is
			// already correct to go turn it off.
			{
				"v2 route returning the same status",
				`Error running remote compact task: unexpected status 404 Not Found: {"detail":"Not Found"}, ` +
					`url: https://chatgpt.com/backend-api/codex/responses`,
				"codex",
			},
			{
				"v2 route failing some other way",
				`Error running remote compact task: remote compaction v2 stream closed before response.completed`,
				"codex",
			},
			// No url tail, so nothing identifies the route — see
			// CodexRetiredCompactionError for why that stays a false negative.
			{
				"no url tail",
				`Error running remote compact task: unexpected status 404 Not Found: {"detail":"Not Found"}`,
				"codex",
			},
			{"unrelated codex failure", "codex turn/start failed: broken pipe", "codex"},
			{"empty", "", "codex"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := annotateCodexRetiredCompaction(tc.errMsg, tc.provider); got != tc.errMsg {
					t.Errorf("error text must be untouched, got: %s", got)
				}
			})
		}
	})
}

// TestCodexCompactionAnnotationCannotChangeMachineDecisions is the same guard
// TestAnnotationCannotChangeMachineDecisions applies to the Hermes hint, and it
// matters more here: the error this hint rides on already contains "Not Found",
// so a single word — "model" — anywhere in the prose would flip the failure out
// of agent_error.unknown into model_not_found_or_unavailable, and an "HTTP 404"
// spelled out in the advice would do the same.
//
// The annotated text does not stop at the daemon: it is persisted in
// agent_task_queue.error and re-scanned there for the life of the row, by
// service.ResumeUnsafeFailure on the write path and by the ILIKE/regex guards
// in GetLastTaskSession / GetLastChatTaskSession on every later resume lookup.
// Those guards decide whether a session pointer survives — and this failure in
// particular must keep its pointer, since the thread resumes normally once the
// user deletes the config line.
func TestCodexCompactionAnnotationCannotChangeMachineDecisions(t *testing.T) {
	t.Parallel()

	// Reasons paired with the error texts a real run reports them for.
	cases := []struct {
		name   string
		errMsg string
		reason string
	}{
		{"the failure this hint targets", codexRetiredCompactionError, "agent_error.unknown"},
		{"poisoned request", `API error 400: {"type":"invalid_request_error","message":"bad image"}`, "api_invalid_request"},
		{"resume overflow", "codex thread/resume failed: token too long", "codex_resume_oversized"},
		{"auth method unresolved", "codex session/resume failed: Could not resolve authentication method", "agent_error.unknown"},
		{"empty assistant message", `messages.2: assistant message content must not be empty`, "agent_error.unknown"},
		{"context overflow", "API Error: prompt is too long: 250000 tokens > 200000 maximum", "agent_error.context_overflow"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Annotate unconditionally — the gate is tested above; here we
			// want the hint attached to every shape to prove it is inert.
			annotated := tc.errMsg + codexRetiredCompactionHint

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

// TestCodexRetiredCompactionKeepsSessionResumable pins the deliberate decision
// behind this being a text-only change: the thread must stay resumable.
//
// The failure looks like the poisoned ones around it — it repeats on every
// following turn and does not recover on its own — but the cause is one line in
// a config file, not the transcript. Once that line is gone the same
// conversation compacts and continues, so retiring the session would destroy
// the context the fix restores. If someone later adds this text to a resume
// guard, this test is where they find out that was the wrong lesson.
func TestCodexRetiredCompactionKeepsSessionResumable(t *testing.T) {
	t.Parallel()

	reason := taskfailure.Classify(codexRetiredCompactionError).String()
	annotated := annotateCodexRetiredCompaction(codexRetiredCompactionError, "codex")
	if service.ResumeUnsafeFailure(reason, annotated) {
		t.Errorf("compaction 404 must stay resume-safe, reason %q", reason)
	}
	if _, poisoned := classifyPoisonedError(annotated); poisoned {
		t.Error("compaction 404 must not be classified as a poisoned session")
	}
	if _, unsafe := classifyResumeUnsafeTransport("codex", annotated); unsafe {
		t.Error("compaction 404 must not be classified as a resume-unsafe transport failure")
	}
}
