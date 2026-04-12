package filetree

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Directories and files to skip when scanning (matches VSCode defaults).
var defaultIgnore = map[string]bool{
	".git":         true,
	"node_modules": true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".DS_Store":    true,
	"vendor":       true,
	"coverage":     true,
	".turbo":       true,
	".cache":       true,
	".venv":        true,
	"venv":         true,
	".tox":         true,
	".mypy_cache":  true,
	".pytest_cache": true,
}

// Scan builds a file tree from the given root directory.
func Scan(root string) ([]*FileNode, error) {
	return scanDir(root, root)
}

func scanDir(dir, root string) ([]*FileNode, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var nodes []*FileNode
	for _, entry := range entries {
		name := entry.Name()

		// Skip ignored entries.
		if defaultIgnore[name] {
			continue
		}
		// Skip hidden files/dirs (dotfiles) except well-known ones.
		if strings.HasPrefix(name, ".") && !isWellKnownDotfile(name) {
			continue
		}

		fullPath := filepath.Join(dir, name)
		relPath, _ := filepath.Rel(root, fullPath)

		if entry.IsDir() {
			children, err := scanDir(fullPath, root)
			if err != nil {
				continue // skip unreadable directories
			}
			nodes = append(nodes, &FileNode{
				Name:     name,
				Path:     relPath,
				Type:     "directory",
				Children: children,
			})
		} else {
			nodes = append(nodes, &FileNode{
				Name: name,
				Path: relPath,
				Type: "file",
			})
		}
	}

	// Sort: directories first, then alphabetical.
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type == "directory"
		}
		return nodes[i].Name < nodes[j].Name
	})

	return nodes, nil
}

// isWellKnownDotfile returns true for dotfiles that should be shown in the tree.
func isWellKnownDotfile(name string) bool {
	switch name {
	case ".github", ".feature-plans", ".agent_context", ".claude", ".config":
		return true
	}
	return false
}

// ScanGitStatus returns the git status of files in the given worktree.
//
// Handles two layouts:
//  1. worktreePath is itself a git repo — scan directly.
//  2. worktreePath contains one or more git repos as top-level subdirs
//     (this is how the daemon stages cloned repos inside an exec env's
//     workdir). Each subdir is scanned and its status entries are
//     prefixed with "<subdir>/" so they line up with the file tree paths.
func ScanGitStatus(worktreePath string) map[string]GitStatus {
	result := make(map[string]GitStatus)

	// Case 1: worktreePath itself is a git repo.
	if isGitRepo(worktreePath) {
		collectGitStatus(worktreePath, "", result)
		return result
	}

	// Case 2: walk top-level subdirs looking for nested git repos.
	entries, err := os.ReadDir(worktreePath)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if defaultIgnore[name] {
			continue
		}
		if strings.HasPrefix(name, ".") && !isWellKnownDotfile(name) {
			continue
		}
		sub := filepath.Join(worktreePath, name)
		if !isGitRepo(sub) {
			continue
		}
		collectGitStatus(sub, name+"/", result)
	}

	return result
}

// isGitRepo reports whether dir contains a .git entry (file or directory).
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// FindBaseRef returns the merge-base commit between HEAD and a well-known
// base branch (origin/main, origin/master, main, master — first that exists),
// or an empty string if none is found. Exported so callers (e.g. the diff
// endpoint) can reuse the same base-branch selection logic as the scanner.
func FindBaseRef(repoPath string) string {
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		cmd := exec.Command("git", "-C", repoPath, "merge-base", "HEAD", ref)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		sha := strings.TrimSpace(string(out))
		if sha != "" {
			return sha
		}
	}
	return ""
}

// collectGitStatus collects the set of files an agent has changed in repoPath,
// relative to either the merge-base against a well-known base branch OR the
// current HEAD when no base can be found. Committed, uncommitted, and
// untracked changes are all merged into result. Each path is prefixed with
// pathPrefix so callers with nested repos can namespace entries.
func collectGitStatus(repoPath, pathPrefix string, result map[string]GitStatus) {
	// 1. Diff against the base branch — picks up BOTH committed work on the
	//    agent's feature branch AND any uncommitted tracked changes.
	if base := FindBaseRef(repoPath); base != "" {
		cmd := exec.Command("git", "-C", repoPath, "diff", "--name-status", base)
		if out, err := cmd.Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				parts := strings.Split(line, "\t")
				if len(parts) < 2 || len(parts[0]) == 0 {
					continue
				}
				code := parts[0][0]
				var status GitStatus
				switch code {
				case 'M', 'T':
					status = StatusModified
				case 'A', 'C':
					status = StatusAdded
				case 'D':
					status = StatusDeleted
				case 'R':
					// Rename: "R100\told\tnew" — record the new path.
					if len(parts) >= 3 {
						result[pathPrefix+parts[2]] = StatusRenamed
					}
					continue
				default:
					continue
				}
				result[pathPrefix+parts[1]] = status
			}
		}
	}

	// 2. Porcelain status — catches untracked files (git diff doesn't show them)
	//    and also overrides with uncommitted modifications/deletions when they
	//    supersede the base diff.
	cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain=v1", "-uall")
	out, err := cmd.Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		var status GitStatus
		if line[0] == '?' {
			status = StatusUntracked
		} else if line[0] != ' ' {
			status = GitStatus(string(line[0]))
		} else {
			status = GitStatus(string(line[1]))
		}
		filePath := line[3:]
		// Handle renames: "R  old -> new"
		if status == StatusRenamed {
			if idx := strings.Index(filePath, " -> "); idx >= 0 {
				filePath = filePath[idx+4:]
			}
		}
		result[pathPrefix+filePath] = status
	}
}

// ScanSnapshot builds a complete snapshot (tree + git status) of a worktree.
func ScanSnapshot(worktreePath string) (*Snapshot, error) {
	tree, err := Scan(worktreePath)
	if err != nil {
		return nil, err
	}

	gitStatus := ScanGitStatus(worktreePath)

	return &Snapshot{
		Tree:      tree,
		GitStatus: gitStatus,
	}, nil
}
