package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type publicSkillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func TestMulticaOperatorPublicSkillContract(t *testing.T) {
	skillDir := publicSkillDir(t, "multica-operator")
	content := readPublicSkillFile(t, skillDir, "SKILL.md")
	frontmatter, body := parsePublicSkill(t, content)

	if frontmatter.Name != "multica-operator" {
		t.Fatalf("name = %q, want multica-operator", frontmatter.Name)
	}
	if !strings.HasPrefix(frontmatter.Description, "Use when ") {
		t.Errorf("description = %q, want a trigger-only description starting with 'Use when '", frontmatter.Description)
	}
	for _, intent := range []string{"business goal", "workflow", "Agent", "Squad", "Skill", "Issue", "Project", "Autopilot"} {
		if !strings.Contains(frontmatter.Description, intent) {
			t.Errorf("description does not cover orchestration intent %q", intent)
		}
	}
	for _, hostSpecific := range []string{"user-invocable:", "allowed-tools:"} {
		if strings.Contains(content, hostSpecific) {
			t.Errorf("portable public skill contains host-specific frontmatter %q", hostSpecific)
		}
	}

	requiredFiles := []string{
		"VERSION",
		"references/version-check.md",
		"references/setup-and-connection.md",
		"references/orchestration.md",
		"references/extensions.md",
		"references/source-map.md",
	}
	for _, path := range requiredFiles {
		if !strings.Contains(body, path) {
			t.Errorf("SKILL.md does not route readers to %q", path)
		}
		readPublicSkillFile(t, skillDir, filepath.FromSlash(path))
	}
	if strings.Index(body, "references/version-check.md") > strings.Index(body, "references/setup-and-connection.md") {
		t.Error("SKILL.md must route to references/version-check.md before references/setup-and-connection.md")
	}

	for _, command := range []string{"multica workspace list --output json"} {
		if !strings.Contains(body, command) {
			t.Errorf("SKILL.md does not define the completion check %q", command)
		}
	}
	normalizedBody := strings.Join(strings.Fields(body), " ")
	for _, contract := range []string{
		"Before any connection, authentication, or workspace command, read `references/setup-and-connection.md`",
		"A successful command result is already fresh verification evidence",
		"do not preload or list workspace resources",
		"Use only commands exposed by the installed `multica` CLI",
		"not currently supported by the CLI",
		"complete it in Multica Web",
		"Do not read a saved profile token",
		"call the Multica API directly",
		"community CLI",
		"For an open-ended business goal, business orchestration, resource selection, or dependent mutations, read `references/orchestration.md`",
		"Keep a concrete action on a known target in the direct-operation flow",
		"Route by the need for orchestration, not by resource count",
		"operational design, not software design",
		"Present two or more mutually exclusive choices as a numbered list",
		"Accept a number-only reply",
		"Return a clickable link for every created or updated Issue",
		"If the CLI response has no Issue URL, build one only from the known Multica app URL, workspace slug, and verified Issue identifier or ID",
		"Never guess the app URL or workspace slug",
	} {
		if !strings.Contains(normalizedBody, contract) {
			t.Errorf("SKILL.md is missing native-host contract anchor %q", contract)
		}
	}

	orchestration := readPublicSkillFile(t, skillDir, filepath.FromSlash("references/orchestration.md"))
	normalizedOrchestration := strings.Join(strings.Fields(orchestration), " ")
	for _, contract := range []string{
		"intent, not resource count",
		"one-time",
		"recurring",
		"coordinated",
		"relevant read-only discovery",
		"dedicated",
		"shared",
		"unknown",
		"Treat unknown as shared",
		"must not create, update, import, or delete a Skill",
		"Binding or unbinding an existing Skill changes the Agent configuration",
		"separate second confirmation",
		"Assigning an unchanged Agent",
		"Temporary embedded instruction",
		"Required tools and permissions",
		"Autopilots assign Agents, not Squads",
		"complete plan",
		"The in-chat orchestration design is the execution plan",
		"After the user confirms it, execute the plan directly",
		"Create a repository design document, specification, or implementation plan only when the user explicitly requests that artifact",
		"material deviation",
		"When an Issue has no unfinished dependency, create it directly as `todo`",
		"Use `backlog` only to park an Issue whose dependencies are not ready",
		"Do not create a ready Issue as `backlog` only to move it immediately to `todo`",
		"trigger last",
		"returned resource ID",
		"resume",
		"Never recreate a resource by name",
		"Multica Web",
	} {
		if !strings.Contains(normalizedOrchestration, contract) {
			t.Errorf("orchestration reference is missing contract anchor %q", contract)
		}
	}

	setup := readPublicSkillFile(t, skillDir, filepath.FromSlash("references/setup-and-connection.md"))
	normalizedSetup := strings.Join(strings.Fields(setup), " ")
	for _, contract := range []string{
		"authorizes installing the missing community CLI",
		"without a separate confirmation",
		"ask whether to use Multica Cloud or a self-hosted deployment",
		"Do not run `setup` or `login` before the user chooses",
		"Do not narrate",
		"stay silent",
		"does not install or update this Skill",
		"Do not run `multica update` automatically",
		"community CLI",
		"https://api.multica.ai",
		"https://multica.ai",
		"config show",
		"multica --profile <name> --server-url https://api.multica.ai setup",
		"multica --profile <name> setup self-host",
		"multica --profile <name> --server-url <server-url> login",
		"workspace list --output json",
		"--profile <name>",
		"[]",
		"install.ps1",
		"rerun `multica version`",
		"Before opening a browser",
		"passwords, verification codes, or tokens in chat",
		"one workspace",
		"multiple workspaces",
		"display names",
		"ask whether the user wants to create one",
		"immutable slug",
		"multica workspace create",
		"multica workspace switch",
		"Workspace:",
		"multica issue list --output json",
		"multica autopilot list --output json",
		"multica project list --output json",
		"multica label list --output json",
		"multica agent list --output json",
		"multica skill list --output json",
		"multica squad list --output json",
	} {
		if !strings.Contains(normalizedSetup, contract) {
			t.Errorf("setup reference is missing contract anchor %q", contract)
		}
	}
	for _, forbidden := range []string{
		"ask before installing software",
		"Do not execute a downloaded installer without user approval.",
	} {
		if strings.Contains(normalizedSetup, forbidden) {
			t.Errorf("setup reference retains superseded cold-start behavior %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"auth status --output json",
		"auth status --help",
		"missing_token",
		"invalid_token",
		"verification_failed",
		"effective server URL",
		"Account: <user.name>",
	} {
		if strings.Contains(normalizedSetup, forbidden) || strings.Contains(normalizedBody, forbidden) {
			t.Errorf("community Skill retains unsupported or unsafe auth contract %q", forbidden)
		}
	}

	versionCheck := readPublicSkillFile(t, skillDir, filepath.FromSlash("references/version-check.md"))
	for _, contract := range []string{
		"once in the current agent session",
		"VERSION",
		"dev",
		"https://raw.githubusercontent.com/multica-ai/multica/marketplace/release.json",
		"installed version",
		"available version",
		"release URL",
		"Marketplace",
		"native GitHub installation",
		"new agent session",
		"do not modify the Skill directory",
		"continue the original request",
		"resolve the directory containing the loaded `SKILL.md`",
		"<absolute-skill-directory>",
		"installed_version=$(tr -d '\\r\\n' < \"<absolute-skill-directory>/VERSION\")",
		"$installedVersion = (Get-Content -Raw (Join-Path '<absolute-skill-directory>' 'VERSION')).Trim()",
		"release_json=$(curl -fsSL",
		"version",
		"release_url",
	} {
		if !strings.Contains(versionCheck, contract) {
			t.Errorf("version-check reference is missing contract anchor %q", contract)
		}
	}
	for _, forbidden := range []string{
		"/workspaces/new",
		"api.github.com/repos/multica-ai/multica/releases/latest",
		"direct GitHub installer",
	} {
		if strings.Contains(body+setup+versionCheck, forbidden) {
			t.Errorf("public Skill retains superseded behavior %q", forbidden)
		}
	}

	extensions := readPublicSkillFile(t, skillDir, filepath.FromSlash("references/extensions.md"))
	for _, contract := range []string{
		"host skill catalog",
		"extends Multica Operator",
		"self-evolution",
		"user instruction",
		"base safety",
		"equally specific",
		"does not implement a runtime loader",
	} {
		if !strings.Contains(extensions, contract) {
			t.Errorf("extension reference is missing contract anchor %q", contract)
		}
	}

	install := readPublicSkillFile(t, filepath.Dir(skillDir), "README.md")
	for _, contract := range []string{
		"multica-operator",
		"marketplace",
		"plugins/multica-operator/skills/multica-operator",
		"Codex",
		"Claude Code",
		"Cursor",
	} {
		if !strings.Contains(install, contract) {
			t.Errorf("public skill installation guide is missing %q", contract)
		}
	}
	if strings.Contains(install, "asks for approval before installing it") {
		t.Error("public skill installation guide retains superseded installer approval behavior")
	}
}

func publicSkillDir(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate public skill test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "skills", name))
}

