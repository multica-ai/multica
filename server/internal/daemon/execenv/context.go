package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	"gopkg.in/yaml.v3"
)

// TaskContextMarkerRelPath is a non-secret marker the daemon writes under the
// task workdir. The CLI uses it as a fallback daemon-task signal when a child
// sandbox strips all MULTICA_* env vars before invoking `multica`.
const TaskContextMarkerRelPath = ".multica/daemon_task_context.json"

// TaskContextMarkerManagedBy is the marker discriminator the CLI checks before
// treating TaskContextMarkerRelPath as daemon-owned.
const TaskContextMarkerManagedBy = "multica-daemon-task"

type taskContextMarkerFile struct {
	ManagedBy string `json:"managed_by"`
	AgentID   string `json:"agent_id,omitempty"`
	IssueID   string `json:"issue_id,omitempty"`
}

// EnsureWorkspacesRootMarker writes a persistent daemon-task marker at
// {workspacesRoot}/.multica/daemon_task_context.json.
//
// The per-workdir marker only protects `multica` invocations whose cwd is
// inside the workdir, because the CLI discovers markers by walking *up* from
// cwd. A sandboxed subprocess that lost every MULTICA_* env var and escaped
// to the workdir's parent directory sits above that marker, finds no daemon
// signal, and would fall back to the user's config PAT — a confirmed
// impersonation path. Every directory under workspacesRoot is daemon-owned,
// so a marker at the root puts the entire tree back under the fail-closed
// guard without touching any directory a user works in.
//
// A pre-existing marker owned by the daemon is left untouched, so the shared,
// node-wide file is not rewritten on every task start. A truncated or otherwise
// unparseable file — the signature of a torn os.WriteFile from a daemon killed
// mid-write — is reclaimed and rewritten, so a crash can never brick the guard
// for the whole node; only a *parseable* marker owned by something else is
// treated as genuinely foreign and refused. The (re)write is atomic
// (temp file + rename): a concurrent CLI walking up from an escaped cwd sees
// either the old bytes or the complete new file, never a half-written marker it
// would misread as "no signal" and fall open on.
func EnsureWorkspacesRootMarker(workspacesRoot string) error {
	if strings.TrimSpace(workspacesRoot) == "" {
		return errors.New("execenv: workspaces root is required")
	}
	path := filepath.Join(workspacesRoot, TaskContextMarkerRelPath)
	if existing, err := os.ReadFile(path); err == nil {
		var marker taskContextMarkerFile
		if json.Unmarshal(existing, &marker) == nil {
			if marker.ManagedBy == TaskContextMarkerManagedBy {
				return nil
			}
			// Parseable but owned by something else: never clobber it.
			return fmt.Errorf("foreign file at workspaces root marker path %s; refusing to overwrite", path)
		}
		// Unparseable content is almost certainly a torn write of our own
		// marker; fall through to reclaim it below.
	} else if !os.IsNotExist(err) {
		// A real read error (e.g. a directory at the path) is not a signal we
		// can safely overwrite; surface it. Callers degrade non-fatally.
		return fmt.Errorf("read workspaces root marker %s: %w", path, err)
	}
	payload := taskContextMarkerFile{ManagedBy: TaskContextMarkerManagedBy}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspaces root marker: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workspaces root marker dir: %w", err)
	}
	if err := writeWorkspacesRootMarkerAtomic(path, data); err != nil {
		return fmt.Errorf("write workspaces root marker: %w", err)
	}
	return nil
}

// writeWorkspacesRootMarkerAtomic writes data to path via a same-directory temp
// file plus a rename, so a concurrent reader observes either the old file or the
// complete new one — never a partial write. Mirrors the daemon-id / CLI-config
// write idiom (see writeDaemonIDFile). Perm is 0644 because the CLI's upward
// walk must be able to read the marker from a subprocess that may run under a
// different uid; the payload is non-secret.
func writeWorkspacesRootMarkerAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon_task_context-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp workspaces root marker: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp workspaces root marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp workspaces root marker: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp workspaces root marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename workspaces root marker: %w", err)
	}
	return nil
}

