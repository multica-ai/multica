package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/pkg/codexcontext"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

const operationalCodexConfig = `project_doc_max_bytes = 0

[skills.bundled]
enabled = false
`

func writeEffectiveContextFiles(workDir, provider string, task TaskContextForEnv, operational *codexcontext.OperationalContext, manifest *sidecarManifest) error {
	if operational == nil {
		return writeContextFiles(workDir, provider, task, manifest)
	}
	if err := writeTaskContextMarker(workDir, task, manifest); err != nil {
		return err
	}
	return nil
}

func prepareOperationalCodexHome(codexHome string, opts CodexHomeOptions, context codexcontext.OperationalContext, logger *slog.Logger) error {
	if err := os.RemoveAll(codexHome); err != nil {
		return fmt.Errorf("clear managed codex-home: %w", err)
	}
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return fmt.Errorf("create codex-home: %w", err)
	}

	sharedHome := resolveSharedCodexHome()
	if err := ensureSymlink(filepath.Join(sharedHome, "auth.json"), filepath.Join(codexHome, "auth.json")); err != nil {
		return fmt.Errorf("link codex authentication: %w", err)
	}
	if err := os.Mkdir(filepath.Join(codexHome, "sessions"), 0o755); err != nil {
		return fmt.Errorf("create fresh sessions directory: %w", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(operationalCodexConfig), 0o600); err != nil {
		return fmt.Errorf("write operational codex config: %w", err)
	}
	policy := codexSandboxPolicyForConfig(opts.GOOS, opts.CodexVersion, windowsSandboxAbsent)
	if err := ensureCodexSandboxConfig(configPath, policy, opts.CodexVersion, logger); err != nil {
		return fmt.Errorf("write codex sandbox config: %w", err)
	}
	if err := ensureCodexMultiAgentDisabledConfig(configPath); err != nil {
		return fmt.Errorf("write codex multi-agent config: %w", err)
	}
	if err := ensureCodexMemoryDisabledConfig(configPath); err != nil {
		return fmt.Errorf("write codex memory config: %w", err)
	}

	skills := operationalSkillsForEnv(context.Skills)
	if len(skills) > 0 {
		if err := writeSkillFiles(filepath.Join(codexHome, "skills"), skills, nil); err != nil {
			return fmt.Errorf("write assigned codex skills: %w", err)
		}
	}
	return nil
}

func operationalSkillsForEnv(skills []skillbundle.Skill) []SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	converted := make([]SkillContextForEnv, len(skills))
	for i, skill := range skills {
		files := make([]SkillFileContextForEnv, len(skill.Files))
		for j, file := range skill.Files {
			files[j] = SkillFileContextForEnv{Path: file.Path, Content: file.Content}
		}
		converted[i] = SkillContextForEnv{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Files:       files,
		}
	}
	return converted
}
