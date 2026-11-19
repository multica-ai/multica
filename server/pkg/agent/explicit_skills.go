package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ExplicitSkill is a member-selected, assigned skill whose directory was
// materialized by the execution environment. Never construct it from a label.
type ExplicitSkill struct {
	Name      string
	Directory string
	Body      string
}

// SkillInvocation preserves both message parts until the provider has checked
// its native capabilities. Prompt is the complete text input; PlatformPrompt
// excludes UserMessage for Claude's last-text-block slash-command contract.
type SkillInvocation struct {
	Prompt         string
	PlatformPrompt string
	UserMessage    string
	Skills         []ExplicitSkill
}

func injectExplicitSkills(prompt string, skills []ExplicitSkill) string {
	var b strings.Builder
	b.WriteString(prompt)
	for _, skill := range skills {
		fmt.Fprintf(&b, "\n\nThe user explicitly selected this skill in this message: %s\nSkill directory (resolve relative files here): %s\n<selected-skill>\n%s\n</selected-skill>\n", skill.Name, skill.Directory, skill.Body)
	}
	return b.String()
}

// FallbackPrompt carries the selection to providers without a native
// user-level skill invocation: the complete prompt with every selected
// skill injected inline.
func (s *SkillInvocation) FallbackPrompt() string {
	return injectExplicitSkills(s.Prompt, s.Skills)
}

func (s *SkillInvocation) claudeTextBlocks(available map[string]bool) []string {
	if len(s.Skills) == 0 || !available[s.Skills[0].Name] {
		return []string{s.FallbackPrompt()}
	}
	// A capsule-only member text strips to nothing and must not leave a
	// trailing space; a real ask joins the command with exactly one.
	command := "/" + s.Skills[0].Name
	if message := strings.TrimSpace(s.UserMessage); message != "" {
		command += " " + message
	}
	return []string{
		injectExplicitSkills(s.PlatformPrompt, s.Skills[1:]),
		command,
	}
}

func (s *SkillInvocation) codexPrompt(catalog json.RawMessage) string {
	var response struct {
		Data []struct {
			Skills []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Enabled *bool  `json:"enabled"`
			} `json:"skills"`
		} `json:"data"`
	}
	if json.Unmarshal(catalog, &response) != nil {
		return s.FallbackPrompt()
	}
	prompt := s.Prompt
	var fallback []ExplicitSkill
	for _, selected := range s.Skills {
		found := false
		for _, entry := range response.Data {
			for _, skill := range entry.Skills {
				if skill.Name == selected.Name && filepath.Clean(skill.Path) == filepath.Join(selected.Directory, "SKILL.md") && skill.Enabled != nil && *skill.Enabled {
					found = true
				}
			}
		}
		if found {
			prompt += "\n\n$" + selected.Name + "\n"
		} else {
			fallback = append(fallback, selected)
		}
	}
	return injectExplicitSkills(prompt, fallback)
}

func (b *codexBackend) skillTurnPrompt(ctx context.Context, client *codexClient, prompt string, opts ExecOptions) string {
	if opts.SkillInvocation == nil {
		return prompt
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	catalog, err := client.request(probeCtx, "skills/list", map[string]any{"cwds": []string{opts.Cwd}})
	if err != nil {
		b.cfg.Logger.Warn("Codex skill discovery unavailable; injecting selected skills", "error", err)
		return opts.SkillInvocation.FallbackPrompt()
	}
	return opts.SkillInvocation.codexPrompt(catalog)
}

func (s *SkillInvocation) antigravityPrompt(supported bool) string {
	if !supported || len(s.Skills) == 0 {
		return s.FallbackPrompt()
	}
	// Print mode expands one leading command. Further selections stay explicit
	// through injection, as they do on Claude.
	return "/" + s.Skills[0].Name + "\n" + injectExplicitSkills(s.Prompt, s.Skills[1:])
}

// piSkillPrompt renders Pi's native leading skill invocation. Pi expands a
// message whose text begins with `/skill:<name> ` inside session.prompt —
// both print mode (the stdin prompt) and RPC mode (a `prompt` command): the
// token is replaced by the skill's full body block and every remaining byte,
// newlines included, is preserved verbatim after it (pi's
// _expandSkillCommand splits the name at the first space; the slug charset
// has no spaces). The lookup runs over the daemon's `--skill` files by
// frontmatter name, which writeContextFiles keeps equal to the directory
// name used here, so a listed file is the capability probe.
//
// omp (oh-my-pi) shares this backend but removed that expansion: its print
// mode never dispatches the stdin prompt as a command, so a `/skill:` prefix
// would reach the model as literal text. omp and every skill file missing
// from this dispatch therefore keep the injection fallback.
func (s *SkillInvocation) piSkillPrompt(skillFiles []string, args ...[]string) string {
	if len(s.Skills) == 0 || piSkillsDisabled(args...) {
		return s.FallbackPrompt()
	}
	want := filepath.Clean(filepath.Join(s.Skills[0].Directory, "SKILL.md"))
	for _, path := range skillFiles {
		if filepath.Clean(path) == want {
			return "/skill:" + s.Skills[0].Name + " " + injectExplicitSkills(s.Prompt, s.Skills[1:])
		}
	}
	return s.FallbackPrompt()
}

// piSkillsDisabled reports whether any argument group — launch prefix, extra
// or custom args — asked Pi not to load skills. Pi still merges the daemon's
// `--skill` paths under --no-skills, but a user who disabled skills must not
// receive a native skill command; the plain injection fallback carries the
// selection instead, matching how a disabled antigravity falls back.
func piSkillsDisabled(args ...[]string) bool {
	for _, group := range args {
		for _, arg := range group {
			if arg == "--no-skills" || arg == "-ns" {
				return true
			}
		}
	}
	return false
}

func slashCommandsDisabled(args ...[]string) bool {
	for _, group := range args {
		for _, arg := range group {
			if arg == "--disable-slash-commands" || strings.HasPrefix(arg, "--disable-slash-commands=") {
				return true
			}
		}
	}
	return false
}

// Probe only on selected-skill turns. Do not cache across executable upgrades
// or infer capabilities from version strings. Failure is an injection fallback.
func supportsAntigravitySlashSkills(ctx context.Context, cfg Config, executable string, opts ExecOptions) bool {
	if slashCommandsDisabled(cfg.LaunchPrefix, opts.ExtraArgs, opts.CustomArgs) {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := cfg.commandAt(executable).exec(probeCtx, "--help")
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(cfg.Env)
	cmd.WaitDelay = time.Second
	output := &tailBuffer{max: 128 << 10}
	cmd.Stdout, cmd.Stderr = output, output
	if runOwned(cmd, cfg.Logger) != nil {
		return false
	}
	return strings.Contains(string(output.Bytes()), "--disable-slash-commands")
}
