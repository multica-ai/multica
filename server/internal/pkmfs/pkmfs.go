// Package pkmfs is a sandboxed filesystem for the Documents tab.
//
// The package wraps an operating-system root directory (the allowlist root,
// configured via MULTICA_PKM_ROOT) and serves read/write operations on
// markdown files inside the workspace's configured pkm_path. All path
// resolution goes through os.Root so traversal outside the allowlist root is
// rejected by the kernel-level sandbox in addition to our own validation.
//
// Writes are restricted to .md files, capped in size by the caller, written
// atomically (temp file → fsync → rename) and refuse to follow a symlink at
// the leaf component.
package pkmfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// MaxFileBytes is the default upper bound on a single write payload (5 MB).
// Handlers should enforce this at the request boundary so we never read more
// than this into memory.
const MaxFileBytes = 5 << 20

// MarkdownExt is the only extension accepted by Write/Create.
const MarkdownExt = ".md"

// Sentinel errors. Handlers translate these into HTTP status codes.
var (
	ErrInvalidPath = errors.New("pkmfs: invalid path")
	ErrTraversal   = errors.New("pkmfs: path escapes allowlist root")
	ErrExtension   = errors.New("pkmfs: only .md files allowed")
	ErrNotFound    = errors.New("pkmfs: not found")
	ErrExist       = errors.New("pkmfs: already exists")
	ErrNotEmpty    = errors.New("pkmfs: directory not empty")
	ErrSymlink     = errors.New("pkmfs: refusing to follow symlink")
	ErrNotMarkdown = errors.New("pkmfs: not a regular .md file")
	ErrNotFolder   = errors.New("pkmfs: not a folder")
)

// FS is a sandboxed view rooted at allowlistRoot and pointing into a
// workspace-specific subdirectory (base) within that root.
type FS struct {
	root *os.Root
	base string
}

// New opens allowlistRoot and verifies that base is a directory inside it.
// allowlistRoot must be an absolute, existing directory; base is a relative
// path under allowlistRoot. base is resolved through os.Root, so any traversal
// outside allowlistRoot is rejected here.
func New(allowlistRoot, base string) (*FS, error) {
	if allowlistRoot == "" {
		return nil, fmt.Errorf("pkmfs: empty allowlist root")
	}
	root, err := os.OpenRoot(allowlistRoot)
	if err != nil {
		return nil, fmt.Errorf("pkmfs: open allowlist root: %w", err)
	}
	cleanedBase, err := cleanRel(base)
	if err != nil {
		root.Close()
		return nil, err
	}
	if cleanedBase != "" {
		info, err := root.Stat(cleanedBase)
		if err != nil {
			root.Close()
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: pkm base %q", ErrNotFound, base)
			}
			return nil, fmt.Errorf("pkmfs: stat base: %w", err)
		}
		if !info.IsDir() {
			root.Close()
			return nil, fmt.Errorf("%w: pkm base %q", ErrNotFolder, base)
		}
	}
	return &FS{root: root, base: cleanedBase}, nil
}

// Close releases the underlying root file descriptor.
func (f *FS) Close() error {
	if f == nil || f.root == nil {
		return nil
	}
	return f.root.Close()
}

// cleanRel validates and normalizes a user-supplied relative path so it is
// safe to join against the allowlist root. Returns the cleaned path with
// forward slashes (suitable for *os.Root methods on every supported OS).
//
// Rejected: empty, NUL byte, absolute, contains a ".." component, contains a
// backslash (Windows separator — server is Linux only).
func cleanRel(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", ErrInvalidPath
	}
	if strings.ContainsRune(rel, '\\') {
		return "", ErrInvalidPath
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." || rel == "/" {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", ErrTraversal
	}
	cleaned := path.Clean(rel)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrTraversal
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", ErrTraversal
		}
		if seg == "" {
			return "", ErrInvalidPath
		}
	}
	return cleaned, nil
}

// joined returns the user-supplied relative path resolved under the workspace
// base, ready for use with *os.Root methods. The empty result refers to the
// base directory itself.
func (f *FS) joined(rel string) (string, error) {
	cleaned, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if cleaned == "" {
		return f.base, nil
	}
	if f.base == "" {
		return cleaned, nil
	}
	return f.base + "/" + cleaned, nil
}

// Stat returns Lstat info on rel without following a leaf symlink.
func (f *FS) Stat(rel string) (os.FileInfo, error) {
	full, err := f.joined(rel)
	if err != nil {
		return nil, err
	}
	if full == "" {
		full = "."
	}
	return f.root.Lstat(full)
}

// requireMarkdownLeaf rejects any path whose leaf is not a `.md` file.
func requireMarkdownLeaf(full string) error {
	leaf := path.Base(full)
	if leaf == "." || leaf == "/" || leaf == "" {
		return ErrExtension
	}
	if !strings.EqualFold(path.Ext(leaf), MarkdownExt) {
		return ErrExtension
	}
	return nil
}

// WriteFile writes data atomically to rel. Overwrites an existing regular
// file at the leaf. Refuses to follow a symlink at the leaf, and refuses any
// non-.md extension. Caller must enforce a size cap on data.
func (f *FS) WriteFile(rel string, data []byte) error {
	full, err := f.joined(rel)
	if err != nil {
		return err
	}
	if full == "" || full == f.base {
		return ErrInvalidPath
	}
	if err := requireMarkdownLeaf(full); err != nil {
		return err
	}
	// Reject leaf that is currently a symlink so we don't overwrite the
	// link target. If it doesn't exist, that's fine — Write is allowed to
	// create.
	if info, err := f.root.Lstat(full); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrSymlink
		}
		if !info.Mode().IsRegular() {
			return ErrNotMarkdown
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Verify parent exists. Without this we'd silently 500 on missing
	// directories instead of returning a clean 404 to the caller.
	if parent := path.Dir(full); parent != "." && parent != "" {
		if _, err := f.root.Stat(parent); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: parent %q", ErrNotFound, parent)
			}
			return err
		}
	}
	return f.atomicWrite(full, data)
}