func readPublicSkillFile(t *testing.T, skillDir, relativePath string) string {
	t.Helper()
	path := filepath.Join(skillDir, relativePath)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read public skill file %s: %v", path, err)
	}
	return string(content)
}

func parsePublicSkill(t *testing.T, content string) (publicSkillFrontmatter, string) {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("SKILL.md must start with YAML frontmatter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	frontmatterEnd := strings.Index(rest, "\n---\n")
	if frontmatterEnd < 0 {
		t.Fatal("SKILL.md frontmatter has no closing delimiter")
	}

	frontmatterYAML := []byte(rest[:frontmatterEnd])
	var fields map[string]any
	if err := yaml.Unmarshal(frontmatterYAML, &fields); err != nil {
		t.Fatalf("parse SKILL.md frontmatter fields: %v", err)
	}
	if len(fields) != 2 || fields["name"] == nil || fields["description"] == nil {
		t.Fatalf("SKILL.md frontmatter fields = %v, want exactly name and description", fields)
	}

	var frontmatter publicSkillFrontmatter
	if err := yaml.Unmarshal(frontmatterYAML, &frontmatter); err != nil {
		t.Fatalf("parse SKILL.md frontmatter: %v", err)
	}
	if strings.TrimSpace(frontmatter.Name) == "" || strings.TrimSpace(frontmatter.Description) == "" {
		t.Fatal("SKILL.md frontmatter requires non-empty name and description")
	}
	return frontmatter, rest[frontmatterEnd+len("\n---\n"):]
}
