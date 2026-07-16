package repocache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type worktreeHandle struct {
	worktree pathHandle
	gitDir   pathHandle
	common   pathHandle
}

type bareCacheHandle struct {
	identity pathHandle
}

func (h *bareCacheHandle) Path() string {
	if h == nil {
		return ""
	}
	return h.identity.path
}

func (h *bareCacheHandle) RecheckPath() error {
	if h == nil {
		return fmt.Errorf("bare repository cache identity is unavailable")
	}
	return h.identity.recheck("bare repository cache")
}

func (h *bareCacheHandle) Close() error {
	if h == nil || h.identity.file == nil {
		return nil
	}
	err := h.identity.file.Close()
	h.identity.file = nil
	return err
}

func (h *worktreeHandle) Path() string {
	return h.worktree.path
}

func (h *worktreeHandle) Close() error {
	if h == nil {
		return nil
	}
	var first error
	for _, identity := range []*pathHandle{&h.worktree, &h.gitDir, &h.common} {
		if identity.file != nil {
			if err := identity.file.Close(); err != nil && first == nil {
				first = err
			}
			identity.file = nil
		}
	}
	return first
}

type pathHandle struct {
	path string
	file *os.File
	info os.FileInfo
}

func (p *pathHandle) recheck(kind string) error {
	if p == nil || p.file == nil || p.info == nil {
		return fmt.Errorf("%s identity is unavailable", kind)
	}
	opened, err := p.file.Stat()
	if err != nil || !os.SameFile(p.info, opened) || opened.Mode() != p.info.Mode() {
		return fmt.Errorf("%s descriptor identity changed: %s", kind, p.path)
	}
	info, err := os.Lstat(p.path)
	if err != nil {
		return fmt.Errorf("recheck %s identity %q: %w", kind, p.path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(p.info, info) {
		return fmt.Errorf("%s path changed after ownership proof: %s", kind, p.path)
	}
	return nil
}

func (h *worktreeHandle) RecheckPaths() error {
	if h == nil {
		return fmt.Errorf("worktree identity is unavailable")
	}
	for _, check := range []struct {
		kind     string
		identity *pathHandle
	}{
		{"worktree", &h.worktree},
		{"worktree git dir", &h.gitDir},
		{"git common dir", &h.common},
	} {
		if err := check.identity.recheck(check.kind); err != nil {
			return err
		}
	}
	return nil
}

func (h *worktreeHandle) VerifyGitDirBacklink() error {
	if h == nil || h.worktree.file == nil || h.worktree.info == nil || h.gitDir.file == nil {
		return fmt.Errorf("worktree identity is unavailable")
	}
	contents, err := readFileAt(h.gitDir.file, "gitdir")
	if err != nil {
		return fmt.Errorf("read worktree git-dir backlink: %w", err)
	}
	gitFile := strings.TrimSpace(string(contents))
	if !filepath.IsAbs(gitFile) || filepath.Base(gitFile) != ".git" || filepath.Clean(gitFile) != gitFile {
		return fmt.Errorf("worktree git-dir backlink is invalid: %q", gitFile)
	}
	info, err := os.Stat(filepath.Dir(gitFile))
	if err != nil {
		return fmt.Errorf("stat worktree git-dir backlink owner: %w", err)
	}
	if !info.IsDir() || !os.SameFile(h.worktree.info, info) {
		return fmt.Errorf("worktree git-dir backlink identifies a different checkout: %s", gitFile)
	}
	return nil
}
