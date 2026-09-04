package execenv

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const taskConfigIntentFile = ".multica_task_config_intent.json"

type taskConfigIntent struct {
	TaskID string `json:"task_id"`
	// WorkDir is relative to the env root. Restart recovery never accepts an
	// absolute path from this file.
	WorkDir string   `json:"work_dir"`
	Paths   []string `json:"paths"`
}

// RegisterTaskConfigIntent persists relative paths and their prepared workdir
// before a task-config write. Unlike the legacy general sidecar manifest, this
// record is safe to use during restart recovery because paths are re-derived
// and checked against the recorded workdir before removal.
func RegisterTaskConfigIntent(envRoot, taskID, workDir string, paths ...string) error {
	envRoot, err := filepath.Abs(envRoot)
	root, rootErr := filepath.Abs(workDir)
	workRel, relErr := filepath.Rel(envRoot, root)
	if err != nil || rootErr != nil || relErr != nil || envRoot == "" || root == "" || !safeIntentTaskID(taskID) || !safeIntentRelativePath(workRel) || len(paths) == 0 {
		return errors.New("execenv: invalid task config intent")
	}
	if !safeIntentParents(envRoot, workRel) {
		return errors.New("execenv: task config intent workdir is unsafe")
	}
	if info, statErr := os.Lstat(root); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("execenv: task config intent workdir is unsafe")
	}
	intent := taskConfigIntent{TaskID: taskID, WorkDir: filepath.ToSlash(workRel)}
	for _, path := range paths {
		absolutePath, absErr := filepath.Abs(path)
		rel, err := filepath.Rel(root, absolutePath)
		if absErr != nil || err != nil || !safeIntentRelativePath(rel) {
			return errors.New("execenv: task config intent escapes workdir")
		}
		intent.Paths = append(intent.Paths, filepath.ToSlash(rel))
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return errors.New("execenv: marshal task config intent failed")
	}
	if err := os.WriteFile(filepath.Join(envRoot, taskConfigIntentFile), data, 0o600); err != nil {
		return errors.New("execenv: write task config intent failed")
	}
	return nil
}

// CleanupTaskConfigIntent removes only regular files or symlinks re-derived
// beneath the intent's workdir. It returns the validated absolute paths so the
// caller can remove their entries from the general sidecar manifest too.
func CleanupTaskConfigIntent(envRoot string) ([]string, error) {
	if envRoot == "" {
		return nil, nil
	}
	intentPath := filepath.Join(envRoot, taskConfigIntentFile)
	data, err := os.ReadFile(intentPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("execenv: read task config intent failed")
	}
	var intent taskConfigIntent
	if json.Unmarshal(data, &intent) != nil {
		return nil, errors.New("execenv: parse task config intent failed")
	}
	envRoot, err = filepath.Abs(envRoot)
	if err != nil || envRoot == "" || !safeIntentTaskID(intent.TaskID) || len(intent.Paths) == 0 || !safeIntentRelativePath(intent.WorkDir) {
		return nil, errors.New("execenv: invalid task config intent")
	}
	if !safeIntentParents(envRoot, intent.WorkDir) {
		return nil, errors.New("execenv: task config intent workdir is unsafe")
	}
	root := filepath.Join(envRoot, filepath.FromSlash(intent.WorkDir))
	if info, statErr := os.Lstat(root); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("execenv: task config intent workdir is unsafe")
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return nil, errors.New("execenv: task config intent workdir is unsafe")
	}
	if owner, ownerErr := ReadEnvRootOwner(envRoot); ownerErr != nil {
		return nil, errors.New("execenv: task config intent owner is unreadable")
	} else if owner.TaskID != "" && owner.TaskID != intent.TaskID {
		return nil, errors.New("execenv: task config intent owner mismatch")
	}
	paths := make([]string, 0, len(intent.Paths))
	for _, rel := range intent.Paths {
		if !safeIntentRelativePath(rel) {
			return paths, errors.New("execenv: invalid task config intent path")
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		check, err := filepath.Rel(root, path)
		if err != nil || !safeIntentRelativePath(check) {
			return paths, errors.New("execenv: task config intent path escapes workdir")
		}
		if !safeIntentParents(root, rel) {
			return paths, errors.New("execenv: task config intent parent is unsafe")
		}
		paths = append(paths, path)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return paths, errors.New("execenv: remove task config file failed")
		}
	}
	if err := os.Remove(intentPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return paths, errors.New("execenv: remove task config intent failed")
	}
	return paths, nil
}

func safeIntentRelativePath(path string) bool {
	path = strings.ReplaceAll(path, "\\", "/")
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.Clean(path) == filepath.FromSlash(path)
}

func safeIntentTaskID(taskID string) bool {
	return taskID != "" && taskID != "." && taskID != ".." && filepath.Base(taskID) == taskID && !strings.ContainsAny(taskID, `/\\`)
}

func safeIntentParents(root, rel string) bool {
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// CleanupTaskConfigManifests is the restart-safe recovery entry point. It
// never trusts absolute paths from the general sidecar manifest.
func CleanupTaskConfigManifests(workspacesRoot string) (int, error) {
	if workspacesRoot == "" {
		return 0, nil
	}
	cleaned := 0
	var firstErr error
	err := filepath.WalkDir(workspacesRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if firstErr == nil {
				firstErr = walkErr
			}
			return nil
		}
		if entry.IsDir() || entry.Name() != taskConfigIntentFile {
			return nil
		}
		envRoot := filepath.Dir(path)
		paths, err := CleanupTaskConfigIntent(envRoot)
		if len(paths) > 0 {
			if cleanupErr := CleanupSidecarFiles(envRoot, paths...); err == nil {
				err = cleanupErr
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		cleaned++
		return nil
	})
	if err != nil && firstErr == nil {
		firstErr = err
	}
	return cleaned, firstErr
}
