// Package terminal is the cerebro interactive-terminal broker. A session
// wraps a child process whose stdin/stdout are streamed to any number of
// subscribers via the Attach API. The HTTP+WS layer in handler.go converts
// that into a browser-facing endpoint.
//
// Today the child is spawned with plain pipes (os/exec). A follow-up will
// swap to creack/pty so raw-mode programs (vim, top, claude TUI) work
// correctly — the Session interface deliberately hides whether the
// underlying I/O is pipe or pty so the swap is local to startSession.
package terminal

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Defaults
const (
	defaultCommand   = "/bin/sh"
	stdoutChunkBytes = 4096
	subscriberBuffer = 64
	maxSessionAge    = 8 * time.Hour
	// historyBytesMax bounds the replay buffer so a long-running session
	// doesn't grow unbounded. New attachers see the last N bytes of stdout.
	historyBytesMax = 64 * 1024
)

// Session is a single PTY-style stream backed by a child process.
type Session struct {
	ID          string
	RuntimeID   string
	WorkspaceID string
	OwnerUserID string
	TaskID      string
	Command     []string
	CreatedAt   time.Time

	// external is true for sessions adopted from an off-server source (today
	// the daemon, via DaemonBridge). Adopted sessions have no in-process
	// subprocess — PushOutput is what feeds stdout, and stdin is exposed to
	// the bridge via StdinChan rather than wired to a pipe.
	external bool

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stdinChan chan []byte // adopted sessions only; nil for internal-process sessions

	mu          sync.Mutex
	subscribers map[*Subscriber]struct{}
	history     []byte // ring of recent stdout bytes; replayed on Attach
	closed      atomic.Bool
	exitErr     error
	exitCode    int
	done        chan struct{}
	// reattachGen increments every time the owning daemon re-announces this
	// session (a duplicate term_attach after a WS reconnect). The disconnect
	// reaper captures it before its grace sleep and skips the close if it
	// changed — so a brief daemon WS flap does not kill a live session.
	reattachGen atomic.Uint64
}

// Subscriber receives stdout chunks. Slow subscribers are evicted (their
// channel is closed) rather than blocking the producer goroutine.
type Subscriber struct {
	C    chan []byte
	once sync.Once
}

func newSubscriber() *Subscriber {
	return &Subscriber{C: make(chan []byte, subscriberBuffer)}
}

func (s *Subscriber) close() {
	s.once.Do(func() { close(s.C) })
}

// Broker is an in-memory session registry. Safe for concurrent use.
type Broker struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// runtimeIndex maps a runtime ID to the most-recently-attached open
	// session for that runtime. Used by GetByRuntime so the active-session
	// HTTP endpoint can answer "is there a terminal I can attach to for
	// this runtime?" without scanning every session.
	runtimeIndex map[string]*Session
}

func NewBroker() *Broker {
	return &Broker{
		sessions:     make(map[string]*Session),
		runtimeIndex: make(map[string]*Session),
	}
}

// CreateOptions controls how a new session is spawned.
type CreateOptions struct {
	RuntimeID   string
	WorkspaceID string
	OwnerUserID string
	// Command is the argv. Empty defaults to defaultCommand.
	Command []string
	// Env is appended to the child env. Optional.
	Env []string
	// Dir is the child's working directory. Empty defaults to the caller's.
	Dir string
}

