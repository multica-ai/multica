//go:build windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
)

// Windows has no openat equivalent in the standard syscall surface used by
// this package. Keep the same no-replace publish contract and retain the
// existing parent validation; Unix builds additionally use directory handles
// to close the pathname race.
type taskConfigWriter struct {
	file       *os.File
	tempPath   string
	targetPath string
}

func openTaskConfigWriter(envRoot, workDir, rel, target, tempPath string) (*taskConfigWriter, error) {
	if _, err := filepath.Abs(envRoot); err != nil {
		return nil, errors.New("task_config: invalid environment root")
	}
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, errors.New("task_config: create temporary destination failed")
	}
	return &taskConfigWriter{file: file, tempPath: tempPath, targetPath: target}, nil
}

func (w *taskConfigWriter) Close() {}

func (w *taskConfigWriter) Publish() error {
	if w == nil || w.file != nil {
		return errors.New("task_config: writer is not ready")
	}
	if err := os.Link(w.tempPath, w.targetPath); err != nil {
		return errors.New("task_config: publish destination failed")
	}
	if err := os.Remove(w.tempPath); err != nil {
		return errors.New("task_config: remove temporary destination failed")
	}
	return nil
}
