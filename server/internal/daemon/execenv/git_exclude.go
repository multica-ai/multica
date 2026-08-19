package execenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// The three files the protection is made of, all in envRoot — daemon
	// scratch — never in the user's repository.
	gitExcludesFileName   = ".multica_git_excludes"
	gitRepoConfigFileName = ".multica_git_repo_config"
	gitGlobalConfigName   = ".multica_git_config"
)

// ErrGitExcludesUnprotected is returned when workDir IS inside a git
// repository but the protection could not be established or could not be
// proven to work. The caller must treat it as fatal and refuse to launch the
// agent: the whole point is that the runtime's own files cannot reach the
// user's history, and an agent running unprotected in a real repository is
// exactly the failure GitHub #7114 reported. Degrading to a warning would make
// the data-integrity guarantee advisory.
var ErrGitExcludesUnprotected = errors.New("execenv: could not protect the user's repository from the runtime's own files")

// GitProtection is the task-scoped arrangement that keeps the daemon's own
// sidecars out of the git commands the AGENT runs, without touching a single
// byte of the user's repository or its git metadata.
//
// It is delivered as environment variables, so it applies to the agent process
// and its children and to nothing else — the user's own `git status` in the
// same directory keeps telling them the truth, and nothing survives a crash
// because there is no on-disk state in their repo to roll back.
//
// The variables point git at a Multica-written global config which:
//
//   - includes the user's real global config, so their settings survive, and
//   - conditionally includes a second config, `includeIf gitdir:<this repo>`,
//     that sets core.excludesFile.
//
// The conditional include is what confines the patterns to the repository this
// task is running against. core.excludesFile on its own is process-global: an
// agent that checks out or steps into a DIFFERENT repository mid-task would
// have had the same `/.multica/`, `/.grok/` patterns applied there, hiding
// that repository's genuine files from its own `git add -A`.
type GitProtection struct {
	// Env is merged into the agent's environment. It must be applied LAST,
	// after custom_env, and then proven with Verify — see Verify's contract.
	Env map[string]string

	workDir string
	// probe is an untracked sidecar path that MUST come back ignored. It is
	// the empirical evidence that the arrangement survived everything layered
	// on top of it.
	probe string
}

// Verify proves, against the FINAL child environment, that git actually
// ignores the runtime's own files in this repository.
//
// Reasoning about precedence is not enough here. The agent's environment is
// assembled from several layers — the daemon's own variables, the agent's
// operator-supplied custom_env, then this protection — and git itself has
// three config sources with their own ordering (GIT_CONFIG_* entries outrank
// config files). A single `GIT_CONFIG_COUNT=0` in custom_env, a pre-existing
// GIT_CONFIG_* tuple, or a git too old for the variables we set would each
// leave the agent unprotected while every individual step looked fine.
//
// So this asks git, with the exact environment the child will get, whether the
// probe path is ignored. Anything other than "yes" is a failure to protect.
func (p *GitProtection) Verify(finalEnv map[string]string) error {
	if p == nil || p.probe == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", p.workDir, "check-ignore", "-q", "--no-index", p.probe)
	cmd.Env = os.Environ()
	for k, v := range finalEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: git does not ignore %s under the agent's final environment "+
			"(a custom_env entry or a pre-existing GIT_CONFIG_* tuple may be overriding it): %v",
			ErrGitExcludesUnprotected, p.probe, err)
	}
	return nil
}

