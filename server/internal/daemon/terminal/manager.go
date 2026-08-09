// Package terminal owns interactive provider processes and their PTYs. It has
// no network or database dependency; callers relay Events over the dedicated
// terminal data plane and persist metadata only.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/terminalproto"
)

const (
	DefaultRingBytes = 8 * 1024 * 1024
	DefaultCols      = 120
	DefaultRows      = 32
	MinCols          = 20
	MaxCols          = 400
	MinRows          = 5
	MaxRows          = 200
)

var (
	ErrSessionNotFound = errors.New("terminal session not found")
	ErrDuplicateInput  = errors.New("duplicate terminal input")
)

type Command struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type StartRequest struct {
	SessionID             uuid.UUID
	TaskID                string
	IssueID               string
	AgentID               string
	WorkspaceID           string
	RuntimeID             string
	DaemonID              string
	Provider              string
	Generation            int
	StructuredObservation string
	ProviderSessionID     string
	Cols                  uint16
	Rows                  uint16
	Command               Command
}

type Event struct {
	Type                  string
	SessionID             uuid.UUID
	TaskID                string
	IssueID               string
	AgentID               string
	WorkspaceID           string
	RuntimeID             string
	DaemonID              string
	Provider              string
	Generation            int
	StructuredObservation string
	ProviderSessionID     string
	Cols                  uint16
	Rows                  uint16
	Sequence              uint64
	Payload               []byte
	Status                string
	ExitCode              *int
	ExitReason            string
	Err                   error
}

type Replay struct {
	Chunks    []Event
	Gap       bool
	OldestSeq uint64
	LatestSeq uint64
}

type Exit struct {
	Code int
	Err  error
}

type Options struct {
	RingBytes int
	StopGrace time.Duration
	OnEvent   func(Event)
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
	opts     Options
}

type outputChunk struct {
	seq  uint64
	data []byte
}

type ptyHandle interface {
	io.Reader
	io.Writer
	io.Closer
	Resize(cols, rows uint16) error
}

type Session struct {
	manager *Manager
	meta    StartRequest
	cmd     *exec.Cmd
	pty     ptyHandle

	mu             sync.Mutex
	writeMu        sync.Mutex
	stopOnce       sync.Once
	seq            uint64
	ring           []outputChunk
	ringBytes      int
	seenInput      map[uint64]struct{}
	seenInputOrder []uint64
	cols           uint16
	rows           uint16
	exit           Exit
	stopRequested  bool
	done           chan struct{}
}

func NewManager(opts Options) *Manager {
	if opts.RingBytes <= 0 {
		opts.RingBytes = DefaultRingBytes
	}
	if opts.StopGrace <= 0 {
		opts.StopGrace = 3 * time.Second
	}
	return &Manager{sessions: make(map[uuid.UUID]*Session), opts: opts}
}

func (m *Manager) Start(ctx context.Context, req StartRequest) (*Session, error) {
	if req.SessionID == uuid.Nil {
		req.SessionID = uuid.New()
	}
	if req.Command.Path == "" || req.Command.Dir == "" || req.TaskID == "" || req.RuntimeID == "" {
		return nil, errors.New("terminal start requires executable, workdir, task, and runtime")
	}
	req.Cols, req.Rows = ClampSize(req.Cols, req.Rows)
	cmd := exec.Command(req.Command.Path, req.Command.Args...)
	cmd.Dir = req.Command.Dir
	cmd.Env = append([]string(nil), req.Command.Env...)
	handle, err := startPTY(cmd, req.Cols, req.Rows)
	if err != nil {
		return nil, fmt.Errorf("start terminal PTY: %w", err)
	}
	s := &Session{
		manager:   m,
		meta:      req,
		cmd:       cmd,
		pty:       handle,
		seenInput: make(map[uint64]struct{}),
		cols:      req.Cols,
		rows:      req.Rows,
		done:      make(chan struct{}),
	}
	m.mu.Lock()
	if _, exists := m.sessions[req.SessionID]; exists {
		m.mu.Unlock()
		_ = terminateProcessTree(cmd.Process, 0)
		_ = handle.Close()
		_ = cmd.Wait()
		return nil, fmt.Errorf("terminal session %s already exists", req.SessionID)
	}
	m.sessions[req.SessionID] = s
	m.mu.Unlock()
	m.emit(Event{Type: "state", SessionID: req.SessionID, TaskID: req.TaskID, IssueID: req.IssueID, AgentID: req.AgentID, WorkspaceID: req.WorkspaceID, RuntimeID: req.RuntimeID, DaemonID: req.DaemonID, Provider: req.Provider, Generation: req.Generation, StructuredObservation: req.StructuredObservation, Cols: req.Cols, Rows: req.Rows, Status: "running"})
	go s.readLoop()
	go s.waitLoop()
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Stop()
		case <-s.done:
		}
	}()
	return s, nil
}

