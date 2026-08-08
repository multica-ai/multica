package execenv

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// sidecarManifestFile is the on-disk JSON Prepare writes into envRoot to
// record every file and intermediate directory it created inside WorkDir.
// CleanupSidecars reads it back to roll the workdir to its pre-Prepare
// state. The file lives in envRoot (daemon scratch), never in WorkDir,
// so a local_directory run does not litter the user's repo with the
// bookkeeping file used to undo the litter.
const sidecarManifestFile = ".multica_sidecar_manifest.json"

const sidecarDirectoryOwnerFile = ".multica-sidecar-owner"

// errPathPreExists is the sentinel recordWriteFile returns when the
// target path already exists. The manifest contract is that we never
// mutate paths we don't own: a pre-existing file belongs to the user
// (or to stale state from a crashed prior run we cannot safely
// distinguish from intentional user content) and the write must be
// refused so cleanup can be a pure deletion of paths we created.
//
// Callers handle this in one of two ways:
//
//   - For per-skill directories the caller allocates a collision-free
//     alternative slug (see allocateCollisionFreeSkillDir) and retries
//     so the agent still discovers the Multica skill, just under a
//     different directory name.
//   - For Multica-only namespaces (.agent_context/issue_context.md,
//     .multica/project/resources.json) the caller swallows the error
//     and proceeds — the agent's runtime brief already carries every
//     fact that would have appeared in those files, so missing-from-
//     disk is degraded behavior, not failure.
var errPathPreExists = errors.New("execenv: refuse to overwrite pre-existing path")

var errSidecarOwnershipMismatch = errors.New("execenv: sidecar ownership mismatch")

// sidecarRaceTestHooks expose the exact filesystem race boundaries that must
// stay deterministic in regression tests. Production never installs hooks.
// The immutable pointer keeps -race runs safe even when unrelated tests run in
// parallel; hook callbacks filter on their temporary root-relative paths.
type sidecarRaceTestHooks struct {
	beforeDetach func(operation, rel string)
	afterLink    func(operation, rel string)
	beforeRecord func(operation, rel string)
}

var sidecarRaceHooks atomic.Pointer[sidecarRaceTestHooks]

func runBeforeSidecarDetach(operation, rel string) {
	if hooks := sidecarRaceHooks.Load(); hooks != nil && hooks.beforeDetach != nil {
		hooks.beforeDetach(operation, rel)
	}
}

func runAfterSidecarLink(operation, rel string) {
	if hooks := sidecarRaceHooks.Load(); hooks != nil && hooks.afterLink != nil {
		hooks.afterLink(operation, rel)
	}
}

func runBeforeSidecarRecord(operation, rel string) {
	if hooks := sidecarRaceHooks.Load(); hooks != nil && hooks.beforeRecord != nil {
		hooks.beforeRecord(operation, rel)
	}
}

// sidecarManifest records the filesystem mutations writeContextFiles and
// its callees make inside the agent's WorkDir for a single task. The
// manifest is the second half of the contract that makes local_directory
// runs byte-exactly reversible:
//
//   - Files lists regular files we created. Legacy manifests use absolute
//     paths; Platform manifests set Rooted and use names relative to the
//     trusted WorkDir. Files are recorded only after recordWriteFile has
//     verified the target did NOT pre-exist, so the manifest's existence
//     rule and the write side's refuse-to-clobber rule are the same invariant
//     viewed from two sides.
//   - Dirs follows the same legacy-absolute/root-confined-relative convention
//     and records directories in root-first creation order. Cleanup walks the
//     list in reverse so deepest dirs
//     get tried first; rmdir of a directory the user has populated since
//     (e.g. .claude/skills/my-own-skill alongside our .claude/skills/
//     issue-review) fails ENOTEMPTY and is skipped silently — the
//     user's content is preserved without any per-dir bookkeeping. A
//     directory is recorded only when it did NOT pre-exist for the same
//     reason files are conditional.
//
// The manifest is intentionally minimal: it carries the paths needed to
// reverse our writes and nothing else. It is not a log of every operation
// and is not a substitute for the runtime config marker block, which has
// its own dedicated round-trip mechanism in runtime_config.go (the brief
// is appended to user-owned content rather than written into a new sidecar
// directory).
type sidecarManifest struct {
	Rooted    bool                        `json:"rooted,omitempty"`
	Files     []string                    `json:"files,omitempty"`
	Dirs      []string                    `json:"dirs,omitempty"`
	Ownership map[string]sidecarOwnership `json:"ownership,omitempty"`

	rootPath string
	root     *os.Root
	preimage map[string]sidecarFilePreimage
}

type sidecarFileSnapshot struct {
	data     []byte
	mode     os.FileMode
	identity os.FileInfo
}

type sidecarFilePreimage struct {
	data        []byte
	mode        os.FileMode
	replacement os.FileInfo
	ownership   sidecarOwnership
}

type sidecarOwnership struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

func fileSidecarOwnership(data []byte) sidecarOwnership {
	sum := sha256.Sum256(data)
	return sidecarOwnership{Kind: "file", SHA256: fmt.Sprintf("%x", sum[:])}
}

func directorySidecarOwnership(token []byte) sidecarOwnership {
	sum := sha256.Sum256(token)
	return sidecarOwnership{Kind: "dir", SHA256: fmt.Sprintf("%x", sum[:])}
}

func (m *sidecarManifest) recordOwnership(rel string, ownership sidecarOwnership) {
	if m.Ownership == nil {
		m.Ownership = make(map[string]sidecarOwnership)
	}
	m.Ownership[filepath.Clean(rel)] = ownership
}

func (m *sidecarManifest) ownershipFor(rel, kind string) (sidecarOwnership, error) {
	if m == nil {
		return sidecarOwnership{}, errors.New("sidecar manifest is required")
	}
	ownership, ok := m.Ownership[filepath.Clean(rel)]
	if !ok || ownership.Kind != kind || ownership.SHA256 == "" {
		return sidecarOwnership{}, fmt.Errorf("%w: %s has no persisted %s ownership", errSidecarOwnershipMismatch, rel, kind)
	}
	return ownership, nil
}

func (m *sidecarManifest) bindRoot(rootPath string) error {
	if m == nil {
		return errors.New("root-confined sidecar manifest is required")
	}
	cleanRoot := filepath.Clean(rootPath)
	if m.root != nil {
		if m.rootPath != cleanRoot {
			return fmt.Errorf("sidecar manifest already bound to %s", m.rootPath)
		}
		return nil
	}
	root, err := openFixedSidecarRoot(cleanRoot)
	if err != nil {
		return fmt.Errorf("open sidecar root %s: %w", cleanRoot, err)
	}
	m.Rooted = true
	m.rootPath = cleanRoot
	m.root = root
	return nil
}

