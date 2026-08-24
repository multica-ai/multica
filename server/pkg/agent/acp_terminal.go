package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	defaultACPOutputByteLimit             = 50_000
	acpTerminalProcessGroupCleanupTimeout = 2 * time.Second
)

// acpTerminal is the daemon-side implementation of ACP's terminal/* methods.
// ACP agents use these calls for shell tools when the client advertises the
// terminal capability. Output is retained as a bounded tail, matching ACP's
// truncation contract without allowing a command to grow memory unboundedly.
type acpTerminal struct {
	cmd        *exec.Cmd
	mu         sync.Mutex
	output     []byte
	limit      int
	truncated  bool
	done       chan struct{}
	exitStatus *acpTerminalExitStatus
}

type acpTerminalExitStatus struct {
	exitCode *uint32
	signal   *string
}

type acpTerminalOutputWriter struct {
	terminal *acpTerminal
}

func (w acpTerminalOutputWriter) Write(p []byte) (int, error) {
	w.terminal.mu.Lock()
	defer w.terminal.mu.Unlock()

	w.terminal.output = append(w.terminal.output, p...)
	if w.terminal.limit > 0 && len(w.terminal.output) > w.terminal.limit {
		start := len(w.terminal.output) - w.terminal.limit
		// ACP output is a string, so truncation must never retain the tail of
		// a rune whose leading byte was discarded.
		for start < len(w.terminal.output) && !utf8.RuneStart(w.terminal.output[start]) {
			start++
		}
		w.terminal.output = append([]byte(nil), w.terminal.output[start:]...)
		w.terminal.truncated = true
	}
	return len(p), nil
}

func (t *acpTerminal) snapshot() (output string, truncated bool, exitStatus *acpTerminalExitStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	raw := append([]byte(nil), t.output...)
	// A pipe read may split a multibyte rune across Write calls. Do not expose
	// that incomplete suffix; a later snapshot will include it once complete.
	if len(raw) > 0 {
		lastRune := len(raw) - 1
		for lastRune > 0 && !utf8.RuneStart(raw[lastRune]) {
			lastRune--
		}
		if !utf8.FullRune(raw[lastRune:]) {
			raw = raw[:lastRune]
		}
	}
	valid := bytes.ToValidUTF8(raw, []byte("\uFFFD"))
	if t.limit > 0 && len(valid) > t.limit {
		start := len(valid) - t.limit
		for start < len(valid) && !utf8.RuneStart(valid[start]) {
			start++
		}
		valid = valid[start:]
	}
	if t.exitStatus != nil {
		status := *t.exitStatus
		exitStatus = &status
	}
	return string(valid), t.truncated, exitStatus
}

func (t *acpTerminal) wait() {
	<-t.done
}

func (t *acpTerminal) kill() error {
	t.mu.Lock()
	cmd := t.cmd
	t.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	signalProcessGroup(cmd, syscall.SIGKILL)
	// Cmd.Wait only describes the direct process. On Unix, descendants can
	// remain in its process group after that process exits, especially when a
	// background child redirects inherited output. Confirm the entire group is
	// gone before terminal/kill or terminal/release reports success.
	if runtime.GOOS != "windows" && !waitProcessGroupGone(cmd, acpTerminalProcessGroupCleanupTimeout) {
		return fmt.Errorf("process group %d still active after %s", cmd.Process.Pid, acpTerminalProcessGroupCleanupTimeout)
	}
	return nil
}

