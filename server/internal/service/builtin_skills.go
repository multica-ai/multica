package service

import (
	"embed"
	"io/fs"
	"path"
	"strings"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

//go:embed builtin_skills
var builtinSkillsFS embed.FS

const builtinSkillsRoot = "builtin_skills"

// PlatformSkillName is the built-in skill carrying Multica's platform
// contracts — issues, mentions, agents, squads, autopilots, projects, runtimes
// and skill import — behind one routing SKILL.md. Every agent receives it.
//
// The daemon's runtime brief names the same skill from its own constant: the
// daemon runs on the user's machine and must not import this package, so the
// two are pinned by the brief's rendered-output tests instead of a shared
// symbol.
const PlatformSkillName = "multica-platform"

// builtinSkillSystemKey restricts a built-in skill to one product-defined
// agent, keyed by that agent's system key. A skill absent from this map is
// universal and every agent receives it.
//
// The scoping exists because a built-in skill costs every agent that receives
// it: its description sits in the always-loaded skill listing, and its files
// are written into every task's workdir. Mika's onboarding walkthrough is one
// agent's procedure, not a platform contract, so shipping it workspace-wide
// spent that budget on nine agents out of ten that can never use it.
var builtinSkillSystemKey = map[string]string{
	"multica-onboarding": MikaSystemKey,
}

// BuiltinSkills returns the platform's built-in skills for an agent with the
// given system key (empty for an ordinary workspace agent), embedded at compile
// time. Every agent receives the universal ones on top of its workspace-bound
// skills, so they teach platform-wide "how to" workflows that the runtime brief
// intentionally leaves to skills.
//
// Layout: builtin_skills/<name>/SKILL.md plus optional supporting files. The
// <name> directory carries a "multica-" prefix so its on-disk slug can never
// collide with a workspace skill a user authored (see writeSkillFiles, which
// derives the skill directory from AgentSkillData.Name).
func (s *TaskService) BuiltinSkills(agentSystemKey string) []AgentSkillData {
	return loadBuiltinSkills(agentSystemKey)
}

// AllBuiltinSkills returns every built-in skill regardless of agent scope. Only
// the bundle-resolve path uses it: the claim already decided which built-ins an
// agent was told about, and a daemon can only ask to resolve a ref it was
// handed, so re-deriving the scope there would cost an agent read to re-answer
// a question the claim answered.
func (s *TaskService) AllBuiltinSkills() []AgentSkillData {
	return loadBuiltinSkillDirs(func(string) bool { return true })
}

func loadBuiltinSkills(agentSystemKey string) []AgentSkillData {
	return loadBuiltinSkillDirs(func(name string) bool {
		want, scoped := builtinSkillSystemKey[name]
		return !scoped || want == agentSystemKey
	})
}

func loadBuiltinSkillDirs(include func(name string) bool) []AgentSkillData {
	entries, err := fs.ReadDir(builtinSkillsFS, builtinSkillsRoot)
	if err != nil {
		return nil
	}
	var skills []AgentSkillData
	for _, entry := range entries {
		if !entry.IsDir() || !include(entry.Name()) {
			continue
		}
		if skill, ok := loadBuiltinSkill(entry.Name()); ok {
			skills = append(skills, skill)
		}
	}
	return skills
}

func loadBuiltinSkill(name string) (AgentSkillData, bool) {
	dir := path.Join(builtinSkillsRoot, name)
	content, err := fs.ReadFile(builtinSkillsFS, path.Join(dir, "SKILL.md"))
	if err != nil {
		// A skill directory without a SKILL.md is malformed — skip it rather
		// than ship an empty skill.
		return AgentSkillData{}, false
	}
	// Source is set here rather than inferred downstream. BuildAgentSkillBundles
	// already derives it for the refs path, but the older inline claim path
	// appends these straight onto the agent's skills, and the daemon needs to
	// tell a built-in from a workspace skill that sanitizes to the same slug.
	// Setting it changes no bundle hash: the bundle builder normalizes Source to
	// this same value before hashing.
	skill := AgentSkillData{Name: name, Source: skillbundle.SourceBuiltin, Content: string(content)}
	// Any other file in the directory becomes a supporting file, preserving
	// its relative path so subdirectories (e.g. references/issues.md) survive.
	_ = fs.WalkDir(builtinSkillsFS, dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(p, dir+"/")
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := fs.ReadFile(builtinSkillsFS, p)
		if readErr != nil {
			return nil
		}
		skill.Files = append(skill.Files, AgentSkillFileData{Path: rel, Content: string(data)})
		return nil
	})
	return skill, true
}
