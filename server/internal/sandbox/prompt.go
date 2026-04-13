package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PromptResult contains both the prompt text and environment variables to inject into the sandbox.
type PromptResult struct {
	Prompt  string
	EnvVars map[string]string
}

// BuildPrompt constructs a self-contained prompt for a sandbox agent.
// All context is pre-fetched from the DB — the agent receives everything it needs in the prompt
// and does not call any Multica CLI or API.
//
// Credentials (Git PAT, AI gateway key) are returned in EnvVars, never embedded in the prompt text.
func BuildPrompt(ctx context.Context, task db.AgentTaskQueue, queries *db.Queries, sandboxCfg db.WorkspaceSandboxConfig, gitPat, aiGatewayKey string) (*PromptResult, error) {
	// Fetch issue
	issue, err := queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return nil, fmt.Errorf("sandbox prompt: get issue: %w", err)
	}

	// Fetch agent
	agent, err := queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return nil, fmt.Errorf("sandbox prompt: get agent: %w", err)
	}

	// Fetch workspace (for repos)
	workspace, err := queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("sandbox prompt: get workspace: %w", err)
	}

	// Fetch comments (recent, for context)
	comments, err := queries.ListComments(ctx, db.ListCommentsParams{
		IssueID:     task.IssueID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		comments = nil // non-fatal
	}

	// Fetch agent skills
	skills, err := queries.ListAgentSkills(ctx, task.AgentID)
	if err != nil {
		skills = nil // non-fatal
	}

	// Build prompt
	var b strings.Builder

	// Task section
	b.WriteString("## Task\n\n")
	b.WriteString(fmt.Sprintf("**%s**\n\n", issue.Title))
	if issue.Description.Valid && issue.Description.String != "" {
		b.WriteString(issue.Description.String)
		b.WriteString("\n\n")
	}

	// Context section (comments)
	if len(comments) > 0 {
		// If comment-triggered, highlight the trigger comment
		triggerID := task.TriggerCommentID
		b.WriteString("## Context\n\n")
		for _, c := range comments {
			if c.Type == "system" {
				continue
			}
			prefix := ""
			if triggerID.Valid && c.ID == triggerID {
				prefix = " (trigger)"
			}
			b.WriteString(fmt.Sprintf("**%s%s:**\n%s\n\n", c.AuthorType, prefix, c.Content))
		}
	}

	// Repositories section
	repos := parseRepos(workspace.Repos)
	if len(repos) > 0 {
		b.WriteString("## Repositories\n\n")
		for _, repo := range repos {
			cloneURL := repo.URL
			if gitPat != "" {
				cloneURL = injectTokenPlaceholder(cloneURL)
			}
			b.WriteString(fmt.Sprintf("```bash\ngit clone %s\ncd %s\n```\n\n", cloneURL, repo.Name))
		}
		if gitPat != "" {
			b.WriteString("Note: `$GIT_TOKEN` is available as an environment variable for git authentication.\n\n")
		}
	}

	// Instructions section
	if agent.Instructions != "" {
		b.WriteString("## Instructions\n\n")
		b.WriteString(agent.Instructions)
		b.WriteString("\n\n")
	}

	// Skills section
	if len(skills) > 0 {
		b.WriteString("## Skills\n\n")
		for _, skill := range skills {
			b.WriteString(fmt.Sprintf("### %s\n\n", skill.Name))
			if skill.Content != "" {
				b.WriteString(skill.Content)
				b.WriteString("\n\n")
			}
		}
	}

	// Output section
	b.WriteString("## Output\n\n")
	b.WriteString("When you are done:\n")
	b.WriteString("- Push your changes to a new branch and create a Pull Request\n")
	b.WriteString("- Write a brief summary of what you did to `./SUMMARY.md`\n")

	// Build env vars (credentials never go in prompt text)
	envVars := make(map[string]string)
	if gitPat != "" {
		envVars["GIT_TOKEN"] = gitPat
	}
	if aiGatewayKey != "" {
		envVars["AI_GATEWAY_API_KEY"] = aiGatewayKey
	}

	return &PromptResult{
		Prompt:  b.String(),
		EnvVars: envVars,
	}, nil
}

// repoInfo represents a parsed repository entry from workspace.repos JSONB.
type repoInfo struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

func parseRepos(raw []byte) []repoInfo {
	if raw == nil {
		return nil
	}

	// Try structured format first: [{"url": "...", "name": "..."}]
	var repos []repoInfo
	if err := json.Unmarshal(raw, &repos); err == nil && len(repos) > 0 && repos[0].URL != "" {
		for i := range repos {
			if repos[i].Name == "" {
				repos[i].Name = repoNameFromURL(repos[i].URL)
			}
		}
		return repos
	}

	// Fallback: array of strings ["https://github.com/org/repo.git"]
	var urls []string
	if err := json.Unmarshal(raw, &urls); err != nil {
		return nil
	}
	repos = nil
	for _, u := range urls {
		if u == "" {
			continue
		}
		repos = append(repos, repoInfo{URL: u, Name: repoNameFromURL(u)})
	}
	return repos
}

func repoNameFromURL(url string) string {
	// Extract repo name from URL like "https://github.com/org/repo.git"
	parts := strings.Split(strings.TrimSuffix(url, ".git"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "repo"
}

// injectTokenPlaceholder replaces auth in a git URL with $GIT_TOKEN reference.
// "https://github.com/org/repo.git" → "https://$GIT_TOKEN@github.com/org/repo.git"
func injectTokenPlaceholder(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "https://$GIT_TOKEN@" + strings.TrimPrefix(url, "https://")
	}
	return url
}

// uuidEqual checks if two pgtype.UUID values are equal.
func uuidEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}

// Ensure the unused import is used (for pgtype.UUID comparison).
var _ pgtype.UUID
