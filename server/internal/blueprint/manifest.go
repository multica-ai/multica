package blueprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "multica.workspace_blueprint/v1"

type Manifest struct {
	Schema     string    `json:"schema"`
	Name       string    `json:"name"`
	ExportedAt string    `json:"exported_at"`
	Squads     []Squad   `json:"squads"`
	Agents     []Agent   `json:"agents"`
	Skills     []Skill   `json:"skills"`
	Warnings   []Warning `json:"warnings,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Squad struct {
	Ref          string        `json:"ref"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Instructions string        `json:"instructions,omitempty"`
	AvatarURL    *string       `json:"avatar_url,omitempty"`
	LeaderRef    string        `json:"leader_ref"`
	Members      []SquadMember `json:"members"`
}

type SquadMember struct {
	Ref  string `json:"ref"`
	Role string `json:"role,omitempty"`
}

type Agent struct {
	Ref                string   `json:"ref"`
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	Instructions       string   `json:"instructions,omitempty"`
	AvatarURL          *string  `json:"avatar_url,omitempty"`
	Runtime            Runtime  `json:"runtime"`
	Visibility         string   `json:"visibility"`
	MaxConcurrentTasks int32    `json:"max_concurrent_tasks"`
	CustomEnvSchema    []EnvVar `json:"custom_env_schema,omitempty"`
	CustomArgs         []string `json:"custom_args,omitempty"`
	MCPConfigRedacted  bool     `json:"mcp_config_redacted,omitempty"`
	SkillRefs          []string `json:"skill_refs,omitempty"`
}

type Runtime struct {
	Mode          string `json:"mode"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
}

type EnvVar struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

type Skill struct {
	Ref         string          `json:"ref"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     string          `json:"content,omitempty"`
	Config      json.RawMessage `json:"config"`
	Files       []SkillFile     `json:"files,omitempty"`
}

type SkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Source struct {
	Name          string
	ExportedAt    time.Time
	Squads        []SourceSquad
	SquadMembers  map[string][]SourceSquadMember
	Agents        []SourceAgent
	AgentSkillIDs map[string][]string
	Skills        []SourceSkill
	SkillFiles    map[string][]SourceSkillFile
}

type SourceSquad struct {
	ID           string
	Name         string
	Description  string
	Instructions string
	AvatarURL    *string
	LeaderID     string
}

type SourceSquadMember struct {
	MemberType string
	MemberID   string
	Role       string
}

type SourceAgent struct {
	ID                 string
	Name               string
	Description        string
	Instructions       string
	AvatarURL          *string
	RuntimeID          string
	RuntimeMode        string
	RuntimeProvider    string
	RuntimeConfig      json.RawMessage
	Visibility         string
	MaxConcurrentTasks int32
	Model              string
	ThinkingLevel      string
	CustomEnv          map[string]string
	CustomArgs         []string
	MCPConfig          json.RawMessage
}

type SourceSkill struct {
	ID          string
	Name        string
	Description string
	Content     string
	Config      json.RawMessage
}

type SourceSkillFile struct {
	Path    string
	Content string
}

