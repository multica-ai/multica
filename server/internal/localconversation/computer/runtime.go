// Package computer manages one persistent JavaScript runtime per conversation.
package computer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Command struct{ Executable, Entry string }
type Action func(context.Context, json.RawMessage) (json.RawMessage, error)
type Result struct {
	Output  string         `json:"output"`
	Value   string         `json:"value"`
	Actions int            `json:"actions"`
	Image   *Image         `json:"image,omitempty"`
	Trace   []ActionRecord `json:"trace"`
}
type ActionRecord struct {
	Op     string `json:"op"`
	App    string `json:"app"`
	Status string `json:"status"`
}
type Image struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}
type frame struct {
	Type        string          `json:"type"`
	ID          string          `json:"id"`
	ExecutionID string          `json:"executionId"`
	Action      json.RawMessage `json:"action"`
	Result      Result          `json:"result"`
	Error       string          `json:"error"`
}
type process struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	frames   chan frame
	done     chan struct{}
	writeMu  sync.Mutex
	busy     sync.Mutex
	stopOnce sync.Once
}

type Manager struct {
	Command  Command
	mu       sync.Mutex
	sessions map[string]*process
	closed   bool
}

// SandboxProfile denies file writes, networking and child execution. Only
// system libraries and this installation's runtime code are readable.
func SandboxProfile(command Command) string {
	exe, _ := filepath.EvalSymlinks(command.Executable)
	if exe == "" {
		exe = command.Executable
	}
	root := filepath.Dir(exe)
	if strings.HasSuffix(root, "/Contents/MacOS") {
		root = filepath.Dir(root)
	}
	return `(version 1)(deny default)(allow process-info* sysctl-read mach-lookup signal)
(allow process-exec (literal ` + strconv.Quote(exe) + `))
(allow file-read-metadata)
(allow file-read* (literal "/") (subpath "/System") (subpath "/usr/lib") (subpath "/usr/share") (literal "/dev/null") (literal "/dev/random") (literal "/dev/urandom") (subpath "/private/var/db/dyld")
(subpath ` + strconv.Quote(root) + `) (subpath ` + strconv.Quote(filepath.Dir(command.Entry)) + `))
(allow file-write-data (literal "/dev/null"))`
}

func (m *Manager) Available() bool {
	return runtime.GOOS == "darwin" && m.Command.Executable != "" && m.Command.Entry != ""
}

func (m *Manager) get(ctx context.Context, key string) (*process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("computer runtime closed")
	}
	if p := m.sessions[key]; p != nil {
		select {
		case <-p.done:
			delete(m.sessions, key)
		default:
			return p, nil
		}
	}
	if !m.Available() {
		return nil, errors.New("computer runtime requires the macOS desktop installation")
	}
	cmd := exec.Command("/usr/bin/sandbox-exec", "-p", SandboxProfile(m.Command), m.Command.Executable, "--max-old-space-size=128", m.Command.Entry)
	cmd.Env = []string{"ELECTRON_RUN_AS_NODE=1", "NODE_REPL_HISTORY=", "LANG=en_US.UTF-8"}
	cmd.Dir = filepath.Dir(m.Command.Entry)
	p, err := start(cmd)
	if err != nil {
		return nil, err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case f, ok := <-p.frames:
		if !ok || f.Type != "ready" {
			p.stop()
			return nil, errors.New("computer runtime did not become ready")
		}
	case <-readyCtx.Done():
		p.stop()
		return nil, readyCtx.Err()
	}
	if m.sessions == nil {
		m.sessions = map[string]*process{}
	}
	m.sessions[key] = p
	return p, nil
}

func start(cmd *exec.Cmd) (*process, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	// Runtime stderr may contain model code; do not put it in daemon logs.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	p := &process{cmd: cmd, in: in, frames: make(chan frame, 16), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		defer close(p.frames)
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var f frame
			if json.Unmarshal(scanner.Bytes(), &f) != nil {
				break
			}
			select {
			case p.frames <- f:
			default:
				p.stop()
				break
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	return p, nil
}

func (p *process) stop() { p.stopOnce.Do(func() { _ = p.in.Close(); _ = p.cmd.Process.Kill() }) }
func (p *process) send(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return json.NewEncoder(p.in).Encode(v)
}

func (m *Manager) Execute(ctx context.Context, key, id, code string, action Action) (result Result, failure error) {
	var trace []ActionRecord
	defer func() { result.Trace = trace; result.Actions = len(trace) }()
	p, err := m.get(ctx, key)
	if err != nil {
		return Result{}, err
	}
	if !p.busy.TryLock() {
		return Result{}, errors.New("computer runtime busy")
	}
	defer p.busy.Unlock()
	if err := p.send(map[string]any{"type": "execute", "id": id, "code": code}); err != nil {
		p.stop()
		return Result{}, err
	}
	count := 0
	for {
		select {
		case <-ctx.Done():
			p.stop()
			return Result{}, fmt.Errorf("outcome_unknown: execution cancelled; inspect the application before retrying: %w", ctx.Err())
		case f, ok := <-p.frames:
			if !ok {
				return Result{}, errors.New("outcome_unknown: computer runtime exited; variables were lost")
			}
			if f.Type == "result" && f.ID == id {
				if f.Error != "" {
					return f.Result, errors.New(f.Error)
				}
				return f.Result, nil
			}
			if f.Type != "action" || f.ExecutionID != id || len(f.Action) == 0 || count >= 60 {
				p.stop()
				return Result{}, errors.New("invalid computer runtime action")
			}
			count++
			value, err := action(ctx, f.Action)
			var target struct {
				Op  string `json:"op"`
				App string `json:"app"`
			}
			_ = json.Unmarshal(f.Action, &target)
			status := "completed"
			if err != nil {
				status = "failed"
			}
			trace = append(trace, ActionRecord{Op: target.Op, App: target.App, Status: status})
			reply := map[string]any{"type": "action_result", "id": f.ID, "result": value}
			if err != nil {
				reply["error"] = err.Error()
				delete(reply, "result")
			}
			if err := p.send(reply); err != nil {
				p.stop()
				return Result{}, err
			}
		}
	}
}

func (m *Manager) Reset(key string) {
	m.mu.Lock()
	p := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()
	if p != nil {
		p.stop()
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()
	for _, p := range sessions {
		p.stop()
	}
}
