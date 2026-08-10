package daemon

import (
	"fmt"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/codexcontext"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

func applyOperationalExecOptions(opts *agent.ExecOptions, context *codexcontext.OperationalContext) {
	if opts == nil || context == nil {
		return
	}
	opts.CodexContextMode = codexcontext.ModeOperational
	opts.BaseInstructions = context.BaseInstructions
	opts.DeveloperInstructions = context.DeveloperInstructions
	opts.ResumeSessionID = ""
	opts.ResumeExpected = false
	opts.ResumeContinuityNotice = ""
	opts.ExtraArgs = nil
	opts.CustomArgs = nil
	opts.McpConfig = nil
}

func resolveCodexTaskContext(task Task, provider string) (codexcontext.Mode, *codexcontext.OperationalContext, error) {
	var rawRuntimeConfig []byte
	if task.Agent != nil {
		rawRuntimeConfig = task.Agent.RuntimeConfig
	}
	mode, err := codexcontext.DecodeMode(rawRuntimeConfig)
	if err != nil {
		return "", nil, fmt.Errorf("decode codex context mode: %w", err)
	}
	if mode != codexcontext.ModeOperational {
		return mode, nil, nil
	}
	if provider != "codex" {
		return "", nil, fmt.Errorf("codex operational context is unsupported for provider %q", provider)
	}

	var instructions string
	var skills []SkillData
	if task.Agent != nil {
		instructions = task.Agent.Instructions
		skills = task.Agent.Skills
	}
	context, err := codexcontext.BuildOperationalContext(codexcontext.BuildInput{
		AgentInstructions: instructions,
		TaskPrompt:        BuildOperationalPrompt(task, provider),
		AssignedSkills:    convertSkillsForOperationalContext(skills),
	})
	if err != nil {
		return "", nil, fmt.Errorf("build codex operational context: %w", err)
	}
	return mode, &context, nil
}

func convertSkillsForOperationalContext(skills []SkillData) []skillbundle.Skill {
	if len(skills) == 0 {
		return nil
	}
	converted := make([]skillbundle.Skill, len(skills))
	for i, skill := range skills {
		files := make([]skillbundle.File, len(skill.Files))
		for j, file := range skill.Files {
			files[j] = skillbundle.File{Path: file.Path, Content: file.Content}
		}
		converted[i] = skillbundle.Skill{
			ID:          skill.ID,
			Source:      skill.Source,
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Files:       files,
		}
	}
	return converted
}
