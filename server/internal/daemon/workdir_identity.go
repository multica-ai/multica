package daemon

const githubRepoResourceType = "github_repo"

func workDirIdentityForTask(task Task, localAssignment *localDirectoryAssignment) string {
	if task.IssueID != "" && localAssignment == nil && taskUsesGitRepoWorktree(task) {
		return task.IssueID
	}
	return task.ID
}

func taskUsesGitRepoWorktree(task Task) bool {
	for _, resource := range task.ProjectResources {
		if resource.ResourceType == githubRepoResourceType {
			return true
		}
	}
	return len(task.Repos) > 0
}