func openFixedSidecarRoot(rootPath string) (*os.Root, error) {
	expected, err := os.Lstat(rootPath)
	if err != nil {
		return nil, err
	}
	if expected.Mode()&os.ModeSymlink != 0 || !expected.IsDir() {
		return nil, fmt.Errorf("sidecar root must be a real directory: %s", rootPath)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if !os.SameFile(expected, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("sidecar root changed while opening: %s", rootPath)
	}
	return root, nil
}

func openFixedSidecarChild(parent *os.Root, name string, expected os.FileInfo) (*os.Root, error) {
	if expected.Mode()&os.ModeSymlink != 0 || !expected.IsDir() {
		return nil, fmt.Errorf("sidecar directory must be a real directory: %s", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		_ = child.Close()
		return nil, err
	}
	if !os.SameFile(expected, opened) {
		_ = child.Close()
		return nil, fmt.Errorf("sidecar directory changed while opening: %s", name)
	}
	return child, nil
}

// openFixedSidecarParent resolves every parent component through a separately
// verified directory handle. os.Root confines paths to its tree, but it still
// follows symlinks that stay inside that tree; passing a multi-component name
// directly would therefore let an in-workdir parent swap redirect a delete or
// truncate. The returned handle is owned by the caller.
func openFixedSidecarParent(root *os.Root, rel string) (*os.Root, string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("invalid rooted sidecar path %q", rel)
	}
	leaf := filepath.Base(clean)
	parentPath := filepath.Dir(clean)
	rootInfo, err := root.Stat(".")
	if err != nil {
		return nil, "", err
	}
	current, err := openFixedSidecarChild(root, ".", rootInfo)
	if err != nil {
		return nil, "", err
	}
	if parentPath == "." {
		return current, leaf, nil
	}
	for _, part := range strings.Split(parentPath, string(filepath.Separator)) {
		expected, err := current.Lstat(part)
		if err != nil {
			_ = current.Close()
			return nil, "", err
		}
		next, err := openFixedSidecarChild(current, part, expected)
		_ = current.Close()
		if err != nil {
			return nil, "", err
		}
		current = next
	}
	return current, leaf, nil
}

func readFixedSidecarFile(parent *os.Root, name string) (*sidecarFileSnapshot, error) {
	expected, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if expected.Mode()&os.ModeSymlink != 0 || !expected.Mode().IsRegular() {
		return nil, fmt.Errorf("sidecar must be a real file: %s", name)
	}
	file, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(expected, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("sidecar changed while opening: %s", name)
	}
	data, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	current, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(expected, current) {
		return nil, fmt.Errorf("sidecar changed while reading: %s", name)
	}
	return &sidecarFileSnapshot{data: data, mode: expected.Mode(), identity: expected}, nil
}

func randomSidecarTempName(name string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(".%s.tmp-%x", name, random[:]), nil
}

type fixedDetachedSidecar struct {
	name string
	info os.FileInfo
}

func removeFixedLeafWithIdentity(parent *os.Root, name string, expected os.FileInfo) error {
	current, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if expected == nil || !os.SameFile(expected, current) {
		return fmt.Errorf("%w: refuse to remove changed temporary sidecar %s", errSidecarOwnershipMismatch, name)
	}
	return parent.Remove(name)
}

func restoreDetachedFixedSidecar(parent *os.Root, original string, detached *fixedDetachedSidecar) error {
	if detached == nil || detached.info == nil {
		return errors.New("detached sidecar identity is required")
	}
	if detached.info.IsDir() {
		return fmt.Errorf("%w: replacement directory preserved at %s", errSidecarOwnershipMismatch, detached.name)
	}
	if err := parent.Link(detached.name, original); err != nil {
		return fmt.Errorf("%w: replacement preserved at %s; no-clobber restore failed: %v", errSidecarOwnershipMismatch, detached.name, err)
	}
	restored, err := parent.Lstat(original)
	if err != nil || !os.SameFile(detached.info, restored) {
		return fmt.Errorf("%w: restored replacement identity changed; preserved at %s", errSidecarOwnershipMismatch, detached.name)
	}
	if err := removeFixedLeafWithIdentity(parent, detached.name, detached.info); err != nil {
		return fmt.Errorf("%w: restored replacement but quarantine cleanup failed: %v", errSidecarOwnershipMismatch, err)
	}
	return nil
}

// detachFixedSidecar atomically moves a leaf to an unpredictable sibling name
// through the already-open parent handle. The identity is checked only after
// the rename: a replacement installed between Lstat and Rename is therefore
// quarantined rather than deleted. Non-directory replacements are restored
// with a no-clobber hard link; directories stay quarantined because portable
// Go has no rename-noreplace operation for directories.
func detachFixedSidecar(parent *os.Root, name, operation, rel string, expected os.FileInfo) (*fixedDetachedSidecar, error) {
	observed, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	quarantine, err := randomSidecarTempName(name + ".quarantine")
	if err != nil {
		return nil, err
	}
	runBeforeSidecarDetach(operation, rel)
	if err := parent.Rename(name, quarantine); err != nil {
		return nil, err
	}
	detachedInfo, err := parent.Lstat(quarantine)
	if err != nil {
		return nil, fmt.Errorf("stat detached sidecar %s: %w", rel, err)
	}
	detached := &fixedDetachedSidecar{name: quarantine, info: detachedInfo}
	if !os.SameFile(observed, detachedInfo) || (expected != nil && !os.SameFile(expected, detachedInfo)) {
		restoreErr := restoreDetachedFixedSidecar(parent, name, detached)
		if restoreErr != nil {
			return nil, restoreErr
		}
		return nil, fmt.Errorf("%w: %s changed before detach", errSidecarOwnershipMismatch, rel)
	}
	return detached, nil
}

func fixedFileMatchesOwnership(parent *os.Root, name string, expected sidecarOwnership) error {
	if expected.Kind != "file" || expected.SHA256 == "" {
		return fmt.Errorf("%w: invalid file ownership for %s", errSidecarOwnershipMismatch, name)
	}
	snapshot, err := readFixedSidecarFile(parent, name)
	if err != nil {
		return err
	}
	if fileSidecarOwnership(snapshot.data) != expected {
		return fmt.Errorf("%w: file digest changed for %s", errSidecarOwnershipMismatch, name)
	}
	return nil
}

func writeFixedSidecarTemp(parent *os.Root, name string, data []byte, perm os.FileMode) (string, os.FileInfo, error) {
	tempName, err := randomSidecarTempName(name)
	if err != nil {
		return "", nil, fmt.Errorf("generate temporary sidecar name: %w", err)
	}
	temp, err := parent.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return "", nil, fmt.Errorf("create temporary sidecar: %w", err)
	}
	tempInfo, statErr := temp.Stat()
	if statErr != nil {
		_ = temp.Close()
		return "", nil, statErr
	}
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = removeFixedLeafWithIdentity(parent, tempName, tempInfo)
		}
	}()
	if err := temp.Chmod(perm); err != nil {
		return "", nil, err
	}
	if _, err := temp.Write(data); err != nil {
		return "", nil, err
	}
	if err := temp.Sync(); err != nil {
		return "", nil, err
	}
	if err := temp.Close(); err != nil {
		return "", nil, err
	}
	info, err := parent.Lstat(tempName)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(tempInfo, info) {
		return "", nil, fmt.Errorf("temporary sidecar changed type: %s", tempName)
	}
	ok = true
	return tempName, info, nil
}

