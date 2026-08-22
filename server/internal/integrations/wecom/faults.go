//go:build wecomfaults

package wecom

// faults.go — a switch that makes a failure happen on purpose, so a recovery
// path can be walked against the real platform instead of only in a test.
//
// Why it exists. The paths worth the most here are the ones that only run when
// something else has already broken: the socket dies at the moment a bubble is
// being sealed, and what the user is left looking at depends on bookkeeping
// nobody can see. None of that can be produced by using the product — you
// cannot make a connection drop by tapping harder — so it was only ever
// reachable from an in-process test, and an in-process test proves the code
// branches, not that the branch does anything useful to a person holding a
// phone.
//
// Not compiled by default, and that is deliberate rather than tidy. A runtime
// switch would put a test on every frame this adapter writes and would leave a
// production binary one environment variable away from dropping frames on
// purpose. Arming this takes a rebuild:
//
//	cd server && go build -tags wecomfaults -o bin/server ./cmd/server
//	MULTICA_WECOM_FAULTS=dead_socket_on_seal ./bin/server
//
// Every fault is one-shot: it fires once and disarms, so a session cannot be
// left quietly broken by an arm nobody remembers. Arming anything logs a
// warning naming what was armed, and each firing logs another — whatever the
// bot does next is not its own behaviour, and somebody reading the log later
// has to know that.

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// FaultDeadSocketOnSeal makes the connection go dead the instant a closing
// stream frame is written, and stay dead for deadSocketWindow.
//
// The window rather than a count of frames, because what is under test is a
// DELIVERY and a delivery is two sends: the closing frame, and the plain
// message it falls back to when that frame does not go. A one-shot on the first
// of them would leave the second to succeed, and the user would get their words
// beside the spinner — which is the outcome, not the failure.
//
// It is armed by the seal rather than by the clock so an operator can decide
// which round breaks by choosing when to make one end, and it fires on
// finish=true alone so the opening frame that paints the bubble is never the
// one that dies.
const FaultDeadSocketOnSeal = "dead_socket_on_seal"

// deadSocketWindow is how long writes keep failing once the fault has fired.
// Long enough to cover the fallback message behind the closing frame, which
// follows it within milliseconds, and comfortably shorter than the fifteen
// seconds before the first booked retry — that attempt has to find a working
// socket, or the test says nothing about whether the round came back.
const deadSocketWindow = 5 * time.Second

var knownFaults = map[string]bool{
	FaultDeadSocketOnSeal: true,
}

var (
	faultMu    sync.Mutex
	armedFault = map[string]bool{}
	// deadUntil is when the manufactured outage ends. Zero when no fault has
	// fired.
	deadUntil time.Time
)

// errInjectedDeadSocket is what the write comes back with. It is wrapped in
// errFrameNotOnTheWire by writeLocked exactly as a real transport failure is,
// which is the point: everything downstream has to see the failure the platform
// would have produced, not a shape of its own.
var errInjectedDeadSocket = errors.New("wecom: INJECTED FAULT: the socket is not taking frames")

// SetFaults arms the faults named in a comma-separated config string and
// returns the ones it recognised, so the caller can log exactly what is live.
// An unknown name is ignored rather than fatal: this is a debugging aid, and
// refusing to boot over a typo in it would be a worse failure than the typo.
func SetFaults(raw string) []string {
	faultMu.Lock()
	defer faultMu.Unlock()
	armedFault = map[string]bool{}
	deadUntil = time.Time{}
	var armed []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || !knownFaults[name] {
			continue
		}
		armedFault[name] = true
		armed = append(armed, name)
	}
	return armed
}

// faultDeadSocketOnSeal opens the outage window when a closing frame is about
// to be written, and disarms. Called from inside the writer's own critical
// section, so the frame that armed it is the first one the outage refuses.
func faultDeadSocketOnSeal(log *slog.Logger, finish bool) {
	if !finish {
		return
	}
	faultMu.Lock()
	if !armedFault[FaultDeadSocketOnSeal] {
		faultMu.Unlock()
		return
	}
	delete(armedFault, FaultDeadSocketOnSeal)
	deadUntil = time.Now().Add(deadSocketWindow)
	faultMu.Unlock()

	if log == nil {
		log = slog.Default()
	}
	log.Warn("wecom: INJECTED FAULT fired — the write failures below are manufactured, not real",
		"fault", FaultDeadSocketOnSeal, "at", "wsSender.writeStreamFrame", "for", deadSocketWindow)
}

// faultDeadSocketRefusesWrite reports the outage to a write that is about to
// happen, as the error the socket would have handed back.
func faultDeadSocketRefusesWrite() error {
	faultMu.Lock()
	defer faultMu.Unlock()
	if deadUntil.IsZero() || !time.Now().Before(deadUntil) {
		return nil
	}
	return fmt.Errorf("%w (%s)", errInjectedDeadSocket, FaultDeadSocketOnSeal)
}
