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
// Copilot:  skills → {workDir}/.github/skills/{name}/SKILL.md  (native project-level discovery)
// OpenCode: skills → {workDir}/.config/opencode/skills/{name}/SKILL.md  (native discovery)
// Pi:       skills → {workDir}/.pi/skills/{name}/SKILL.md  (native discovery)
// Cursor:   skills → {workDir}/.cursor/skills/{name}/SKILL.md  (native discovery)
// Kimi:     skills → {workDir}/.kimi/skills/{name}/SKILL.md  (native discovery)
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
	case "copilot":
		// GitHub Copilot CLI natively discovers project-level skills from
		// .github/skills/<name>/SKILL.md (takes precedence over user-level
		// skills in ~/.copilot/skills/).
		// See: https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference
		skillsDir = filepath.Join(workDir, ".github", "skills")
	case "opencode":
		// OpenCode natively discovers skills from .config/opencode/skills/ in the workdir.
		skillsDir = filepath.Join(workDir, ".config", "opencode", "skills")
	case "pi":
		// Pi natively discovers skills from .pi/skills/ in the workdir.
		skillsDir = filepath.Join(workDir, ".pi", "skills")
	case "cursor":
		// Cursor natively discovers skills from .cursor/skills/ in the workdir.
		skillsDir = filepath.Join(workDir, ".cursor", "skills")
	case "kimi":
		// Kimi Code CLI auto-discovers project-level skills from .kimi/skills/
		// in the workdir. See https://moonshotai.github.io/kimi-cli/en/customization/skills.html
		skillsDir = filepath.Join(workDir, ".kimi", "skills")
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
	if ctx.AutopilotRunID != "" {
		return renderAutopilotContext(ctx)
	}

	var b strings.Builder

	b.WriteString("# Task Assignment\n\n")
	fmt.Fprintf(&b, "**Issue ID:** %s\n\n", ctx.IssueID)

	if ctx.TriggerCommentID != "" {
		b.WriteString("**Trigger:** Comment Reply\n")
		b.WriteString("**Triggering comment ID:** `" + ctx.TriggerCommentID + "`\n\n")
	} else {
		b.WriteString("**Trigger:** New Assignment\n\n")
	}

	b.WriteString("## Quick Start\n\n")
	fmt.Fprintf(&b, "Run `multica issue get %s --output json` to fetch the full issue details.\n\n", ctx.IssueID)

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

func renderAutopilotContext(ctx TaskContextForEnv) string {
	var b strings.Builder

	b.WriteString("# Autopilot Run\n\n")
	fmt.Fprintf(&b, "**Autopilot run ID:** %s\n\n", ctx.AutopilotRunID)
	if ctx.AutopilotID != "" {
		fmt.Fprintf(&b, "**Autopilot ID:** %s\n\n", ctx.AutopilotID)
	}
	if ctx.AutopilotTitle != "" {
		fmt.Fprintf(&b, "**Title:** %s\n\n", ctx.AutopilotTitle)
	}
	if ctx.AutopilotSource != "" {
		fmt.Fprintf(&b, "**Trigger source:** %s\n\n", ctx.AutopilotSource)
	}
	if ctx.AutopilotTriggerPayload != "" {
		fmt.Fprintf(&b, "## Trigger Payload\n\n```json\n%s\n```\n\n", ctx.AutopilotTriggerPayload)
	}

	b.WriteString("## Quick Start\n\n")
	b.WriteString("This is a run-only autopilot task with no assigned issue. Do not run `multica issue get` unless the autopilot instructions explicitly ask you to create or update an issue.\n\n")
	if ctx.AutopilotID != "" {
		fmt.Fprintf(&b, "Run `multica autopilot get %s --output json` if you need the full autopilot configuration.\n\n", ctx.AutopilotID)
	}
	if strings.TrimSpace(ctx.AutopilotDescription) != "" {
		b.WriteString("## Autopilot Instructions\n\n")
		b.WriteString(ctx.AutopilotDescription)
		b.WriteString("\n\n")
	}

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