// writeContextFiles renders and writes .agent_context/issue_context.md and
// skills into the appropriate provider-native location.
//
// Claude:      skills → {workDir}/.claude/skills/{name}/SKILL.md  (native discovery)
// CodeBuddy:   skills → {workDir}/.codebuddy/skills/{name}/SKILL.md  (native discovery — CodeBuddy is a Claude Code fork but uses its own config directory, not .claude/; see https://www.codebuddy.ai/docs/cli/skills)
// Codex:       skills → handled separately in Prepare via codex-home
// Hermes:      skills → handled separately in Prepare via hermes-home (HERMES_HOME/skills; Hermes has no workspace-relative discovery, see hermes_home.go)
// Copilot:     skills → {workDir}/.github/skills/{name}/SKILL.md  (native project-level discovery)
// OpenCode:    skills → {workDir}/.opencode/skills/{name}/SKILL.md  (native discovery)
// OpenClaw:    skills → {workDir}/skills/{name}/SKILL.md  (native discovery — paired with a per-task synthesized openclaw-config.json that pins agents.defaults.workspace to workDir; see openclaw_config.go)
// Pi:          skills → {workDir}/.pi/skills/{name}/SKILL.md  (native discovery)
// Cursor:      skills → {workDir}/.cursor/skills/{name}/SKILL.md  (native discovery)
// Kimi:        skills → {workDir}/.kimi/skills/{name}/SKILL.md  (native discovery)
// Kiro:        skills → {workDir}/.kiro/skills/{name}/SKILL.md  (native discovery)
// Qoder:       skills → {workDir}/.qoder/skills/{name}/SKILL.md  (project-level; see docs.qoder.com/cli/Skills.md)
// Qwen Code:    skills → {workDir}/.qwen/skills/{name}/SKILL.md  (native project-level discovery)
// Antigravity: skills → {workDir}/.agents/skills/{name}/SKILL.md  (native discovery — see https://antigravity.google/docs/gcli-migration "Workspace skills")
// Default:     skills → {workDir}/.agent_context/skills/{name}/SKILL.md
//
// manifest, when non-nil, is populated with every file we created and every
// intermediate directory we had to MkdirAll (skipping any that pre-existed).
// CleanupSidecars uses it to roll the workdir back to its pre-Prepare
// state for local_directory tasks. Callers that don't need cleanup —
// cloud-mode tasks whose envRoot is wiped wholesale by the GC loop — may
// pass nil to skip the bookkeeping entirely.
func writeContextFiles(workDir, provider string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	if err := writeTaskContextMarker(workDir, ctx, manifest); err != nil {
		return err
	}

	contextDir := filepath.Join(workDir, ".agent_context")
	if err := recordMkdirAll(contextDir, 0o755, manifest); err != nil {
		return fmt.Errorf("create .agent_context dir: %w", err)
	}

	content := renderIssueContext(provider, ctx)
	path := filepath.Join(contextDir, "issue_context.md")
	if err := recordWriteFile(path, []byte(content), 0o644, manifest); err != nil {
		// A pre-existing path means the user already owns
		// .agent_context/issue_context.md — either they created it
		// themselves or it survived from a crashed prior run we can't
		// safely distinguish from intentional content. Refusing the
		// write is the correct call: the runtime brief (CLAUDE.md /
		// AGENTS.md) already carries every fact this file
		// would, so the agent runs fine without the sidecar copy.
		// Anything else is a real failure.
		if !errors.Is(err, errPathPreExists) {
			return fmt.Errorf("write issue_context.md: %w", err)
		}
	}

	if len(ctx.AgentSkills) > 0 {
		// Hermes materializes skills into its per-task HERMES_HOME/skills during
		// Prepare (Hermes has no workspace-relative discovery), so it needs no
		// workdir-local skills dir at all — skip the resolve too, to avoid
		// leaving an empty .agent_context/skills/ behind.
		if provider != "hermes" {
			skillsDir, err := resolveSkillsDir(workDir, provider, manifest)
			if err != nil {
				return fmt.Errorf("resolve skills dir: %w", err)
			}
			// Codex skills are written to codex-home in Prepare; skip here.
			if provider != "codex" {
				if err := writeSkillFiles(skillsDir, ctx.AgentSkills, manifest); err != nil {
					return fmt.Errorf("write skill files: %w", err)
				}
			}
		}
	}

	// Project resources are best-effort: a write failure logs but does not
	// block task startup. Missing resources surface as the agent simply not
	// seeing the file, which matches the "scoped, not dumped" design (the
	// meta skill content always lists what the agent should expect).
	if err := writeProjectResources(workDir, ctx, manifest); err != nil {
		// Caller logs warnings; avoid noisy returns for non-fatal context.
		return fmt.Errorf("write project resources: %w", err)
	}

	return nil
}

