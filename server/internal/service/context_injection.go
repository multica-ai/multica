package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ContextInjection handles building rich context for agent tasks.
type ContextInjection struct {
	Queries *db.Queries
}

// NewContextInjection creates a new ContextInjection service.
func NewContextInjection(q *db.Queries) *ContextInjection {
	return &ContextInjection{Queries: q}
}

// ProjectContext holds collected project metadata for agent injection.
type ProjectContext struct {
	FileTree      string   `json:"file_tree,omitempty"`
	RecentCommits string   `json:"recent_commits,omitempty"`
	OpenIssues    []string `json:"open_issues,omitempty"`
}

// CollectProjectContext gathers project metadata from a directory.
func (ci *ContextInjection) CollectProjectContext(projectDir string) ProjectContext {
	ctx := ProjectContext{}

	// File tree (limited to top 2 levels, 100 entries).
	ctx.FileTree = collectFileTree(projectDir, 2, 100)

	// Recent commits (last 10).
	ctx.RecentCommits = collectRecentCommits(projectDir, 10)

	return ctx
}

// BuildIssueContext collects issue details and comments for agent injection.
func (ci *ContextInjection) BuildIssueContext(ctx context.Context, issueID pgtype.UUID) string {
	issue, err := ci.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Issue: %s\n\n", issue.Title)
	if issue.Description.Valid && issue.Description.String != "" {
		b.WriteString(issue.Description.String)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "**Status:** %s | **Priority:** %s\n\n", issue.Status, issue.Priority)

	// Add comments.
	comments, err := ci.Queries.ListComments(ctx, db.ListCommentsParams{
		IssueID:     issueID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err == nil && len(comments) > 0 {
		b.WriteString("## Comments\n\n")
		for _, c := range comments {
			authorLabel := "User"
			if c.AuthorType == "agent" {
				authorLabel = "Agent"
			}
			fmt.Fprintf(&b, "**%s** (%s):\n%s\n\n", authorLabel, util.TimestampToString(c.CreatedAt), c.Content)
		}
	}

	return b.String()
}

// GenerateDynamicClaudeMD creates a CLAUDE.md tailored to a specific project.
func (ci *ContextInjection) GenerateDynamicClaudeMD(ctx context.Context, projectDir string, issueID pgtype.UUID, agentInstructions string, skills []AgentSkillData) string {
	var b strings.Builder

	b.WriteString("# Multica Agent Runtime\n\n")
	b.WriteString("You are a coding agent in the Multica platform.\n\n")

	// Agent identity.
	if agentInstructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(agentInstructions)
		b.WriteString("\n\n")
	}

	// Project context.
	projCtx := ci.CollectProjectContext(projectDir)
	if projCtx.FileTree != "" {
		b.WriteString("## Project Structure\n\n")
		b.WriteString("```\n")
		b.WriteString(projCtx.FileTree)
		b.WriteString("```\n\n")
	}
	if projCtx.RecentCommits != "" {
		b.WriteString("## Recent Commits\n\n")
		b.WriteString("```\n")
		b.WriteString(projCtx.RecentCommits)
		b.WriteString("```\n\n")
	}

	// Issue context.
	issueCtx := ci.BuildIssueContext(ctx, issueID)
	if issueCtx != "" {
		b.WriteString(issueCtx)
	}

	// Skills.
	if len(skills) > 0 {
		b.WriteString("## Available Skills\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**: %s\n", skill.Name, skill.Content)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// collectFileTree lists files up to the given depth.
func collectFileTree(dir string, maxDepth, maxEntries int) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use find to collect file tree.
	args := []string{dir, "-maxdepth", fmt.Sprintf("%d", maxDepth), "-type", "f,d", "-not", "-path", "*/.git/*", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/.next/*"}
	cmd := exec.CommandContext(ctx, "find", args...)
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("collectFileTree failed", "dir", dir, "error", err)
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > maxEntries {
		lines = lines[:maxEntries]
		lines = append(lines, fmt.Sprintf("... (%d more entries)", len(lines)-maxEntries))
	}

	// Convert to relative paths.
	var result []string
	for _, line := range lines {
		rel, err := filepath.Rel(dir, line)
		if err != nil {
			continue
		}
		if rel == "." {
			continue
		}
		result = append(result, rel)
	}

	return strings.Join(result, "\n")
}

// collectRecentCommits returns the last N git log entries.
func collectRecentCommits(dir string, n int) string {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", fmt.Sprintf("-%d", n))
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}
