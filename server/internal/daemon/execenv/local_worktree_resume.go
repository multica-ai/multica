package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// LocalWorktreeResume is an optional checkout destination whose task-root
// lock the daemon holds through finalization. The current task still owns a
// separate environment for its logs, scratch files and runtime configuration.
type LocalWorktreeResume struct {
	RootDir string
	WorkDir string
	Branch  string
}

// checkoutPath is called under the repo lock, after resolving the normal
// branch plan. A branchless source delivered a read-only round whose freshly
// allocated branch was dropped by Finalize.
func (resume LocalWorktreeResume) checkoutPath(rel string, plan taskBranchPlan) string {
	if !plan.tracksState {
		return ""
	}
	if resume.Branch == "" {
		if plan.continues || plan.reset {
			return ""
		}
	} else if resume.Branch != plan.name || (!plan.continues && !plan.reset) {
		return ""
	}
	path := filepath.Join(resume.RootDir, localWorktreeDirName)
	if filepath.Join(path, rel) != resume.WorkDir {
		return ""
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return ""
	}
	return path
}

func validateRestoredWorktreeCwd(checkout, rel string) error {
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		checkout = filepath.Join(checkout, part)
		info, err := os.Lstat(checkout)
		if errors.Is(err, os.ErrNotExist) {
			// Prepare creates resource directories missing from the tree.
			return nil
		}
		if err != nil || info.Mode().Type() != os.ModeDir {
			return fmt.Errorf("execenv: restored worktree cwd crosses a non-directory at %q", checkout)
		}
	}
	return nil
}

// LocalWorktreeResumeRoot locates the task root named by the original cwd and
// its server-supplied owner, even if GC has already removed the directory.
func LocalWorktreeResumeRoot(workspacesRoot, priorWorkDir string, owner EnvRootOwner) (string, error) {
	if !filepath.IsAbs(priorWorkDir) {
		return "", errors.New("prior worktree cwd must be absolute")
	}
	configuredRoot, err := filepath.Abs(workspacesRoot)
	if err != nil {
		return "", err
	}
	root := configuredRoot
	rel, err := filepath.Rel(root, priorWorkDir)
	if err != nil || !filepath.IsLocal(rel) {
		root, err = util.ResolveSymlinks(configuredRoot)
		if err != nil {
			return "", err
		}
		rel, err = filepath.Rel(root, priorWorkDir)
		if err != nil || !filepath.IsLocal(rel) {
			return "", errors.New("prior worktree cwd is outside the workspaces root")
		}
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 3 || parts[2] != localWorktreeDirName {
		return "", errors.New("prior cwd is outside a managed local worktree")
	}
	oldRoot := filepath.Join(root, parts[0], parts[1])
	if err := ValidateEnvRootOwnerPath(root, oldRoot, owner); err != nil {
		return "", err
	}
	return oldRoot, nil
}

// ClaimLocalWorktreeResumeRoot locks a validated old root without resetting it.
// A recreated root retains its original owner for ordinary GC. An existing
// checkout may contain live or recoverable work.
func ClaimLocalWorktreeResumeRoot(workspacesRoot, oldRoot string, owner EnvRootOwner) (*EnvRootClaim, error) {
	ws, err := os.OpenRoot(workspacesRoot)
	if err != nil {
		return nil, err
	}
	defer ws.Close()
	rootRel := filepath.Join(filepath.Base(filepath.Dir(oldRoot)), filepath.Base(oldRoot))
	for _, dir := range []string{filepath.Dir(rootRel), rootRel} {
		if err := ws.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err := ws.Lstat(dir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("prior task path %q is not a directory", dir)
		}
	}
	pinned, err := ws.OpenRoot(rootRel)
	if err != nil {
		return nil, err
	}
	defer pinned.Close()
	info, err := pinned.Stat(".")
	if err != nil {
		return nil, err
	}
	namedInfo, err := os.Stat(oldRoot)
	if err != nil || !os.SameFile(info, namedInfo) {
		return nil, errors.New("prior task root changed identity")
	}

	claim, lockedInfo, err := LockEnvRootForReuse(ws, rootRel, oldRoot)
	if err != nil {
		return nil, err
	}
	if claim == nil {
		return nil, errors.New("prior task root disappeared")
	}
	if !os.SameFile(info, lockedInfo) {
		claim.Release()
		return nil, errors.New("prior task root changed while acquiring the lock")
	}
	if err := ensureLocalWorktreeResumeOwner(pinned, owner); err != nil {
		claim.Release()
		return nil, err
	}
	if _, err := pinned.Lstat(localWorktreeDirName); !errors.Is(err, os.ErrNotExist) {
		claim.Release()
		return nil, errors.New("prior worktree path is occupied")
	}
	return claim, nil
}

func ensureLocalWorktreeResumeOwner(root *os.Root, expected EnvRootOwner) error {
	info, err := root.Lstat(envRootOwnerFile)
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("prior worktree owner marker is not a regular file")
		}
		data, err := root.ReadFile(envRootOwnerFile)
		if err != nil {
			return err
		}
		owner, decodeErr := decodeEnvRootOwner(data)
		if decodeErr != nil || owner.TaskID != expected.TaskID ||
			(owner.WorkspaceID != "" && owner.WorkspaceID != expected.WorkspaceID) {
			return errors.New("prior worktree task root has a different owner")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if name != envRootLockFile {
			return errors.New("prior worktree task root holds files without an owner")
		}
	}
	marker, err := root.OpenFile(envRootOwnerFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	writeErr := json.NewEncoder(marker).Encode(expected)
	if err := errors.Join(writeErr, marker.Close()); err != nil {
		_ = root.Remove(envRootOwnerFile)
		return err
	}
	return nil
}
