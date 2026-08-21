package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	taskRootIndexDir   = ".task_roots"
	taskRootRecordFile = "root.json"
)

type taskRootRecord struct {
	WorkspaceID  string `json:"workspace_id"`
	TaskID       string `json:"task_id"`
	RelativePath string `json:"relative_path"`
}

// ResolveRootDir returns the one physical env root assigned to a task. The
// first caller freezes the readable path in an index keyed only by stable IDs;
// later claims keep that path even when display labels are added or renamed.
func ResolveRootDir(params RootDirParams) (string, error) {
	proposed := PredictRootDir(params)
	if proposed == "" {
		return "", nil
	}

	recordDir := taskRootRecordDir(params)
	record, err := readTaskRootRecord(recordDir)
	if err == nil {
		return validateTaskRootRecord(params, record)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	candidate, err := findOwnedTaskRoot(params)
	if err != nil {
		return "", err
	}
	if candidate == "" {
		candidate = proposed
	}
	relative, err := filepath.Rel(params.WorkspacesRoot, candidate)
	if err != nil {
		return "", fmt.Errorf("execenv: make task root relative: %w", err)
	}
	record = taskRootRecord{
		WorkspaceID:  params.WorkspaceID,
		TaskID:       params.TaskID,
		RelativePath: relative,
	}
	if err := installTaskRootRecord(recordDir, record); err != nil {
		return "", err
	}

	// Another claimant may have won the atomic install with different readable
	// labels. Always re-read the authoritative record instead of returning our
	// proposal.
	record, err = readTaskRootRecord(recordDir)
	if err != nil {
		return "", err
	}
	return validateTaskRootRecord(params, record)
}

func taskRootRecordDir(params RootDirParams) string {
	return filepath.Join(
		params.WorkspacesRoot,
		taskRootIndexDir,
		stableIdentityKey(params.WorkspaceID+"\x00"+params.TaskID),
	)
}

func stableIdentityKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func readTaskRootRecord(recordDir string) (taskRootRecord, error) {
	data, err := os.ReadFile(filepath.Join(recordDir, taskRootRecordFile))
	if err != nil {
		return taskRootRecord{}, err
	}
	var record taskRootRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return taskRootRecord{}, fmt.Errorf("execenv: decode task root record: %w", err)
	}
	return record, nil
}

func installTaskRootRecord(recordDir string, record taskRootRecord) error {
	parent := filepath.Dir(recordDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("execenv: create task root index: %w", err)
	}
	tmpDir, err := os.MkdirTemp(parent, ".pending-")
	if err != nil {
		return fmt.Errorf("execenv: create task root record: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("execenv: encode task root record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, taskRootRecordFile), data, 0o644); err != nil {
		return fmt.Errorf("execenv: write task root record: %w", err)
	}
	if err := os.Rename(tmpDir, recordDir); err != nil {
		// A complete non-empty directory is installed atomically. If it exists,
		// another claimant won and its record is authoritative.
		if _, readErr := readTaskRootRecord(recordDir); readErr == nil {
			return nil
		}
		return fmt.Errorf("execenv: install task root record: %w", err)
	}
	return nil
}