func publishFixedSidecarNoClobber(parent *os.Root, name string, data []byte, perm os.FileMode, operation, rel string) (os.FileInfo, error) {
	tempName, tempInfo, err := writeFixedSidecarTemp(parent, name, data, perm)
	if err != nil {
		return nil, err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = removeFixedLeafWithIdentity(parent, tempName, tempInfo)
		}
	}()
	if err := parent.Link(tempName, name); err != nil {
		if _, statErr := parent.Lstat(name); statErr == nil {
			return nil, fmt.Errorf("%w: %s", errPathPreExists, name)
		}
		return nil, err
	}
	runAfterSidecarLink(operation, rel)
	published, err := parent.Lstat(name)
	if err != nil || !os.SameFile(tempInfo, published) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: published sidecar identity changed: %s", errSidecarOwnershipMismatch, rel)
	}
	if err := removeFixedLeafWithIdentity(parent, tempName, tempInfo); err != nil {
		return nil, fmt.Errorf("remove temporary sidecar after publish: %w", err)
	}
	removeTemp = false
	return published, nil
}

func atomicWriteFixedSidecarNoClobber(parent *os.Root, name string, data []byte, perm os.FileMode) error {
	_, err := publishFixedSidecarNoClobber(parent, name, data, perm, "publish-manifest", name)
	return err
}

func atomicReplaceFixedSidecar(parent *os.Root, name string, expected os.FileInfo, expectedOwnership sidecarOwnership, data []byte, perm os.FileMode, operation, rel string) (os.FileInfo, error) {
	detached, err := detachFixedSidecar(parent, name, operation, rel, expected)
	if err != nil {
		return nil, err
	}
	restore := func(cause error) (os.FileInfo, error) {
		if restoreErr := restoreDetachedFixedSidecar(parent, name, detached); restoreErr != nil {
			return nil, errors.Join(cause, restoreErr)
		}
		return nil, cause
	}
	if detached.info.Mode()&os.ModeSymlink != 0 || !detached.info.Mode().IsRegular() {
		return restore(fmt.Errorf("%w: replacement source is not a real file: %s", errSidecarOwnershipMismatch, rel))
	}
	if err := fixedFileMatchesOwnership(parent, detached.name, expectedOwnership); err != nil {
		return restore(err)
	}
	replacement, err := publishFixedSidecarNoClobber(parent, name, data, perm, "replace-publish", rel)
	if err != nil {
		return restore(err)
	}
	if err := removeFixedLeafWithIdentity(parent, detached.name, detached.info); err != nil {
		return nil, fmt.Errorf("remove replaced sidecar quarantine: %w", err)
	}
	return replacement, nil
}

func (m *sidecarManifest) closeRoot() error {
	if m == nil || m.root == nil {
		return nil
	}
	err := m.root.Close()
	m.root = nil
	return err
}

// recordMkdirAll behaves like os.MkdirAll(path, perm) but additionally
// records every parent directory it had to create (skipping any that
// already existed) into m so CleanupSidecars can rmdir them later. The
// recorded paths are appended in root-first order; Cleanup iterates in
// reverse so the deepest directory is removed first.
//
// When m is nil this is identical to os.MkdirAll — the Reuse path uses
// the nil mode because Reuse runs on cloud workdirs that the GC loop
// wipes wholesale, so per-file cleanup is irrelevant and tracking the
// dirs would just leave stale manifest bytes around.
func recordMkdirAll(path string, perm os.FileMode, m *sidecarManifest) error {
	if path == "" {
		return os.MkdirAll(path, perm)
	}
	if m == nil {
		return os.MkdirAll(path, perm)
	}
	if m.Rooted {
		if m.root == nil {
			return errors.New("root-confined sidecar manifest is not bound")
		}
		rel, err := pathRelativeToRoot(m.rootPath, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
		rootInfo, err := m.root.Stat(".")
		if err != nil {
			return err
		}
		current, err := openFixedSidecarChild(m.root, ".", rootInfo)
		if err != nil {
			return err
		}
		defer func() { _ = current.Close() }()
		prefix := ""
		for _, part := range parts {
			if prefix == "" {
				prefix = part
			} else {
				prefix = filepath.Join(prefix, part)
			}
			info, statErr := current.Lstat(part)
			created := false
			switch {
			case statErr == nil:
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return fmt.Errorf("sidecar directory must be a real directory: %s", prefix)
				}
			case errors.Is(statErr, fs.ErrNotExist):
				if err := current.Mkdir(part, perm); err != nil {
					// Another writer may have created the directory after our
					// Lstat. Accept that race only when the winner created the
					// same real directory shape we require; symlinks and files
					// remain hard failures.
					info, statErr = current.Lstat(part)
					if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
						return err
					}
				} else {
					created = true
					info, statErr = current.Lstat(part)
					if statErr != nil {
						return fmt.Errorf("verify created sidecar directory %s: %w", prefix, statErr)
					}
					if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
						return fmt.Errorf("created sidecar directory changed type: %s", prefix)
					}
				}
			default:
				return fmt.Errorf("stat sidecar directory %s: %w", prefix, statErr)
			}
			next, err := openFixedSidecarChild(current, part, info)
			if err != nil {
				return fmt.Errorf("open sidecar directory %s: %w", prefix, err)
			}
			if created {
				var token [32]byte
				if _, err := rand.Read(token[:]); err != nil {
					_ = next.Close()
					return fmt.Errorf("generate directory ownership token for %s: %w", prefix, err)
				}
				if _, err := publishFixedSidecarNoClobber(next, sidecarDirectoryOwnerFile, token[:], 0o600, "publish-owner-token", filepath.Join(prefix, sidecarDirectoryOwnerFile)); err != nil {
					_ = next.Close()
					return fmt.Errorf("publish directory ownership token for %s: %w", prefix, err)
				}
				runBeforeSidecarRecord("record-owned-dir", prefix)
				currentInfo, err := current.Lstat(part)
				if err != nil || !os.SameFile(info, currentInfo) {
					_ = next.Close()
					return fmt.Errorf("%w: created directory changed before record: %s", errSidecarOwnershipMismatch, prefix)
				}
				m.Dirs = append(m.Dirs, prefix)
				m.recordOwnership(prefix, directorySidecarOwnership(token[:]))
			}
			_ = current.Close()
			current = next
		}
		return nil
	}
	// Walk leaf-first, collecting ancestors that don't currently exist.
	// We stop at the first existing ancestor (or the filesystem root) so
	// pre-existing user directories are never recorded — Cleanup must
	// not rmdir a path the user owned before this task started.
	var toCreate []string
	cur := filepath.Clean(path)
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat ancestor %s: %w", cur, err)
		}
		toCreate = append(toCreate, cur)
		parent := filepath.Dir(cur)
		if parent == cur || parent == "." {
			break
		}
		cur = parent
	}
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	// Reverse leaf-first → root-first so Cleanup can reverse-iterate
	// to peel directories from the leaves upward.
	for i, j := 0, len(toCreate)-1; i < j; i, j = i+1, j-1 {
		toCreate[i], toCreate[j] = toCreate[j], toCreate[i]
	}
	m.Dirs = append(m.Dirs, toCreate...)
	return nil
}

