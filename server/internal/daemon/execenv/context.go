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
	"github.com/multica-ai/multica/server/pkg/agent"
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
	ManagedBy     string `json:"managed_by"`
	AgentID       string `json:"agent_id,omitempty"`
	IssueID       string `json:"issue_id,omitempty"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
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

// writeContextFiles writes the task's sidecar files: the task-context marker,
// agent skills in the appropriate provider-native location, and project
// resources.
//
// It deliberately writes no per-task Markdown brief. There used to be an
// .agent_context/issue_context.md carrying the issue id, trigger comment id,
// handoff note, quick-create input, and autopilot run data — every one of
// which the runtime brief and the per-turn user message already carry. No
// provider read the file (nothing in either surface pointed at it), so it was
// a third copy that had to be kept in sync with the two that agents actually
// see, for no reader at all (MUL-6984).
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
// Reasonix:    skills → {workDir}/.reasonix/skills/{name}/SKILL.md  (native discovery)
// DSH:         skills → {workDir}/.dsh/skills/{name}/SKILL.md  (native discovery)
// Kiro:        skills → {workDir}/.kiro/skills/{name}/SKILL.md  (native discovery)
// Qoder/Qoder CN: skills → {workDir}/.qoder/skills/{name}/SKILL.md  (project-level; see the provider docs)
// Qwen Code:    skills → {workDir}/.qwen/skills/{name}/SKILL.md  (native project-level discovery)
// QwenPaw:      skills → {workDir}/.qwenpaw/skills/{name}/SKILL.md  (native project-level discovery)
// MiniMax Code: skills → {workDir}/.minimax/skills/{name}/SKILL.md  (native project-level discovery)
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
		ManagedBy:     TaskContextMarkerManagedBy,
		AgentID:       ctx.AgentID,
		IssueID:       ctx.IssueID,
		ChatSessionID: ctx.ChatSessionID,
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
	// Built-in runtime identities (e.g. "omp") declare their skills dir in
	// the descriptor; resolve generically before the protocol-family switch.
	if desc, ok := agent.BuiltinRuntimeByID(provider); ok {
		return filepath.Join(workDir, desc.SkillsDir)
	}
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
	case "codearts":
		// CodeArts Agent discovers project skills from its provider-owned
		// .codeartsdoer/skills directory.
		return filepath.Join(workDir, ".codeartsdoer", "skills")
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
	case "reasonix":
		// Reasonix discovers project skills from .reasonix/skills/ and loads
		// AGENTS.md independently, so repository memory and task skills coexist.
		return filepath.Join(workDir, ".reasonix", "skills")
	case "dsh":
		// DSH scans both .dsh/skills and .agents/skills. Prefer its branded
		// project root so runtime-specific skills stay isolated.
		return filepath.Join(workDir, ".dsh", "skills")
	case "kiro":
		// Kiro CLI auto-discovers project-level skills from .kiro/skills/
		// in the workdir.
		return filepath.Join(workDir, ".kiro", "skills")
	case "qoder", "qoderclicn":
		// Both Qoder CLI editions discover project-level skills under
		// .qoder/skills/. Their user-level roots differ, which is handled by
		// listRuntimeLocalSkills.
		return filepath.Join(workDir, ".qoder", "skills")
	case "qwen":
		// Qwen Code discovers project-level skills from .qwen/skills/ in the workdir.
		return filepath.Join(workDir, ".qwen", "skills")
	case "qwenpaw":
		// QwenPaw discovers workspace-level skills from <workDir>/skill_pool/.
		// See get_workspace_skills_dir in QwenPaw's skill_system/store.py.
		return filepath.Join(workDir, "skill_pool")
	case "mcode":
		// MiniMax Code discovers project-level skills from .minimax/skills/.
		return filepath.Join(workDir, ".minimax", "skills")
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
//     rewrite `name` to the slug and keep every other key verbatim. See below
//     for why `name` specifically is not left alone.
//   - Frontmatter present and has a non-empty `name` but YAML is invalid (e.g.
//     unquoted colon in description) → strip and re-synthesize so runtimes like
//     Codex don't discard the skill on parse errors.
//   - Frontmatter present but missing `name` (e.g. an upstream skill whose
//     YAML only set `description`, with the directory slug filling in for
//     `name` at import time) → prepend `name: <slug>` as the first key of
//     the existing block so OpenCode can still route the skill.
//
// `name` is the one key Multica must own. Runtimes disagree on which field
// identifies a skill — Claude routes on the directory name, OpenCode on the
// frontmatter `name` — so letting the two diverge gives a single skill two
// different invocable names depending on where it runs (MUL-5529). The slug is
// authoritative because it is what lands on disk and the only value carrying a
// uniqueness guarantee (allocateCollisionFreeSkillDir); a frontmatter `name` is
// author-supplied and two imported skills may both claim the same one. Every
// other key stays byte-identical, so deliberately shaped upstream frontmatter
// still survives the round-trip.
func ensureSkillFrontmatter(content, slug, description string) string {
	fmStart, ok := frontmatterBodyStart(content)
	if !ok {
		return synthesizeFrontmatter(content, slug, description)
	}
	if isFrontmatterValidYAML(content) {
		// The parser, not the spelling, decides whether a name exists. A
		// lexical scan only recognizes a bare `name:` with a value on the same
		// line, so `"name": x` and a valueless `name:` read as nameless and
		// earn an injected second `name` — a duplicate mapping key that strict
		// loaders reject outright, leaving the skill unloadable rather than
		// merely misnamed.
		if frontmatterHasNameKey(content) {
			if rewritten, verified := setFrontmatterName(content, fmStart, slug); verified {
				return rewritten
			}
			// The surgical rewrite could not be proven correct — an anchor on
			// the name value is one way that happens, since replacing the line
			// removes the anchor and strands any alias pointing at it. The
			// block itself is still valid YAML, so rebuild it from the parsed
			// node instead of re-synthesizing: that keeps every other key,
			// including policy fields like disable-model-invocation whose loss
			// would re-expose a skill the author hid from model invocation.
			if rebuilt, ok := renameFrontmatterNameViaNode(content, slug); ok {
				return rebuilt
			}
			_, body, _ := frontmatterParts(content)
			return synthesizeFrontmatter(body, slug, description)
		}
		// No name key at all, confirmed by the parser rather than inferred.
		// Inject one as the first key and keep the rest verbatim (including
		// `description`, body, and any runtime-specific keys the import path
		// preserved).
		return content[:fmStart] + "name: " + slug + "\n" + content[fmStart:]
	}

	// Invalid YAML: there is no parse to consult, so fall back to the lexical
	// scan. A block with a name is stripped and re-synthesized so runtimes like
	// Codex don't hard-reject the whole skill at load time; frontmatterParts
	// returns the full content as the body when it can't find a closing
	// delimiter, so the malformed block is kept rather than silently dropped.
	if hasFrontmatterName(content[fmStart:]) {
		_, body, _ := frontmatterParts(content)
		return synthesizeFrontmatter(body, slug, description)
	}
	return content[:fmStart] + "name: " + slug + "\n" + content[fmStart:]
}

// frontmatterHasNameKey reports whether content's frontmatter parses as a
// mapping carrying a top-level `name` key, whatever its spelling or value.
//
// Quoting is syntax, not identity: `"name":` and `name:` are the same key, and
// a key with an empty value is still the key. Only nesting makes a difference —
// `metadata:\n  name: x` has no top-level name, so one still has to be added.
func frontmatterHasNameKey(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok {
		return false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmBody), &doc); err != nil {
		return false
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false
	}
	mapping := doc.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "name" {
			return true
		}
	}
	return false
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
// just after the opening `---` line) contains a top-level `name` key before
// the closing `---`, whatever its spelling or value.
//
// This is the malformed-block twin of frontmatterHasNameKey: it answers the
// same question for blocks that do not parse, where the caller is choosing
// between re-synthesizing (name present) and injecting a fresh `name` (name
// absent). The invariant is key presence, not value presence — `"name":`,
// `'name':`, and a valueless `name:` are all the key, and injecting a second
// one above any of them yields a duplicate mapping key that strict loaders
// reject, leaving the skill unloadable rather than healed (MUL-5529). Reading
// a plain scalar like `name:value` as the key over-detects, and deliberately
// so: the block is already unparseable, and re-synthesis is the route that
// repairs it.
//
// Only unindented keys count. An indented `name:` belongs to a nested mapping
// (`metadata:\n  name: foo`), so the block still has no top-level name.
func hasFrontmatterName(fmBody string) bool {
	closeIdx := strings.Index(fmBody, "\n---")
	if closeIdx < 0 {
		// Missing close — scan everything we have. The frontmatter is
		// malformed and strict runtimes reject it anyway, but detecting an
		// existing name keeps us from layering a second one on top.
		closeIdx = len(fmBody)
	}
	for _, line := range strings.Split(fmBody[:closeIdx], "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "name:") ||
			strings.HasPrefix(line, `"name":`) ||
			strings.HasPrefix(line, `'name':`) {
			return true
		}
	}
	return false
}

// frontmatterNameValueSpan returns the byte range of the whole top-level `name`
// entry — key plus value, however many lines the value occupies — inside a
// frontmatter body that is already known to be valid YAML.
//
// The extent comes from yaml.v3's own line numbers rather than from
// indentation, because indentation does not bound a YAML value. A quoted scalar
// may wrap onto a line at the *same* indentation as its key, and so may a flow
// collection:
//
//	name: "upstream
//	continued"
//	name: [a,
//	b]
//
// Both parse; an indentation rule stops at the key's line and leaves the
// remainder stranded, turning a rewrite into invalid YAML. Asking the parser
// where the next top-level key starts is the only reliable boundary.
func frontmatterNameValueSpan(fmBody string) (start, end int, ok bool) {
	closeIdx := strings.Index(fmBody, "\n---")
	if closeIdx < 0 {
		closeIdx = len(fmBody)
	}
	block := fmBody[:closeIdx]

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		return 0, 0, false
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, 0, false
	}
	mapping := doc.Content[0]

	// yaml.Node lines are 1-based.
	nameLine, nextKeyLine := 0, 0
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Value != "name" {
			continue
		}
		nameLine = key.Line
		if i+2 < len(mapping.Content) {
			nextKeyLine = mapping.Content[i+2].Line
		}
		break
	}
	if nameLine == 0 {
		return 0, 0, false
	}

	lines := strings.Split(block, "\n")
	lastLine := len(lines)
	if nextKeyLine > 0 {
		lastLine = nextKeyLine - 1
	}
	if lastLine > len(lines) {
		lastLine = len(lines)
	}
	// Blank lines and unindented comments sitting between this entry and the
	// next key are not part of the value: block scalar content must be
	// indented, so an unindented `#` can only be a comment. Leave them in
	// place instead of swallowing them into the replacement.
	for lastLine > nameLine {
		line := strings.TrimSuffix(lines[lastLine-1], "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			lastLine--
			continue
		}
		break
	}
	if lastLine < nameLine {
		return 0, 0, false
	}

	offset := 0
	for i, line := range lines {
		if i == nameLine-1 {
			start = offset
		}
		if i == lastLine-1 {
			return start, offset + len(line), true
		}
		offset += len(line) + 1 // +1 for the newline Split consumed
	}
	return 0, 0, false
}

