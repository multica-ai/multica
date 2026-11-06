package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OmpSkill is an entry in OMP's resolved skill roster, including its selected
// file when multiple sources provide the same name.
type OmpSkill struct {
	Name        string
	Description string
	Path        string
}

// OMP's RPC command catalog drops file paths and hides skills when skill
// commands are disabled. The supported extension API retains those paths. A
// run-only overlay enables command enumeration without changing skill loading,
// source toggles, exclusions, or the user's persisted settings.
const ompSkillProbe = `import { writeFileSync } from "node:fs";
export default function (pi) {
  pi.on("session_start", () => {
    const skills = pi.getCommands().filter(command => command.source === "skill");
    writeFileSync(process.env.MULTICA_OMP_SKILLS_OUTPUT, JSON.stringify({ skills }), { mode: 0o600 });
  });
}
`

// DiscoverOmpSkills asks the selected runtime command for its effective
// user-level skills. The empty working directory excludes project-specific
// settings and Multica-assigned skills. No prompt or model request is sent.
func DiscoverOmpSkills(ctx context.Context, runtimeCmd Command) ([]OmpSkill, error) {
	dir, err := os.MkdirTemp("", "multica-omp-skills-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	extension := filepath.Join(dir, "inventory.ts")
	config := filepath.Join(dir, "config.yml")
	output := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(extension, []byte(ompSkillProbe), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(config, []byte("skills:\n  enableSkillCommands: true\n"), 0o600); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := runtimeCmd.exec(runCtx, "--mode", "rpc", "--no-session", "--no-title", "--no-ui",
		"--config", config, "--extension", extension)
	cmd.Dir = dir
	cmd.Env = replaceEnvValue(os.Environ(), "MULTICA_OMP_SKILLS_OUTPUT", output)
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	hideAgentWindow(cmd)
	if err := runOwned(cmd, runtimeCmd.logger); err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("omp skill discovery: %w", runCtx.Err())
		}
		return nil, fmt.Errorf("omp skill discovery: %w", err)
	}
	file, err := os.Open(output)
	if err != nil {
		return nil, fmt.Errorf("omp did not return a skill inventory: %w", err)
	}
	defer file.Close()
	const maxSnapshotSize = 8 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxSnapshotSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSnapshotSize {
		return nil, fmt.Errorf("omp skill inventory exceeds %d bytes", maxSnapshotSize)
	}
	return parseOmpSkills(data)
}

func parseOmpSkills(data []byte) ([]OmpSkill, error) {
	var snapshot struct {
		Skills *[]struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Source      string `json:"source"`
			Path        string `json:"path"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("invalid omp skill inventory: %w", err)
	}
	if snapshot.Skills == nil {
		return nil, fmt.Errorf("omp skill inventory is missing skills")
	}
	skills := make([]OmpSkill, 0, len(*snapshot.Skills))
	seen := make(map[string]bool)
	for _, entry := range *snapshot.Skills {
		name, ok := strings.CutPrefix(entry.Name, "skill:")
		if !ok || name == "" || entry.Source != "skill" || !filepath.IsAbs(entry.Path) || filepath.Base(entry.Path) != "SKILL.md" {
			return nil, fmt.Errorf("invalid entry in omp skill inventory")
		}
		if !seen[name] {
			skills = append(skills, OmpSkill{Name: name, Description: entry.Description, Path: entry.Path})
			seen[name] = true
		}
	}
	return skills, nil
}