// recordWriteFile writes data to path with perm and records the path in
// m for later cleanup, but ONLY when path does not already exist. When
// path is occupied — by a regular file, a symlink, a directory, or any
// other filesystem entry — the function returns errPathPreExists
// without touching the path. The user's bytes (or pre-existing entry
// type) are preserved exactly.
//
// This is the invariant the manifest design rests on: cleanup is a
// pure deletion of paths we created, never a restore. Overwriting a
// pre-existing path and then refusing to delete it on cleanup (the
// pre-fix behavior) destroys user data twice — once at write time and
// once by leaving the corrupted bytes in place at exit. Refusing to
// overwrite removes both halves of that failure mode.
//
// When m is nil this collapses to a plain os.WriteFile — the Reuse
// path uses the nil mode because Reuse runs on cloud workdirs that
// the GC loop wipes wholesale, so per-file collision avoidance is
// irrelevant.
func recordWriteFile(path string, data []byte, perm os.FileMode, m *sidecarManifest) error {
	if m == nil {
		return os.WriteFile(path, data, perm)
	}
	if m.Rooted {
		if m.root == nil {
			return errors.New("root-confined sidecar manifest is not bound")
		}
		rel, err := pathRelativeToRoot(m.rootPath, path)
		if err != nil {
			return err
		}
		parent, leaf, err := openFixedSidecarParent(m.root, rel)
		if err != nil {
			return err
		}
		defer parent.Close()
		published, err := publishFixedSidecarNoClobber(parent, leaf, data, perm, "publish-owned-file", rel)
		if err != nil {
			if errors.Is(err, errPathPreExists) {
				return fmt.Errorf("%w: %s", errPathPreExists, path)
			}
			return err
		}
		runBeforeSidecarRecord("record-owned-file", rel)
		current, err := parent.Lstat(leaf)
		if err != nil || !os.SameFile(published, current) {
			return fmt.Errorf("%w: published sidecar changed before record: %s", errSidecarOwnershipMismatch, rel)
		}
		if err := fixedFileMatchesOwnership(parent, leaf, fileSidecarOwnership(data)); err != nil {
			return err
		}
		m.Files = append(m.Files, rel)
		m.recordOwnership(rel, fileSidecarOwnership(data))
		return nil
	}
	_, statErr := os.Lstat(path)
	if statErr == nil {
		// Any existing entry — regular file, symlink, directory —
		// is a collision. Refuse to touch it.
		return fmt.Errorf("%w: %s", errPathPreExists, path)
	}
	if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("stat target %s: %w", path, statErr)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	m.Files = append(m.Files, path)
	return nil
}

func readExistingSidecarFile(path string, m *sidecarManifest) (*sidecarFileSnapshot, error) {
	if m == nil || !m.Rooted {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		return &sidecarFileSnapshot{data: data, mode: info.Mode(), identity: info}, nil
	}
	if m.root == nil {
		return nil, errors.New("root-confined sidecar manifest is not bound")
	}
	rel, err := pathRelativeToRoot(m.rootPath, path)
	if err != nil {
		return nil, err
	}
	parent, leaf, err := openFixedSidecarParent(m.root, rel)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return readFixedSidecarFile(parent, leaf)
}

func overwriteExistingSidecarFile(path string, data []byte, perm os.FileMode, m *sidecarManifest, snapshot *sidecarFileSnapshot) error {
	if m == nil || !m.Rooted {
		return os.WriteFile(path, data, perm)
	}
	if m.root == nil {
		return errors.New("root-confined sidecar manifest is not bound")
	}
	rel, err := pathRelativeToRoot(m.rootPath, path)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return errors.New("existing sidecar snapshot is required")
	}
	parent, leaf, err := openFixedSidecarParent(m.root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	replacement, err := atomicReplaceFixedSidecar(parent, leaf, snapshot.identity, fileSidecarOwnership(snapshot.data), data, perm, "replace-file", rel)
	if err != nil {
		return err
	}
	if m.preimage == nil {
		m.preimage = make(map[string]sidecarFilePreimage)
	}
	m.preimage[rel] = sidecarFilePreimage{
		data:        append([]byte(nil), snapshot.data...),
		mode:        snapshot.mode,
		replacement: replacement,
		ownership:   fileSidecarOwnership(data),
	}
	m.recordOwnership(rel, fileSidecarOwnership(data))
	return nil
}

func recordExistingSidecarOwnership(path string, m *sidecarManifest) error {
	if m == nil {
		return nil
	}
	if !m.Rooted {
		m.Files = append(m.Files, path)
		return nil
	}
	rel, err := pathRelativeToRoot(m.rootPath, path)
	if err != nil {
		return err
	}
	for _, existing := range m.Files {
		if filepath.Clean(existing) == rel {
			return nil
		}
	}
	m.Files = append(m.Files, rel)
	return nil
}