// setFrontmatterName replaces the top-level frontmatter `name` entry — key and
// full value, however many lines it spans — with a single-line `name: <slug>`,
// leaving every other byte of content untouched. fmStart is the offset where
// the YAML body begins (see frontmatterBodyStart).
//
// verified reports that the result was re-parsed and its `name` really is slug.
// Callers must treat false as "do not use this output": the directory ==
// frontmatter-name invariant is the entire reason this function exists, so a
// rewrite that cannot prove it is worse than no rewrite at all.
func setFrontmatterName(content string, fmStart int, slug string) (result string, verified bool) {
	fmBody := content[fmStart:]
	start, end, ok := frontmatterNameValueSpan(fmBody)
	if !ok {
		return "", false
	}
	replacement := "name: " + slug
	// strings.Split on "\n" leaves the "\r" of a CRLF ending inside the line;
	// carry it over so the block doesn't end up with mixed terminators.
	if strings.HasSuffix(fmBody[start:end], "\r") {
		replacement += "\r"
	}
	rewritten := content[:fmStart+start] + replacement + content[fmStart+end:]
	if !frontmatterNameIs(rewritten, slug) {
		return "", false
	}
	return rewritten, true
}

// renameFrontmatterNameViaNode rebuilds the frontmatter block from its parsed
// YAML node with `name` set to slug, and returns the reassembled document.
//
// This is the middle ground between the surgical byte rewrite and full
// re-synthesis. It gives up the original block's exact formatting — the
// re-marshal normalizes quoting and indentation — but keeps every key and its
// value, which re-synthesis does not. That difference matters beyond tidiness:
// synthesizeFrontmatter emits only name and description, so a block carrying
// `disable-model-invocation: true` would come back out without it and the
// skill, once on disk, would be advertised by the runtime's native discovery
// despite the author having hidden it.
//
// An anchor on the name value needs care in both directions. Simply dropping it
// strands any `*alias` that referenced it and invalidates the document. Simply
// keeping it is worse: every alias then resolves to the slug, so a document
// that expressed a setting by reusing the name's value silently acquires a
// different setting. `disable-model-invocation: *shared` against
// `name: &shared "true"` flips from true to false — the same skill-exposing
// regression as dropping the key outright, just by another route.
//
// So aliases are materialized to the value they resolved to *before* the
// rename, and the anchor is then removed as unreferenced. Every field keeps the
// value it had; only `name` changes.
func renameFrontmatterNameViaNode(content, slug string) (string, bool) {
	fmBody, body, ok := frontmatterParts(content)
	if !ok {
		return "", false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmBody), &doc); err != nil {
		return "", false
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", false
	}

	mapping := doc.Content[0]
	var value *yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "name" {
			value = mapping.Content[i+1]
			break
		}
	}
	if value == nil {
		return "", false
	}

	// Pin every alias to what it resolves to now, before the rename can change
	// it out from under them. Done first so the clones capture the original
	// value rather than the slug.
	if value.Anchor != "" {
		materializeAliasesOf(&doc, value)
	}

	// Collapse whatever the value was (scalar, flow sequence, …) to a plain
	// string. The anchor goes with it: nothing references it any more.
	value.Kind = yaml.ScalarNode
	value.Tag = "!!str"
	value.Style = 0
	value.Value = slug
	value.Content = nil
	value.Anchor = ""

	marshaled, err := yaml.Marshal(&doc)
	if err != nil {
		return "", false
	}
	rebuilt := "---\n" + string(marshaled) + "---\n" + body
	if !frontmatterNameIs(rebuilt, slug) {
		return "", false
	}
	return rebuilt, true
}

