// Package processtree runs bounded helper commands whose descendants must not
// survive cancellation. It uses a Unix process group or a Windows Job Object.
package processtree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const gracefulStopTimeout = time.Second

// CombinedOutput runs an unstarted command and returns its combined output.
// Cancellation terminates the entire process tree, waits for it to disappear,
// and returns the context cause rather than a platform-specific exit status.
func CombinedOutput(ctx context.Context, cmd *exec.Cmd, waitDelay time.Duration) ([]byte, error) {
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := run(ctx, cmd, waitDelay)
	return output.Bytes(), err
}

// Output is the process-tree-safe equivalent of exec.Cmd.Output.
func Output(ctx context.Context, cmd *exec.Cmd, waitDelay time.Duration) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := run(ctx, cmd, waitDelay)
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		exitErr.Stderr = append([]byte(nil), stderr.Bytes()...)
	}
	return stdout.Bytes(), err
}

// Run executes an unstarted command while owning its complete process tree.
func Run(ctx context.Context, cmd *exec.Cmd, waitDelay time.Duration) error {
	return run(ctx, cmd, waitDelay)
}

func run(ctx context.Context, cmd *exec.Cmd, waitDelay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	controller, err := newController(cmd)
	if err != nil {
		return fmt.Errorf("create process-tree controller: %w", err)
	}
	defer controller.close()

	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := controller.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = controller.finish(cmd)
		return fmt.Errorf("attach process tree: %w", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var stopErr error
	cancelled := false
	select {
	case err = <-waitDone:
	case <-ctx.Done():
		cancelled = true
		stopErr = controller.interrupt(cmd)
		timer := time.NewTimer(gracefulStopTimeout)
		select {
		case err = <-waitDone:
			timer.Stop()
		case <-timer.C:
			stopErr = errors.Join(stopErr, controller.stop(cmd))
			err = <-waitDone
		}
	}
	finishErr := controller.finish(cmd)
	if lifecycleErr := errors.Join(stopErr, finishErr); lifecycleErr != nil {
		return fmt.Errorf("stop process tree: %w", lifecycleErr)
	}
	if cancelled {
		return context.Cause(ctx)
	}
	return err
}