// allocateCollisionFreeSkillDir picks a directory under skillsParent
// whose path does NOT currently exist, so writeSkillFiles can lay
// down a Multica skill without colliding with a user-installed skill
// of the same slug. The first attempt is always the natural baseSlug
// — that's the path provider-native discovery already knows. On
// collision we append `-multica`, then `-multica-2`, `-multica-3`,
// … until a free slot is found. The chosen slug is returned alongside
// the absolute path so callers can use it in frontmatter and brief
// listings.
//
// The collision-free fallback name is still a sibling under the same
// skillsParent, so provider-native discovery still picks the skill up
// (each subdir under .claude/skills/ etc. is scanned independently).
// The user's directory at baseSlug is left bit-for-bit intact.
//
// The probe is bounded to a small ceiling — a user with thousands of
// collisions on the same slug indicates an upstream bug, not a
// realistic state. Returning an error in that case forces the caller
// to surface the problem instead of looping forever.
func allocateCollisionFreeSkillDir(skillsParent, baseSlug string) (slug, dir string, err error) {
	const maxAttempts = 64
	for i := 0; i < maxAttempts; i++ {
		candidate := skillSlugCandidate(baseSlug, i)
		path := filepath.Join(skillsParent, candidate)
		if _, statErr := os.Lstat(path); statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				return candidate, path, nil
			}
			return "", "", fmt.Errorf("stat candidate %s: %w", path, statErr)
		}
	}
	return "", "", fmt.Errorf("allocate collision-free skill dir under %s: exhausted %d attempts for base %q", skillsParent, maxAttempts, baseSlug)
}

// skillSlugCandidate is the nth name to try for a skill whose natural slug is
// baseSlug: the bare slug first, then `-multica`, then numbered variants.
//
// Two callers must agree on this sequence — allocateCollisionFreeSkillDir,
// which probes the filesystem, and resolveSkillSlugs, which deduplicates a
// batch in memory before anything is written. If they disagreed, a skill would
// be listed under one name and written under another.
func skillSlugCandidate(baseSlug string, attempt int) string {
	switch {
	case attempt <= 0:
		return baseSlug
	case attempt == 1:
		return baseSlug + "-multica"
	default:
		return fmt.Sprintf("%s-multica-%d", baseSlug, attempt)
	}
}

// writeSidecarManifest persists m to {envRoot}/{sidecarManifestFile}.
// Empty manifests are still written so a later Cleanup that finds the
// file knows tracking was attempted (vs. an old build that predates this
// mechanism, where the file is absent and Cleanup must no-op). Failures are
// returned to the caller. Existing providers preserve their warning-only
// behavior; Platform treats persistence as transactional and rolls back the
// in-memory manifest before failing closed.
func writeSidecarManifest(envRoot string, m *sidecarManifest) error {
	if envRoot == "" {
		return nil
	}
	if m == nil {
		m = &sidecarManifest{}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal sidecar manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, sidecarManifestFile), data, 0o644)
}

func writePlatformSidecarManifest(envRoot string, m *sidecarManifest) error {
	if envRoot == "" {
		return nil
	}
	if m == nil {
		m = &sidecarManifest{}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal sidecar manifest: %w", err)
	}
	envRootHandle, err := openFixedSidecarRoot(envRoot)
	if err != nil {
		return fmt.Errorf("open sidecar manifest root: %w", err)
	}
	defer envRootHandle.Close()
	if err := atomicWriteFixedSidecarNoClobber(envRootHandle, sidecarManifestFile, data, 0o644); err != nil {
		return fmt.Errorf("publish sidecar manifest: %w", err)
	}
	return nil
}

type fixedManifestFile struct {
	root     *os.Root
	snapshot *sidecarFileSnapshot
}

func readSidecarManifestFile(envRoot string, fixed bool) ([]byte, *fixedManifestFile, error) {
	manifestPath := filepath.Join(envRoot, sidecarManifestFile)
	if !fixed {
		data, err := os.ReadFile(manifestPath)
		return data, nil, err
	}
	root, err := openFixedSidecarRoot(envRoot)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := readFixedSidecarFile(root, sidecarManifestFile)
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	return snapshot.data, &fixedManifestFile{root: root, snapshot: snapshot}, nil
}

func (f *fixedManifestFile) close() error {
	if f == nil || f.root == nil {
		return nil
	}
	err := f.root.Close()
	f.root = nil
	return err
}

func (f *fixedManifestFile) remove() error {
	if f == nil || f.root == nil || f.snapshot == nil {
		return errors.New("fixed sidecar manifest handle is not open")
	}
	return detachAndDeleteOwnedFixedFile(
		f.root,
		sidecarManifestFile,
		sidecarManifestFile,
		"cleanup-manifest",
		f.snapshot.identity,
		fileSidecarOwnership(f.snapshot.data),
	)
}

func decodeSidecarManifest(data []byte, m *sidecarManifest) error {
	if err := ValidateNoDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("validate sidecar manifest JSON: %w", err)
	}
	return json.Unmarshal(data, m)
}

func detachAndDeleteOwnedFixedFile(parent *os.Root, leaf, rel, operation string, expectedIdentity os.FileInfo, ownership sidecarOwnership) error {
	detached, err := detachFixedSidecar(parent, leaf, operation, rel, expectedIdentity)
	if err != nil {
		return err
	}
	restore := func(cause error) error {
		if restoreErr := restoreDetachedFixedSidecar(parent, leaf, detached); restoreErr != nil {
			return errors.Join(cause, restoreErr)
		}
		return cause
	}
	if detached.info.Mode()&os.ModeSymlink != 0 || !detached.info.Mode().IsRegular() {
		return restore(fmt.Errorf("%w: owned file changed type: %s", errSidecarOwnershipMismatch, rel))
	}
	if err := fixedFileMatchesOwnership(parent, detached.name, ownership); err != nil {
		return restore(err)
	}
	if err := removeFixedLeafWithIdentity(parent, detached.name, detached.info); err != nil {
		return fmt.Errorf("remove verified sidecar quarantine %s: %w", rel, err)
	}
	return nil
}

func removeFixedSidecarFile(root *os.Root, rel, operation string, ownership sidecarOwnership) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	return detachAndDeleteOwnedFixedFile(parent, leaf, rel, operation, nil, ownership)
}

func removeLegacyFixedSidecarFile(root *os.Root, rel string) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(leaf)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("legacy sidecar is not a real file: %s", rel)
	}
	return parent.Remove(leaf)
}

