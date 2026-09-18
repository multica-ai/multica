package agent

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// runtimeStream owns a streaming runtime CLI's stdout and decides when the run
// is over, instead of letting an inherited file descriptor decide it.
//
// # The problem
//
// A backend built as `cmd.StdoutPipe()` → `for scanner.Scan()` → `cmd.Wait()`
// ends its run at stdout EOF, and EOF requires every write end of the pipe to
// be closed — including the ones descendants inherited. Two shapes break that
// assumption, both observed:
//
//   - The leader exits after writing its terminal event, but a descendant it
//     forked still holds stdout. The scanner never returns, cmd.Wait() is never
//     reached, and the finished result sits in the daemon's buffer while the
//     task holds its execution slot until the 2h idle watchdog force-stops it
//     and reports a successful run as failed.
//   - The leader writes its terminal event and then does not exit at all. This
//     is what openclaw was seen doing in production (complete result at T+24s,
//     process still alive and slot still held at T+8min, see
//     openclaw_stdout.go) and what cursor's adapter calls out as well.
//
// cmd.WaitDelay cannot cover either one: it only arms once Wait is called, and
// Wait is queued behind the scanner that is stuck.
//
// # The rule
//
// EOF stays the primary signal — it is the only one that proves no more output
// is coming. This type adds two bounded fallbacks for when EOF cannot arrive:
//
//   - The leader has been reaped. Anything still holding the pipe is a
//     descendant, not the agent, so the run is over. A short drain collects
//     whatever is still buffered and the read end is closed. Silence no longer
//     carries meaning here, which is exactly why this is safe: a pause means
//     "thinking" only while the process that would resume is alive.
//   - The adapter reported its protocol terminal boundary. The run is over by
//     the protocol's own account, so after a generous idle grace the process
//     tree is ended and the read end is closed.
//
// Both fallbacks require idleness, never a timer alone. Cutting a stream that
// is still producing would discard work the agent already did — strictly worse
// than the hang being fixed. That constraint is the same one openclaw's reader
// documents ("Idle alone is not enough … Parseable alone is not enough
// either"), and a run that reaches EOF on its own never consults either
// fallback and pays nothing.
type runtimeStream struct {
	cmd    *exec.Cmd
	logger *slog.Logger
	label  string

	// r is the parent's read end; w is the child's write end, dropped right
	// after start so the pipe can reach EOF at all. Owning both is what keeps
	// cmd.Wait() from being held hostage by a descendant: os/exec never sees a
	// pipe it has to drain, so Wait returns as soon as the leader is reaped.
	r *os.File
	w *os.File

	// stderr gets the same treatment, for the same reason. os/exec turns a
	// non-*os.File Stderr — every backend here passes a log/tail writer — into
	// a pipe plus a copy goroutine that cmd.Wait() joins, so a descendant
	// holding stderr blocks Wait exactly like one holding stdout. That path is
	// bounded by cmd.WaitDelay rather than unbounded, but it ends in
	// exec.ErrWaitDelay, which backends read as the CLI having failed: the
	// successful run whose stdout this type just rescued would still be
	// reported as "claude exited with error: WaitDelay expired before I/O
	// complete". Pumping stderr ourselves leaves os/exec no pipes at all.
	errR     *os.File
	errW     *os.File
	errW2    io.Writer // the backend's sink; nil when it captures no stderr
	pumpDone chan struct{}

	// stdin is owned here for a third reason: cmd.Wait() closes the pipes
	// os/exec created, and this type calls Wait in the background the moment
	// the leader is reaped. With os/exec's own StdinPipe that tears the prompt
	// write out from under a launcher whose real CLI has not read it yet — the
	// shim exits, the CLI keeps reading, and the write fails with "file already
	// closed". Owning the pipe leaves closing it to the adapter, which does so
	// when the prompt is fully written.
	stdinR *os.File

	terminalGrace time.Duration
	exitDrain     time.Duration

	lastByte atomic.Int64 // UnixNano of the most recent non-empty read
	terminal atomic.Bool  // adapter reported its protocol terminal boundary
	ended    atomic.Bool  // the stream ended on its own (EOF or read error)
	closing  atomic.Bool  // this type is closing the read end deliberately
	forced   atomic.Bool  // this type ended the process after a terminal event

	waitErr  error
	waitDone chan struct{}

	closeOnce sync.Once
	stopped   chan struct{} // closed once the read end has been closed
}

