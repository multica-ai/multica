package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildPrompt constructs the task prompt for an agent CLI.
func BuildPrompt(task Task, workdir string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n")
	b.WriteString("Complete the assigned issue using the local environment.\n")
	b.WriteString("Return a concise Markdown comment suitable for posting back to the issue.\n")
	b.WriteString("If you cannot complete the task because context, files, or permissions are missing, return status \"blocked\" and explain the blocker in the comment.\n\n")

	fmt.Fprintf(&b, "Working directory: %s\n", workdir)
	fmt.Fprintf(&b, "Agent: %s\n", task.Context.Agent.Name)
	fmt.Fprintf(&b, "Issue title: %s\n\n", task.Context.Issue.Title)

	if task.Context.Issue.Description != "" {
		b.WriteString("Issue description:\n")
		b.WriteString(task.Context.Issue.Description)
		b.WriteString("\n\n")
	}

	if len(task.Context.Issue.AcceptanceCriteria) > 0 {
		b.WriteString("Acceptance criteria:\n")
		for _, item := range task.Context.Issue.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}

	if len(task.Context.Issue.ContextRefs) > 0 {
		b.WriteString("Context refs:\n")
		for _, item := range task.Context.Issue.ContextRefs {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}

	if repo := task.Context.Issue.Repository; repo != nil {
		b.WriteString("Repository context:\n")
		if repo.URL != "" {
			fmt.Fprintf(&b, "- url: %s\n", repo.URL)
		}
		if repo.Branch != "" {
			fmt.Fprintf(&b, "- branch: %s\n", repo.Branch)
		}
		if repo.Path != "" {
			fmt.Fprintf(&b, "- path: %s\n", repo.Path)
		}
		b.WriteString("\n")
	}

	if task.Context.WorkspaceContext != "" {
		b.WriteString("Workspace context:\n")
		b.WriteString(task.Context.WorkspaceContext)
		b.WriteString("\n\n")
	}

	if task.Context.Agent.Skills != "" {
		b.WriteString("Agent skills/instructions:\n")
		b.WriteString(task.Context.Agent.Skills)
		b.WriteString("\n\n")
	}

	b.WriteString("Comment requirements:\n")
	b.WriteString("- Lead with the outcome.\n")
	b.WriteString("- Mention concrete files or commands if you changed anything.\n")
	b.WriteString("- Mention blockers or follow-up actions if relevant.\n")

	return b.String()
}

// ResolveTaskWorkdir determines the working directory for a task.
func ResolveTaskWorkdir(reposRoot string, repo *RepoRef) (string, error) {
	base := reposRoot
	if repo == nil || strings.TrimSpace(repo.Path) == "" {
		return base, nil
	}

	path := strings.TrimSpace(repo.Path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("repository path not found: %s", path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository path is not a directory: %s", path)
	}
	return path, nil
}
