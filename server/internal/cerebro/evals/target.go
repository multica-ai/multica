package evals

import (
	"encoding/json"
	"errors"
	"strings"
)

// target.go gives the in-app builder (FIR-3496, Fase 2) a typed reading of the
// `target` JSONB object: what an eval runs each task against. The column stays
// raw JSON so externally authored, file-based evals round-trip untouched; this
// is the shape the in-app runner loads.

// TargetKind enumerates what an eval runs each task against.
const (
	TargetPrompt   = "prompt"
	TargetAgent    = "agent"
	TargetSkill    = "skill"
	TargetWorkflow = "workflow"
)

// TargetSpec is the typed `target` object. Kind selects the adapter; Ref is the
// agent/skill/workflow identifier the adapter runs; System and Model are
// optional overrides passed to a model-backed target.
type TargetSpec struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	System string `json:"system"`
	Model  string `json:"model"`
}

// ParseTarget reads the target column into a TargetSpec. A missing target or an
// empty kind is an error: the runner must never fall back to a silent default,
// or an eval could "pass" against a target the author never chose.
func ParseTarget(target json.RawMessage) (TargetSpec, error) {
	var spec TargetSpec
	if len(target) == 0 {
		return spec, errors.New("eval has no target")
	}
	if err := json.Unmarshal(target, &spec); err != nil {
		return spec, err
	}
	spec.Kind = strings.TrimSpace(strings.ToLower(spec.Kind))
	spec.Ref = strings.TrimSpace(spec.Ref)
	spec.System = strings.TrimSpace(spec.System)
	spec.Model = strings.TrimSpace(spec.Model)
	if spec.Kind == "" {
		return spec, errors.New("target kind is required")
	}
	return spec, nil
}
