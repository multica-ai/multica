package handler

import "github.com/multica-ai/multica/server/internal/service"

const operationalWorkflowSkillName = "multica-operational-workflow"

func operatingModeUsesOperationalWorkflow(mode string) bool {
	mode = normaliseStoredAgentOperatingMode(mode)
	return mode == "operational" || mode == "hybrid"
}

func filterOperationalWorkflowSkillData(skills []service.AgentSkillData, mode string) []service.AgentSkillData {
	if operatingModeUsesOperationalWorkflow(mode) {
		return skills
	}
	filtered := make([]service.AgentSkillData, 0, len(skills))
	for _, skill := range skills {
		if skill.Name == operationalWorkflowSkillName && (skill.Source == "" || skill.Source == "builtin") {
			continue
		}
		filtered = append(filtered, skill)
	}
	return filtered
}

func filterOperationalWorkflowSkillRefs(skills []service.AgentSkillRefData, mode string) []service.AgentSkillRefData {
	if operatingModeUsesOperationalWorkflow(mode) {
		return skills
	}
	filtered := make([]service.AgentSkillRefData, 0, len(skills))
	for _, skill := range skills {
		if skill.ID == "builtin:"+operationalWorkflowSkillName ||
			(skill.Source == "builtin" && skill.Name == operationalWorkflowSkillName) {
			continue
		}
		filtered = append(filtered, skill)
	}
	return filtered
}