// PrepareGitExcludes builds the protection for the sidecars at paths.
//
// Returns a nil protection and a nil error when workDir is not inside a git
// repository — a local_directory resource may point at a plain folder, and
// there is nothing to protect. Any failure while a git repository IS present
// is returned wrapped in ErrGitExcludesUnprotected.
func PrepareGitExcludes(envRoot, workDir string, paths []string) (*GitProtection, error) {
	root, ok := gitRepoRoot(workDir)
	if !ok {
		return nil, nil
	}
	if envRoot == "" {
		return nil, fmt.Errorf("%w: no daemon scratch directory to hold the excludes file", ErrGitExcludesUnprotected)
	}

	patterns, dropped := gitExcludePatterns(root, paths)
	if dropped > 0 {
		// Silently writing a shorter list is how a protection mechanism turns
		// into a no-op nobody notices: #7114 is what an unprotected run looks
		// like.
		return nil, fmt.Errorf("%w: %d of %d sidecar paths could not be expressed relative to %s",
			ErrGitExcludesUnprotected, dropped, len(paths), root)
	}
	if len(patterns) == 0 {
		return nil, nil
	}

	excludesBody := "# Multica runtime files for the task running in this repository.\n" +
		"# Task-scoped: read only by the agent process, and never written into\n" +
		"# your repository.\n"
	// The user's own global excludes have to be carried across: our
	// core.excludesFile REPLACES theirs inside this repository, so without
	// this the agent would start seeing every file they globally ignore.
	inherited, err := inheritedGlobalExcludes(workDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitExcludesUnprotected, err)
	}
	if inherited != "" {
		excludesBody += "\n# --- your own global excludes, preserved verbatim ---\n" + inherited + "\n"
	}
	excludesBody += "\n" + strings.Join(patterns, "\n") + "\n"

	excludesPath := filepath.Join(envRoot, gitExcludesFileName)
	if err := os.WriteFile(excludesPath, []byte(excludesBody), 0o644); err != nil {
		return nil, fmt.Errorf("%w: write %s: %v", ErrGitExcludesUnprotected, excludesPath, err)
	}

	repoConfigPath := filepath.Join(envRoot, gitRepoConfigFileName)
	repoConfig := "[core]\n\texcludesFile = " + gitConfigValue(excludesPath) + "\n"
	if err := os.WriteFile(repoConfigPath, []byte(repoConfig), 0o644); err != nil {
		return nil, fmt.Errorf("%w: write %s: %v", ErrGitExcludesUnprotected, repoConfigPath, err)
	}

	// gitdir patterns are matched against the repository's git dir with
	// forward slashes on every platform; a trailing slash matches everything
	// beneath it, which covers the repo and its linked worktrees' admin dirs.
	globalConfig := ""
	for _, inheritedCfg := range userGlobalConfigPaths() {
		globalConfig += "[include]\n\tpath = " + gitConfigValue(inheritedCfg) + "\n"
	}
	globalConfig += "[includeIf \"gitdir:" + filepath.ToSlash(root) + "/\"]\n" +
		"\tpath = " + gitConfigValue(repoConfigPath) + "\n"
	globalConfigPath := filepath.Join(envRoot, gitGlobalConfigName)
	if err := os.WriteFile(globalConfigPath, []byte(globalConfig), 0o644); err != nil {
		return nil, fmt.Errorf("%w: write %s: %v", ErrGitExcludesUnprotected, globalConfigPath, err)
	}

	return &GitProtection{
		Env: map[string]string{
			// GIT_CONFIG_GLOBAL replaces the user's ~/.gitconfig for this
			// process tree only (git >= 2.32). Verify is what catches a git
			// too old to honour it.
			"GIT_CONFIG_GLOBAL": globalConfigPath,
		},
		workDir: workDir,
		probe:   strings.TrimSuffix(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(patterns[0], "/"))), string(filepath.Separator)),
	}, nil
}