func writeTaskContextMarker(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	dir := filepath.Dir(filepath.Join(workDir, TaskContextMarkerRelPath))
	if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
		return fmt.Errorf("create .multica dir: %w", err)
	}
	// The sidecar manifest removes this marker on normal local_directory
	// cleanup. If a crash leaves it behind, the CLI intentionally treats it
	// as daemon context and fails closed instead of using a user PAT.
	payload := taskContextMarkerFile{
		ManagedBy: TaskContextMarkerManagedBy,
		AgentID:   ctx.AgentID,
		IssueID:   ctx.IssueID,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task context marker: %w", err)
	}
	if err := recordWriteFile(filepath.Join(workDir, TaskContextMarkerRelPath), data, 0o644, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			path := filepath.Join(workDir, TaskContextMarkerRelPath)
			existing, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read existing task context marker: %w", readErr)
			}
			var marker taskContextMarkerFile
			if json.Unmarshal(existing, &marker) != nil || marker.ManagedBy != TaskContextMarkerManagedBy {
				return fmt.Errorf("write task context marker: %w", err)
			}
			if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
				return fmt.Errorf("refresh task context marker: %w", writeErr)
			}
			if manifest != nil {
				manifest.Files = append(manifest.Files, path)
			}
			return nil
		}
		return fmt.Errorf("write task context marker: %w", err)
	}
	return nil
}

// projectResourceFile is the on-disk JSON written into the agent's working
// directory. Schema is intentionally a thin pass-through of the API response
// so consumers (skills, future tooling) don't need a separate parser.
type projectResourceFile struct {
	ProjectID          string                  `json:"project_id,omitempty"`
	ProjectTitle       string                  `json:"project_title,omitempty"`
	ProjectDescription string                  `json:"project_description,omitempty"`
	Resources          []ProjectResourceForEnv `json:"resources"`
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
//
// manifest, when non-nil, is populated with the .multica/project chain
// of created directories and the resources.json file so CleanupSidecars
// can undo them on local_directory teardown.
func writeProjectResources(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	if ctx.ProjectID == "" && len(ctx.ProjectResources) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, ".multica", "project")
	if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
		return err
	}
	resources := ctx.ProjectResources
	if resources == nil {
		resources = []ProjectResourceForEnv{}
	}
	payload := projectResourceFile{
		ProjectID:          ctx.ProjectID,
		ProjectTitle:       ctx.ProjectTitle,
		ProjectDescription: ctx.ProjectDescription,
		Resources:          resources,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := recordWriteFile(filepath.Join(dir, "resources.json"), data, 0o644, manifest); err != nil {
		// .multica/project/resources.json is Multica-owned and a
		// pre-existing path is almost certainly user content the
		// manifest must not destroy. The runtime brief already lists
		// every project resource so the agent runs fine without the
		// JSON sidecar — collision degrades to brief-only mode.
		if !errors.Is(err, errPathPreExists) {
			return err
		}
	}
	return nil
}

