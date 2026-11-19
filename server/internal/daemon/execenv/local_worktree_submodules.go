package execenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const localSubmoduleRefPrefix = "refs/multica/local-submodules/"

type localSubmodule struct {
	name   string
	path   string
	commit string
}

// Use the index after replay so a continued task checks out the previous
// delivery, including new gitlinks. Only registered gitlinks are submodules.
func localSubmodules(repo string) ([]localSubmodule, error) {
	modulesFile := filepath.Join(repo, ".gitmodules")
	info, err := os.Lstat(modulesFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", modulesFile)
	}
	names, err := readSubmoduleNames(repo, "", "--file=.gitmodules")
	if err != nil || len(names) == 0 {
		return nil, err
	}
	index, err := runGitStdout(repo, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	return parseSubmoduleEntries(index, names, "160000 ", " 0"), nil
}

func submodulesInTree(gitDir, tree string) ([]localSubmodule, error) {
	entry, err := runGitStdout(gitDir, "--git-dir="+gitDir, "ls-tree", "-z", tree, "--", ".gitmodules")
	if err != nil || entry == "" {
		return nil, err
	}
	names, err := readSubmoduleNames(gitDir, gitDir, "--blob="+tree+":.gitmodules")
	if err != nil || len(names) == 0 {
		return nil, err
	}
	entries, err := runGitStdout(gitDir, "--git-dir="+gitDir, "ls-tree", "-rz", tree)
	if err != nil {
		return nil, err
	}
	return parseSubmoduleEntries(entries, names, "160000 commit ", ""), nil
}

func readSubmoduleNames(dir, gitDir, input string) (map[string]string, error) {
	var args []string
	if gitDir != "" {
		args = append(args, "--git-dir="+gitDir)
	}
	args = append(args, "config", input, "--null", "--get-regexp", `^submodule\..*\.path$`)
	config, err := runGitStdout(dir, args...)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0 {
			return nil, nil
		}
		return nil, err
	}
	names := make(map[string]string)
	for _, record := range strings.Split(strings.TrimSuffix(config, "\x00"), "\x00") {
		key, path, ok := strings.Cut(record, "\n")
		if !ok {
			return nil, fmt.Errorf("invalid submodule record in %s", input)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "submodule."), ".path")
		if !validModulePath(name) || !validModulePath(path) {
			return nil, fmt.Errorf("invalid submodule name %q or path %q", name, path)
		}
		names[path] = name
	}
	return names, nil
}

func parseSubmoduleEntries(entries string, names map[string]string, prefix, suffix string) []localSubmodule {
	var modules []localSubmodule
	for _, entry := range strings.Split(entries, "\x00") {
		header, path, _ := strings.Cut(entry, "\t")
		name, registered := names[path]
		if registered && strings.HasPrefix(header, prefix) && strings.HasSuffix(header, suffix) {
			commit := strings.TrimSuffix(strings.TrimPrefix(header, prefix), suffix)
			modules = append(modules, localSubmodule{name: name, path: path, commit: commit})
		}
	}
	return modules
}

func validModulePath(path string) bool {
	native := filepath.FromSlash(path)
	return filepath.IsLocal(native) && native != "." && filepath.Clean(native) == native && !strings.Contains(path, "\\")
}

// A traditional submodule may have an embedded .git directory. An absorbed
// submodule uses its superproject's modules directory, even when deinitialized.
func (m localSubmodule) durableGitDir(source, parentGitDir string) (string, error) {
	dir := filepath.Join(source, filepath.FromSlash(m.path))
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return runGitTrimmed(dir, "rev-parse", "--absolute-git-dir")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return filepath.Join(parentGitDir, "modules", filepath.FromSlash(m.name)), nil
}

// A checkout hook can create submodule work before replay or preparation fails.
func hasPopulatedSubmodule(repo string) (bool, error) {
	modules, err := localSubmodules(repo)
	if err != nil {
		return false, err
	}
	for _, module := range modules {
		populated, err := submoduleCheckoutPopulated(filepath.Join(repo, filepath.FromSlash(module.path)))
		if populated || err != nil {
			return populated, err
		}
	}
	return false, nil
}

