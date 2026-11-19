package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type codexForkReceipt struct {
	RequestID string `json:"request_id"`
	ChildID   string `json:"child_id"`
}

// publishCodexFork stages the child and its archived ancestors in the same
// filesystem as the conversation store. The callback must finish writing and
// close the provider process before returning the child ID.
func publishCodexFork(storeDir, requestID string, stage func(string) (string, error)) (string, error) {
	if storeDir == "" || requestID == "" {
		return "", errors.New("Codex fork requires a destination store and request ID")
	}
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return "", fmt.Errorf("create Codex fork destination: %w", err)
	}
	lock, err := openLockFile(filepath.Join(storeDir, ".fork.lock"))
	if err != nil {
		return "", fmt.Errorf("open Codex fork lock: %w", err)
	}
	defer lock.Close()
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil {
		return "", fmt.Errorf("acquire Codex fork lock: %w", err)
	}
	if !locked {
		return "", errors.New("another Codex fork is in progress for this chat")
	}

	destination := filepath.Join(storeDir, "fork")
	receiptPath := filepath.Join(destination, "receipt.json")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(receiptPath)
		if err != nil {
			return "", fmt.Errorf("read published Codex fork receipt: %w", err)
		}
		var receipt codexForkReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return "", fmt.Errorf("decode published Codex fork receipt: %w", err)
		}
		if receipt.RequestID != requestID {
			return "", errors.New("a different Codex fork already owns the destination")
		}
		if !validPublishedCodexFork(destination, receipt.ChildID) {
			return "", errors.New("published Codex fork history is unavailable")
		}
		return receipt.ChildID, nil
	}

	staging, err := os.MkdirTemp(storeDir, ".fork-staging-")
	if err != nil {
		return "", fmt.Errorf("create Codex fork staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	for _, name := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(staging, name), 0o700); err != nil {
			return "", fmt.Errorf("create Codex fork staging subdirectory: %w", err)
		}
	}
	childID, err := stage(staging)
	if err != nil {
		return "", fmt.Errorf("stage Codex fork: %w", err)
	}
	if !validPublishedCodexFork(staging, childID) {
		return "", errors.New("staged Codex fork history is unavailable")
	}
	data, err := json.Marshal(codexForkReceipt{RequestID: requestID, ChildID: childID})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, "receipt.json"), data, 0o600); err != nil {
		return "", fmt.Errorf("write Codex fork receipt: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return "", fmt.Errorf("publish Codex fork: %w", err)
	}
	return childID, nil
}

func validPublishedCodexFork(dir, childID string) bool {
	if err := uuid.Validate(childID); err != nil {
		return false
	}
	archive, err := os.Lstat(filepath.Join(dir, "archived_sessions"))
	return err == nil && archive.IsDir() && CodexResumeRolloutPresent(dir, childID)
}