func (m *Manager) Get(id uuid.UUID) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) Sessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *Manager) StopAll() {
	for _, s := range m.Sessions() {
		_ = s.Stop()
	}
}

func (m *Manager) emit(event Event) {
	if m.opts.OnEvent == nil {
		return
	}
	if event.Payload != nil {
		event.Payload = append([]byte(nil), event.Payload...)
	}
	m.opts.OnEvent(event)
}

func (s *Session) ID() uuid.UUID { return s.meta.SessionID }

func (s *Session) Metadata() StartRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.meta
	meta.Command = Command{}
	meta.Cols, meta.Rows = s.cols, s.rows
	return meta
}

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Wait() Exit {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exit
}

// SetProviderSessionID publishes the provider's durable resume identifier once
// it becomes discoverable. Codex writes that identifier to its rollout after
// the PTY has started, so it is intentionally not part of StartRequest input.
func (s *Session) SetProviderSessionID(providerSessionID string) {
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" {
		return
	}
	s.mu.Lock()
	if s.meta.ProviderSessionID == providerSessionID {
		s.mu.Unlock()
		return
	}
	s.meta.ProviderSessionID = providerSessionID
	status := "running"
	select {
	case <-s.done:
		status = "exited"
	default:
	}
	event := Event{
		Type: "state", SessionID: s.meta.SessionID, TaskID: s.meta.TaskID,
		IssueID: s.meta.IssueID, AgentID: s.meta.AgentID, WorkspaceID: s.meta.WorkspaceID,
		RuntimeID: s.meta.RuntimeID, DaemonID: s.meta.DaemonID, Provider: s.meta.Provider,
		Generation: s.meta.Generation, StructuredObservation: s.meta.StructuredObservation,
		ProviderSessionID: providerSessionID, Cols: s.cols, Rows: s.rows,
		Sequence: s.seq, Status: status,
	}
	s.mu.Unlock()
	s.manager.emit(event)
}

func (s *Session) WriteInput(inputID uint64, payload []byte) error {
	if len(payload) == 0 || len(payload) > terminalproto.MaxPayloadBytes {
		return fmt.Errorf("terminal input size %d is outside 1..%d", len(payload), terminalproto.MaxPayloadBytes)
	}
	s.mu.Lock()
	if _, exists := s.seenInput[inputID]; exists {
		s.mu.Unlock()
		return ErrDuplicateInput
	}
	s.seenInput[inputID] = struct{}{}
	s.seenInputOrder = append(s.seenInputOrder, inputID)
	if len(s.seenInputOrder) > 4096 {
		delete(s.seenInput, s.seenInputOrder[0])
		s.seenInputOrder = s.seenInputOrder[1:]
	}
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.pty.Write(payload)
	return err
}

func (s *Session) CtrlC(inputID uint64) error {
	return s.WriteInput(inputID, []byte{0x03})
}

