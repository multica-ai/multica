package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

var discoverOmpSkills = agent.DiscoverOmpSkills

func (d *Daemon) ompSkills(ctx context.Context, rt Runtime) ([]agent.OmpSkill, error) {
	if spec, ok := d.customProfileLaunchForRuntime(rt.ID); ok {
		return discoverOmpSkills(ctx, agent.NewCommand(spec.path, agent.FilterLaunchPrefix(rt.Provider, spec.fixedArgs, d.logger)))
	}
	// A missing custom command must not silently probe the default profile.
	if rt.ProfileID != "" {
		return nil, fmt.Errorf("omp runtime profile command is unavailable")
	}
	entry, ok := d.agents()[rt.Provider]
	if !ok {
		return nil, fmt.Errorf("no agent configured for provider %q", rt.Provider)
	}
	entry, _ = d.resolveAgentEntry(ctx, rt.Provider, entry)
	return discoverOmpSkills(ctx, agent.NewCommand(entry.Path, nil))
}

func (d *Daemon) listLocalSkills(ctx context.Context, rt Runtime) ([]runtimeLocalSkillSummary, bool, error) {
	if rt.Provider != "omp" {
		return listRuntimeLocalSkills(rt.Provider)
	}
	resolved, err := d.ompSkills(ctx, rt)
	if err != nil {
		return nil, true, err
	}
	skills := make([]runtimeLocalSkillSummary, 0, len(resolved))
	for _, entry := range resolved {
		dir := filepath.Dir(entry.Path)
		files, err := collectLocalSkillFiles(dir, false)
		if err != nil {
			return nil, true, err
		}
		root := localSkillRootProvider
		if home, err := os.UserHomeDir(); err == nil {
			rel, err := filepath.Rel(filepath.Join(home, ".agents", "skills"), dir)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				root = localSkillRootUniversal
			}
		}
		skills = append(skills, runtimeLocalSkillSummary{
			Key: entry.Name, Name: entry.Name, Description: entry.Description,
			SourcePath: relativizeHomePath(dir), Provider: rt.Provider,
			Root: root, FileCount: len(files) + 1,
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Key < skills[j].Key })
	return skills, true, nil
}

func (d *Daemon) loadLocalSkillBundle(ctx context.Context, rt Runtime, skillKey string) (*runtimeLocalSkillBundle, bool, error) {
	if rt.Provider != "omp" {
		return loadRuntimeLocalSkillBundle(rt.Provider, skillKey)
	}
	key, err := normalizeLocalSkillKey(skillKey)
	if err != nil {
		return nil, true, err
	}
	resolved, err := d.ompSkills(ctx, rt)
	if err != nil {
		return nil, true, err
	}
	for _, entry := range resolved {
		if entry.Name != key {
			continue
		}
		dir := filepath.Dir(entry.Path)
		content, err := readLocalSkillMainFile(dir)
		if err != nil {
			return nil, true, err
		}
		files, err := collectLocalSkillFiles(dir, true)
		if err != nil {
			return nil, true, err
		}
		return &runtimeLocalSkillBundle{
			Name: entry.Name, Description: entry.Description, Content: content,
			SourcePath: relativizeHomePath(dir), Provider: rt.Provider, Files: files,
		}, true, nil
	}
	return nil, true, fmt.Errorf("local skill not found in the selected omp profile")
}
