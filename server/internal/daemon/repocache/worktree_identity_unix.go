//go:build linux || darwin

package repocache

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const internalGitWorktreeMode = "__multica_internal_git_worktree_fd_v1"
const internalGitBareCacheMode = "__multica_internal_git_bare_cache_fd_v1"

func identityBoundWorktreeAccessSupported() bool {
	return true
}

func init() {
	if len(os.Args) >= 5 && os.Args[1] == internalGitBareCacheMode {
		if err := verifyDirectoryFDPath(3, os.Args[2]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "verify bare repository cache identity: %v\n", err)
			os.Exit(126)
		}
		if err := unix.Fchdir(3); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "bind bare repository cache cwd: %v\n", err)
			os.Exit(126)
		}
		boundCache, err := os.Getwd()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "resolve bound bare repository cache cwd: %v\n", err)
			os.Exit(126)
		}
		unix.CloseOnExec(3)
		execTrustedGit(os.Args[3], os.Args[4:], boundCache)
	}
	if len(os.Args) < 6 || os.Args[1] != internalGitWorktreeMode {
		return
	}
	for _, identity := range []struct {
		fd   int
		path string
		kind string
	}{
		{3, os.Args[2], "worktree"},
		{4, os.Args[3], "worktree git dir"},
		{5, os.Args[4], "git common dir"},
	} {
		if err := verifyDirectoryFDPath(identity.fd, identity.path); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "verify %s identity: %v\n", identity.kind, err)
			os.Exit(126)
		}
	}
	if err := unix.Fchdir(3); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bind git worktree cwd: %v\n", err)
		os.Exit(126)
	}
	boundWorktree, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "resolve bound git worktree cwd: %v\n", err)
		os.Exit(126)
	}
	for fd := 3; fd <= 5; fd++ {
		unix.CloseOnExec(fd)
	}
	execTrustedGit(os.Args[5], os.Args[6:], boundWorktree)
}

func execTrustedGit(gitPath string, args []string, boundPWD string) {
	if !filepath.IsAbs(gitPath) {
		_, _ = fmt.Fprintln(os.Stderr, "trusted git path is not absolute")
		os.Exit(126)
	}
	resolvedGitPath, err := filepath.EvalSymlinks(gitPath)
	if err != nil || resolvedGitPath != gitPath {
		_, _ = fmt.Fprintln(os.Stderr, "trusted git path identity changed")
		os.Exit(126)
	}
	info, err := os.Stat(gitPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		_, _ = fmt.Fprintln(os.Stderr, "trusted git path is not an immutable executable file")
		os.Exit(126)
	}
	env := replaceEnv(os.Environ(), "PWD", boundPWD)
	if err := unix.Exec(gitPath, append([]string{gitPath}, args...), env); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "exec trusted git: %v\n", err)
		os.Exit(126)
	}
}