func (c *hermesClient) acpTerminalCreate(params json.RawMessage) (map[string]any, error) {
	var p struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Env     []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"env"`
		Cwd             string `json:"cwd"`
		OutputByteLimit int    `json:"outputByteLimit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("terminal/create params: %w", err)
	}
	if strings.TrimSpace(p.Command) == "" {
		return nil, fmt.Errorf("terminal/create requires command")
	}

	cwd := p.Cwd
	if cwd == "" {
		cwd = c.terminalCwd
	}
	if cwd == "" {
		cwd = "."
	}

	env := append([]string(nil), c.terminalEnv...)
	for _, item := range p.Env {
		if item.Name == "" {
			continue
		}
		env = append(env, item.Name+"="+item.Value)
	}

	var cmd *exec.Cmd
	if len(p.Args) > 0 {
		cmd = NewCommand(p.Command, nil).exec(c.terminalContext(), p.Args...)
	} else if runtime.GOOS == "windows" {
		cmd = NewCommand("cmd.exe", nil).exec(c.terminalContext(), "/d", "/s", "/c", p.Command)
	} else {
		cmd = NewCommand("/bin/sh", nil).exec(c.terminalContext(), "-c", p.Command)
	}
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		signalProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}
	cmd.Dir = cwd
	cmd.Env = env

	limit := p.OutputByteLimit
	if limit <= 0 {
		limit = defaultACPOutputByteLimit
	}
	t := &acpTerminal{cmd: cmd, limit: limit, done: make(chan struct{})}
	writer := acpTerminalOutputWriter{terminal: t}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := startOwnedProcessTree(cmd, c.cfg.Logger); err != nil {
		return nil, fmt.Errorf("terminal/create start: %w", err)
	}

	id := c.nextACPTerminalID()
	c.terminalMu.Lock()
	if c.terminals == nil {
		c.terminals = make(map[string]*acpTerminal)
	}
	c.terminals[id] = t
	c.terminalMu.Unlock()

	go func() {
		defer releaseProcessGroup(cmd)
		err := cmd.Wait()
		status := &acpTerminalExitStatus{}
		if err == nil {
			code := uint32(0)
			status.exitCode = &code
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			if code := exitErr.ExitCode(); code >= 0 {
				exitCode := uint32(code)
				status.exitCode = &exitCode
			} else {
				status.signal = acpProcessExitSignal(exitErr.ProcessState)
			}
		}
		t.mu.Lock()
		t.exitStatus = status
		t.mu.Unlock()
		close(t.done)
	}()

	return map[string]any{"terminalId": id}, nil
}

func (c *hermesClient) terminalContext() context.Context {
	// The ACP transport is normally attached to a live task context. A nil
	// context is not valid for CommandContext, so keep this defensive fallback
	// for unit tests that construct hermesClient directly.
	if c.terminalCtx != nil {
		return c.terminalCtx
	}
	return context.Background()
}

func (c *hermesClient) nextACPTerminalID() string {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	c.nextTerminalID++
	return fmt.Sprintf("multica-terminal-%d", c.nextTerminalID)
}

func (c *hermesClient) acpTerminalFor(id string) (*acpTerminal, bool) {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	t, ok := c.terminals[id]
	return t, ok
}

func (c *hermesClient) acpTerminalRelease(id string) error {
	c.terminalMu.Lock()
	t, ok := c.terminals[id]
	c.terminalMu.Unlock()
	if !ok {
		return nil
	}
	if err := t.kill(); err != nil {
		return fmt.Errorf("release terminal %q: %w", id, err)
	}

	// Delete only after cleanup is confirmed so a failed release can be retried.
	c.terminalMu.Lock()
	if c.terminals[id] == t {
		delete(c.terminals, id)
	}
	c.terminalMu.Unlock()
	return nil
}

func (c *hermesClient) acpTerminalResponse(method string, params json.RawMessage) (map[string]any, error) {
	var p struct {
		TerminalID string `json:"terminalId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("%s params: %w", method, err)
	}
	t, ok := c.acpTerminalFor(p.TerminalID)
	if !ok {
		return nil, fmt.Errorf("unknown terminal %q", p.TerminalID)
	}

	switch method {
	case "terminal/output":
		output, truncated, exitStatus := t.snapshot()
		result := map[string]any{"output": output, "truncated": truncated}
		if exitStatus != nil {
			result["exitStatus"] = acpTerminalExitStatusResult(exitStatus)
		}
		return result, nil
	case "terminal/wait_for_exit":
		t.wait()
		_, _, exitStatus := t.snapshot()
		if exitStatus == nil {
			return nil, fmt.Errorf("terminal %q exited without status", p.TerminalID)
		}
		return acpTerminalExitStatusResult(exitStatus), nil
	case "terminal/kill":
		if err := t.kill(); err != nil {
			return nil, fmt.Errorf("kill terminal %q: %w", p.TerminalID, err)
		}
		return map[string]any{}, nil
	case "terminal/release":
		return map[string]any{}, c.acpTerminalRelease(p.TerminalID)
	default:
		return nil, fmt.Errorf("unsupported terminal method %q", method)
	}
}

func acpTerminalExitStatusResult(status *acpTerminalExitStatus) map[string]any {
	result := map[string]any{"exitCode": nil, "signal": nil}
	if status.exitCode != nil {
		result["exitCode"] = int(*status.exitCode)
	}
	if status.signal != nil {
		result["signal"] = *status.signal
	}
	return result
}
