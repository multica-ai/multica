package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RunWorkspaceParams is copied from a capability-gated, host-bound resource
// snapshot. Empty BaseCommit selects a scratch room. A full commit selects a
// clean, independent repository; no dirty-tree snapshot or owner ref is written.
type RunWorkspaceParams struct {
	SourcePath string
	BaseCommit string
	HostID     string
	RuntimeID  string
}

type RunWorkspaceReceipt struct {
	RunID        string    `json:"run_id"`
	IssueID      string    `json:"issue_id"`
	ProfileID    string    `json:"profile_id"`
	HostID       string    `json:"host_id"`
	RuntimeID    string    `json:"runtime_id"`
	SourcePath   string    `json:"source_path"`
	BaseCommit   string    `json:"base_commit,omitempty"`
	WorkDir      string    `json:"work_dir"`
	Branch       string    `json:"branch,omitempty"`
	SessionOwner string    `json:"session_owner"`
	PreparedAt   time.Time `json:"prepared_at"`
}

// gitRunWorkspace deliberately ignores user Git hooks, templates and filters.
// The source repository is only read by upload-pack; all Git writes go into the
// owned room. No network remote or publication authority is configured.
func gitRunWorkspace(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_TEMPLATE_DIR=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run workspace git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func prepareRunWorkspace(params PrepareParams, root, workDir string) error {
	p := params.RunWorkspace
	if p == nil || p.HostID == "" || p.RuntimeID == "" {
		return fmt.Errorf("run workspace requires host and runtime ownership")
	}
	if !filepath.IsAbs(p.SourcePath) {
		return fmt.Errorf("run workspace source must be absolute")
	}
	real, err := filepath.EvalSymlinks(p.SourcePath)
	if err != nil {
		return fmt.Errorf("run workspace source: %w", err)
	}
	if filepath.Clean(real) != filepath.Clean(p.SourcePath) {
		return fmt.Errorf("run workspace requires a canonical source path")
	}
	if p.BaseCommit != "" {
		if len(p.BaseCommit) != 40 || strings.Trim(p.BaseCommit, "0123456789abcdef") != "" {
			return fmt.Errorf("run workspace base must be a full immutable commit ID")
		}
	}
	if rel, err := filepath.Rel(root, workDir); err != nil || rel != "workdir" {
		return fmt.Errorf("run workspace escaped its owned root")
	}
	info, err := os.Lstat(workDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("run workspace must be a real owned directory")
	}
	entries, err := os.ReadDir(workDir)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("run workspace must be empty before allocation")
	}

	receipt := RunWorkspaceReceipt{RunID: params.TaskID, IssueID: params.Task.IssueID, ProfileID: params.Task.AgentID,
		HostID: p.HostID, RuntimeID: p.RuntimeID, SourcePath: real, BaseCommit: p.BaseCommit,
		WorkDir: workDir, SessionOwner: params.TaskID, PreparedAt: time.Now().UTC()}
	// Write ownership before any Git work. A crashed or partly prepared attempt
	// must be reconciled; redelivery cannot erase its receipt or partial output.
	if p.BaseCommit != "" {
		receipt.Branch = "multica/run-" + params.TaskID
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, "run-workspace.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("run workspace ownership: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if p.BaseCommit == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err = gitRunWorkspace(ctx, workDir, "init", "--quiet"); err != nil {
		return err
	}
	if _, err = gitRunWorkspace(ctx, workDir, "fetch", "--quiet", "--no-tags", "--depth=1", "--", real, p.BaseCommit); err != nil {
		return err
	}
	if _, err = gitRunWorkspace(ctx, workDir, "checkout", "--quiet", "-b", receipt.Branch, p.BaseCommit); err != nil {
		return err
	}
	head, err := gitRunWorkspace(ctx, workDir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != p.BaseCommit {
		return fmt.Errorf("run workspace base identity mismatch")
	}
	// A tracked symlink must not turn a clean checkout into a writable route
	// outside this room. No submodules are initialized and no owner config copied.
	return filepath.WalkDir(workDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("run workspace symlink: %w", err)
		}
		rel, err := filepath.Rel(workDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("run workspace contains an escaping symlink")
		}
		return nil
	})
}