// Create spawns a new session. The caller is the only one with the ID until
// it shares it via the API. ctx scopes process lifetime — cancelling kills
// the child.
func (b *Broker) Create(ctx context.Context, opts CreateOptions) (*Session, error) {
	argv := opts.Command
	if len(argv) == 0 {
		argv = []string{defaultCommand}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Environ(), opts.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	// Stderr → stdout so the browser sees both.
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	s := &Session{
		ID:          uuid.NewString(),
		RuntimeID:   opts.RuntimeID,
		WorkspaceID: opts.WorkspaceID,
		OwnerUserID: opts.OwnerUserID,
		Command:     argv,
		CreatedAt:   time.Now().UTC(),
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		subscribers: make(map[*Subscriber]struct{}),
		done:        make(chan struct{}),
	}

	b.mu.Lock()
	b.sessions[s.ID] = s
	if s.RuntimeID != "" {
		b.runtimeIndex[s.RuntimeID] = s
	}
	b.mu.Unlock()

	go s.readLoop()
	go s.reaper(b)

	return s, nil
}

// AdoptOptions configures an adopted (external) session. Used when an
// off-server source — today the daemon, via DaemonBridge — already owns the
// child process and wants the broker to ferry its output to attached
// browsers. The broker spawns no subprocess of its own.
type AdoptOptions struct {
	RuntimeID   string
	WorkspaceID string
	OwnerUserID string
	TaskID      string
	// Command is the argv that the external owner ran. Stored so the UI can
	// label the session; empty is allowed.
	Command string
}

// Adopt registers an externally-owned session. The caller (DaemonBridge) is
// responsible for calling PushOutput as bytes arrive and Exit when the
// owning process ends. Stdin from browsers is exposed via StdinChan so the
// bridge can forward it to the daemon over WS.
func (b *Broker) Adopt(opts AdoptOptions) (*Session, error) {
	argv := []string{}
	if strings.TrimSpace(opts.Command) != "" {
		argv = []string{opts.Command}
	}
	s := &Session{
		ID:          uuid.NewString(),
		RuntimeID:   opts.RuntimeID,
		WorkspaceID: opts.WorkspaceID,
		OwnerUserID: opts.OwnerUserID,
		TaskID:      opts.TaskID,
		Command:     argv,
		CreatedAt:   time.Now().UTC(),
		external:    true,
		stdinChan:   make(chan []byte, subscriberBuffer),
		subscribers: make(map[*Subscriber]struct{}),
		done:        make(chan struct{}),
	}

	b.mu.Lock()
	b.sessions[s.ID] = s
	if s.RuntimeID != "" {
		b.runtimeIndex[s.RuntimeID] = s
	}
	b.mu.Unlock()

	go s.reaper(b)

	return s, nil
}

// Get returns the session by ID, or nil.
func (b *Broker) Get(id string) *Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

// GetByRuntime returns the most-recent open session for the runtime, or
// nil. A session counts as "open" as long as its done channel hasn't been
// closed; the active-session HTTP endpoint uses this to answer "is there a
// terminal I can attach to right now?". The lookup is O(1) via runtimeIndex.
func (b *Broker) GetByRuntime(runtimeID string) *Session {
	if runtimeID == "" {
		return nil
	}
	b.mu.Lock()
	s := b.runtimeIndex[runtimeID]
	b.mu.Unlock()
	if s == nil {
		return nil
	}
	if s.closed.Load() {
		// Stale entry — a closed session left it behind. Best-effort cleanup
		// so future callers don't keep paying the closed-session detour, but
		// terminate/reaper own the authoritative removal.
		b.mu.Lock()
		if cur := b.runtimeIndex[runtimeID]; cur == s {
			delete(b.runtimeIndex, runtimeID)
		}
		b.mu.Unlock()
		return nil
	}
	return s
}

// List returns all sessions matching the (workspace, owner) scope. Empty
// args mean "no filter on that dimension".
func (b *Broker) List(workspaceID, ownerUserID string) []*Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*Session, 0, len(b.sessions))
	for _, s := range b.sessions {
		if workspaceID != "" && s.WorkspaceID != workspaceID {
			continue
		}
		if ownerUserID != "" && s.OwnerUserID != ownerUserID {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Close terminates a session and removes it from the broker.
func (b *Broker) Close(id string) error {
	b.mu.Lock()
	s := b.sessions[id]
	delete(b.sessions, id)
	if s != nil && s.RuntimeID != "" {
		if cur := b.runtimeIndex[s.RuntimeID]; cur == s {
			delete(b.runtimeIndex, s.RuntimeID)
		}
	}
	b.mu.Unlock()
	if s == nil {
		return errors.New("session not found")
	}
	s.terminate(nil)
	return nil
}

// Attach registers a Subscriber for stdout chunks. It returns the
// subscriber, a function to write stdin, and an unsubscribe func that the
// caller MUST invoke when done. The subscriber receives the recent stdout
// history (last historyBytesMax bytes) as the first chunk so a fresh
// attach renders the current screen state correctly.
func (s *Session) Attach() (sub *Subscriber, write func([]byte) error, detach func()) {
	sub = newSubscriber()
	s.mu.Lock()
	s.subscribers[sub] = struct{}{}
	var replay []byte
	if len(s.history) > 0 {
		replay = make([]byte, len(s.history))
		copy(replay, s.history)
	}
	s.mu.Unlock()
	if replay != nil {
		// Non-blocking send — buffer is size 64 and this is the first
		// write, so it always fits unless the caller pre-loaded it.
		select {
		case sub.C <- replay:
		default:
		}
	}

	write = func(p []byte) error {
		if s.closed.Load() {
			return errors.New("session closed")
		}
		if s.external {
			// Adopted session: forward to the StdinChan that the bridge owner
			// (DaemonBridge) reads. Non-blocking — a stalled consumer must
			// not back up the WS read pump. v1 is read-only display so this
			// channel is expected to be empty.
			select {
			case s.stdinChan <- append([]byte(nil), p...):
				return nil
			default:
				return errors.New("stdin buffer full")
			}
		}
		_, err := s.stdin.Write(p)
		return err
	}
	detach = func() {
		s.mu.Lock()
		delete(s.subscribers, sub)
		s.mu.Unlock()
		sub.close()
	}
	return sub, write, detach
}

// Done returns a channel that is closed when the session terminates.
func (s *Session) Done() <-chan struct{} { return s.done }

// ExitErr returns the child process's exit error, or nil. Only meaningful
// after Done is closed.
func (s *Session) ExitErr() error { return s.exitErr }

// ExitCode returns the process exit code, if known. Only meaningful after
// Done is closed; defaults to 0 when unset.
func (s *Session) ExitCode() int { return s.exitCode }

// External reports whether this session is backed by an external owner
// (today: the daemon) rather than an in-process subprocess.
func (s *Session) External() bool { return s.external }

// PushOutput broadcasts a chunk of stdout to subscribers and appends to the
// replay-history ring. Only meaningful for adopted sessions; calling it on
// an internal-process session is harmless but unnecessary (readLoop already
// owns the producer side there).
func (s *Session) PushOutput(p []byte) {
	if s.closed.Load() || len(p) == 0 {
		return
	}
	chunk := make([]byte, len(p))
	copy(chunk, p)
	s.fanout(chunk)
}

// StdinChan exposes the inbound stdin channel for adopted sessions. The
// bridge that owns the external process reads from here and forwards each
// frame back to the daemon. Returns nil for internal-process sessions.
func (s *Session) StdinChan() <-chan []byte { return s.stdinChan }

// Exit closes the session with the given exit code and optional error.
// Idempotent; safe to call from the bridge regardless of session lineage.
func (s *Session) Exit(code int, err error) {
	if s.closed.Load() {
		return
	}
	s.exitCode = code
	s.terminate(err)
}

// readLoop pumps stdout to every subscriber. Slow subscribers are evicted.
func (s *Session) readLoop() {
	buf := make([]byte, stdoutChunkBytes)
	for {
		n, err := s.stdout.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.fanout(chunk)
		}
		if err != nil {
			break
		}
	}
	waitErr := s.cmd.Wait()
	s.terminate(waitErr)
}

// fanout broadcasts to every subscriber, evicting any whose buffer is
// full. Also appends to the bounded history buffer so a fresh Attach can
// replay recent output.
func (s *Session) fanout(chunk []byte) {
	s.mu.Lock()
	// Append to history, trimming from the head if we exceed the cap.
	s.history = append(s.history, chunk...)
	if len(s.history) > historyBytesMax {
		drop := len(s.history) - historyBytesMax
		s.history = s.history[drop:]
	}
	slow := make([]*Subscriber, 0)
	for sub := range s.subscribers {
		select {
		case sub.C <- chunk:
		default:
			slow = append(slow, sub)
		}
	}
	for _, sub := range slow {
		delete(s.subscribers, sub)
	}
	s.mu.Unlock()
	for _, sub := range slow {
		sub.close()
	}
}

// terminate is idempotent. It records the exit error, signals Done, closes
// stdin, and closes all subscribers. The broker map entry is NOT removed
// here — Close (explicit) and reaper (TTL) own that.
func (s *Session) terminate(exitErr error) {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.exitErr = exitErr
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.stdinChan != nil {
		// Closing lets goroutines reading StdinChan unblock and return.
		close(s.stdinChan)
		s.stdinChan = nil
	}
	close(s.done)
	s.mu.Lock()
	subs := s.subscribers
	s.subscribers = map[*Subscriber]struct{}{}
	s.mu.Unlock()
	for sub := range subs {
		sub.close()
	}
	// Best-effort kill so the child actually goes away if Wait hasn't
	// returned yet (e.g. caller invoked terminate before readLoop saw EOF).
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// reaper removes the session from the broker after it has been done for
// maxSessionAge, to bound memory in case nobody calls Close.
func (s *Session) reaper(b *Broker) {
	<-s.done
	t := time.NewTimer(maxSessionAge)
	defer t.Stop()
	<-t.C
	b.mu.Lock()
	if cur := b.sessions[s.ID]; cur == s {
		delete(b.sessions, s.ID)
	}
	if s.RuntimeID != "" {
		if cur := b.runtimeIndex[s.RuntimeID]; cur == s {
			delete(b.runtimeIndex, s.RuntimeID)
		}
	}
	b.mu.Unlock()
}