func openDetachedOwnedSidecarDir(parent *os.Root, rel string, detached *fixedDetachedSidecar, ownership sidecarOwnership) (*os.Root, os.FileInfo, error) {
	if detached == nil || detached.info == nil || detached.info.Mode()&os.ModeSymlink != 0 || !detached.info.IsDir() {
		return nil, nil, fmt.Errorf("%w: owned directory changed type: %s", errSidecarOwnershipMismatch, rel)
	}
	if ownership.Kind != "dir" || ownership.SHA256 == "" {
		return nil, nil, fmt.Errorf("%w: invalid directory ownership for %s", errSidecarOwnershipMismatch, rel)
	}
	dirRoot, err := openFixedSidecarChild(parent, detached.name, detached.info)
	if err != nil {
		return nil, nil, err
	}
	token, err := readFixedSidecarFile(dirRoot, sidecarDirectoryOwnerFile)
	if err != nil {
		_ = dirRoot.Close()
		return nil, nil, fmt.Errorf("%w: read directory ownership token for %s: %v", errSidecarOwnershipMismatch, rel, err)
	}
	if directorySidecarOwnership(token.data) != ownership {
		_ = dirRoot.Close()
		return nil, nil, fmt.Errorf("%w: directory ownership token changed for %s", errSidecarOwnershipMismatch, rel)
	}
	return dirRoot, token.identity, nil
}

func removeFixedSidecarDir(root *os.Root, rel, operation string, ownership sidecarOwnership) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	detached, err := detachFixedSidecar(parent, leaf, operation, rel, nil)
	if err != nil {
		return err
	}
	dirRoot, tokenIdentity, err := openDetachedOwnedSidecarDir(parent, rel, detached, ownership)
	if err != nil {
		return fmt.Errorf("%w; replacement directory preserved at %s", err, detached.name)
	}
	dir, err := dirRoot.Open(".")
	if err != nil {
		_ = dirRoot.Close()
		return err
	}
	entries, readErr := dir.ReadDir(-1)
	_ = dir.Close()
	if readErr != nil {
		_ = dirRoot.Close()
		return readErr
	}
	for _, entry := range entries {
		if entry.Name() != sidecarDirectoryOwnerFile {
			_ = dirRoot.Close()
			return fmt.Errorf("%w: owned directory gained content and was preserved at %s", errSidecarOwnershipMismatch, detached.name)
		}
	}
	if err := detachAndDeleteOwnedFixedFile(dirRoot, sidecarDirectoryOwnerFile, filepath.Join(rel, sidecarDirectoryOwnerFile), "cleanup-owner-token", tokenIdentity, directoryTokenFileOwnership(ownership)); err != nil {
		_ = dirRoot.Close()
		return err
	}
	if err := dirRoot.Close(); err != nil {
		return err
	}
	current, err := parent.Lstat(detached.name)
	if err != nil || !os.SameFile(detached.info, current) {
		return fmt.Errorf("%w: detached directory changed before removal: %s", errSidecarOwnershipMismatch, rel)
	}
	return parent.Remove(detached.name)
}

func removeLegacyFixedSidecarDir(root *os.Root, rel string) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(leaf)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("legacy sidecar is not a real directory: %s", rel)
	}
	return parent.Remove(leaf)
}

func directoryTokenFileOwnership(directoryOwnership sidecarOwnership) sidecarOwnership {
	return sidecarOwnership{Kind: "file", SHA256: directoryOwnership.SHA256}
}

func fixedSidecarDirHasEntries(root *os.Root, rel string) (hasEntries bool, ok bool) {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return false, false
	}
	defer parent.Close()
	expected, err := parent.Lstat(leaf)
	if errors.Is(err, fs.ErrNotExist) {
		return false, true
	}
	if err != nil || expected.Mode()&os.ModeSymlink != 0 || !expected.IsDir() {
		return false, false
	}
	dirRoot, err := openFixedSidecarChild(parent, leaf, expected)
	if err != nil {
		return false, false
	}
	defer dirRoot.Close()
	dir, err := dirRoot.Open(".")
	if err != nil {
		return false, false
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, false
	}
	return len(entries) > 0, true
}

func removeAllFixedSidecarDir(root *os.Root, rel string, ownership sidecarOwnership) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	detached, err := detachFixedSidecar(parent, leaf, "reuse-dir", rel, nil)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dirRoot, _, err := openDetachedOwnedSidecarDir(parent, rel, detached, ownership)
	if err != nil {
		return fmt.Errorf("%w; replacement directory preserved at %s", err, detached.name)
	}
	if err := dirRoot.Close(); err != nil {
		return err
	}
	current, err := parent.Lstat(detached.name)
	if err != nil || !os.SameFile(detached.info, current) {
		return fmt.Errorf("%w: detached skill directory changed before removal: %s", errSidecarOwnershipMismatch, rel)
	}
	return parent.RemoveAll(detached.name)
}

func restoreFixedSidecarPreimage(root *os.Root, rel string, preimage sidecarFilePreimage) error {
	parent, leaf, err := openFixedSidecarParent(root, rel)
	if err != nil {
		return err
	}
	defer parent.Close()
	_, err = atomicReplaceFixedSidecar(parent, leaf, preimage.replacement, preimage.ownership, preimage.data, preimage.mode.Perm(), "restore-file", rel)
	return err
}

func rollbackSidecarManifest(m *sidecarManifest) error {
	if m == nil {
		return nil
	}
	defer m.closeRoot()
	var firstErr error
	blockedDirs := make(map[string]struct{})
	blockAncestors := func(path string) {
		for parent := filepath.Dir(filepath.Clean(path)); parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
			blockedDirs[parent] = struct{}{}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
		}
	}
	removePath := func(path string) error {
		if !m.Rooted {
			return os.Remove(path)
		}
		if m.root == nil {
			return errors.New("root-confined sidecar manifest is not bound")
		}
		rel, err := pathRelativeToRoot(m.rootPath, path)
		if err != nil {
			return err
		}
		ownership, err := m.ownershipFor(rel, "file")
		if err != nil {
			return err
		}
		return removeFixedSidecarFile(m.root, rel, "rollback-file", ownership)
	}
	removeDir := func(path string) error {
		if !m.Rooted {
			return os.Remove(path)
		}
		if m.root == nil {
			return errors.New("root-confined sidecar manifest is not bound")
		}
		rel, err := pathRelativeToRoot(m.rootPath, path)
		if err != nil {
			return err
		}
		ownership, err := m.ownershipFor(rel, "dir")
		if err != nil {
			return err
		}
		return removeFixedSidecarDir(m.root, rel, "rollback-dir", ownership)
	}
	directoryHasEntries := func(path string) (bool, bool) {
		if !m.Rooted || m.root == nil {
			return dirHasEntries(path)
		}
		rel, err := pathRelativeToRoot(m.rootPath, path)
		if err != nil {
			return false, false
		}
		return fixedSidecarDirHasEntries(m.root, rel)
	}
	for _, path := range m.Files {
		if m.Rooted {
			rel, err := pathRelativeToRoot(m.rootPath, path)
			if err == nil {
				if preimage, ok := m.preimage[rel]; ok {
					err = restoreFixedSidecarPreimage(m.root, rel, preimage)
					if err != nil && firstErr == nil {
						firstErr = fmt.Errorf("restore %s: %w", path, err)
					}
					if err != nil {
						blockAncestors(path)
					}
					continue
				}
			}
		}
		if err := removePath(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove %s: %w", path, err)
			}
			blockAncestors(path)
		}
	}
	for i := len(m.Dirs) - 1; i >= 0; i-- {
		path := m.Dirs[i]
		if _, blocked := blockedDirs[filepath.Clean(path)]; blocked {
			continue
		}
		if err := removeDir(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			hasEntries, ok := directoryHasEntries(path)
			if hasEntries && ok {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("rmdir %s: %w", path, err)
			}
		}
	}
	return firstErr
}

