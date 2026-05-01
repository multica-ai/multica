package execenv

import (
	"encoding/json"
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
// Kiro:     skills → {workDir}/.kiro/skills/{name}/SKILL.md  (native discovery)
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

	// Project resources are best-effort: a write failure logs but does not
	// block task startup. Missing resources surface as the agent simply not
	// seeing the file, which matches the "scoped, not dumped" design (the
	// meta skill content always lists what the agent should expect).
	if err := writeProjectResources(workDir, ctx); err != nil {
		// Caller logs warnings; avoid noisy returns for non-fatal context.
		return fmt.Errorf("write project resources: %w", err)
	}

	return nil
}

// projectResourceFile is the on-disk JSON written into the agent's working
// directory. Schema is intentionally a thin pass-through of the API response
// so consumers (skills, future tooling) don't need a separate parser.
type projectResourceFile struct {
	ProjectID    string                  `json:"project_id,omitempty"`
	ProjectTitle string                  `json:"project_title,omitempty"`
	Resources    []ProjectResourceForEnv `json:"resources"`
}

// MarshalJSON renders the resource_ref field as raw JSON instead of a base64
// blob. The struct's other fields are simple strings.
func (p ProjectResourceForEnv) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID           string          `json:"id"`
		ResourceType string          `json:"resource_type"`
		ResourceRef  json.RawMessage `json:"resource_ref"`
		Label        string          `json:"label,omitempty"`
	}
	ref := p.ResourceRef
	if len(ref) == 0 {
		ref = json.RawMessage("{}")
	}
	return json.Marshal(alias{
		ID:           p.ID,
		ResourceType: p.ResourceType,
		ResourceRef:  ref,
		Label:        p.Label,
	})
}

// writeProjectResources writes .multica/project/resources.json into the
// working directory when the task carries project context. The file is
// always written when a project is attached (even with zero resources) so
// agents can rely on its presence as a signal that a project exists.
func writeProjectResources(workDir string, ctx TaskContextForEnv) error {
	if ctx.ProjectID == "" && len(ctx.ProjectResources) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, ".multica", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	resources := ctx.ProjectResources
	if resources == nil {
		resources = []ProjectResourceForEnv{}
	}
	payload := projectResourceFile{
		ProjectID:    ctx.ProjectID,
		ProjectTitle: ctx.ProjectTitle,
		Resources:    resources,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "resources.json"), data, 0o644)
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
	case "kiro":
		// Kiro CLI auto-discovers project-level skills from .kiro/skills/
		// in the workdir.
		skillsDir = filepath.Join(workDir, ".kiro", "skills")
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
	if ctx.ChannelID != "" {
		return renderChannelMentionContext(ctx)
	}
	if ctx.AutopilotRunID != "" {
		return renderAutopilotContext(ctx)
	}
	if ctx.QuickCreatePrompt != "" {
		return renderQuickCreateContext(ctx)
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

// renderQuickCreateContext renders issue_context.md for quick-create tasks.
// This file carries only task data (user input, skills). Behavioral rules
// and guardrails live in AGENTS.md (runtime config) and the per-turn prompt
// to avoid redundancy and conflicting instructions.
func renderQuickCreateContext(ctx TaskContextForEnv) string {
	var b strings.Builder
	b.WriteString("# Quick Create\n\n")
	b.WriteString("**Trigger:** Quick-create modal\n\n")
	b.WriteString("## User input\n\n")
	b.WriteString("> ")
	b.WriteString(ctx.QuickCreatePrompt)
	b.WriteString("\n\n")
	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Agent Skills\n\n")
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

// renderChannelMentionContext renders issue_context.md for tasks
// triggered by an @-mention in a channel message. Channel tasks have
// no issue, no commits, and no branches — they're conversational. The
// agent's reply is captured as the agent CLI's stdout and posted back
// as a `channel_message` by the server's CompleteTask handler.
func renderChannelMentionContext(ctx TaskContextForEnv) string {
	var b strings.Builder
	b.WriteString("# Channel Mention\n\n")
	if ctx.ChannelName != "" {
		fmt.Fprintf(&b, "**Channel:** #%s\n", ctx.ChannelName)
	}
	if ctx.ChannelKind != "" {
		fmt.Fprintf(&b, "**Kind:** %s\n", ctx.ChannelKind)
	}
	if ctx.ChannelMessageID != "" {
		fmt.Fprintf(&b, "**Message ID:** %s\n", ctx.ChannelMessageID)
	}
	if ctx.ChannelAuthorName != "" {
		who := ctx.ChannelAuthorName
		if ctx.ChannelAuthorType == "agent" {
			who += " (agent)"
		}
		fmt.Fprintf(&b, "**Mentioned by:** %s\n", who)
	}
	b.WriteString("\n")

	if ctx.ChannelMessageContent != "" {
		b.WriteString("## Triggering message\n\n")
		b.WriteString("> ")
		b.WriteString(strings.ReplaceAll(ctx.ChannelMessageContent, "\n", "\n> "))
		b.WriteString("\n\n")
	}

	b.WriteString("## What to do\n\n")
	b.WriteString("Respond conversationally as a teammate. Your final stdout is captured ")
	b.WriteString("as your reply and posted to the channel as a `channel_message` from your ")
	b.WriteString("agent identity. There is no separate `multica` command to call.\n\n")
	b.WriteString("Do NOT run `multica issue ...` commands — there is no issue here.\n")

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("\n## Available skills\n\n")
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
	}

	return b.String()
}
