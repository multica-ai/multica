package daemon

import (
	"errors"
	"fmt"
	"path/filepath"
)

// validateControllerTask rejects host, attempt, source and repository substitution
// before allocating a directory or running any provider executable.
func validateControllerTask(task Task, hostID string) error {
	m := task.LaunchAuthority
	if m == nil {
		return nil
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.RunID != task.ID || m.IssueID != task.IssueID || m.ProfileID != task.AgentID || m.RuntimeID != task.RuntimeID || m.HostID != hostID || m.WorkspaceID != task.WorkspaceID {
		return errors.New("launch authority does not match this host and attempt")
	}
	a, err := localDirectoryAssignmentForTask(task, hostID)
	if err != nil {
		return err
	}
	if a == nil || !a.UsesRunWorkspace() || filepath.Clean(a.AbsPath) != filepath.Clean(m.SourcePath) || a.Ref.BaseCommit != m.BaseCommit {
		return errors.New("execution room differs from launch authority")
	}
	if len(task.Repos) != len(m.AllowedRepositories) {
		return errors.New("repository allowlist differs from launch authority")
	}
	for i, repo := range task.Repos {
		if repo.URL != m.AllowedRepositories[i].URL || repo.Ref != m.AllowedRepositories[i].Commit {
			return fmt.Errorf("repository %d differs from launch authority", i)
		}
	}
	return nil
}
