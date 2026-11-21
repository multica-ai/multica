package agent

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Codex prefers the cwd entry (even without a trust level) to the validated
// main Git checkout. Instruction root markers do not define this trust root.
func codexActiveProjectUntrusted(cwd string, projects map[string]codexProjectTrust) bool {
	lookup := func(path string) (codexProjectTrust, bool) {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			canonical = path
		}
		for _, key := range []string{canonical, path} {
			if project, ok := projects[key]; ok {
				return project, true
			}
			if runtime.GOOS == "windows" {
				keys := make([]string, 0, len(projects))
				for candidate := range projects {
					keys = append(keys, candidate)
				}
				slices.Sort(keys)
				for _, candidate := range keys {
					if codexWindowsTrustKey(candidate) == codexWindowsTrustKey(key) {
						return projects[candidate], true
					}
				}
			}
		}
		return codexProjectTrust{}, false
	}
	if project, ok := lookup(cwd); ok {
		return project.TrustLevel == "untrusted"
	}
	if root := codexGitTrustRoot(cwd); root != "" {
		project, _ := lookup(root)
		return project.TrustLevel == "untrusted"
	}
	return false
}

func codexWindowsTrustKey(path string) string {
	data := []byte(path)
	for i, c := range data {
		if c >= 'A' && c <= 'Z' {
			data[i] = c + ('a' - 'A')
		}
	}
	return string(data)
}

// Mirrors the native filesystem-only Git trust-root lookup, including registered
// linked worktrees. A .git directory without HEAD is not a repository root.
func codexGitTrustRoot(cwd string) string {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dot := filepath.Join(dir, ".git")
		info, err := os.Stat(dot)
		if err == nil {
			if info.IsDir() {
				if _, err := os.Stat(filepath.Join(dot, "HEAD")); err == nil {
					return dir
				}
			} else {
				return codexLinkedWorktreeTrustRoot(dir, dot)
			}
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func codexGitMetadata(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return "", false
	}
	return strings.Trim(string(data), " \t\r\n\v\f"), true
}

func codexJoinNative(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func codexGitdir(path string) (string, bool) {
	text, ok := codexGitMetadata(path)
	if !ok || !strings.HasPrefix(text, "gitdir:") {
		return "", false
	}
	target := strings.Trim(strings.TrimPrefix(text, "gitdir:"), " \t\r\n\v\f")
	if target == "" {
		return "", false
	}
	return codexJoinNative(filepath.Dir(path), target), true
}

func codexLinkedWorktreeTrustRoot(checkout, dot string) string {
	gitdir, ok := codexGitdir(dot)
	if !ok {
		return ""
	}
	info, err := os.Lstat(gitdir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	canonical, err := filepath.EvalSymlinks(gitdir)
	if err != nil {
		return ""
	}
	worktrees := filepath.Dir(canonical)
	if filepath.Base(worktrees) != "worktrees" {
		return ""
	}
	common := filepath.Dir(worktrees)
	registered, ok := codexGitMetadata(filepath.Join(canonical, "gitdir"))
	if !ok || registered == "" {
		return ""
	}
	registered = codexJoinNative(canonical, registered)
	if filepath.Base(registered) != ".git" {
		return ""
	}
	linkedCommon, ok := codexGitMetadata(filepath.Join(canonical, "commondir"))
	if !ok || linkedCommon == "" {
		return ""
	}
	linkedCommon = codexJoinNative(canonical, linkedCommon)
	registeredRoot, err1 := filepath.EvalSymlinks(filepath.Dir(registered))
	checkoutRoot, err2 := filepath.EvalSymlinks(checkout)
	actualCommon, err3 := filepath.EvalSymlinks(linkedCommon)
	if err1 != nil || err2 != nil || err3 != nil || registeredRoot != checkoutRoot || actualCommon != common {
		return ""
	}
	mainRoot := filepath.Dir(filepath.Dir(filepath.Dir(gitdir)))
	mainGit := filepath.Join(mainRoot, ".git")
	mainInfo, err := os.Stat(mainGit)
	if err != nil {
		return ""
	}
	if !mainInfo.IsDir() {
		var ok bool
		mainGit, ok = codexGitdir(mainGit)
		if !ok {
			return ""
		}
	}
	actualMain, err := filepath.EvalSymlinks(mainGit)
	if err != nil || actualMain != common {
		return ""
	}
	return mainRoot
}