func submoduleCheckoutPopulated(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return true, fmt.Errorf("submodule path %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	return len(entries) != 0, err
}

func (s *taskSubmodules) prepare(repo, source, sourceGitDir, previousHead string) error {
	modules, err := localSubmodules(repo)
	if err != nil {
		return err
	}
	for _, module := range modules {
		if err := s.prepareModule(module, repo, source, sourceGitDir, previousHead); err != nil {
			return fmt.Errorf("prepare submodule %q: %w", module.path, err)
		}
	}
	return nil
}

func (s *taskSubmodules) prepareModule(m localSubmodule, repo, source, sourceGitDir, previousHead string) error {
	dir := filepath.Join(repo, filepath.FromSlash(m.path))
	populated, err := submoduleCheckoutPopulated(dir)
	s.existingCheckout = s.existingCheckout || populated || err != nil
	if err != nil {
		return err
	}
	gitDir, err := m.durableGitDir(source, sourceGitDir)
	if err != nil {
		return err
	}
	childPreviousHead := m.commit
	if populated {
		childPreviousHead, err = s.reuseCheckout(m, repo, dir, gitDir, previousHead)
		if err != nil {
			return err
		}
	} else {
		from, err := findSubmoduleCommit(m.commit, gitDir)
		if err != nil || from == "" {
			return err // Leave locally unavailable submodules uninitialized.
		}
		if err := cloneLocalSubmodule(m, repo, dir, from); err != nil {
			return err
		}
	}
	return s.prepare(dir, filepath.Join(source, filepath.FromSlash(m.path)), gitDir, childPreviousHead)
}

func (s *taskSubmodules) reuseCheckout(m localSubmodule, parent, dir, sourceGitDir, parentPreviousHead string) (string, error) {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		return "", errors.New("populated submodule path does not contain a Git checkout")
	} else if err != nil {
		return "", err
	}
	actual, err := resolveGitRoot(dir)
	if err != nil {
		return "", err
	}
	expected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if actual != expected || !withinModuleRoot(s.taskRoot, actual) {
		return "", fmt.Errorf("Git checkout at %s is outside the expected task submodule path", actual)
	}
	gitDir, err := runGitTrimmed(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	if err := s.checkTaskGitDir(gitDir); err != nil {
		return "", err
	}
	head, err := runGitTrimmed(dir, "rev-parse", "--verify", "HEAD")
	if err != nil || head == m.commit {
		return head, err
	}
	dirty, err := runGitTrimmed(dir, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return "", err
	}
	if dirty != "" {
		return "", fmt.Errorf("existing checkout at %s has local changes and cannot move to %s", shortID(head), shortID(m.commit))
	}
	available, err := submoduleHasCommit(gitDir, m.commit)
	if err != nil {
		return "", err
	}
	if !available {
		from, err := findSubmoduleCommit(m.commit, sourceGitDir)
		if err != nil {
			return "", err
		}
		if from == "" {
			return "", fmt.Errorf("gitlink commit %s is unavailable locally", shortID(m.commit))
		}
		if err := fetchSubmoduleCommit(dir, gitDir, from, m.commit); err != nil {
			return "", err
		}
	}
	// A checkout hook normally uses the parent's pin from before alignment.
	// Its current HEAD may already reference a different nested revision.
	// Any other detached work must remain reachable from the new pin.
	previous, _ := runGitTrimmed(parent, "rev-parse", "--verify", "--quiet", parentPreviousHead+":"+m.path)
	if head != previous {
		if _, err := runGit(dir, "merge-base", "--is-ancestor", head, m.commit); err != nil {
			return "", fmt.Errorf("existing checkout at %s contains commits outside the expected history of %s", shortID(head), shortID(m.commit))
		}
	}
	return head, checkoutSubmoduleCommit(dir, m.commit)
}

func cloneLocalSubmodule(m localSubmodule, repo, dir, gitDir string) error {
	// The local clone has its own HEAD, index and object store. Git uses
	// hardlinks where possible, so pruning in the source cannot remove
	// objects the agent still needs.
	if out, err := runGit(repo, "clone", "--local", "--no-checkout", "--", gitDir, dir); err != nil {
		return fmt.Errorf("clone local submodule: %s: %w", strings.TrimSpace(out), err)
	}
	shallow, err := runGitTrimmed(gitDir, "--git-dir="+gitDir, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	cloneGitDir := filepath.Join(dir, ".git")
	available, err := submoduleHasCommit(cloneGitDir, m.commit)
	if err != nil {
		return err
	}
	if shallow == "true" || !available {
		if err := fetchSubmoduleCommit(dir, cloneGitDir, gitDir, m.commit); err != nil {
			return err
		}
	}
	if err := checkoutSubmoduleCommit(dir, m.commit); err != nil {
		return err
	}
	if origin, err := runGitTrimmed(gitDir, "--git-dir="+gitDir, "config", "--local", "--get", "remote.origin.url"); err == nil {
		if _, err := runGit(dir, "remote", "set-url", "origin", origin); err != nil {
			return fmt.Errorf("configure submodule origin: %w", err)
		}
	} else if _, err := runGit(dir, "remote", "remove", "origin"); err != nil {
		return fmt.Errorf("remove temporary submodule origin: %w", err)
	}
	return nil
}

func checkoutSubmoduleCommit(dir, commit string) error {
	if out, err := runGit(dir, "checkout", "--no-recurse-submodules", "--no-overwrite-ignore", "--detach", commit); err != nil {
		return fmt.Errorf("check out gitlink commit: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

func (w *LocalWorktree) saveSubmodules() error {
	if _, err := os.Lstat(filepath.Join(w.Path, ".gitmodules")); errors.Is(err, os.ErrNotExist) {
		// An explicit commit can still reference submodules even if the
		// worktree has staged their removal.
		entry, err := runGitStdout(w.Path, "ls-tree", "-z", "HEAD", "--", ".gitmodules")
		if err != nil || entry == "" {
			return err
		}
	}
	taskGitDir, err := runGitTrimmed(w.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	sourceGitDir, err := runGitTrimmed(w.GitRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	save := taskSubmodules{taskRoot: w.Path, taskGitDir: taskGitDir}
	if err := save.commitWorktree(w.Path); err != nil {
		return err
	}
	staged, err := stagedWorktreeTree(w.Path)
	if err != nil {
		return err
	}
	// The agent's own commit and the staged worktree can point at different
	// detached revisions. Both trees must remain usable after cleanup.
	repo := submoduleLocations{
		taskPath: w.Path, taskGitDir: taskGitDir,
		sourcePath: w.GitRoot, sourceGitDir: sourceGitDir,
	}
	baseline := w.BaseCommit
	if baseline == "" {
		baseline = "HEAD"
	}
	for _, tree := range []string{"HEAD", staged} {
		if err := save.retainTree(repo, taskGitDir, tree, taskGitDir, baseline); err != nil {
			return err
		}
	}
	return nil
}

type taskSubmodules struct {
	taskRoot         string
	taskGitDir       string
	existingCheckout bool
}

type submoduleLocations struct {
	taskPath     string
	taskGitDir   string
	sourcePath   string
	sourceGitDir string
}

func (s taskSubmodules) checkTaskGitDir(gitDir string) error {
	real, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return err
	}
	commonDir, err := runGitTrimmed(real, "--git-dir="+real, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		return err
	}
	for _, path := range []string{real, commonDir} {
		if !withinModuleRoot(s.taskRoot, path) && !withinModuleRoot(s.taskGitDir, path) {
			return fmt.Errorf("Git directory %s is outside the task worktree", path)
		}
	}
	return nil
}

func (s taskSubmodules) commitWorktree(repo string) error {
	modules, err := localSubmodules(repo)
	if err != nil {
		return err
	}
	for _, module := range modules {
		dir := filepath.Join(repo, filepath.FromSlash(module.path))
		if _, err := os.Lstat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
			continue // A deinitialized checkout has no uncommitted files.
		} else if err != nil {
			return err
		}
		if err := s.commitModule(dir); err != nil {
			return fmt.Errorf("save submodule %q: %w", module.path, err)
		}
	}
	return nil
}

func (s taskSubmodules) commitModule(dir string) error {
	gitDir, err := runGitTrimmed(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	if err := s.checkTaskGitDir(gitDir); err != nil {
		return err
	}
	if paths, err := unmergedPaths(dir); err != nil {
		return err
	} else if len(paths) > 0 {
		return fmt.Errorf("unresolved merge in %s", quotedPaths(paths))
	}
	if err := s.commitWorktree(dir); err != nil {
		return err
	}
	dirty, err := worktreeIsDirty(dir)
	if err != nil || !dirty {
		return err
	}
	_, err = commitEverything(dir, "chore(agent): uncommitted submodule changes from task", false)
	return err
}

func (s taskSubmodules) retainTree(repo submoduleLocations, gitDir, tree, baseDir, base string) error {
	modules, err := submodulesInTree(gitDir, tree)
	if err != nil || len(modules) == 0 {
		return err
	}
	previous := make(map[string]string)
	if baseDir != "" && base != "" {
		oldModules, err := submodulesInTree(baseDir, base)
		if err != nil {
			return err
		}
		for _, m := range oldModules {
			previous[m.name] = m.commit
		}
	}
	for _, module := range modules {
		if err := s.retainModule(repo, module, previous[module.name]); err != nil {
			return fmt.Errorf("save submodule %q at %s: %w", module.path, shortID(module.commit), err)
		}
	}
	return nil
}

func (s taskSubmodules) retainModule(parent submoduleLocations, module localSubmodule, base string) error {
	repo := submoduleLocations{
		taskPath:   filepath.Join(parent.taskPath, filepath.FromSlash(module.path)),
		sourcePath: filepath.Join(parent.sourcePath, filepath.FromSlash(module.path)),
	}
	var err error
	if parent.taskGitDir != "" {
		repo.taskGitDir, err = module.durableGitDir(parent.taskPath, parent.taskGitDir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(repo.taskGitDir); errors.Is(err, os.ErrNotExist) {
			repo.taskGitDir = ""
		} else if err != nil {
			return err
		}
		if repo.taskGitDir != "" {
			if err := s.checkTaskGitDir(repo.taskGitDir); err != nil {
				return err
			}
		}
	}
	unchanged := base == module.commit
	if unchanged && repo.taskGitDir == "" {
		return nil
	}
	repo.sourceGitDir, err = module.durableGitDir(parent.sourcePath, parent.sourceGitDir)
	if err != nil {
		return err
	}
	from, err := findSubmoduleCommit(module.commit, repo.taskGitDir, repo.sourceGitDir)
	if err != nil {
		return err
	}
	if from == "" {
		if unchanged {
			return nil // A preexisting gitlink may be unavailable locally.
		}
		return errors.New("referenced commit is missing from both local submodule repositories")
	}
	baseDir, err := findSubmoduleCommit(base, repo.taskGitDir, repo.sourceGitDir)
	if err != nil {
		return err
	}
	// Persist exactly the nested gitlinks recorded in this commit. The
	// submodule and its descendants may have switched HEADs or been deinitialized.
	if err := s.retainTree(repo, from, module.commit, baseDir, base); err != nil {
		return err
	}
	// A hook can introduce a task-only commit before the baseline is
	// recorded. Check the source before skipping an unchanged gitlink.
	if unchanged {
		durable, err := findSubmoduleCommit(module.commit, repo.sourceGitDir)
		if err != nil || durable != "" {
			return err
		}
	}
	return retainSubmoduleCommit(s.taskRoot, repo.sourceGitDir, from, module.commit)
}

func findSubmoduleCommit(commit string, gitDirs ...string) (string, error) {
	if commit == "" {
		return "", nil
	}
	for _, dir := range gitDirs {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "HEAD")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", err
		}
		found, err := submoduleHasCommit(dir, commit)
		if err != nil || found {
			return dir, err
		}
	}
	return "", nil
}

func submoduleHasCommit(gitDir, commit string) (bool, error) {
	out, err := runGitTrimmed(gitDir, "--git-dir="+gitDir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return out == commit, nil
}

func withinModuleRoot(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

func retainSubmoduleCommit(repo, gitDir, from, commit string) error {
	gitDirArg := "--git-dir=" + gitDir
	if from == gitDir {
		_, err := runGit(repo, gitDirArg, "update-ref", localSubmoduleRefPrefix+commit, commit)
		return err
	}
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		format, err := runGitTrimmed(from, "--git-dir="+from, "rev-parse", "--show-object-format")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
			return err
		}
		scratch, err := os.MkdirTemp("", "multica-submodule-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(scratch)
		// Initialize it as a non-bare repository, so Git can attach the
		// user's checkout later. Nested modules can already exist within it.
		if out, err := runGit(repo, "init", "--separate-git-dir="+gitDir, "--object-format="+format, scratch); err != nil {
			return fmt.Errorf("create durable submodule Git directory: %s: %w", strings.TrimSpace(out), err)
		}
		if origin, err := runGitTrimmed(from, "--git-dir="+from, "config", "--local", "--get", "remote.origin.url"); err == nil {
			if _, err := runGit(repo, gitDirArg, "remote", "add", "origin", origin); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	return fetchSubmoduleCommit(repo, gitDir, from, commit)
}

func fetchSubmoduleCommit(repo, gitDir, from, commit string) error {
	gitDirArg := "--git-dir=" + gitDir
	ref := localSubmoduleRefPrefix + commit
	// Fetch locally by SHA and retain shallow boundaries. Git can decline
	// a ref update without failing the fetch, so verify the result explicitly.
	if out, err := runGit(repo, gitDirArg, "-c", "protocol.file.allow=always", "fetch", "--update-shallow",
		"--no-tags", "--no-write-fetch-head", "--no-auto-maintenance", "--no-recurse-submodules", "--", from, commit+":"+ref); err != nil {
		return fmt.Errorf("retain commit %s in %s: %s: %w", commit, gitDir, strings.TrimSpace(out), err)
	}
	stored, err := runGitTrimmed(repo, gitDirArg, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return fmt.Errorf("verify retained commit %s: %w", commit, err)
	}
	if stored != commit {
		return fmt.Errorf("retained ref %s points at %s, expected %s", ref, stored, commit)
	}
	return nil
}
