package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

// A host whose only provider is a dsh without the Multica runtime profile is
// the exact host the automatic install exists for, and it is the one host on
// which the install could never once finish.
//
// The sequence, from a real Windows daemon.log, is 32 milliseconds long: the
// probe reports the missing profile and starts the install, registration finds
// nothing to register, Run returns that error, and the process exits — killing
// the install's process tree with it. Every restart repeats it. Allowing
// startup to continue while that install is in flight is what breaks the loop.
func TestStartupMayProceedWithoutRuntimes(t *testing.T) {
	registerFailure := fmt.Errorf("%w for any of the %d workspace(s)", errNoWorkspaceRuntimesRegistered, 1)

	t.Run("an in-flight install earns the exception", func(t *testing.T) {
		d := &Daemon{}
		d.dshInstallInFlight.Store(true)
		if !d.startupMayProceedWithoutRuntimes(registerFailure) {
			t.Fatal("startup refused to continue while the install that would fix it is running")
		}
	})

	t.Run("nothing else does", func(t *testing.T) {
		d := &Daemon{}
		// The same failure with no install running: a genuinely empty machine
		// still fails fast, which is the documented contract.
		if d.startupMayProceedWithoutRuntimes(registerFailure) {
			t.Error("a machine with nothing installed was allowed to start")
		}

		// An install in flight does not forgive unrelated failures. A rejected
		// token or an unreachable server must still stop startup, or the
		// daemon idles forever on a problem it cannot solve.
		d.dshInstallInFlight.Store(true)
		for name, err := range map[string]error{
			"auth":    errors.New("list workspaces: 401 unauthorized"),
			"network": errors.New("list workspaces: connection refused"),
			"none":    nil,
		} {
			if d.startupMayProceedWithoutRuntimes(err) {
				t.Errorf("%s failure was treated as the install bootstrap", name)
			}
		}
	})
}

// The other half of the bootstrap. A daemon that started with nothing
// registered tracks no workspace, and every converge path iterates the
// workspaces it tracks — so the kick that normally brings a finished install
// online would have nothing to look at, and the runtime would wait for the
// periodic consistency sync instead.
func TestRegisterAfterDshProfileInstall_PicksUpTheWorkspaceWhenNoneIsTracked(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	// Any provider will do: what is under test is that the workspace gets
	// picked up at all. Not dsh, deliberately — the dsh path would re-run the
	// profile probe against a fake binary and drop it again, which would make
	// this assert nothing.
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	if got := d.trackedWorkspaceCount(); got != 0 {
		t.Fatalf("precondition: tracked workspaces = %d, want 0", got)
	}

	d.registerAfterDshProfileInstall(context.Background())

	if got := d.trackedWorkspaceCount(); got == 0 {
		t.Fatal("the workspace was never picked up; the runtime would wait for the periodic sync")
	}
}

// With a workspace already tracked there is something for a converge round to
// look at, so the cheap kick is enough and the install must not spend a
// workspace sync on it.
func TestRegisterAfterDshProfileInstall_KicksWhenAWorkspaceIsTracked(t *testing.T) {
	d := &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         map[string]*workspaceState{"ws-1": {}},
		agentDiscoveryKick: make(chan struct{}, 1),
		// client stays nil: reaching the sync path would panic, which is the
		// assertion — this branch must not take it.
	}

	d.registerAfterDshProfileInstall(context.Background())

	select {
	case <-d.agentDiscoveryKick:
	case <-time.After(time.Second):
		t.Fatal("no discovery round was requested after the install finished")
	}
}

// A converge round may condemn dsh and nothing else.
//
// On main only refreshAgentVersions acts on demotable verdicts, on its own
// ten-minute cadence, and that is the schedule every provider's verdict was
// tuned against. dsh needs a round it can be forced into, because its
// precondition — the runtime profile — can change without any version changing;
// letting that round condemn Claude Code or Codex too would move them onto a
// different schedule as a side effect of a DSH fix.
func TestDshOnlyVerdicts(t *testing.T) {
	demotable := map[string]runtimeVerdict{
		"claude": {reason: "version too old"},
		"codex":  {reason: "agent CLI is not executable"},
		"dsh":    {reason: dshMissingProfileReason},
	}

	got := dshOnlyVerdicts(demotable)
	if len(got) != 1 {
		t.Fatalf("verdicts = %v, want only dsh", got)
	}
	if got["dsh"].reason != dshMissingProfileReason {
		t.Errorf("dsh verdict = %q, want it carried through unchanged", got["dsh"].reason)
	}

	// Nothing to do is nil, so the caller's length check reads the same either
	// way.
	if got := dshOnlyVerdicts(map[string]runtimeVerdict{"claude": {reason: "x"}}); got != nil {
		t.Errorf("verdicts = %v, want nil when dsh is not condemned", got)
	}
	if got := dshOnlyVerdicts(nil); got != nil {
		t.Errorf("verdicts = %v, want nil for an empty round", got)
	}
}
