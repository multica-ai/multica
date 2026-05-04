package execenv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultAfterCreateHookTimeout is intentionally long enough for dependency
	// installs while still bounding a stuck setup script before provider launch.
	DefaultAfterCreateHookTimeout = 30 * time.Minute
	AfterCreateHookTimeoutEnv     = "MULTICA_PROJECT_AFTER_CREATE_TIMEOUT"
	afterCreateLogName            = "after_create.log"
	afterCreateOutputTailBytes    = 8 * 1024
)

// AfterCreateHook describes a project-scoped setup script to run in a fresh
// task workdir before Multica context files and provider runtime config exist.
type AfterCreateHook struct {
	Script  string
	Env     map[string]string
	Timeout time.Duration
}

// AfterCreateHookError includes enough context for the daemon to block the task
// while preserving the workdir and log location for debugging.
type AfterCreateHookError struct {
	EnvRoot    string
	WorkDir    string
	LogPath    string
	OutputTail string
	TimedOut   bool
	Err        error
}

func (e *AfterCreateHookError) Error() string {
	if e == nil {
		return ""
	}
	status := "failed"
	if e.TimedOut {
		status = "timed out"
	}
	msg := fmt.Sprintf("project after_create hook %s", status)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if e.LogPath != "" {
		msg += fmt.Sprintf("\n\nLog: %s", e.LogPath)
	}
	tail := strings.TrimSpace(e.OutputTail)
	if tail != "" {
		msg += "\n\nOutput:\n" + tail
	}
	return msg
}

func (e *AfterCreateHookError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func resolveAfterCreateHookTimeout(override time.Duration) (time.Duration, error) {
	if override > 0 {
		return override, nil
	}
	raw := strings.TrimSpace(os.Getenv(AfterCreateHookTimeoutEnv))
	if raw == "" {
		return DefaultAfterCreateHookTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", AfterCreateHookTimeoutEnv, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: duration must be positive", AfterCreateHookTimeoutEnv)
	}
	return d, nil
}

func runAfterCreateHook(ctx context.Context, envRoot, workDir string, hook AfterCreateHook, logger *slog.Logger) error {
	script := hook.Script
	if strings.TrimSpace(script) == "" {
		return nil
	}
	timeout, err := resolveAfterCreateHookTimeout(hook.Timeout)
	logPath := filepath.Join(envRoot, "logs", afterCreateLogName)
	if err != nil {
		return &AfterCreateHookError{
			EnvRoot: envRoot,
			WorkDir: workDir,
			LogPath: logPath,
			Err:     err,
		}
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("after_create hook: create log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("after_create hook: open log: %w", err)
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "multica project after_create hook\ncwd: %s\ntimeout: %s\n\n", workDir, timeout)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(runCtx, script)
	cmd.Dir = workDir
	cmd.Env = hookEnv(hook.Env, workDir, envRoot)

	tail := &tailWriter{max: afterCreateOutputTailBytes}
	writer := io.MultiWriter(logFile, tail)
	cmd.Stdout = writer
	cmd.Stderr = writer

	if logger != nil {
		logger.Info("execenv: running project after_create hook", "workdir", workDir, "log", logPath, "timeout", timeout.String())
	}
	err = cmd.Run()
	timedOut := runCtx.Err() == context.DeadlineExceeded
	if err != nil {
		fmt.Fprintf(logFile, "\n\nhook exit: %v\n", err)
		return &AfterCreateHookError{
			EnvRoot:    envRoot,
			WorkDir:    workDir,
			LogPath:    logPath,
			OutputTail: tail.String(),
			TimedOut:   timedOut,
			Err:        err,
		}
	}
	fmt.Fprintln(logFile, "\n\nhook exit: 0")
	return nil
}

func shellCommand(ctx context.Context, script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", script)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}

func hookEnv(extra map[string]string, workDir, envRoot string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	env = append(env,
		"MULTICA_WORKDIR="+workDir,
		"MULTICA_ENV_ROOT="+envRoot,
	)
	return env
}

type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	if w.max <= 0 {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.max:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	return string(w.buf)
}