// resolveSkillsDir returns the directory where skills should be written
// based on the agent provider, creating it. manifest, when non-nil, is
// populated with every intermediate directory we had to MkdirAll so
// CleanupSidecars can rmdir them on local_directory teardown.
func resolveSkillsDir(workDir, provider string, manifest *sidecarManifest) (string, error) {
	skillsDir := skillsDirPath(workDir, provider)
	if err := recordMkdirAll(skillsDir, 0o755, manifest); err != nil {
		return "", err
	}
	return skillsDir, nil
}

// skillsDirPath returns the provider-native skills parent directory under
// workDir WITHOUT creating it or recording anything. resolveSkillsDir wraps
// this with the MkdirAll/manifest bookkeeping; the reuse-path skill rollback
// (removeReusedManagedSkillDirs) needs the bare path with no side effects so
// it can match the managed skill roots the prior manifest recorded.
func skillsDirPath(workDir, provider string) string {
	switch provider {
	case "claude":
		// Claude Code natively discovers skills from .claude/skills/ in the workdir.
		return filepath.Join(workDir, ".claude", "skills")
	case "codebuddy":
		// CodeBuddy Code is a Claude Code fork but uses its own native
		// project-level skill directory .codebuddy/skills/, not
		// .claude/skills/. See https://www.codebuddy.ai/docs/cli/skills.
		return filepath.Join(workDir, ".codebuddy", "skills")
	case "copilot":
		// GitHub Copilot CLI natively discovers project-level skills from
		// .github/skills/<name>/SKILL.md (takes precedence over user-level
		// skills in ~/.copilot/skills/).
		// See: https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference
		return filepath.Join(workDir, ".github", "skills")
	case "opencode":
		// OpenCode natively discovers project skills from .opencode/skills/ in
		// the workdir. ConfigPaths.directories() walks up from the discovery
		// root looking for a bare `.opencode` directory (no opencode.json
		// signal required), then skill/index.ts scans `{skill,skills}/**/SKILL.md`
		// under each match. Discovery is anchored at the task workdir via
		// `opencode run --dir <workDir>` + PWD override in opencodeBackend —
		// without those, OpenCode walks from the daemon's inherited PWD and
		// misses .opencode/skills + AGENTS.md entirely (MUL-2416).
		return filepath.Join(workDir, ".opencode", "skills")
	case "deveco":
		// DevEco Code (Huawei's OpenCode fork) natively discovers project
		// skills from .deveco/skills/ in the workdir, mirroring OpenCode's
		// .opencode/skills layout under its DEVECO_-prefixed brand. Discovery
		// is anchored at the task workdir via `deveco run --dir <workDir>` +
		// the PWD override in devecoBackend, same anchor OpenCode uses.
		return filepath.Join(workDir, ".deveco", "skills")
	case "openclaw":
		// OpenClaw's native skill scanner reads <workspaceDir>/skills/. The
		// daemon pairs this with a per-task synthesized openclaw-config.json
		// (see openclaw_config.go) that pins agents.defaults.workspace to
		// workDir, so writing here is what the CLI actually scans. Before
		// MUL-2219 this used to fall back to .agent_context/skills/, which
		// no openclaw scan path ever inspected.
		return filepath.Join(workDir, "skills")
	case "pi":
		// Pi natively discovers skills from .pi/skills/ in the workdir.
		return filepath.Join(workDir, ".pi", "skills")
	case "cursor":
		// Cursor natively discovers skills from .cursor/skills/ in the workdir.
		return filepath.Join(workDir, ".cursor", "skills")
	case "kimi":
		// Kimi Code CLI auto-discovers project-level skills from .kimi/skills/
		// in the workdir. See https://moonshotai.github.io/kimi-cli/en/customization/skills.html
		return filepath.Join(workDir, ".kimi", "skills")
	case "kiro":
		// Kiro CLI auto-discovers project-level skills from .kiro/skills/
		// in the workdir.
		return filepath.Join(workDir, ".kiro", "skills")
	case "qoder":
		// Qoder CLI discovers project-level skills under .qoder/skills/.
		// See https://docs.qoder.com/cli/Skills.md
		return filepath.Join(workDir, ".qoder", "skills")
	case "qwen":
		// Qwen Code discovers project-level skills from .qwen/skills/ in the workdir.
		return filepath.Join(workDir, ".qwen", "skills")
	case "traecli":
		// Official TRAE CLI discovers project-level skills from .traecli/skills/
		// in the workdir (global skills live in ~/.traecli/skills). See
		// https://docs.trae.cn/cli_skills
		return filepath.Join(workDir, ".traecli", "skills")
	case "antigravity":
		// Antigravity (`agy`) auto-discovers workspace-level skills from
		// .agents/skills/ in the workdir. The CLI inherits Gemini CLI's
		// workspace skill layout; see https://antigravity.google/docs/gcli-migration
		// under "Workspace skills".
		return filepath.Join(workDir, ".agents", "skills")
	case "grok":
		// Grok Build CLI discovers project-level skills from .grok/skills/
		// (and also scans .agents/skills/). Prefer the native .grok tree.
		// See Grok user-guide skills.md.
		return filepath.Join(workDir, ".grok", "skills")
	default:
		// Fallback: write to .agent_context/skills/ (referenced by meta config).
		return filepath.Join(workDir, ".agent_context", "skills")
	}
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// ensureSkillFrontmatter returns SKILL.md content guaranteed to lead with a
// YAML frontmatter block carrying a parseable, non-empty `name` key.
//
// Runtimes like OpenCode silently drop SKILL.md whose frontmatter is missing
// or whose `name` doesn't parse, so we handle three cases:
//
//   - No frontmatter at all → synthesize one with `name: <slug>` (and the DB
//     description when available).
//   - Frontmatter present, has a non-empty `name`, AND parses as valid YAML →
//     leave it untouched. The upstream import may have shaped that block
//     deliberately to match a specific runtime, and we don't want to clobber it.
//   - Frontmatter present and has a non-empty `name` but YAML is invalid (e.g.
//     unquoted colon in description) → strip and re-synthesize so runtimes like
//     Codex don't discard the skill on parse errors.
//   - Frontmatter present but missing `name` (e.g. an upstream skill whose
//     YAML only set `description`, with the directory slug filling in for
//     `name` at import time) → prepend `name: <slug>` as the first key of
//     the existing block so OpenCode can still route the skill.
func ensureSkillFrontmatter(content, slug, description string) string {
	fmStart, ok := frontmatterBodyStart(content)
	if !ok {
		return synthesizeFrontmatter(content, slug, description)
	}
	// Frontmatter exists and has a parseable name. If it's valid YAML, leave
	// it untouched so upstream-imported frontmatter survives round-trips.
	if hasFrontmatterName(content[fmStart:]) {
		if isFrontmatterValidYAML(content) {
			return content
		}
		// Frontmatter has a name but the YAML is invalid (e.g. unquoted
		// colon in the description). Strip and re-synthesize so runtimes
		// like Codex don't hard-reject the whole skill at load time.
		// frontmatterParts returns the full content as the body when it
		// can't find a closing delimiter, so the malformed block is kept
		// rather than silently dropped.
		_, body, _ := frontmatterParts(content)
		return synthesizeFrontmatter(body, slug, description)
	}
	// Frontmatter exists but lacks a parseable `name`. Inject one as the
	// first key of the existing block and keep the rest verbatim (including
	// `description`, body, and any runtime-specific keys the import path
	// preserved).
	return content[:fmStart] + "name: " + slug + "\n" + content[fmStart:]
}

// synthesizeFrontmatter produces a SKILL.md body with a YAML frontmatter block
// carrying at least `name` and (when non-empty) `description`. The description
// is always escaped as a double-quoted YAML string so values containing colons,
// brackets, or other YAML-significant characters parse safely.
func synthesizeFrontmatter(body, slug, description string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", slug)
	if d := strings.TrimSpace(description); d != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlEscapeInline(d))
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String()
}

