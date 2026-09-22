package workflow

import (
	"encoding/json"
	"testing"
)

func TestBlankTemplateJSONEmitsArrayBindings(t *testing.T) {
	encoded, err := json.Marshal(ListTemplates())
	if err != nil {
		t.Fatal(err)
	}
	var templates []struct {
		ID       string            `json:"id"`
		Bindings []json.RawMessage `json:"bindings"`
	}
	if err := json.Unmarshal(encoded, &templates); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, template := range templates {
		if template.ID == "blank" {
			found = true
			if template.Bindings == nil {
				t.Fatalf("blank template bindings = null, want JSON array: %s", encoded)
			}
		}
	}
	if !found {
		t.Fatal("blank template is missing")
	}
}
