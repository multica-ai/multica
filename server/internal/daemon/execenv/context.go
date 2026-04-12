package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// writeContextFiles renders and writes .agent_context/issue_context.md and
// skills into the appropriate provider-native location.
//
// Claude:   skills → {workDir}/.claude/skills/{name}/SKILL.md  (native discovery)
// Codex:    skills → handled separately in Prepare via codex-home
// OpenCode: skills → {workDir}/.config/opencode/skills/{name}/SKILL.md  (native discovery)
// Default:  skills → {workDir}/.agent_context/skills/{name}/SKILL.md
func writeContextFiles(workDir, provider string, ctx TaskContextForEnv) error {
	contextDir := filepath.Join(workDir, ".agent_context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return fmt.Errorf("create .agent_context dir: %w", err)
	}

	content := renderIssueContext(provider, ctx)
	path := filepath.Join(contextDir, "issue_context.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write issue_context.md: %w", err)
	}

	if len(ctx.AgentSkills) > 0 {
		skillsDir, err := resolveSkillsDir(workDir, provider)
		if err != nil {
			return fmt.Errorf("resolve skills dir: %w", err)
		}
		// Codex skills are written to codex-home in Prepare; skip here.
		if provider != "codex" {
			if err := writeSkillFiles(skillsDir, ctx.AgentSkills); err != nil {
				return fmt.Errorf("write skill files: %w", err)
			}
		}
	}

	return nil
}

// resolveSkillsDir returns the directory where skills should be written
// based on the agent provider.
func resolveSkillsDir(workDir, provider string) (string, error) {
	var skillsDir string
	switch provider {
	case "claude":
		// Claude Code natively discovers skills from .claude/skills/ in the workdir.
		skillsDir = filepath.Join(workDir, ".claude", "skills")
	case "opencode":
		// OpenCode natively discovers skills from .config/opencode/skills/ in the workdir.
		skillsDir = filepath.Join(workDir, ".config", "opencode", "skills")
	default:
		// Fallback: write to .agent_context/skills/ (referenced by meta config).
		skillsDir = filepath.Join(workDir, ".agent_context", "skills")
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return "", err
	}
	return skillsDir, nil
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeSkillName converts a skill name to a safe directory name.
func sanitizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}

// writeSkillFiles writes skill directories into the given parent directory.
// Each skill gets its own subdirectory containing SKILL.md and supporting files.
func writeSkillFiles(skillsDir string, skills []SkillContextForEnv) error {
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	for _, skill := range skills {
		dir := filepath.Join(skillsDir, sanitizeSkillName(skill.Name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		// Write main SKILL.md
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill.Content), 0o644); err != nil {
			return err
		}

		// Write supporting files
		for _, f := range skill.Files {
			fpath := filepath.Join(dir, f.Path)
			if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(fpath, []byte(f.Content), 0o644); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderIssueContext builds the markdown content for issue_context.md.
func renderIssueContext(provider string, ctx TaskContextForEnv) string {
	var b strings.Builder

	b.WriteString("# Task Assignment\n\n")
	fmt.Fprintf(&b, "**Issue ID:** %s\n", ctx.IssueID)
	if ctx.Issue != nil {
		if ctx.Issue.Identifier != "" {
			fmt.Fprintf(&b, "**Issue:** %s\n", ctx.Issue.Identifier)
		}
		fmt.Fprintf(&b, "**Title:** %s\n", ctx.Issue.Title)
		if ctx.Issue.Status != "" {
			fmt.Fprintf(&b, "**Status:** %s\n", ctx.Issue.Status)
		}
		if ctx.Issue.Priority != "" {
			fmt.Fprintf(&b, "**Priority:** %s\n", ctx.Issue.Priority)
		}
		if ctx.Issue.ParentIssueID != "" {
			fmt.Fprintf(&b, "**Parent issue ID:** %s\n", ctx.Issue.ParentIssueID)
		}
		if ctx.Issue.ProjectID != "" {
			fmt.Fprintf(&b, "**Project ID:** %s\n", ctx.Issue.ProjectID)
		}
		if ctx.Issue.AssigneeType != "" || ctx.Issue.AssigneeID != "" {
			fmt.Fprintf(&b, "**Assignee:** %s %s\n", strings.TrimSpace(ctx.Issue.AssigneeType), strings.TrimSpace(ctx.Issue.AssigneeID))
		}
	}
	b.WriteString("\n")

	if ctx.TriggerCommentID != "" {
		b.WriteString("**Trigger:** Comment Reply\n")
		b.WriteString("**Triggering comment ID:** `" + ctx.TriggerCommentID + "`\n\n")
	} else {
		b.WriteString("**Trigger:** New Assignment\n\n")
	}

	if ctx.Issue != nil && strings.TrimSpace(ctx.Issue.Description) != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(strings.TrimSpace(ctx.Issue.Description))
		b.WriteString("\n\n")
	}

	b.WriteString("## Quick Start\n\n")
	b.WriteString("Use this file as the primary task context. The daemon injected it before launching you so sandboxed runtimes can work even when the local Multica API is unavailable.\n")
	fmt.Fprintf(&b, "If you need newer comments or platform writeback and the CLI works, you may run `multica issue get %s --output json`. Do not block on that command just to understand the assignment.\n\n", ctx.IssueID)

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		b.WriteString("The following skills are available to you:\n\n")
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}
