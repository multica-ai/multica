//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// claudeTUIBackend implements Backend by driving the real Claude Code TUI
// over a pseudo-terminal. Unlike claudeBackend (which uses -p stream-json),
// this backend types the prompt into the interactive UI char-by-char.
//
// Structured events (tool_use, tool_result, session start/stop) are recovered
// via Claude Code's hook system: at Execute time the backend writes a
// .claude/settings.local.json into the task workdir that configures
// `type: command` hooks invoking `curl` back to the daemon's hook HTTP
// server. Pre/PostToolUse drive MessageToolUse/MessageToolResult events on
// the Messages channel; Stop ends the run with last_assistant_message as
// the canonical Result.Output. PTY ANSI text is still forwarded as
// MessageText for live progress visibility but is not used for the final
// output or completion detection.
//
// If Hooks is nil (e.g. in tests), completion falls back to a silence
// window and Output accumulates the (noisy) PTY text.
//
// Not supported on Windows (creack/pty has no Windows ConPTY backend in
// the version we depend on).
type claudeTUIBackend struct {
	cfg Config
}

// claudeTUIFallbackSilence is how long the backend waits without any PTY
// activity before declaring a turn complete when no Stop hook arrives. Real
// completion should fire from the Stop hook; this is only a safety net.
const claudeTUIFallbackSilence = 2 * time.Minute

// ansiEscape matches the common ANSI control sequences the Claude Code TUI
// emits for redraws, colors, and cursor moves. Plain-text approximation is
// good enough for the MessageText stream; the authoritative final text comes
// from the Stop hook's last_assistant_message.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[=>]|\r`)

