package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxLocalRepos = 200

func discoverLocalRepos(roots []string) []LocalRepoData {
	out := make([]LocalRepoData, 0)
	seen := make(map[string]struct{})
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(rootAbs)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(rootAbs, name)
			if !isGitRepoDir(path) {
				continue
			}
			clean, err := filepath.EvalSymlinks(path)
			if err != nil {
				clean = path
			}
			clean, err = filepath.Abs(clean)
			if err != nil {
				continue
			}
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			out = append(out, LocalRepoData{Name: name, Path: clean, Root: rootAbs})
			if len(out) >= maxLocalRepos {
				return sortedLocalRepos(out)
			}
		}
	}
	return sortedLocalRepos(out)
}

func sortedLocalRepos(repos []LocalRepoData) []LocalRepoData {
	sort.Slice(repos, func(i, j int) bool {
		return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
	})
	return repos
}

func isGitRepoDir(path string) bool {
	if st, err := os.Stat(filepath.Join(path, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
		return true
	}
	return false
}

func localRepoAllowed(roots []string, repoPath string) bool {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return false
	}
	clean, err := filepath.EvalSymlinks(repoPath)
	if err != nil {
		clean = repoPath
	}
	clean, err = filepath.Abs(clean)
	if err != nil {
		return false
	}
	for _, repo := range discoverLocalRepos(roots) {
		if repo.Path == clean {
			return true
		}
	}
	return false
}
