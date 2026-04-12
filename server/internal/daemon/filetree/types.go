// Package filetree provides file tree scanning and change detection for agent worktrees.
package filetree

// FileNode represents a file or directory in the worktree.
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // "file" or "directory"
	Children []*FileNode `json:"children,omitempty"`
}

// GitStatus represents the git status of a file.
type GitStatus string

const (
	StatusModified  GitStatus = "M"
	StatusAdded     GitStatus = "A"
	StatusDeleted   GitStatus = "D"
	StatusUntracked GitStatus = "?"
	StatusRenamed   GitStatus = "R"
)

// Snapshot is a complete file tree snapshot with git status information.
type Snapshot struct {
	Tree      []*FileNode          `json:"tree"`
	GitStatus map[string]GitStatus `json:"git_status"`
}
