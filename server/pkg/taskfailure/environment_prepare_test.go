package taskfailure

import "testing"

// TestNormalizeDaemonReasonUpgradesEnvironmentPrepare covers the mixed-version
// window for #7913. Installed daemons update on their own cadence, so until a
// host carries the structural tag it keeps classifying its own setup errors
// with a classifier built for agent output — and reports a non-empty
// agent_error.* value, which FailTask's "classify only when empty" branch
// deliberately leaves alone. Recognising the wrapper the daemon has always
// written moves those rows out of the agent namespace the moment the server
// deploys.
//
// The witnesses are the real messages from the report and from main: the
// Windows lock that opened the issue, a full volume, a denied permission, a
// reuse that could not stat the prior workdir, and a local runtime config the
// daemon refused to boot past.
func TestNormalizeDaemonReasonUpgradesEnvironmentPrepare(t *testing.T) {
	t.Parallel()

	rawErrors := map[string]string{
		"windows lock": "prepare execution environment: execenv: remove existing env: unlinkat " +
			`C:\Users\u\multica_workspaces_p\ws\prefix\workdir: ` +
			"The process cannot access the file because it is being used by another process",
		"disk full":         "prepare execution environment: execenv: mkdir /home/u/multica_workspaces/ws: no space left on device",
		"permission denied": "prepare execution environment: execenv: open ownership manifest: permission denied",
		"reuse io error":    "reuse execution environment: stat workdir: input/output error",
		// Preparation is not only filesystem work: it also writes and validates
		// the per-provider local config. "exit status 1" is exactly the text
		// Classify reads as agent_error.process_failure, and a config the
		// OpenClaw CLI cannot parse is not a process the agent ran.
		"malformed openclaw config": "prepare execution environment: execenv: prepare openclaw config: " +
			"read openclaw agents.list: exit status 1",
	}

	// Every agent-side label an older daemon can produce here is wrong for the
	// same reason — the run died before an agent process existed — so the whole
	// namespace is upgraded, not an enumerated subset. provider_server_error is
	// what the original report saw; unknown is where main lands today.
	legacyReasons := []string{
		string(ReasonAgentUnknown),
		string(ReasonAgentProviderServerError),
		string(ReasonAgentProviderNetwork),
		string(ReasonAgentProcessFailure),
		"agent_error",
	}

	for name, rawError := range rawErrors {
		for _, legacy := range legacyReasons {
			if got := NormalizeDaemonReason(legacy, rawError); got != ReasonEnvironmentPrepareFailed {
				t.Errorf("%s: NormalizeDaemonReason(%q) = %q, want %q", name, legacy, got, ReasonEnvironmentPrepareFailed)
			}
		}
		// A current daemon already reports the precise reason; normalization
		// must leave it alone.
		if got := NormalizeDaemonReason(string(ReasonEnvironmentPrepareFailed), rawError); got != ReasonEnvironmentPrepareFailed {
			t.Errorf("%s: NormalizeDaemonReason(environment_prepare_failed) = %q, want it preserved", name, got)
		}
	}
}

// TestNormalizeDaemonReasonEnvironmentPrepareStaysNarrow pins the three ways
// this rule must NOT fire. Each protects something the rule would otherwise
// take away: a finer label for the same phase, a platform-side reason that was
// never a misattribution, and an agent failure whose text merely quotes the
// wrapper.
func TestNormalizeDaemonReasonEnvironmentPrepareStaysNarrow(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reason   string
		rawError string
		want     Reason
	}{
		// The OpenClaw config-discovery stall is a preparation failure too, and
		// its own rule runs first because it names the specific cause. Losing
		// that to the generic bucket would drop the copy that tells the user to
		// raise MULTICA_OPENCLAW_CLI_TIMEOUT.
		"openclaw cli timeout keeps its own reason": {
			reason: string(ReasonAgentProviderNetwork),
			rawError: "prepare execution environment: execenv: prepare openclaw config: " +
				"locate openclaw active config: openclaw config file: context deadline exceeded " +
				"(process: signal: killed)",
			want: ReasonRuntimeCLITimeout,
		},
		// A platform-side reason was never a misattribution: whatever wrote it
		// knew more than the wrapper does. The prepare budget running out is
		// classified structurally by the daemon and stays a timeout.
		"platform-side reason is untouched": {
			reason:   string(ReasonTimeout),
			rawError: "prepare execution environment: execenv: mkdir: no space left on device",
			want:     ReasonTimeout,
		},
		// The witness has to OPEN the error. An agent that printed the phrase
		// mid-run — a log tail, a quoted earlier failure — did reach an agent,
		// and its own classification is the true one.
		"prefix only, not a mention anywhere": {
			reason:   string(ReasonAgentProcessFailure),
			rawError: "agent crashed while reading logs: prepare execution environment: execenv: mkdir failed",
			want:     ReasonAgentProcessFailure,
		},
	}

	for name, tc := range cases {
		if got := NormalizeDaemonReason(tc.reason, tc.rawError); got != tc.want {
			t.Errorf("%s: NormalizeDaemonReason = %q, want %q", name, got, tc.want)
		}
	}
}

// TestClassifyStillReadsPrepareErrorsAsAgentErrors documents what the text
// classifier does with these messages on its own, and why the fix could not be
// a new Classify rule. Classify is handed agent and provider output; a host
// that could not set the run up is neither, and no bucket it owns is correct. The
// classification has to happen upstream — structurally in the daemon, or from
// the wrapper prefix at the normalization boundary.
func TestClassifyStillReadsPrepareErrorsAsAgentErrors(t *testing.T) {
	t.Parallel()

	const rawError = "prepare execution environment: execenv: mkdir /home/u/multica_workspaces/ws: no space left on device"
	if got := Classify(rawError); !got.IsAgentError() {
		t.Fatalf("Classify(prepare error) = %q; the premise of this fix is that Classify cannot label these", got)
	}
	if got := NormalizeDaemonReason(Classify(rawError).String(), rawError); got != ReasonEnvironmentPrepareFailed {
		t.Errorf("NormalizeDaemonReason(Classify(...)) = %q, want %q — the empty-reason path must land on the same reason", got, ReasonEnvironmentPrepareFailed)
	}
}