const (
	// runtimeStreamExitDrain bounds how long a stream keeps reading after the
	// leader has been reaped. The leader is gone, so this only collects bytes
	// already in the pipe; 2s matches collectDrainGrace and probeWaitDelay,
	// the equivalent bounds elsewhere in this package.
	runtimeStreamExitDrain = 2 * time.Second

	// runtimeStreamTerminalGrace is the default idle window an adapter gets
	// after reporting its terminal boundary, before the tree is ended.
	//
	// Set by the longest *legitimate* silence that can follow a terminal event,
	// not by how fast we would like to notice a hang: cutting early kills a run
	// that was still alive, cutting late costs a few seconds of convergence.
	// The two are not symmetric. Pi's automatic retry backs off 2s/4s/8s and is
	// operator-tunable, which is what sets this floor; adapters whose protocol
	// allows a longer pause pass their own value.
	runtimeStreamTerminalGrace = 10 * time.Second

	// runtimeStreamTerminateGrace is how long the process group gets between
	// SIGTERM and SIGKILL once the protocol has declared the run over.
	runtimeStreamTerminateGrace = 5 * time.Second

	// runtimeStreamStderrJoin bounds the wait for the stderr pump once the
	// leader is reaped. Deliberately not the configurable drain: stderr is
	// diagnostics, so a descendant sitting on it must never decide how long the
	// run's terminal result is withheld.
	runtimeStreamStderrJoin = 2 * time.Second

	runtimeStreamPoll = 100 * time.Millisecond
)

// runtimeStreamExitDrainSupported gates the leader-exit fallback.
//
// Off on Windows: there the leader is routinely a cmd.exe or npm shim and the
// real CLI is its child, so "leader reaped" does not imply the agent is done,
// and draining on it would cut a live run. The terminal-boundary fallback is
// platform-independent and still applies. A test may flip this.
var runtimeStreamExitDrainSupported = runtime.GOOS != "windows"

// newRuntimeStream installs the stream's own pipe as cmd.Stdout. The caller
// must not have started cmd, and must leave Stdout unset; start() starts it.
func newRuntimeStream(cmd *exec.Cmd, label string, cfg Config) (*runtimeStream, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	terminalGrace := cfg.streamTerminalGrace
	if terminalGrace <= 0 {
		terminalGrace = runtimeStreamTerminalGrace
	}
	exitDrain := cfg.streamExitDrain
	if exitDrain <= 0 {
		exitDrain = runtimeStreamExitDrain
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &runtimeStream{
		cmd:           cmd,
		logger:        logger,
		label:         label,
		r:             r,
		w:             w,
		terminalGrace: terminalGrace,
		exitDrain:     exitDrain,
		waitDone:      make(chan struct{}),
		pumpDone:      make(chan struct{}),
		stopped:       make(chan struct{}),
	}, nil
}

// stdinPipe replaces cmd.StdinPipe(). Call it before start(); the returned
// writer is the adapter's to write the prompt to and to close.
func (s *runtimeStream) stdinPipe() (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	s.stdinR = r
	s.cmd.Stdin = r
	return w, nil
}

// captureStderr routes the child's stderr to w through a pipe this type owns.
// Call it before start(); backends pass the same writer they used to assign to
// cmd.Stderr.
func (s *runtimeStream) captureStderr(w io.Writer) error {
	r, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	s.errR, s.errW, s.errW2 = r, pw, w
	s.cmd.Stderr = pw
	return nil
}

// start launches the process tree and the goroutines that reap it and bound
// the stream. It replaces the backend's own startOwnedProcessTree call.
func (s *runtimeStream) start() error {
	if err := startOwnedProcessTree(s.cmd, s.logger); err != nil {
		_ = s.r.Close()
		_ = s.w.Close()
		if s.errR != nil {
			_ = s.errR.Close()
			_ = s.errW.Close()
		}
		if s.stdinR != nil {
			_ = s.stdinR.Close()
		}
		return err
	}
	// Drop the parent's write ends immediately: while this process holds one,
	// no amount of reaping can produce EOF.
	_ = s.w.Close()
	s.w = nil
	if s.errW != nil {
		_ = s.errW.Close()
		s.errW = nil
	}
	// The child has its own descriptor now; the parent's copy of the read end
	// would keep the child's stdin from ever reaching EOF.
	if s.stdinR != nil {
		_ = s.stdinR.Close()
		s.stdinR = nil
	}
	s.lastByte.Store(time.Now().UnixNano())

	go func() {
		if s.errR == nil {
			close(s.pumpDone)
			return
		}
		_, _ = io.Copy(s.errW2, s.errR)
		close(s.pumpDone)
	}()
	go func() {
		s.waitErr = s.cmd.Wait()
		close(s.waitDone)
	}()
	go s.supervise()
	return nil
}

// Read implements io.Reader for the backend's scanner.
//
// A read that fails because this type closed the read end is reported as a
// clean io.EOF: the close is a decision made here, not a stream error, and
// backends classify scanner errors as run failures.
func (s *runtimeStream) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.lastByte.Store(time.Now().UnixNano())
	}
	if err != nil {
		s.ended.Store(true)
		if s.closing.Load() {
			return n, io.EOF
		}
	}
	return n, err
}