func (s *Session) Resize(cols, rows uint16) error {
	cols, rows = ClampSize(cols, rows)
	if err := s.pty.Resize(cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	s.manager.emit(Event{Type: "state", SessionID: s.meta.SessionID, TaskID: s.meta.TaskID, IssueID: s.meta.IssueID, AgentID: s.meta.AgentID, WorkspaceID: s.meta.WorkspaceID, RuntimeID: s.meta.RuntimeID, DaemonID: s.meta.DaemonID, Provider: s.meta.Provider, Generation: s.meta.Generation, StructuredObservation: s.meta.StructuredObservation, Cols: cols, Rows: rows, Status: "running"})
	return nil
}

func ClampSize(cols, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	if cols < MinCols {
		cols = MinCols
	}
	if cols > MaxCols {
		cols = MaxCols
	}
	if rows < MinRows {
		rows = MinRows
	}
	if rows > MaxRows {
		rows = MaxRows
	}
	return cols, rows
}

func (s *Session) Replay(after uint64) Replay {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := Replay{LatestSeq: s.seq}
	if len(s.ring) == 0 {
		return result
	}
	result.OldestSeq = s.ring[0].seq
	result.Gap = after+1 < result.OldestSeq
	for _, chunk := range s.ring {
		if chunk.seq <= after {
			continue
		}
		result.Chunks = append(result.Chunks, Event{Type: "output", SessionID: s.meta.SessionID, Sequence: chunk.seq, Payload: append([]byte(nil), chunk.data...)})
	}
	return result
}

func (s *Session) Stop() error {
	var stopErr error
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopRequested = true
		s.mu.Unlock()
		stopErr = terminateProcessTree(s.cmd.Process, s.manager.opts.StopGrace)
		_ = s.pty.Close()
	})
	return stopErr
}

func (s *Session) readLoop() {
	buf := make([]byte, terminalproto.MaxPayloadBytes)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.appendOutput(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !isPTYClosedError(err) {
				s.manager.emit(Event{Type: "error", SessionID: s.meta.SessionID, TaskID: s.meta.TaskID, RuntimeID: s.meta.RuntimeID, Err: err})
			}
			return
		}
	}
}

func (s *Session) appendOutput(data []byte) {
	chunk := append([]byte(nil), data...)
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.ring = append(s.ring, outputChunk{seq: seq, data: chunk})
	s.ringBytes += len(chunk)
	for s.ringBytes > s.manager.opts.RingBytes && len(s.ring) > 1 {
		s.ringBytes -= len(s.ring[0].data)
		s.ring = s.ring[1:]
	}
	s.mu.Unlock()
	s.manager.emit(Event{Type: "output", SessionID: s.meta.SessionID, TaskID: s.meta.TaskID, IssueID: s.meta.IssueID, AgentID: s.meta.AgentID, WorkspaceID: s.meta.WorkspaceID, RuntimeID: s.meta.RuntimeID, DaemonID: s.meta.DaemonID, Provider: s.meta.Provider, Generation: s.meta.Generation, Sequence: seq, Payload: chunk})
}

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	_ = s.pty.Close()
	s.mu.Lock()
	s.exit = Exit{Code: code, Err: err}
	reason := "completed"
	if s.stopRequested {
		reason = "stopped"
	} else if code != 0 {
		reason = "process_error"
	}
	seq := s.seq
	s.mu.Unlock()
	close(s.done)
	s.manager.emit(Event{Type: "exit", SessionID: s.meta.SessionID, TaskID: s.meta.TaskID, IssueID: s.meta.IssueID, AgentID: s.meta.AgentID, WorkspaceID: s.meta.WorkspaceID, RuntimeID: s.meta.RuntimeID, DaemonID: s.meta.DaemonID, Provider: s.meta.Provider, Generation: s.meta.Generation, Sequence: seq, Status: "exited", ExitCode: &code, ExitReason: reason, Err: err})
}