func openBareCacheHandle(path string) (*bareCacheHandle, error) {
	identity, err := openDirectoryHandle(path)
	if err != nil {
		return nil, err
	}
	handle := &bareCacheHandle{identity: identity}
	if err := handle.RecheckPath(); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

func verifyDirectoryFDPath(fd int, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("path must be absolute and canonical: %q", path)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return err
	}
	var current unix.Stat_t
	if err := unix.Lstat(path, &current); err != nil {
		return err
	}
	if opened.Dev != current.Dev || opened.Ino != current.Ino || opened.Mode != current.Mode || current.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("descriptor and path identify different directories: %s", path)
	}
	return nil
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func openWorktreeHandle(path string) (*worktreeHandle, error) {
	identity, err := openDirectoryHandle(path)
	if err != nil {
		return nil, err
	}
	handle := &worktreeHandle{worktree: identity}
	if err := handle.worktree.recheck("worktree"); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

func openDirectoryHandle(path string) (pathHandle, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return pathHandle{}, fmt.Errorf("directory path must be absolute and canonical: %q", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return pathHandle{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return pathHandle{}, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return pathHandle{}, fmt.Errorf("path is not a directory: %s", path)
	}
	return pathHandle{path: path, file: file, info: info}, nil
}

func (g *gitBroker) worktreeCommand(ctx context.Context, handle *worktreeHandle, args ...string) (*exec.Cmd, error) {
	if handle == nil || handle.worktree.file == nil {
		return nil, fmt.Errorf("worktree identity is required")
	}
	launcher, err := worktreeLauncherExecutable()
	if err != nil {
		return nil, err
	}
	if err := handle.RecheckPaths(); err != nil {
		return nil, err
	}
	cmdArgs := append([]string{
		internalGitWorktreeMode,
		handle.worktree.path,
		handle.gitDir.path,
		handle.common.path,
		g.executable,
	}, args...)
	cmd := exec.CommandContext(ctx, launcher, cmdArgs...)
	cmd.Env = append([]string(nil), g.env...)
	cmd.Env = replaceEnv(cmd.Env, "GIT_CONFIG", "/dev/null")
	cmd.ExtraFiles = []*os.File{handle.worktree.file, handle.gitDir.file, handle.common.file}
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

func (g *gitBroker) bareCacheCommand(ctx context.Context, handle *bareCacheHandle, args ...string) (*exec.Cmd, error) {
	if handle == nil || handle.identity.file == nil {
		return nil, fmt.Errorf("bare repository cache identity is required")
	}
	launcher, err := worktreeLauncherExecutable()
	if err != nil {
		return nil, err
	}
	if err := handle.RecheckPath(); err != nil {
		return nil, err
	}
	cmdArgs := append([]string{internalGitBareCacheMode, handle.Path(), g.executable}, args...)
	cmd := exec.CommandContext(ctx, launcher, cmdArgs...)
	cmd.Env = append([]string(nil), g.env...)
	cmd.ExtraFiles = []*os.File{handle.identity.file}
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

func worktreeLauncherExecutable() (string, error) {
	if runtime.GOOS == "linux" {
		// /proc/self/exe is resolved by the kernel against the forked child, so
		// the launcher cannot be redirected by replacing the daemon's path.
		const procSelfExecutable = "/proc/self/exe"
		info, err := os.Stat(procSelfExecutable)
		if err != nil {
			return "", fmt.Errorf("resolve kernel-bound worktree git launcher: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("kernel-bound worktree git launcher is not an executable regular file")
		}
		return procSelfExecutable, nil
	}

	// Darwin has no procfs executable descriptor suitable for exec. Product
	// task execution remains disabled there; operator-only repository work gets
	// a path identity check immediately before exec construction.
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve worktree git launcher: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("resolve worktree git launcher identity: %w", err)
	}
	info, err := os.Lstat(self)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("worktree git launcher is not an executable regular file: %s", self)
	}
	return self, nil
}

func readFileAt(dir *os.File, name string) ([]byte, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	return io.ReadAll(file)
}

func validateTrustedExecutablePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat trusted executable %q: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("git executable is not owned by root: %s", path)
	}
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("stat trusted executable parent %q: %w", current, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("git executable parent is not a root-owned system trust anchor: %s", current)
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

func validateRepositoryMetadataIdentity(identity *pathHandle) error {
	if err := identity.recheck("repository metadata"); err != nil {
		return err
	}
	return validateDaemonOwnedDirectoryPath(identity.path, "repository metadata directory")
}

func validateDaemonOwnedDirectoryPath(path, kind string) error {
	return validateDaemonOwnedDirectoryPathForUID(path, kind, os.Geteuid())
}

func validateDaemonOwnedDirectoryPathForUID(path, kind string, daemonUID int) error {
	canonical, err := canonicalExistingDir(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", kind, err)
	}
	for current := canonical; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("stat %s %q: %w", kind, current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a canonical directory: %s", kind, current)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("read %s owner identity: %s", kind, current)
		}
		if current == canonical {
			if int(stat.Uid) != daemonUID {
				return fmt.Errorf("%s is not owned by daemon effective UID %d: %s", kind, daemonUID, current)
			}
		} else if stat.Uid != 0 && int(stat.Uid) != daemonUID {
			return fmt.Errorf("%s parent is not owned by root or daemon effective UID %d: %s", kind, daemonUID, current)
		}
		writable := info.Mode().Perm()&0o022 != 0
		rootOwnedStickyParent := current != canonical && stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
		if writable && !rootOwnedStickyParent {
			return fmt.Errorf("%s is group/world writable: %s", kind, current)
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

func excludeFromGitDirHandle(handle *worktreeHandle, pattern string) error {
	if err := handle.RecheckPaths(); err != nil {
		return err
	}
	infoFD, err := ensureDirectoryAt(handle.gitDir.file, "info", 0o755)
	if err != nil {
		return fmt.Errorf("create info dir: %w", err)
	}
	defer unix.Close(infoFD)
	existing, err := readFileAtFD(infoFD, "exclude")
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read exclude file: %w", err)
	}
	if strings.Contains(string(existing), pattern) {
		return handle.RecheckPaths()
	}
	fd, err := unix.Openat(infoFD, "exclude", unix.O_APPEND|unix.O_CREAT|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o644)
	if err != nil {
		return fmt.Errorf("open exclude file: %w", err)
	}
	file := os.NewFile(uintptr(fd), "exclude")
	_, writeErr := fmt.Fprintf(file, "\n%s\n", pattern)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write exclude pattern: %w", writeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return handle.RecheckPaths()
}

func installCoAuthoredByHookInCommonDirHandle(handle *worktreeHandle) error {
	if err := handle.RecheckPaths(); err != nil {
		return err
	}
	hooksFD, err := ensureDirectoryAt(handle.common.file, "hooks", 0o755)
	if err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	defer unix.Close(hooksFD)
	if err := writeFileAtFD(hooksFD, "prepare-commit-msg", []byte(prepareCommitMsgHook), 0o755); err != nil {
		return fmt.Errorf("write prepare-commit-msg hook: %w", err)
	}
	return handle.RecheckPaths()
}

func removeCoAuthoredByHookInCommonDirHandle(handle *worktreeHandle) error {
	if err := handle.RecheckPaths(); err != nil {
		return err
	}
	hooksFD, err := unix.Openat(int(handle.common.file.Fd()), "hooks", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if err == unix.ENOENT {
			return nil
		}
		return fmt.Errorf("open hooks dir: %w", err)
	}
	defer unix.Close(hooksFD)
	contents, err := readFileAtFD(hooksFD, "prepare-commit-msg")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read prepare-commit-msg hook: %w", err)
	}
	if isDaemonInstalledHook(contents) {
		if err := unix.Unlinkat(hooksFD, "prepare-commit-msg", 0); err != nil && err != unix.ENOENT {
			return fmt.Errorf("remove prepare-commit-msg hook: %w", err)
		}
	}
	return handle.RecheckPaths()
}

func ensureDirectoryAt(parent *os.File, name string, mode uint32) (int, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err == nil {
		return fd, nil
	}
	if err != unix.ENOENT {
		return -1, err
	}
	if err := unix.Mkdirat(int(parent.Fd()), name, mode); err != nil && err != unix.EEXIST {
		return -1, err
	}
	return unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func readFileAtFD(parentFD int, name string) ([]byte, error) {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	return io.ReadAll(file)
}

func writeFileAtFD(parentFD int, name string, contents []byte, mode uint32) error {
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