// isFrontmatterValidYAML reports whether the opening YAML frontmatter block of
// content parses as a YAML mapping. Returns false when there is no frontmatter,
// the block has no closing delimiter, is empty, or unmarshalling fails.
func isFrontmatterValidYAML(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}
	var m map[string]any
	return yaml.Unmarshal([]byte(fmBody), &m) == nil
}

// frontmatterParts splits content into the raw YAML frontmatter body (the text
// between the opening `---` line and the closing `---` line) and the document
// body that follows the closing delimiter. ok is false when content has no
// opening delimiter or no closing delimiter line; in that case body is the full
// content so callers can keep a malformed block instead of dropping it.
//
// A closing delimiter is a line whose only content is `---`, terminated by
// `\n`, `\r\n`, or end-of-file. Centralizing the rule here keeps the validity
// check and the re-synthesis path from disagreeing on where a block ends (e.g.
// for EOF- or CRLF-terminated frontmatter), which previously left a stale block
// behind when the two definitions diverged.
func frontmatterParts(content string) (fmBody, body string, ok bool) {
	start, ok := frontmatterBodyStart(content)
	if !ok {
		return "", content, false
	}
	rest := content[start:]
	for searchFrom := 0; ; {
		nl := strings.Index(rest[searchFrom:], "\n---")
		if nl < 0 {
			return "", content, false
		}
		closeAt := searchFrom + nl
		after := rest[closeAt+len("\n---"):]
		switch {
		case after == "" || after == "\r":
			return rest[:closeAt], "", true
		case strings.HasPrefix(after, "\n"):
			return rest[:closeAt], after[len("\n"):], true
		case strings.HasPrefix(after, "\r\n"):
			return rest[:closeAt], after[len("\r\n"):], true
		default:
			// Not a standalone delimiter line (e.g. "----" or "--- text");
			// keep scanning for the real close.
			searchFrom = closeAt + len("\n---")
		}
	}
}

