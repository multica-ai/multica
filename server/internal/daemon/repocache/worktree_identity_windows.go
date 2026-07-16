//go:build windows

package repocache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type worktreePublication struct {
	stagingPath string
	finalPath   string
}

func newWorktreePublication(_ string, finalPath string) (*worktreePublication, error) {
	return &worktreePublication{stagingPath: finalPath, finalPath: finalPath}, nil
}

func (p *worktreePublication) StagingPath() string            { return p.stagingPath }
func (p *worktreePublication) Prepare(*worktreeHandle) error  { return nil }
func (p *worktreePublication) Publish(*worktreeHandle) error  { return nil }
func (p *worktreePublication) Commit()                        {}
func (p *worktreePublication) Rollback(*worktreeHandle) error { return nil }
func (p *worktreePublication) Close() error                   { return nil }
func cleanupOwnedWorktreeHandle(*worktreeHandle) error        { return nil }
func removeDirectoryContentsAtFDWithHook(int, func(int, string)) error {
	return fmt.Errorf("descriptor-relative cleanup is unsupported on windows")
}

func identityBoundWorktreeAccessSupported() bool {
	return false
}

func openWorktreeHandle(string) (*worktreeHandle, error) {
	return nil, fmt.Errorf("identity-bound existing worktree updates are unsupported on windows")
}

func openBareCacheHandle(path string) (*bareCacheHandle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}
	return &bareCacheHandle{identity: pathHandle{path: path, file: file, info: info}}, nil
}

func openDirectoryHandle(string) (pathHandle, error) {
	return pathHandle{}, fmt.Errorf("identity-bound directory access is unsupported on windows")
}

func (g *gitBroker) worktreeCommand(context.Context, *worktreeHandle, ...string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("identity-bound existing worktree updates are unsupported on windows")
}

func (g *gitBroker) bareCacheCommand(ctx context.Context, handle *bareCacheHandle, args ...string) (*exec.Cmd, error) {
	if err := handle.RecheckPath(); err != nil {
		return nil, err
	}
	cmdArgs := append([]string{"-C", handle.Path()}, args...)
	cmd := exec.CommandContext(ctx, g.executable, cmdArgs...)
	cmd.Env = append([]string(nil), g.env...)
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

func worktreeLauncherExecutable() (string, error) {
	return "", fmt.Errorf("identity-bound existing worktree updates are unsupported on windows")
}

func readFileAt(*os.File, string) ([]byte, error) {
	return nil, fmt.Errorf("identity-bound metadata access is unsupported on windows")
}

func validateTrustedExecutablePath(string) error {
	return nil
}

func validateRepositoryMetadataIdentity(*pathHandle) error {
	return fmt.Errorf("identity-bound repository metadata is unsupported on windows")
}

func validateDaemonOwnedDirectoryPath(string, string) error {
	// Windows product task execution is not advertised. Preserve the existing
	// operator repo-cache behavior while reused worktree access remains
	// explicitly fail closed in openWorktreeHandle.
	return nil
}

func validateDaemonOwnedDirectoryPathForUID(string, string, int) error {
	return nil
}

func excludeFromGitDirHandle(*worktreeHandle, string) error {
	return fmt.Errorf("identity-bound repository metadata is unsupported on windows")
}

func installCoAuthoredByHookInCommonDirHandle(*worktreeHandle) error {
	return fmt.Errorf("identity-bound repository metadata is unsupported on windows")
}

func removeCoAuthoredByHookInCommonDirHandle(*worktreeHandle) error {
	return fmt.Errorf("identity-bound repository metadata is unsupported on windows")
}
