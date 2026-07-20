//go:build windows

package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	windowsPreparationTreeHelperMode = "windows-preparation-tree-helper"
	windowsPreparationDelayedMode    = "windows-preparation-delayed-writer"
)

func testArgsAfterDoubleDash() []string {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			return os.Args[i+1:]
		}
	}
	return nil
}

// TestWindowsPreparationJobHelperProcess starts a deliberately orphaned child
// and then blocks forever. The parent test cancels this helper and verifies the
// Job Object kills the delayed child before it can write.
func TestWindowsPreparationJobHelperProcess(t *testing.T) {
	args := testArgsAfterDoubleDash()
	if len(args) != 3 || args[0] != windowsPreparationTreeHelperMode {
		return
	}
	var request preparationRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	marker, ready := args[1], args[2]
	child := exec.Command(
		os.Args[0],
		"-test.run=^TestWindowsPreparationDelayedWriter$",
		"--",
		windowsPreparationDelayedMode,
		marker,
	)
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		os.Exit(4)
	}
	select {}
}

func TestWindowsPreparationDelayedWriter(t *testing.T) {
	args := testArgsAfterDoubleDash()
	if len(args) != 2 || args[0] != windowsPreparationDelayedMode {
		return
	}
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(args[1], []byte("leaked"), 0o600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPrepareIsolated_WindowsKillsDescendantBeforeRetry(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "old-attempt-marker")
	ready := filepath.Join(dir, "descendant-ready")
	command := []string{
		os.Args[0],
		"-test.run=^TestWindowsPreparationJobHelperProcess$",
		"--",
		windowsPreparationTreeHelperMode,
		marker,
		ready,
	}
	params := PrepareParams{
		WorkspacesRoot: filepath.Join(dir, "workspaces"),
		WorkspaceID:    "ws-windows-job",
		TaskID:         "12345678-1111-2222-3333-444444444444",
		Provider:       "claude",
		Task:           TaskContextForEnv{IssueID: "issue-windows-job"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := PrepareIsolated(ctx, command, params, nil)
		result <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-result
			t.Fatal("helper did not start its delayed descendant")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareIsolated error = %v, want context canceled", err)
	}

	// The child would write after two seconds if only the direct helper were
	// killed. Waiting beyond that point makes the absence of the marker a
	// runtime proof that the Job Object terminated the whole tree.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("timed-out helper descendant survived and wrote marker: %v", err)
	}

	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer retryCancel()
	env, err := PrepareIsolated(retryCtx, preparationHelperTestCommand(), params, nil)
	if err != nil {
		t.Fatalf("immediate retry PrepareIsolated: %v", err)
	}
	if env == nil || env.RootDir != PredictRootDir(params.WorkspacesRoot, params.WorkspaceID, params.TaskID) {
		t.Fatalf("retry environment = %#v, want the predicted root", env)
	}
}