// CreateFile writes data to a new file. Returns ErrExist if the leaf is
// already present (whether file, directory, or symlink). Returns ErrNotFound
// if the parent directory is missing.
func (f *FS) CreateFile(rel string, data []byte) error {
	full, err := f.joined(rel)
	if err != nil {
		return err
	}
	if full == "" || full == f.base {
		return ErrInvalidPath
	}
	if err := requireMarkdownLeaf(full); err != nil {
		return err
	}
	if parent := path.Dir(full); parent != "." && parent != "" {
		info, err := f.root.Stat(parent)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: parent %q", ErrNotFound, parent)
			}
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: parent %q", ErrNotFolder, parent)
		}
	}
	if _, err := f.root.Lstat(full); err == nil {
		return ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return f.atomicWrite(full, data)
}

// CreateFolder creates a directory at rel. Parent must exist. Returns ErrExist
// if rel is already present.
func (f *FS) CreateFolder(rel string) error {
	full, err := f.joined(rel)
	if err != nil {
		return err
	}
	if full == "" || full == f.base {
		return ErrInvalidPath
	}
	if parent := path.Dir(full); parent != "." && parent != "" {
		info, err := f.root.Stat(parent)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: parent %q", ErrNotFound, parent)
			}
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: parent %q", ErrNotFolder, parent)
		}
	}
	if _, err := f.root.Lstat(full); err == nil {
		return ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := f.root.Mkdir(full, 0o755); err != nil {
		return fmt.Errorf("pkmfs: mkdir: %w", err)
	}
	return nil
}

// DeleteFile removes a regular .md file at rel. Refuses symlinks and refuses
// non-.md leaves.
func (f *FS) DeleteFile(rel string) error {
	full, err := f.joined(rel)
	if err != nil {
		return err
	}
	if full == "" || full == f.base {
		return ErrInvalidPath
	}
	if err := requireMarkdownLeaf(full); err != nil {
		return err
	}
	info, err := f.root.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrSymlink
	}
	if !info.Mode().IsRegular() {
		return ErrNotMarkdown
	}
	if err := f.root.Remove(full); err != nil {
		return fmt.Errorf("pkmfs: remove: %w", err)
	}
	return nil
}

// DeleteFolder removes a folder at rel. When force is false the folder must
// be empty. When force is true the folder is removed recursively. The base
// directory itself cannot be deleted.
func (f *FS) DeleteFolder(rel string, force bool) error {
	full, err := f.joined(rel)
	if err != nil {
		return err
	}
	if full == "" || full == f.base {
		return ErrInvalidPath
	}
	info, err := f.root.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrSymlink
	}
	if !info.IsDir() {
		return ErrNotFolder
	}
	if !force {
		empty, err := f.isFolderEmpty(full)
		if err != nil {
			return err
		}
		if !empty {
			return ErrNotEmpty
		}
		if err := f.root.Remove(full); err != nil {
			return fmt.Errorf("pkmfs: rmdir: %w", err)
		}
		return nil
	}
	if err := f.root.RemoveAll(full); err != nil {
		return fmt.Errorf("pkmfs: rmdir -rf: %w", err)
	}
	return nil
}

func (f *FS) isFolderEmpty(full string) (bool, error) {
	dir, err := f.root.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, ErrNotFound
		}
		return false, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return len(names) == 0, nil
}

// atomicWrite writes to a sibling temp file in the same directory, fsyncs the
// data, then renames into place. Both file creation and rename go through
// *os.Root, so neither can escape the allowlist root.
func (f *FS) atomicWrite(full string, data []byte) error {
	dir := path.Dir(full)
	leaf := path.Base(full)
	if dir == "." {
		dir = ""
	}
	// Build a unique temp name in the same directory so rename is atomic
	// (same filesystem). Hidden by leading dot and namespaced by leaf so
	// concurrent writes to different files don't collide.
	tmpLeaf := fmt.Sprintf(".tmp-%s.%d", leaf, os.Getpid())
	for i := 0; i < 32; i++ {
		candidate := fmt.Sprintf("%s.%d", tmpLeaf, i)
		full := joinDir(dir, candidate)
		if _, err := f.root.Lstat(full); errors.Is(err, os.ErrNotExist) {
			tmpLeaf = candidate
			break
		}
	}
	tmpPath := joinDir(dir, tmpLeaf)
	// O_EXCL guards against an attacker pre-creating the temp name as a
	// symlink and tricking us into writing through it.
	tf, err := f.root.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("pkmfs: create temp: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			f.root.Remove(tmpPath)
		}
	}()
	if _, err := tf.Write(data); err != nil {
		tf.Close()
		return fmt.Errorf("pkmfs: write temp: %w", err)
	}
	if err := tf.Sync(); err != nil {
		tf.Close()
		return fmt.Errorf("pkmfs: fsync temp: %w", err)
	}
	if err := tf.Close(); err != nil {
		return fmt.Errorf("pkmfs: close temp: %w", err)
	}
	if err := f.root.Rename(tmpPath, full); err != nil {
		return fmt.Errorf("pkmfs: rename: %w", err)
	}
	cleanup = false
	return nil
}

func joinDir(dir, leaf string) string {
	if dir == "" {
		return leaf
	}
	return dir + "/" + leaf
}
