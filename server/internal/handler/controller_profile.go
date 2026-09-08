package handler

import (
	"github.com/multica-ai/multica/server/internal/runcontrol"
	"github.com/multica-ai/multica/server/internal/service"
)

func controllerSkills(inputs runcontrol.ProfileSnapshot) []service.AgentSkillData {
	files := map[string][]service.AgentSkillFileData{}
	for _, file := range inputs.Files {
		id := uuidToString(file.SkillID)
		files[id] = append(files[id], service.AgentSkillFileData{Path: file.Path, Content: file.Content})
	}
	result := make([]service.AgentSkillData, 0, len(inputs.Skills))
	for _, skill := range inputs.Skills {
		id := uuidToString(skill.ID)
		result = append(result, service.AgentSkillData{ID: id, Name: skill.Name, Description: skill.Description, Content: skill.Content, Files: files[id]})
	}
	return result
}
