package handler

import (
	"github.com/multica-ai/multica/server/internal/service"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

func resolveSelectedSkillInvocations(sourceTexts []string, skills []service.AgentSkillData, provider string) ([]SkillInvocationData, error) {
	assigned := make([]skillpkg.AssignedSkill, 0, len(skills))
	for _, skill := range skills {
		assigned = append(assigned, skillpkg.AssignedSkill{
			ID:      skill.ID,
			Name:    skill.Name,
			Content: skill.Content,
		})
	}
	selected, err := skillpkg.ResolveSelectedSkillInvocations(sourceTexts, assigned, provider)
	if err != nil {
		return nil, err
	}
	out := make([]SkillInvocationData, 0, len(selected))
	for _, skill := range selected {
		out = append(out, SkillInvocationData{Name: skill.Name})
	}
	return out, nil
}
