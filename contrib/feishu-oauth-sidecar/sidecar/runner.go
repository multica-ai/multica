package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type ScriptRunner interface {
	Run(ctx context.Context, script string, args ...string) (RunResult, error)
}

type RunResult struct {
	Stdout string
	Stderr string
}

type CommandRunner struct {
	ScriptsDir string
}

type ExecError struct {
	Script   string
	Args     []string
	ExitCode int
	Stderr   string
	Timeout  bool
	Cause    error
}

func (e *ExecError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("script %s timed out", e.Script)
	}
	if e.ExitCode != 0 {
		return fmt.Sprintf("script %s exited with code %d", e.Script, e.ExitCode)
	}
	if e.Cause != nil {
		return fmt.Sprintf("script %s failed: %v", e.Script, e.Cause)
	}
	return fmt.Sprintf("script %s failed", e.Script)
}

func (r *CommandRunner) Run(ctx context.Context, script string, args ...string) (RunResult, error) {
	cmd := exec.CommandContext(ctx, "bash", append([]string{scriptPath(r.ScriptsDir, script)}, args...)...)
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	result := RunResult{Stdout: outBuf.String(), Stderr: errBuf.String()}
	if err == nil {
		return result, nil
	}

	execErr := &ExecError{Script: script, Args: args, Stderr: result.Stderr, Cause: err, ExitCode: -1}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		execErr.Timeout = true
		return result, execErr
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		execErr.ExitCode = exitErr.ExitCode()
	}
	return result, execErr
}

func scriptPath(dir, script string) string {
	return strings.TrimRight(dir, "/") + "/" + script
}