// gitConfigValue quotes a path for use as a git config value, so a path
// containing spaces, a backslash (Windows) or a quote survives the round trip.
func gitConfigValue(p string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(p, `\`, `\\`), `"`, `\"`) + `"`
}

// userGlobalConfigPaths lists the global config files git would have read, so
// the replacement config can include them and leave the user's settings in
// force. Only existing files are returned: git tolerates a missing include
// path, but listing one buys nothing.
func userGlobalConfigPaths() []string {
	var out []string
	if explicit := os.Getenv("GIT_CONFIG_GLOBAL"); explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			out = append(out, explicit)
		}
		return out
	}
	for _, candidate := range []string{xdgGitConfigPath(), homeGitConfigPath()} {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			out = append(out, candidate)
		}
	}
	return out
}

func homeGitConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gitconfig")
}

func xdgGitConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "config")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git", "config")
}

// inheritedGlobalExcludes returns the contents of whatever core.excludesFile
// resolved to before we override it. A missing or unset file is not an error —
// most users have neither.
func inheritedGlobalExcludes(workDir string) (string, error) {
	current, err := runGitTrimmed(workDir, "config", "--get", "core.excludesFile")
	if err != nil || current == "" {
		// `--get` exits non-zero when the key is unset; fall back to git's
		// documented default location.
		current = defaultGlobalExcludesPath()
		if current == "" {
			return "", nil
		}
	}
	if strings.HasPrefix(current, "~") {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", nil
		}
		current = filepath.Join(home, strings.TrimPrefix(current, "~"))
	}
	data, err := os.ReadFile(current)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the global excludes file %s: %w", current, err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// defaultGlobalExcludesPath is git's fallback when core.excludesFile is unset:
// $XDG_CONFIG_HOME/git/ignore, or ~/.config/git/ignore.
func defaultGlobalExcludesPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "ignore")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git", "ignore")
}

// gitRepoRoot returns the repository root containing workDir, canonicalised.
// ok is false when workDir is not inside a git repository, which callers treat
// as "nothing to protect" rather than an error.
func gitRepoRoot(workDir string) (string, bool) {
	out, err := runGitTrimmed(workDir, "rev-parse", "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return canonicalPath(out), true
}

// canonicalPath resolves symlinks so two spellings of one directory compare
// equal, INCLUDING for paths that do not exist yet.
//
// Both properties are load-bearing. A local_directory resource keeps the path
// the user typed while git reports the resolved one, so comparing them raw
// made every pattern look like it pointed outside the repository. And the
// tracked-path scan runs before Prepare has created any sidecar, so the paths
// it canonicalises are precisely the ones EvalSymlinks cannot resolve —
// falling back to the raw path there silently skipped the whole scan for any
// symlinked resource. Resolving the deepest existing ancestor and re-appending
// the remainder handles both.
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := filepath.Dir(p)
	rest := filepath.Base(p)
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Clean(p)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// gitExcludePatterns turns absolute sidecar paths into ignore patterns
// anchored at the repository root, sorted so repeated runs on an unchanged
// sidecar set produce identical bytes. dropped counts paths that could not be
// expressed relative to root; the caller treats a non-zero count as a failure
// to protect rather than as a shorter list.
func gitExcludePatterns(root string, paths []string) (patterns []string, dropped int) {
	seen := make(map[string]struct{}, len(paths))
	patterns = make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(root, canonicalPath(p))
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			dropped++
			continue
		}
		pattern := "/" + filepath.ToSlash(rel)
		// A trailing slash restricts the pattern to directories. Stat rather
		// than trusting the caller: the manifest records files and dirs in one
		// list shape, and a directory pattern that matched a file (or the
		// reverse) would silently fail to exclude anything.
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			pattern += "/"
		}
		if _, dup := seen[pattern]; dup {
			continue
		}
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns, dropped
}

// SidecarScanRoots lists every path under workDir that writeContextFiles and
// its callees may create, in canonical form.
//
// Kept next to the tracked-path scan because it is the scan's coverage: a
// sidecar target missing from this list is a path the scan cannot protect. The
// entries mirror the recordWriteFile / recordMkdirAll call sites —
// context.go (.agent_context, .multica, the runtime's skills tree),
// cursor_mcp.go (.cursor) and reasonix_permissions.go (reasonix.toml).
func SidecarScanRoots(workDir, provider string) []string {
	return []string{
		canonicalPath(filepath.Join(workDir, ".agent_context")),
		canonicalPath(filepath.Join(workDir, ".multica")),
		canonicalPath(filepath.Join(workDir, ".cursor")),
		canonicalPath(filepath.Join(workDir, reasonixProjectConfigFile)),
		canonicalPath(skillsDirPath(workDir, provider)),
	}
}

// GitTrackedFilesUnder returns the canonical absolute paths of files git
// already tracks beneath roots, in the repository containing workDir.
//
// This is the state no ignore rule can help with. A repository already
// polluted by GitHub #7114 typically carries committed sidecars whose files a
// later cleanup deleted from the working tree: `os.Lstat` says "absent, safe
// to create", so Prepare writes there again — and because the path is tracked,
// no excludes file can hide it and the next `git add -A` stages Multica
// content once more. That is the reported loop, reproducing on exactly the
// repositories that already suffered it.
//
// Prepare therefore treats these paths as belonging to the user and refuses to
// write them: skills get a collision-free alternative directory, and the
// Multica-only markers degrade to absent, which their callers already
// tolerate.
//
// A non-git workDir yields no paths and no error — there is nothing to scan.
// A git failure IS an error: an empty result is indistinguishable from "this
// repository is clean", and guessing clean is what re-arms the loop.
func GitTrackedFilesUnder(workDir string, roots []string) (map[string]struct{}, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	repoRoot, ok := gitRepoRoot(workDir)
	if !ok {
		return nil, nil
	}
	args := []string{"ls-files", "-z", "--"}
	for _, r := range roots {
		rel, err := filepath.Rel(repoRoot, canonicalPath(r))
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		args = append(args, filepath.ToSlash(rel))
	}
	if len(args) == 3 {
		return nil, nil
	}
	// Pathspec-limited so this stays cheap even on a very large repository.
	out, err := runGitStdout(workDir, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: list tracked sidecar paths in %s: %v", ErrGitExcludesUnprotected, repoRoot, err)
	}

	tracked := make(map[string]struct{})
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		tracked[filepath.Join(repoRoot, filepath.FromSlash(entry))] = struct{}{}
	}
	if len(tracked) == 0 {
		return nil, nil
	}
	return tracked, nil
}

// excludablePaths returns the minimal set of paths covering everything the
// manifest records: every created directory that has no created ancestor, plus
// every created file that sits outside all of them.
//
// Minimal matters because each pattern is a line in the ignore file. Recording
// `.grok/skills/multica-a`, `.grok/skills/multica-b`, … when we also created
// `.grok/` itself would write a dozen redundant lines describing one subtree.
// Emitting the shallowest created path is both smaller and more accurate: it
// covers exactly the tree we brought into existence and stops at the boundary
// where the user's own content begins.
func (m sidecarManifest) excludablePaths() []string {
	roots := make([]string, 0, len(m.Dirs))
	for _, d := range m.Dirs {
		if !hasAncestorIn(d, roots) {
			roots = append(roots, d)
		}
	}
	out := append([]string(nil), roots...)
	for _, f := range m.Files {
		if !hasAncestorIn(f, roots) {
			out = append(out, f)
		}
	}
	return out
}

// hasAncestorIn reports whether path lies inside any of the given directories.
func hasAncestorIn(path string, dirs []string) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, path)
		if err != nil || rel == "." {
			continue
		}
		if filepath.IsLocal(rel) {
			return true
		}
	}
	return false
}
