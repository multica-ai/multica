package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestTaskRunFailureReasonLabelsEnvironmentSetup is the daemon half of #7913.
// The first four messages are the real ones: the Windows lock from the
// original report, and the three shapes main produces today. The last two are
// the other half of what preparation does — the per-provider local config it
// writes and validates. Every one of them is the machine running the agent
// talking, and every one of them used to be handed to taskfailure.Classify —
// a classifier that only knows how to read agent and provider output — so
// they landed in agent_error.* and made a host problem look like a
// model-provider fault on the board.
func TestTaskRunFailureReasonLabelsEnvironmentSetup(t *testing.T) {
	cases := map[string]error{
		"windows lock": asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
			errors.New(`execenv: remove existing env: unlinkat C:\Users\u\multica_workspaces_p\ws\prefix\workdir: `+
				"The process cannot access the file because it is being used by another process"))),
		"disk full": asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
			errors.New("execenv: mkdir /home/u/multica_workspaces/ws: no space left on device"))),
		"permission denied": asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
			errors.New("execenv: open ownership manifest: permission denied"))),
		"reuse io error": asEnvironmentSetupFailure(fmt.Errorf("reuse execution environment: %w",
			errors.New("stat workdir: input/output error"))),
		// Not every preparation failure is the filesystem. Prepare also writes
		// and validates the per-provider local config — Codex home, the Hermes
		// overlay, Cursor MCP, and an OpenClaw config it deliberately fails
		// closed on when the CLI cannot read it (execenv.Prepare). Those are
		// the same verdict: the machine running the agent could not set the run
		// up. Losing them to Classify would put a broken local config back in
		// the provider's namespace, and the user-facing copy must stay wide
		// enough to send the reader to the raw error rather than only to df.
		"malformed openclaw config": asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
			errors.New("execenv: prepare openclaw config: read openclaw agents.list: exit status 1"))),
		"codex home": asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
			errors.New("execenv: prepare codex-home: seed config.toml: invalid TOML at line 3"))),
	}

	want := taskfailure.ReasonEnvironmentPrepareFailed.String()
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if got := taskRunFailureReason(err); got != want {
				t.Errorf("taskRunFailureReason = %q, want %q", got, want)
			}
			if taskfailure.Reason(taskRunFailureReason(err)).IsAgentError() {
				t.Error("an environment that never launched an agent must not be filed under agent_error.*")
			}
		})
	}
}

// TestEnvironmentSetupFailurePreservesMessage pins the tag as invisible on the
// wire. The wrapper exists to be read by taskRunFailureReason, not by a person:
// the text is what gets persisted into agent_task_queue.error and shown under
// the chat bubble, and it is also the witness NormalizeDaemonReason keys on for
// daemons too old to send the reason. Prefixing it would break both.
func TestEnvironmentSetupFailurePreservesMessage(t *testing.T) {
	inner := fmt.Errorf("prepare execution environment: %w", errors.New("execenv: mkdir: no space left on device"))
	tagged := asEnvironmentSetupFailure(inner)

	if tagged.Error() != inner.Error() {
		t.Errorf("tagged message = %q, want %q unchanged", tagged.Error(), inner.Error())
	}
	if !errors.Is(tagged, inner) {
		t.Error("the tag must unwrap to the error it carries")
	}
	if got := taskfailure.NormalizeDaemonReason(taskfailure.Classify(tagged.Error()).String(), tagged.Error()); got != taskfailure.ReasonEnvironmentPrepareFailed {
		t.Errorf("server-side normalization of the same text = %q, want %q", got, taskfailure.ReasonEnvironmentPrepareFailed)
	}
}

// TestTaskRunFailureReasonPrefersSpecificPreparationSentinels guards the branch
// order. Preparation is one phase with several known causes, and three of them
// already have their own reason. The generic bucket is what is left over, so it
// has to be checked last — a run that died on the OpenClaw CLI deadline still
// needs the copy naming MULTICA_OPENCLAW_CLI_TIMEOUT, and one that ran out of
// skill downloads still needs to be retryable.
func TestTaskRunFailureReasonPrefersSpecificPreparationSentinels(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"openclaw cli timeout": {
			err: asEnvironmentSetupFailure(fmt.Errorf(
				"prepare execution environment: execenv: prepare openclaw config: locate openclaw active config: %w",
				fmt.Errorf("openclaw config file: context deadline exceeded (process: signal: killed): %w", execenv.ErrOpenclawCLITimeout))),
			want: taskfailure.ReasonRuntimeCLITimeout.String(),
		},
		"skill bundle unavailable": {
			err: asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w",
				fmt.Errorf("%w: download skill: connection refused", errSkillBundleUnavailable))),
			want: taskfailure.ReasonSkillBundleUnavailable.String(),
		},
		"prepare budget exhausted": {
			err:  fmt.Errorf("%w after 5m0s", errTaskPrepareTimeout),
			want: taskfailure.ReasonTimeout.String(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := taskRunFailureReason(tc.err); got != tc.want {
				t.Errorf("taskRunFailureReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTaskRunFailureReasonLeavesAgentErrorsAlone pins the other side of the
// boundary: only errors the daemon itself tagged move out of the agent
// namespace. An untagged failure is one the agent process produced, and its
// text remains the best evidence available.
func TestTaskRunFailureReasonLeavesAgentErrorsAlone(t *testing.T) {
	err := errors.New("API Error: 429 rate limit exceeded")

	want := taskfailure.ReasonAgentProviderCapacityOrRateLimit.String()
	if got := taskRunFailureReason(err); got != want {
		t.Errorf("taskRunFailureReason = %q, want %q", got, want)
	}
}
