package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// hermesProviderUnconfiguredACPError is the failure exactly as discoverACPModels
// renders it when hermes resolves no provider — verified against hermes 0.20.0
// by pointing it at a HERMES_HOME whose configured provider cannot be resolved.
// The error frame carries its message in a `data` OBJECT rather than a string,
// which is why the transport renders it as raw JSON and why ProviderUnconfigured
// matches on a substring instead of equality.
//
// The end-to-end path that produces this string is covered in
// hermes_model_discovery_unix_test.go; keeping a literal here lets the wording
// assertions below run on every platform, including the one that cannot execute
// a shell fixture.
var hermesProviderUnconfiguredACPError = fmt.Sprintf(
	"ACP model discovery session/new failed: session/new: Internal error (code=-32603, data={%q:%q})",
	"details",
	"No LLM provider configured. Run `hermes model` to select a provider, "+
		"or run `hermes setup` for first-time configuration.",
)

// TestHermesDiscoveryUnconfiguredHintPointsAtTheDaemonEnvironment pins the
// content of the hint, not just its presence. Discovery builds no per-task
// HERMES_HOME overlay and never reads an agent's custom_env, so the task path's
// advice (annotateHermesProviderUnconfigured in internal/daemon) is not merely
// unhelpful here — following it cannot put a single model in the picker. The
// hint has to send the reader at the daemon's own environment instead.
func TestHermesDiscoveryUnconfiguredHintPointsAtTheDaemonEnvironment(t *testing.T) {
	t.Parallel()

	err := annotateHermesDiscoveryUnconfigured(errString(hermesProviderUnconfiguredACPError))
	if err == nil {
		t.Fatal("expected the unconfigured-provider failure to be annotated")
	}
	msg := err.Error()

	// Hermes' own message survives: it is what the runtime actually said.
	if !strings.Contains(msg, "No LLM provider configured") {
		t.Errorf("the runtime's own message must be preserved, got: %s", msg)
	}
	// And the hint carries what hermes could not know — that it was answering
	// the daemon, not the user's shell — plus a check the user can run.
	for _, want := range []string{"daemon", "login shell", "env -i"} {
		if !strings.Contains(msg, want) {
			t.Errorf("hint must mention %q, got: %s", want, msg)
		}
	}
	// The task path's remedy must not leak in: it would send the reader to
	// edit config that discovery provably never reads.
	if !strings.Contains(hermesDiscoveryUnconfiguredHint, "will not populate this picker") {
		t.Error("hint must say outright that HERMES_HOME / custom_env cannot fix the picker")
	}
}

// TestAnnotateHermesDiscoveryUnconfiguredLeavesOtherFailuresAlone proves the
// predicate, not the transport, is the gate — including for stages a fake ACP
// binary cannot reach, since a missing binary never speaks the protocol at all.
func TestAnnotateHermesDiscoveryUnconfiguredLeavesOtherFailuresAlone(t *testing.T) {
	t.Parallel()

	if got := annotateHermesDiscoveryUnconfigured(nil); got != nil {
		t.Errorf("nil error must stay nil, got: %v", got)
	}
	for _, msg := range []string{
		`ACP model discovery executable lookup failed: exec: "hermes": executable file not found in $PATH`,
		"ACP model discovery initialize failed: unexpected EOF",
		"ACP model discovery session/new failed: session/new: Internal error (code=-32603, data=401 invalid api key)",
		"ACP model discovery completion failed: context deadline exceeded",
	} {
		in := errString(msg)
		if got := annotateHermesDiscoveryUnconfigured(in); got.Error() != msg {
			t.Errorf("error text must be untouched\n got: %s\nwant: %s", got.Error(), msg)
		}
	}
}

// TestHermesDiscoveryHintDoesNotReclassifyTheFailure keeps the hint inert. The
// same predicate that gates it (taskfailure.ProviderUnconfigured) also feeds
// Classify, and this text is appended to an error string other code reads —
// so it must not change what that error is understood to be.
func TestHermesDiscoveryHintDoesNotReclassifyTheFailure(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{
		hermesProviderUnconfiguredACPError,
		"ACP model discovery initialize failed: unexpected EOF",
		"API Error: prompt is too long: 250000 tokens > 200000 maximum",
	} {
		annotated := msg + hermesDiscoveryUnconfiguredHint
		if got, want := taskfailure.Classify(annotated), taskfailure.Classify(msg); got != want {
			t.Errorf("hint moved the failure reason for %q: %q -> %q", msg, want, got)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
