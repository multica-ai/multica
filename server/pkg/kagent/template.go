package kagent

import "encoding/json"

// UnmarshalJSON retains unsupported and future capability fields instead of
// silently hiding them from reconciliation. Presence, even null, fails closed.
func (t *AgentTemplateSpec) UnmarshalJSON(data []byte) error {
	type managed AgentTemplateSpec
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var value managed
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	for _, name := range []string{"modelConfig", "description", "systemPrompt", "systemPromptFrom"} {
		delete(fields, name)
	}
	if len(fields) != 0 {
		value.ExtraFields = fields
	}
	*t = AgentTemplateSpec(value)
	return nil
}

func (t AgentTemplateSpec) MarshalJSON() ([]byte, error) {
	type managed AgentTemplateSpec
	raw, err := json.Marshal(managed(t))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for name, value := range t.ExtraFields {
		// ExtraFields cannot override any managed field, including omitted ones.
		switch name {
		case "modelConfig", "description", "systemPrompt", "systemPromptFrom":
			continue
		}
		fields[name] = value
	}
	return json.Marshal(fields)
}
