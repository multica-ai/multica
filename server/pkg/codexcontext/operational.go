package codexcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

type Mode string

const (
	ModeInherited   Mode = "inherited"
	ModeOperational Mode = "operational"
)

const operationalBaseInstructions = "You are an agent executing one task for Multica. Follow the developer instructions, complete the current task, and return the requested Multica result."

type BuildInput struct {
	AgentInstructions string
	TaskPrompt        string
	AssignedSkills    []skillbundle.Skill
}

type OperationalContext struct {
	BaseInstructions      string
	DeveloperInstructions string
	Prompt                string
	Skills                []skillbundle.Skill
}

func DecodeMode(raw json.RawMessage) (Mode, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ModeInherited, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return "", errors.New("invalid runtime_config")
	}
	codexRaw, ok := root["codex"]
	if !ok {
		return ModeInherited, nil
	}
	if trimmed := bytes.TrimSpace(codexRaw); len(trimmed) == 0 || trimmed[0] != '{' {
		return "", errors.New("codex configuration must be an object")
	}

	var codex map[string]json.RawMessage
	if err := json.Unmarshal(codexRaw, &codex); err != nil || codex == nil {
		return "", errors.New("invalid codex configuration")
	}
	modeRaw, ok := codex["context_mode"]
	if !ok {
		return ModeInherited, nil
	}

	var mode Mode
	if err := json.Unmarshal(modeRaw, &mode); err != nil {
		return "", errors.New("codex.context_mode must be a string")
	}
	switch mode {
	case ModeInherited, ModeOperational:
		return mode, nil
	default:
		return "", errors.New("unsupported codex.context_mode")
	}
}

func BuildOperationalContext(input BuildInput) (OperationalContext, error) {
	seen := make(map[string]struct{}, len(input.AssignedSkills))
	for _, skill := range input.AssignedSkills {
		normalized := skillbundle.NormalizeName(skill.Name)
		if _, exists := seen[normalized]; exists {
			return OperationalContext{}, errors.New("assigned skills contain duplicate normalized names")
		}
		seen[normalized] = struct{}{}
	}

	return OperationalContext{
		BaseInstructions:      operationalBaseInstructions,
		DeveloperInstructions: strings.TrimSpace(input.AgentInstructions),
		Prompt:                input.TaskPrompt,
		Skills:                cloneSkills(input.AssignedSkills),
	}, nil
}

func cloneSkills(skills []skillbundle.Skill) []skillbundle.Skill {
	if len(skills) == 0 {
		return nil
	}
	cloned := make([]skillbundle.Skill, len(skills))
	for i, skill := range skills {
		cloned[i] = skill
		cloned[i].Files = append([]skillbundle.File(nil), skill.Files...)
	}
	return cloned
}