// terminalObserved reports that the adapter has parsed its protocol's terminal
// boundary: the run's outcome is decided and nothing further is expected.
func (s *runtimeStream) terminalObserved() { s.terminal.Store(true) }

// runContinues withdraws a previous terminalObserved because the protocol
// showed the run is not over after all — Pi emitting auto_retry_start after
// agent_end is the case this exists for.
func (s *runtimeStream) runContinues() { s.terminal.Store(false) }

// close releases the scanner. Backends call it from their cancellation path;
// the supervisor calls it when a fallback fires.
func (s *runtimeStream) close() {
	s.closeOnce.Do(func() {
		s.closing.Store(true)
		_ = s.r.Close()
		if s.errR != nil {
			_ = s.errR.Close()
		}
		close(s.stopped)
	})
}

// wait returns the leader's exit error.
//
// It returns nil when this type ended the process itself after the adapter
// reported a terminal boundary: the exit status then describes our own
// termination, and backends read a non-nil exit error as the run having
// failed. Everything else is reported unchanged.
func (s *runtimeStream) wait() error {
	<-s.waitDone
	// Join the stderr pump: backends read their stderr tail right after this
	// returns, and os/exec used to provide that ordering. A descendant still
	// holding stderr cannot stall it — the pump ends when close() drops the
	// read end, and the bound below covers the one case no fallback reaches
	// (Windows, where the leader-exit fallback is off).
	select {
	case <-s.pumpDone:
	case <-time.After(runtimeStreamStderrJoin):
		s.close()
		<-s.pumpDone
	}
	if s.forced.Load() {
		return nil
	}
	return s.waitErr
}

func (s *runtimeStream) pumpFinished() bool {
	select {
	case <-s.pumpDone:
		return true
	default:
		return false
	}
}

// idleFor measures stdout only: it is the protocol stream, and the decision
// being made is about the run, not about diagnostics. Both pipes are closed
// together once it fires.
func (s *runtimeStream) idleFor(d time.Duration) bool {
	return time.Since(time.Unix(0, s.lastByte.Load())) >= d
}

// supervise watches for the two shapes EOF cannot cover. It exits as soon as
// the stream ends on its own, which is the normal case and costs nothing.
func (s *runtimeStream) supervise() {
	ticker := time.NewTicker(runtimeStreamPoll)
	defer ticker.Stop()

	waitCh := s.waitDone
	exited := false
	for {
		select {
		case <-s.stopped:
			return
		case <-waitCh:
			exited = true
			waitCh = nil // a closed channel would spin this loop
		case <-ticker.C:
		}

		// Both pipes reaching EOF is not the end of the story: a CLI can close
		// its output and stay alive, and then the adapter is still parked in
		// wait(). Supervision ends when the process itself is gone.
		if exited && s.ended.Load() && s.pumpFinished() {
			return
		}

		switch {
		case exited && runtimeStreamExitDrainSupported:
			if s.idleFor(s.exitDrain) {
				s.logger.Warn(s.label+" stdout still open after the process exited; closing it",
					"drain", s.exitDrain.String())
				s.close()
				return
			}
		case s.terminal.Load():
			if s.idleFor(s.terminalGrace) {
				s.endRun()
				return
			}
		}
	}
}

// endRun ends a process tree that reported its terminal event and then neither
// produced more output nor exited. The run is over by its own protocol, so the
// tree is run-owned leftovers: signal it, then close the read end regardless of
// whether the signal landed, so a descendant that escaped the group cannot hold
// the task open either.
func (s *runtimeStream) endRun() {
	s.forced.Store(true)
	s.logger.Warn(s.label+" reported its terminal event but did not exit; ending the process tree",
		"grace", s.terminalGrace.String())
	if s.cmd.Process != nil {
		signalProcessGroup(s.cmd, syscall.SIGTERM)
		if !waitProcessGroupGone(s.cmd, runtimeStreamTerminateGrace) {
			signalProcessGroup(s.cmd, syscall.SIGKILL)
		}
	}
	s.close()
}