func validateTaskRootRecord(params RootDirParams, record taskRootRecord) (string, error) {
	if record.WorkspaceID != params.WorkspaceID || record.TaskID != params.TaskID {
		return "", fmt.Errorf("execenv: task root record identity mismatch")
	}
	relative := filepath.Clean(record.RelativePath)
	if relative == "." || filepath.IsAbs(relative) {
		return "", fmt.Errorf("execenv: invalid task root relative path %q", record.RelativePath)
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == ".." || parts[1] == ".." {
		return "", fmt.Errorf("execenv: invalid task root relative path %q", record.RelativePath)
	}
	if !validTaskRootSegment(parts[0], params.WorkspaceID, true) || !validTaskRootSegment(parts[1], params.TaskID, false) {
		return "", fmt.Errorf("execenv: task root relative path %q does not match its stable identity", record.RelativePath)
	}
	return filepath.Join(params.WorkspacesRoot, relative), nil
}

func validTaskRootSegment(segment, id string, workspace bool) bool {
	segment = strings.ToLower(segment)
	id = strings.ToLower(id)
	key := strings.ToLower(taskKey(id))
	if workspace && segment == id {
		return true
	}
	if !workspace && segment == key {
		return true
	}
	return strings.HasSuffix(segment, "-"+key)
}

// RemoveRootDirRecord removes the stable index after GC has reclaimed a
// terminal task root. It verifies that the record still points at envRoot so a
// stale cleanup can never remove another task's identity.
func RemoveRootDirRecord(workspacesRoot, envRoot string, owner EnvRootOwner) error {
	if workspacesRoot == "" || envRoot == "" || owner.WorkspaceID == "" || owner.TaskID == "" {
		return nil
	}
	params := RootDirParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    owner.WorkspaceID,
		TaskID:         owner.TaskID,
	}
	recordDir := taskRootRecordDir(params)
	record, err := readTaskRootRecord(recordDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	resolved, err := validateTaskRootRecord(params, record)
	if err != nil {
		return err
	}
	if filepath.Clean(resolved) != filepath.Clean(envRoot) {
		return fmt.Errorf("execenv: task root record points to %s, not reclaimed root %s", resolved, envRoot)
	}
	if err := os.RemoveAll(recordDir); err != nil {
		return fmt.Errorf("execenv: remove task root record: %w", err)
	}
	// Best effort: this succeeds only after the final task record is gone.
	_ = os.Remove(filepath.Join(workspacesRoot, taskRootIndexDir))
	return nil
}

// findOwnedTaskRoot adopts roots created before the stable index existed. The
// owner marker is authoritative; readable suffixes only narrow the scan.
func findOwnedTaskRoot(params RootDirParams) (string, error) {
	workspaceEntries, err := os.ReadDir(params.WorkspacesRoot)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("execenv: scan existing task roots: %w", err)
	}
	workspaceSuffix := strings.ToLower(taskKey(params.WorkspaceID))
	var found string
	for _, workspaceEntry := range workspaceEntries {
		if !workspaceEntry.IsDir() || strings.HasPrefix(workspaceEntry.Name(), ".") {
			continue
		}
		name := workspaceEntry.Name()
		if name != params.WorkspaceID && !strings.HasSuffix(strings.ToLower(name), "-"+workspaceSuffix) {
			continue
		}
		workspaceDir := filepath.Join(params.WorkspacesRoot, name)
		taskEntries, readErr := os.ReadDir(workspaceDir)
		if readErr != nil {
			return "", fmt.Errorf("execenv: read candidate workspace root %s: %w", workspaceDir, readErr)
		}
		for _, taskEntry := range taskEntries {
			if !taskEntry.IsDir() {
				continue
			}
			taskName := strings.ToLower(taskEntry.Name())
			taskSuffix := strings.ToLower(taskKey(params.TaskID))
			if taskName != taskSuffix && !strings.HasSuffix(taskName, "-"+taskSuffix) {
				continue
			}
			candidate := filepath.Join(workspaceDir, taskEntry.Name())
			owner, readErr := ReadEnvRootOwner(candidate)
			if readErr != nil {
				return "", fmt.Errorf("execenv: read candidate env root owner for %s: %w", candidate, readErr)
			}
			if owner.TaskID == "" {
				empty, emptyErr := dirIsEmpty(candidate)
				if emptyErr != nil {
					return "", fmt.Errorf("execenv: inspect candidate env root %s: %w", candidate, emptyErr)
				}
				if !empty {
					return "", fmt.Errorf("execenv: candidate env root %s holds files but has no owner", candidate)
				}
			} else if owner.TaskID != params.TaskID {
				continue
			}
			if owner.WorkspaceID != "" && owner.WorkspaceID != params.WorkspaceID {
				continue
			}
			if found != "" && found != candidate {
				return "", fmt.Errorf("execenv: task %s owns multiple env roots", params.TaskID)
			}
			found = candidate
		}
	}
	return found, nil
}