// CleanupSidecars rolls the user's workdir back to its pre-Prepare
// state by removing every file the manifest at envRoot records and
// then rmdir-ing every directory it records, deepest first.
//
// Two failure modes the function deliberately swallows:
//
//   - ENOENT on a recorded path. The file or directory was already
//     gone — either the user removed it during the task, or a prior
//     Cleanup run on the same envRoot already cleared it. Either
//     way there is nothing left for this call to do.
//   - Non-empty directory on rmdir. The user has populated a
//     directory we created (added a sibling file under .claude/
//     skills/, for example) and rmdir-ing would destroy that
//     content. We detect this by re-reading the directory after
//     rmdir fails: a non-empty listing means "user owns this — stop
//     here." This is the must-fix from PR #3444 review — the
//     previous version swallowed ANY non-ENOENT rmdir error as
//     "non-empty," which silently dropped real I/O failures
//     (EACCES, EPERM, EBUSY) and made cleanup look successful when
//     it wasn't.
//
// All other errors — ReadFile failure, JSON parse failure, real
// EACCES/EPERM/EIO during file deletion, real EACCES/EPERM/EIO
// during dir removal — are captured into firstErr and surfaced to
// the caller. Cleanup still continues for the remaining manifest
// entries so a single bad path does not strand the rest of the
// rollback.
//
// The function is a no-op when:
//   - envRoot is empty (no daemon scratch for this task),
//   - the manifest file is missing (older build, or Prepare did not run).
//
// Pair this with CleanupRuntimeConfig on the local_directory cleanup
// path: that function handles the runtime brief inside CLAUDE.md /
// AGENTS.md, this one handles the sidecar tree
// (.agent_context/, .multica/, .claude/skills/, .github/skills/,
// .opencode/skills/, skills/, .pi/skills/, .cursor/skills/,
// .kimi/skills/, .reasonix/skills/, .kiro/skills/, .agents/skills/, fallback
// .agent_context/skills/). The two together restore the workdir to
// byte-exact pre-task state.
func CleanupSidecars(envRoot string) error {
	return cleanupSidecars(envRoot, "")
}

// CleanupSidecarsAt is the root-confined variant used when the caller has the
// trusted task WorkDir. Legacy absolute manifest entries are translated to
// names beneath a fixed os.Root handle, so a swapped parent symlink cannot
// redirect deletion outside the workdir.
func CleanupSidecarsAt(envRoot, workDir string) error {
	return cleanupSidecars(envRoot, workDir)
}

func cleanupSidecars(envRoot, workDir string) error {
	if envRoot == "" {
		return nil
	}
	manifestPath := filepath.Join(envRoot, sidecarManifestFile)
	data, fixedManifest, err := readSidecarManifestFile(envRoot, workDir != "")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sidecar manifest %s: %w", manifestPath, err)
	}
	if fixedManifest != nil {
		defer fixedManifest.close()
	}
	var m sidecarManifest
	if err := decodeSidecarManifest(data, &m); err != nil {
		return fmt.Errorf("parse sidecar manifest %s: %w", manifestPath, err)
	}
	if m.Rooted && workDir == "" {
		return errors.New("root-confined sidecar manifest requires trusted workdir")
	}

	var firstErr error
	blockedDirs := make(map[string]struct{})
	blockAncestors := func(path string) {
		for parent := filepath.Dir(filepath.Clean(path)); parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
			blockedDirs[parent] = struct{}{}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
		}
	}
	captureErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	var workRoot *os.Root
	if workDir != "" {
		workRoot, err = openFixedSidecarRoot(workDir)
		if err != nil {
			return fmt.Errorf("open sidecar cleanup root %s: %w", workDir, err)
		}
		defer workRoot.Close()
	}
	removeFile := func(path string) error {
		if workRoot == nil {
			return os.Remove(path)
		}
		rel, err := pathRelativeToRoot(workDir, path)
		if err != nil {
			return err
		}
		if !m.Rooted {
			return removeLegacyFixedSidecarFile(workRoot, rel)
		}
		ownership, err := m.ownershipFor(rel, "file")
		if err != nil {
			return err
		}
		return removeFixedSidecarFile(workRoot, rel, "cleanup-file", ownership)
	}
	removeDir := func(path string) error {
		if workRoot == nil {
			return os.Remove(path)
		}
		rel, err := pathRelativeToRoot(workDir, path)
		if err != nil {
			return err
		}
		if !m.Rooted {
			return removeLegacyFixedSidecarDir(workRoot, rel)
		}
		ownership, err := m.ownershipFor(rel, "dir")
		if err != nil {
			return err
		}
		return removeFixedSidecarDir(workRoot, rel, "cleanup-dir", ownership)
	}
	directoryHasEntries := func(path string) (bool, bool) {
		if workRoot == nil {
			return dirHasEntries(path)
		}
		rel, err := pathRelativeToRoot(workDir, path)
		if err != nil {
			return false, false
		}
		return fixedSidecarDirHasEntries(workRoot, rel)
	}

	for _, f := range m.Files {
		if err := removeFile(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			captureErr(fmt.Errorf("remove %s: %w", f, err))
			blockAncestors(f)
		}
	}

	// Reverse iterate so the deepest directory is tried first. When
	// rmdir fails we re-read the directory to tell ENOTEMPTY (user
	// content present — skip silently) apart from real I/O errors
	// (permission denied, busy, etc. — capture and surface).
	for i := len(m.Dirs) - 1; i >= 0; i-- {
		d := m.Dirs[i]
		if _, blocked := blockedDirs[filepath.Clean(d)]; blocked {
			continue
		}
		err := removeDir(d)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		hasEntries, ok := directoryHasEntries(d)
		switch {
		case !ok:
			// ReadDir also failed — we can't tell ENOTEMPTY apart
			// from a real I/O error. Surface the ORIGINAL rmdir
			// error (not the ReadDir failure) so the operator sees
			// the actual cleanup blocker; the ReadDir branch is
			// just diagnostic plumbing and would distract from the
			// root cause. Silently skipping here was the v1 bug:
			// it hid EACCES on locked directories behind a phantom
			// "directory non-empty" assumption.
			captureErr(fmt.Errorf("rmdir %s: %w", d, err))
		case hasEntries:
			// User has populated this dir since Prepare ran. Leave
			// it in place without surfacing the rmdir error — the
			// whole point of the manifest design is to preserve
			// user content under directories we created.
		default:
			// Empty directory but rmdir still failed → real I/O
			// error (EACCES, EPERM, EBUSY, EIO, or a directory we
			// mistakenly recorded that we don't actually own).
			// Surface it so the caller can log a warning and an
			// operator can investigate.
			captureErr(fmt.Errorf("rmdir %s: %w", d, err))
		}
	}

	if workRoot != nil && firstErr != nil {
		// Root-confined cleanup is fail closed: retain the manifest so the
		// failed rollback cannot be mistaken for a completed cleanup and can be
		// retried after the swapped/blocked path is repaired.
		return firstErr
	}
	removeManifest := func() error { return os.Remove(manifestPath) }
	if fixedManifest != nil {
		removeManifest = fixedManifest.remove
	}
	if err := removeManifest(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		captureErr(fmt.Errorf("remove manifest %s: %w", manifestPath, err))
	}

	return firstErr
}

