package evals

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var (
	evalKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

func validateEvalInput(input *EvalInput) error {
	input.Key = strings.TrimSpace(input.Key)
	input.Version = strings.TrimSpace(input.Version)
	input.Title = strings.TrimSpace(input.Title)
	input.Objective = strings.TrimSpace(input.Objective)
	input.Status = strings.TrimSpace(input.Status)
	if !evalKeyPattern.MatchString(input.Key) {
		return errors.New("key must be lowercase kebab-case")
	}
	if !versionPattern.MatchString(input.Version) {
		return errors.New("version must use semantic versioning")
	}
	if input.Title == "" || input.Objective == "" {
		return errors.New("title and objective are required")
	}
	if !oneOf(input.Status, "draft", "active", "paused", "retired") {
		return errors.New("invalid status")
	}
	for field, value := range map[string]json.RawMessage{
		"owner": input.Owner, "target": input.Target, "datasets": input.Datasets,
		"graders": input.Graders, "thresholds": input.Thresholds, "runner": input.Runner,
		"source": input.Source,
	} {
		if len(value) == 0 || !json.Valid(value) {
			return errors.New(field + " must be valid JSON")
		}
	}
	for field, value := range map[string]json.RawMessage{
		"owner": input.Owner, "target": input.Target, "runner": input.Runner, "source": input.Source,
	} {
		if !isJSONObject(value) {
			return errors.New(field + " must be a JSON object")
		}
	}
	for field, value := range map[string]json.RawMessage{
		"datasets": input.Datasets, "graders": input.Graders, "thresholds": input.Thresholds,
	} {
		if !isJSONArray(value) {
			return errors.New(field + " must be a JSON array")
		}
	}
	if input.Status == "active" {
		if emptyJSONArray(input.Datasets) || emptyJSONArray(input.Graders) || emptyJSONArray(input.Thresholds) {
			return errors.New("active evals require datasets, graders, and thresholds")
		}
		var source map[string]any
		_ = json.Unmarshal(input.Source, &source)
		if stringValue(source, "repository") == "" || stringValue(source, "commit") == "" || stringValue(source, "path") == "" {
			return errors.New("active evals require a pinned source repository, commit, and path")
		}
	}
	return nil
}

func validateRunInput(input EvalRunInput) error {
	if !oneOf(input.Status, "queued", "running", "passed", "failed", "error") {
		return errors.New("invalid run status")
	}
	if input.CostCents < 0 || input.LatencyMS < 0 {
		return errors.New("cost_cents and latency_ms cannot be negative")
	}
	if len(input.Results) == 0 || !json.Valid(input.Results) {
		return errors.New("results must be valid JSON")
	}
	return nil
}

func validateBindingInput(input *BindingInput) error {
	input.Phase = strings.TrimSpace(input.Phase)
	if !oneOf(input.Phase, "plan", "delivery", "monitor") {
		return errors.New("invalid phase")
	}
	if input.Phase == "monitor" && input.Blocking {
		return errors.New("monitor bindings must be advisory")
	}
	if input.WorkflowID == uuidNil || input.EvalID == uuidNil {
		return errors.New("workflow_id and eval_id are required")
	}
	return nil
}

func emptyJSONArray(value json.RawMessage) bool {
	var items []json.RawMessage
	return json.Unmarshal(value, &items) == nil && len(items) == 0
}

func isJSONObject(value json.RawMessage) bool {
	var object map[string]any
	return json.Unmarshal(value, &object) == nil && object != nil
}

func isJSONArray(value json.RawMessage) bool {
	var items []any
	return json.Unmarshal(value, &items) == nil && items != nil
}

func stringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

var uuidNil = [16]byte{}