func BuildManifest(src Source) (Manifest, error) {
	exportedAt := src.ExportedAt
	if exportedAt.IsZero() {
		exportedAt = time.Now().UTC()
	}

	name := strings.TrimSpace(src.Name)
	if name == "" {
		name = "Workspace Blueprint"
	}

	squadRefs := refsFor(src.Squads, "squad", func(s SourceSquad) string { return s.ID }, func(s SourceSquad) string { return s.Name })
	agentRefs := refsFor(src.Agents, "agent", func(a SourceAgent) string { return a.ID }, func(a SourceAgent) string { return a.Name })
	skillRefs := refsFor(src.Skills, "skill", func(s SourceSkill) string { return s.ID }, func(s SourceSkill) string { return s.Name })

	manifest := Manifest{
		Schema:     SchemaVersion,
		Name:       name,
		ExportedAt: exportedAt.UTC().Format(time.RFC3339),
		Squads:     make([]Squad, 0, len(src.Squads)),
		Agents:     make([]Agent, 0, len(src.Agents)),
		Skills:     make([]Skill, 0, len(src.Skills)),
	}

	for _, squad := range src.Squads {
		leaderRef := agentRefs[squad.LeaderID]
		members := make([]SquadMember, 0, len(src.SquadMembers[squad.ID]))
		for _, member := range src.SquadMembers[squad.ID] {
			if member.MemberType != "agent" {
				continue
			}
			ref := agentRefs[member.MemberID]
			if ref == "" {
				continue
			}
			members = append(members, SquadMember{
				Ref:  ref,
				Role: member.Role,
			})
		}
		manifest.Squads = append(manifest.Squads, Squad{
			Ref:          squadRefs[squad.ID],
			Name:         squad.Name,
			Description:  squad.Description,
			Instructions: squad.Instructions,
			AvatarURL:    squad.AvatarURL,
			LeaderRef:    leaderRef,
			Members:      members,
		})
	}

	for _, agent := range src.Agents {
		skillRefsForAgent := make([]string, 0, len(src.AgentSkillIDs[agent.ID]))
		for _, skillID := range src.AgentSkillIDs[agent.ID] {
			if ref := skillRefs[skillID]; ref != "" {
				skillRefsForAgent = append(skillRefsForAgent, ref)
			}
		}
		sort.Strings(skillRefsForAgent)

		manifest.Agents = append(manifest.Agents, Agent{
			Ref:          agentRefs[agent.ID],
			Name:         agent.Name,
			Description:  agent.Description,
			Instructions: agent.Instructions,
			AvatarURL:    agent.AvatarURL,
			Runtime: Runtime{
				Mode:          agent.RuntimeMode,
				Provider:      agent.RuntimeProvider,
				Model:         agent.Model,
				ThinkingLevel: agent.ThinkingLevel,
			},
			Visibility:         agent.Visibility,
			MaxConcurrentTasks: agent.MaxConcurrentTasks,
			CustomEnvSchema:    envSchema(agent.CustomEnv),
			CustomArgs:         append([]string(nil), agent.CustomArgs...),
			MCPConfigRedacted:  hasJSONObject(agent.MCPConfig),
			SkillRefs:          skillRefsForAgent,
		})
	}

	for _, skill := range src.Skills {
		files := make([]SkillFile, 0, len(src.SkillFiles[skill.ID]))
		for _, file := range src.SkillFiles[skill.ID] {
			files = append(files, SkillFile{Path: file.Path, Content: file.Content})
		}
		config := json.RawMessage(`{}`)
		if len(strings.TrimSpace(string(skill.Config))) > 0 {
			config = append(json.RawMessage(nil), skill.Config...)
		}
		manifest.Skills = append(manifest.Skills, Skill{
			Ref:         skillRefs[skill.ID],
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Config:      config,
			Files:       files,
		})
	}

	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	var errs []error
	if manifest.Schema != SchemaVersion {
		errs = append(errs, fmt.Errorf("schema must be %q", SchemaVersion))
	}

	seenRefs := map[string]struct{}{}
	skillRefs := map[string]struct{}{}
	agentRefs := map[string]struct{}{}

	checkRef := func(ref, kind string) {
		if strings.TrimSpace(ref) == "" {
			errs = append(errs, fmt.Errorf("%s ref is required", kind))
			return
		}
		if _, ok := seenRefs[ref]; ok {
			errs = append(errs, fmt.Errorf("duplicate ref %q", ref))
			return
		}
		seenRefs[ref] = struct{}{}
	}

	for _, skill := range manifest.Skills {
		checkRef(skill.Ref, "skill")
		skillRefs[skill.Ref] = struct{}{}
		if strings.TrimSpace(skill.Name) == "" {
			errs = append(errs, fmt.Errorf("skill %q name is required", skill.Ref))
		}
		if len(skill.Config) > 0 && !json.Valid(skill.Config) {
			errs = append(errs, fmt.Errorf("skill %q config must be valid JSON", skill.Ref))
		}
		for _, file := range skill.Files {
			if !safeSkillFilePath(file.Path) {
				errs = append(errs, fmt.Errorf("unsafe skill file path %q in %s", file.Path, skill.Ref))
			}
		}
	}

	for _, agent := range manifest.Agents {
		checkRef(agent.Ref, "agent")
		agentRefs[agent.Ref] = struct{}{}
		if strings.TrimSpace(agent.Name) == "" {
			errs = append(errs, fmt.Errorf("agent %q name is required", agent.Ref))
		}
		if agent.Runtime.Mode != "local" && agent.Runtime.Mode != "cloud" {
			errs = append(errs, fmt.Errorf("agent %q runtime mode must be local or cloud", agent.Ref))
		}
		for _, skillRef := range agent.SkillRefs {
			if _, ok := skillRefs[skillRef]; !ok {
				errs = append(errs, fmt.Errorf("agent %q references missing skill %q", agent.Ref, skillRef))
			}
		}
	}

	for _, squad := range manifest.Squads {
		checkRef(squad.Ref, "squad")
		if strings.TrimSpace(squad.Name) == "" {
			errs = append(errs, fmt.Errorf("squad %q name is required", squad.Ref))
		}
		if squad.LeaderRef == "" {
			errs = append(errs, fmt.Errorf("squad %q leader_ref is required", squad.Ref))
		} else if _, ok := agentRefs[squad.LeaderRef]; !ok {
			errs = append(errs, fmt.Errorf("squad %q references missing leader %q", squad.Ref, squad.LeaderRef))
		}
		for _, member := range squad.Members {
			if _, ok := agentRefs[member.Ref]; !ok {
				errs = append(errs, fmt.Errorf("squad %q references missing member %q", squad.Ref, member.Ref))
			}
		}
	}

	return errors.Join(errs...)
}

func refsFor[T any](items []T, kind string, idFn func(T) string, nameFn func(T) string) map[string]string {
	refs := make(map[string]string, len(items))
	used := map[string]int{}
	for _, item := range items {
		id := idFn(item)
		if id == "" {
			continue
		}
		base := slugify(nameFn(item))
		if base == "" {
			base = kind
		}
		count := used[base]
		used[base] = count + 1
		if count > 0 {
			base = fmt.Sprintf("%s-%d", base, count+1)
		}
		refs[id] = kind + "." + base
	}
	return refs
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func envSchema(env map[string]string) []EnvVar {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	schema := make([]EnvVar, 0, len(keys))
	for _, key := range keys {
		schema = append(schema, EnvVar{Name: key, Required: false, Secret: true})
	}
	return schema
}

func hasJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}

func safeSkillFilePath(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	clean := path.Clean(p)
	if clean == "." || clean != p {
		return false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}
