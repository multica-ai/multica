package evals

import (
	"encoding/json"
	"testing"
)

func validInput() EvalInput {
	return EvalInput{
		Key: "catalog-contract", Version: "1.0.0", Title: "Catalog contract", Status: "active",
		Objective: "Reject malformed catalog entries.", Description: "Deterministic contract.",
		Owner:      json.RawMessage(`{"team":"Firtal AI","contact":"jeh@firtal.com"}`),
		Target:     json.RawMessage(`{"kind":"code","locator":"https://github.com/firtal-group/firtal-evals","ref":"main"}`),
		Datasets:   json.RawMessage(`[{"id":"regressions","version":"1.0.0","split":"held_out","path":"cases/regressions.jsonl"}]`),
		Graders:    json.RawMessage(`[{"id":"validator","type":"deterministic","path":"graders/validator.json"}]`),
		Thresholds: json.RawMessage(`[{"metric":"validation_passed","operator":"eq","value":true}]`),
		Runner:     json.RawMessage(`{"command":["python3","scripts/validate_catalog.py"],"working_directory":"."}`),
		Source:     json.RawMessage(`{"repository":"https://github.com/firtal-group/firtal-evals","commit":"abc123","path":"evals/catalog-contract/eval.json"}`),
	}
}

func TestValidateEvalInputAcceptsVersionedContract(t *testing.T) {
	input := validInput()
	if err := validateEvalInput(&input); err != nil {
		t.Fatalf("validateEvalInput: %v", err)
	}
}

func TestValidateEvalInputRejectsSkillStyleNameAndEmptyActiveContract(t *testing.T) {
	input := validInput()
	input.Key = "Catalog Eval Skill"
	if err := validateEvalInput(&input); err == nil {
		t.Fatal("expected invalid key")
	}
	input = validInput()
	input.Graders = json.RawMessage(`[]`)
	if err := validateEvalInput(&input); err == nil {
		t.Fatal("expected active grader requirement")
	}
}

func TestValidateEvalInputRequiresTypedJSONShapes(t *testing.T) {
	input := validInput()
	input.Target = json.RawMessage(`[]`)
	if err := validateEvalInput(&input); err == nil {
		t.Fatal("expected target object requirement")
	}
	input = validInput()
	input.Datasets = json.RawMessage(`{}`)
	if err := validateEvalInput(&input); err == nil {
		t.Fatal("expected datasets array requirement")
	}
}

func TestValidateActiveEvalRequiresPinnedSource(t *testing.T) {
	input := validInput()
	input.Source = json.RawMessage(`{"repository":"https://github.com/firtal-group/firtal-evals","commit":"","path":"evals/catalog-contract/eval.json"}`)
	if err := validateEvalInput(&input); err == nil {
		t.Fatal("expected pinned source commit requirement")
	}
}