func (b *claudeTUIBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}
	if opts.Cwd == "" {
		return nil, fmt.Errorf("claude-tui: opts.Cwd is required (hook settings.local.json must be written into it)")
	}

	hardTimeout := opts.Timeout
	if hardTimeout == 0 {
		hardTimeout = 60 * time.Minute
	}
	silenceWindow := opts.SemanticInactivityTimeout
	if silenceWindow == 0 {
		silenceWindow = claudeTUIFallbackSilence
	}

	// Install the per-task hook config so Claude posts back to the daemon's
	// hook server. The token is the resume session id when present (so
	// follow-up turns share routing) or a fresh nonce otherwise — task UUID
	// would be cleaner but the backend doesn't have access to it. Either
	// way the token only needs to be unique among concurrent Executes.
	token := opts.ResumeSessionID
	if token == "" {
		token = fmt.Sprintf("tui-%d-%d", time.Now().UnixNano(), os.Getpid())
	}

	var (
		hookEvents <-chan HookEvent
		hookCancel func()
	)
	settingsRestore, err := installTUIHookSettings(opts.Cwd, b.cfg.Hooks, token)
	if err != nil {
		return nil, fmt.Errorf("claude-tui: install hook settings: %w", err)
	}
	if b.cfg.Hooks != nil {
		hookEvents, hookCancel = b.cfg.Hooks.Subscribe(token)
	}

	runCtx, cancel := context.WithTimeout(ctx, hardTimeout)

	// Build command without -p / --output-format — let claude render its
	// real interactive TUI. CustomArgs still honored (e.g. --model).
	args := append([]string{}, opts.ExtraArgs...)
	args = append(args, opts.CustomArgs...)
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	cmd := exec.CommandContext(runCtx, execPath, args...)
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)

	b.cfg.Logger.Info("agent command (tui)",
		"exec", execPath,
		"args", args,
		"cwd", opts.Cwd,
		"silence", silenceWindow.String(),
		"timeout", hardTimeout.String(),
		"hooks", b.cfg.Hooks != nil,
		"token", token,
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		cancel()
		if hookCancel != nil {
			hookCancel()
		}
		settingsRestore()
		return nil, fmt.Errorf("start claude (tui pty): %w", err)
	}

	b.cfg.Logger.Info("claude tui started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// lastByteAt: silence-window baseline, updated whenever the PTY writes
	// anything. Guards against the hook server going silent (e.g. claude
	// crashed before Stop fired) by forcing eventual completion.
	var (
		lastMu     sync.Mutex
		lastByteAt = time.Now()
	)
	bump := func() {
		lastMu.Lock()
		lastByteAt = time.Now()
		lastMu.Unlock()
	}
	since := func() time.Duration {
		lastMu.Lock()
		defer lastMu.Unlock()
		return time.Since(lastByteAt)
	}

	// Reader goroutine: ANSI-strip PTY bytes and stream as MessageText.
	var (
		outMu  sync.Mutex
		pend   strings.Builder
		ptyOut strings.Builder // PTY-derived output, used only when hooks unavailable
	)
	flushLine := func(line string) {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			return
		}
		outMu.Lock()
		ptyOut.WriteString(line)
		ptyOut.WriteByte('\n')
		outMu.Unlock()
		trySend(msgCh, Message{Type: MessageText, Content: line})
	}

	// trustPromptSeen flips when the reader spots the "trust this folder"
	// modal in cleaned PTY output. The typer polls this flag and only sends
	// "1\r" when it's set — avoids injecting a stray "1" into the prompt on
	// already-trusted workspaces.
	var trustPromptSeen atomic.Bool
	const trustPromptMarker = "trust this folder"

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				bump()
				clean := ansiEscape.ReplaceAllString(string(buf[:n]), "")
				if !trustPromptSeen.Load() && strings.Contains(strings.ToLower(clean), trustPromptMarker) {
					trustPromptSeen.Store(true)
				}
				pend.WriteString(clean)
				for {
					s := pend.String()
					idx := strings.IndexByte(s, '\n')
					if idx < 0 {
						break
					}
					flushLine(s[:idx])
					pend.Reset()
					pend.WriteString(s[idx+1:])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Typer: drive the TUI. Wait up to 4s watching for a trust-folder modal;
	// if it shows, dismiss with "1\r" and let the UI settle. Then type the
	// real prompt char by char and submit with CR. Skipping the trust
	// keystrokes when no modal is present avoids injecting a stray "1" into
	// the prompt on already-trusted workspaces.
	go func() {
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if trustPromptSeen.Load() {
				_, _ = ptmx.Write([]byte("1\r"))
				time.Sleep(800 * time.Millisecond)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Brief settle to ensure the input box is focused even when the
		// trust modal wasn't shown.
		time.Sleep(1500 * time.Millisecond)
		for _, r := range prompt {
			if _, err := ptmx.Write([]byte(string(r))); err != nil {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
		_, _ = ptmx.Write([]byte("\r"))
		bump()
	}()

	// Watcher: wins one of {Stop hook, ctx done, silence window} and tears
	// the run down. Hook events also feed structured Messages to the daemon.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer settingsRestore()
		defer func() { _ = ptmx.Close() }()
		defer func() {
			if hookCancel != nil {
				hookCancel()
			}
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var hookFinalText string

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		decided := false
		for !decided {
			select {
			case <-runCtx.Done():
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("claude tui timed out after %s", hardTimeout)
				} else {
					finalStatus = "aborted"
					finalError = "execution cancelled"
				}
				decided = true

			case ev, ok := <-hookEvents:
				if !ok {
					hookEvents = nil
					continue
				}
				switch ev.Type {
				case HookSessionStart:
					if ev.SessionID != "" {
						sessionID = ev.SessionID
						trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
					}
				case HookPreToolUse:
					var input map[string]any
					if len(ev.ToolInput) > 0 {
						_ = json.Unmarshal(ev.ToolInput, &input)
					}
					trySend(msgCh, Message{
						Type:   MessageToolUse,
						Tool:   ev.ToolName,
						CallID: ev.ToolUseID,
						Input:  input,
					})
				case HookPostToolUse:
					out := ""
					if len(ev.ToolResponse) > 0 {
						out = string(ev.ToolResponse)
					}
					trySend(msgCh, Message{
						Type:   MessageToolResult,
						Tool:   ev.ToolName,
						CallID: ev.ToolUseID,
						Output: out,
					})
				case HookStop:
					if ev.SessionID != "" {
						sessionID = ev.SessionID
					}
					hookFinalText = ev.LastAssistantText
					finalStatus = "completed"
					decided = true
				}

			case <-ticker.C:
				if since() >= silenceWindow {
					b.cfg.Logger.Warn("claude tui silence fallback fired (no Stop hook)", "silence", silenceWindow.String())
					decided = true
				}
			}
		}

		// SIGINT first so claude can flush trailing UI; CommandContext will
		// kill if it doesn't exit within WaitDelay.
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGINT)
		}
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
		}
		_ = ptmx.Close()
		<-readerDone

		if tail := strings.TrimRight(pend.String(), " \t\r\n"); tail != "" {
			flushLine(tail)
		}
		_ = cmd.Wait()

		duration := time.Since(startTime)
		b.cfg.Logger.Info("claude tui finished",
			"pid", cmd.Process.Pid,
			"status", finalStatus,
			"duration", duration.Round(time.Millisecond).String(),
			"session_id", sessionID,
			"via_hook", hookFinalText != "",
		)

		// Prefer the Stop hook's clean text; fall back to PTY accumulation.
		outText := hookFinalText
		if outText == "" {
			outMu.Lock()
			outText = ptyOut.String()
			outMu.Unlock()
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     outText,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// installTUIHookSettings writes a per-task .claude/settings.local.json into
// cwd that routes all four hook events through curl to the daemon's HTTP
// server. If the file already exists (e.g. checked-out repo carries one),
// it is moved aside and the returned restore func puts it back when the
// run ends. When hooks is nil the function is a no-op and restore is a
// no-op too — the backend will fall back to silence-window completion.
func installTUIHookSettings(cwd string, hooks HookSubscriber, token string) (func(), error) {
	noop := func() {}
	if hooks == nil {
		return noop, nil
	}

	dir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return noop, err
	}
	settingsPath := filepath.Join(dir, "settings.local.json")
	backupPath := settingsPath + ".multica-tui.bak"

	// Back up an existing user file. We only restore if WE wrote a fresh
	// file (i.e. a backup exists at the end) — if backup creation fails
	// we refuse to clobber and surface the error.
	var hadExisting bool
	if _, err := os.Stat(settingsPath); err == nil {
		hadExisting = true
		if err := os.Rename(settingsPath, backupPath); err != nil {
			return noop, fmt.Errorf("backup existing settings.local.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return noop, err
	}

	url := hooks.BaseURL() + "?task=" + token
	// Hook stdin is JSON; curl reads it directly and posts to the daemon's
	// HTTP server. The `|| true` swallows curl errors so a transient daemon
	// hiccup doesn't surface to claude as a hook failure (claude treats
	// non-zero exit as a hook problem and shows a warning to the user).
	cmd := fmt.Sprintf(
		`curl -sS -X POST -H 'Content-Type: application/json' --data-binary @- '%s' >/dev/null 2>&1 || true`,
		url,
	)
	type hookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type hookMatcher struct {
		Matcher string      `json:"matcher"`
		Hooks   []hookEntry `json:"hooks"`
	}
	type settings struct {
		Hooks map[string][]hookMatcher `json:"hooks"`
	}
	one := []hookMatcher{{Matcher: "", Hooks: []hookEntry{{Type: "command", Command: cmd}}}}
	body, err := json.MarshalIndent(settings{
		Hooks: map[string][]hookMatcher{
			"SessionStart": one,
			"PreToolUse":   one,
			"PostToolUse":  one,
			"Stop":         one,
		},
	}, "", "  ")
	if err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, settingsPath)
		}
		return noop, err
	}
	if err := os.WriteFile(settingsPath, body, 0o644); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, settingsPath)
		}
		return noop, err
	}

	restore := func() {
		if hadExisting {
			_ = os.Rename(backupPath, settingsPath)
			return
		}
		_ = os.Remove(settingsPath)
	}
	return restore, nil
}