// frontmatterBodyStart returns the byte offset where the YAML body begins
// (just after the opening `---` line) and whether a valid opening delimiter
// was found.
func frontmatterBodyStart(content string) (int, bool) {
	if strings.HasPrefix(content, "---\n") {
		return 4, true
	}
	if strings.HasPrefix(content, "---\r\n") {
		return 5, true
	}
	return 0, false
}

// hasFrontmatterName reports whether the frontmatter body (the slice starting
// just after the opening `---` line) contains a parseable, non-empty `name:`
// scalar before the closing `---`.
func hasFrontmatterName(fmBody string) bool {
	closeIdx := strings.Index(fmBody, "\n---")
	if closeIdx < 0 {
		// Missing close — scan everything we have and fall through. The
		// frontmatter is malformed and OpenCode will reject it anyway, but
		// detecting an existing name keeps us from layering a second one
		// on top.
		closeIdx = len(fmBody)
	}
	for _, line := range strings.Split(fmBody[:closeIdx], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "name:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		v = strings.Trim(v, `"'`)
		if v != "" {
			return true
		}
	}
	return false
}

// yamlEscapeInline returns a double-quoted YAML scalar that always parses as
// a string. Plain scalars are deliberately avoided: values like `[foo]`,
// `{x: y}`, `false`, `null`, or `2024-01-01` would parse as flow sequences,
// flow mappings, booleans, nulls, or timestamps under YAML 1.2, and
// OpenCode's frontmatter check rejects non-string descriptions outright. We
// flatten newlines (frontmatter values are single-line per key) and escape
// `\` and `"` so any input is a safe inline string.
func yamlEscapeInline(s string) string {
	flat := strings.ReplaceAll(s, "\r\n", " ")
	flat = strings.ReplaceAll(flat, "\n", " ")
	flat = strings.ReplaceAll(flat, "\r", " ")
	escaped := strings.ReplaceAll(flat, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

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
// Each skill gets its own subdirectory containing SKILL.md and supporting
// files. manifest, when non-nil, is populated with every newly-created
// directory and file so CleanupSidecars can remove them on
// local_directory teardown without touching user-owned skill directories
// that happen to live alongside ours under the same skills/ parent.
//
// When a Multica skill's natural slug collides with a user-installed
// skill at the same path, we allocate a collision-free sibling slug
// (e.g. `issue-review-multica`) and write there instead. Provider-native
// discovery still picks it up because every subdir under skillsDir is a
// distinct skill; the user's original directory stays bit-for-bit
// intact. Without this fallback writeSkillFiles would have to either
// overwrite user bytes (the bug PR #3444 review caught) or skip the
// skill entirely (which would silently drop a Multica skill the agent
// expects to see).
func writeSkillFiles(skillsDir string, skills []SkillContextForEnv, manifest *sidecarManifest) error {
	if err := recordMkdirAll(skillsDir, 0o755, manifest); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	for _, skill := range skills {
		baseSlug := sanitizeSkillName(skill.Name)
		slug, dir, err := allocateCollisionFreeSkillDir(skillsDir, baseSlug)
		if err != nil {
			return fmt.Errorf("allocate skill dir for %q: %w", skill.Name, err)
		}
		if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
			return err
		}

		// ensureSkillFrontmatter synthesises a `name:` value when the
		// upstream skill is missing one. Use the chosen slug (which
		// may differ from baseSlug on collision) so the YAML name
		// matches the directory name; runtimes that key on either
		// stay consistent.
		body := ensureSkillFrontmatter(skill.Content, slug, skill.Description)
		if err := recordWriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644, manifest); err != nil {
			return err
		}

		// Write supporting files. The skill directory is collision-
		// free by construction, so a recordWriteFile collision under
		// it would mean the skill's bundled files list two entries
		// at the same path — that's an upstream data bug, not a
		// user-content collision, and we surface it.
		//
		// One common data bug is storing SKILL.md as both the primary
		// content (skill.Content) and as a supporting file. Skip the
		// duplicate so the agent still gets every unique file. The check
		// is canonical (see skillpkg.IsReservedContentPath) so a
		// non-canonical spelling like "./SKILL.md" — which filepath.Join
		// resolves onto the same dir/SKILL.md we just wrote — is caught
		// too, instead of colliding and failing prep with errPathPreExists.
		for _, f := range skill.Files {
			if skillpkg.IsReservedContentPath(f.Path) {
				continue
			}
			fpath := filepath.Join(dir, f.Path)
			if err := recordMkdirAll(filepath.Dir(fpath), 0o755, manifest); err != nil {
				return err
			}
			if err := recordWriteFile(fpath, []byte(f.Content), 0o644, manifest); err != nil {
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

	// Assignment handoff note (MUL-3375): the assigner's scoping instruction for
	// this run. Distinct from a comment — there is no thread to reply to.
	if ctx.HandoffNote != "" {
		b.WriteString("## Handoff Note\n\n")
		b.WriteString("The person who assigned this issue left this instruction for the run. Treat it as scope guidance and follow it before doing anything broader:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", ctx.HandoffNote)
	}

	b.WriteString("## Quick Start\n\n")
	fmt.Fprintf(&b, "Run `multica issue get %s --output json` to fetch the full issue details.\n\n", ctx.IssueID)

	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		b.WriteString("The following skills are available to you:\n\n")
		for _, skill := range skills {
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
	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		for _, skill := range skills {
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

	skills := modelVisibleSkills(ctx.AgentSkills)
	if len(skills) > 0 {
		b.WriteString("## Agent Skills\n\n")
		b.WriteString("The following skills are available to you:\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}
