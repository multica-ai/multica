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
)

// Session is a single PTY-style stream backed by a child process.
type Session struct {
	ID          string
	RuntimeID   string
	WorkspaceID string
	OwnerUserID string
	Command     []string
	CreatedAt   time.Time

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu          sync.Mutex
	subscribers map[*Subscriber]struct{}
	closed      atomic.Bool
	exitErr     error
	done        chan struct{}
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
}

func NewBroker() *Broker {
	return &Broker{sessions: make(map[string]*Session)}
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
	b.mu.Unlock()

	go s.readLoop()
	go s.reaper(b)

	return s, nil
}

// Get returns the session by ID, or nil.
func (b *Broker) Get(id string) *Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
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
	b.mu.Unlock()
	if s == nil {
		return errors.New("session not found")
	}
	s.terminate(nil)
	return nil
}

// Attach registers a Subscriber for stdout chunks. It returns the
// subscriber, a function to write stdin, and an unsubscribe func that the
// caller MUST invoke when done.
func (s *Session) Attach() (sub *Subscriber, write func([]byte) error, detach func()) {
	sub = newSubscriber()
	s.mu.Lock()
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()

	write = func(p []byte) error {
		if s.closed.Load() {
			return errors.New("session closed")
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

// fanout broadcasts to every subscriber, evicting any whose buffer is full.
func (s *Session) fanout(chunk []byte) {
	s.mu.Lock()
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
	_ = s.stdin.Close()
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
	b.mu.Unlock()
}