// materializeAliasesOf replaces every alias node under root that points at
// target with an inline copy of target's current value, so those fields keep
// that value even after target is rewritten.
func materializeAliasesOf(root, target *yaml.Node) {
	for _, child := range root.Content {
		if child.Kind == yaml.AliasNode && child.Alias == target {
			// Each alias gets its own copy; sharing one node would make the
			// encoder re-introduce an anchor and alias pair.
			*child = *cloneYAMLNode(target)
			continue
		}
		materializeAliasesOf(child, target)
	}
}

// cloneYAMLNode deep-copies a node, stripping anchor/alias identity so the copy
// is emitted inline rather than as a reference.
func cloneYAMLNode(n *yaml.Node) *yaml.Node {
	clone := *n
	clone.Anchor = ""
	clone.Alias = nil
	if len(n.Content) > 0 {
		clone.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			clone.Content[i] = cloneYAMLNode(child)
		}
	}
	return &clone
}

// frontmatterNameIs reports whether content's frontmatter parses and its
// top-level `name` is exactly want.
func frontmatterNameIs(content, want string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok {
		return false
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(fmBody), &m); err != nil {
		return false
	}
	name, _ := m["name"].(string)
	return name == want
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

	// resolveSkillSlugs deduplicates within the batch first, so two skills whose
	// names sanitize alike ("A B" / "A-B") get distinct bases here instead of
	// racing for the same directory. The listings derive from the same function,
	// which is what keeps them naming the directories this loop creates.
	// allocateCollisionFreeSkillDir still runs on top, for collisions against
	// directories we did not write (user-installed skills).
	batchSlugs := resolveSkillSlugs(skills)

	for i, skill := range skills {
		slug, dir, err := allocateCollisionFreeSkillDir(skillsDir, batchSlugs[i])
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