func pathRelativeToRoot(rootPath, path string) (string, error) {
	if rootPath == "" {
		return "", errors.New("sidecar root is required")
	}
	rel := path
	var err error
	if filepath.IsAbs(path) {
		rel, err = filepath.Rel(rootPath, path)
		if err != nil {
			return "", fmt.Errorf("relativize sidecar path %s: %w", path, err)
		}
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("sidecar path %s escapes root %s", path, rootPath)
	}
	return rel, nil
}

// removeReusedManagedSkillDirs force-removes the skill directories the prior
// dispatch recorded under skillsParent in its sidecar manifest at envRoot,
// even when they are now non-empty. It is the reuse-path companion to
// CleanupSidecars and runs just before it.
//
// CleanupSidecars deliberately preserves a recorded directory once it has
// become non-empty — the agent may have dropped a file inside a dir we
// created, and on the local_directory teardown path that content must
// survive. But that same preservation reopens #3684 on the reuse path: if a
// prior-run agent wrote into .claude/skills/issue-review/, CleanupSidecars
// deletes the recorded SKILL.md yet keeps the directory, so the canonical
// slug stays occupied and the refreshed skill dodges to
// issue-review-multica. A managed skill directory is platform-owned — the
// manifest is proof we created it — so on reuse we reclaim the whole
// directory (dropping any scratch the agent left inside it, exactly as the
// Codex path's os.RemoveAll(skillsDir) already does) and let the refresh
// re-create it at its natural slug.
//
// Only directories whose immediate parent is skillsParent are removed, so
// the blast radius is exactly the platform's own skill roots: sibling skills
// the agent installed under the same parent, checked-out repos, and the rest
// of the workdir are untouched. The reuse path only ever runs on cloud
// workdirs (the daemon skips Reuse for local_directory tasks), so there is no
// user-owned skills tree to protect here in the first place.
//
// envRoot or skillsParent empty and a missing manifest are no-ops. Read,
// parse, validation, and deletion failures are returned to the caller. The
// manifest file is left in place; CleanupSidecars, which runs next, owns
// deleting it.
func removeReusedManagedSkillDirs(envRoot, skillsParent string) error {
	return removeReusedManagedSkillDirsAt(envRoot, "", skillsParent)
}

func removeReusedManagedSkillDirsAt(envRoot, workDir, skillsParent string) error {
	if envRoot == "" || skillsParent == "" {
		return nil
	}
	data, fixedManifest, err := readSidecarManifestFile(envRoot, workDir != "")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sidecar manifest for reuse skill rollback: %w", err)
	}
	if fixedManifest != nil {
		defer fixedManifest.close()
	}
	var m sidecarManifest
	if err := decodeSidecarManifest(data, &m); err != nil {
		return fmt.Errorf("parse sidecar manifest for reuse skill rollback: %w", err)
	}
	if m.Rooted && workDir == "" {
		return errors.New("root-confined reuse manifest requires trusted workdir")
	}

	cleanParent := filepath.Clean(skillsParent)
	var workRoot *os.Root
	if workDir != "" {
		workRoot, err = openFixedSidecarRoot(workDir)
		if err != nil {
			return fmt.Errorf("open reuse sidecar root %s: %w", workDir, err)
		}
		defer workRoot.Close()
		cleanParent, err = pathRelativeToRoot(workDir, skillsParent)
		if err != nil {
			return err
		}
	}
	var firstErr error
	for _, d := range m.Dirs {
		candidate := filepath.Clean(d)
		if workRoot != nil {
			candidate, err = pathRelativeToRoot(workDir, d)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		if filepath.Dir(candidate) != cleanParent {
			continue
		}
		if workRoot != nil {
			ownership, ownershipErr := m.ownershipFor(candidate, "dir")
			if ownershipErr != nil {
				err = ownershipErr
			} else {
				err = removeAllFixedSidecarDir(workRoot, candidate, ownership)
			}
		} else {
			err = os.RemoveAll(d)
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove managed skill dir %s: %w", d, err)
		}
	}
	return firstErr
}

// dirHasEntries inspects dir and reports whether it currently contains
// any entries. The second return value distinguishes three states
// CleanupSidecars must handle separately:
//
//   - (false, true) — dir exists and is empty, OR dir disappeared
//     between the failed rmdir and our readdir (the race collapses
//     into "empty" so cleanup keeps moving). When paired with a
//     non-ENOENT rmdir failure in CleanupSidecars this is the
//     "empty + rmdir refused" branch — a real I/O error that gets
//     surfaced.
//   - (true, true) — dir has user content. When paired with a rmdir
//     failure this is the intended ENOTEMPTY branch — skip silently
//     so the user's content is preserved.
//   - (_, false) — readdir failed with a real I/O error (EACCES on a
//     chmod'd dir, ENOTDIR on a recorded path that isn't actually a
//     dir, EIO on a hardware fault, etc.). The caller cannot tell
//     ENOTEMPTY from a real failure and MUST surface the original
//     rmdir error instead of silently skipping. The v1 of this
//     helper returned `true` here, which made CleanupSidecars treat
//     every readdir failure as "user content present" and hid the
//     underlying rmdir error.
func dirHasEntries(dir string) (hasEntries bool, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, true
		}
		return false, false
	}
	return len(entries) > 0, true
}
