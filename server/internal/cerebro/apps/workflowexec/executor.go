// Package workflowexec runs the v1 linear mini-app workflow contract. Hatchet
// tasks call this executor; infrastructure concerns stay outside the format so
// the visual builder and CLI produce identical JSON.
package workflowexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

type TokenSource interface {
	Key(context.Context) (string, error)
}

type RegistryCall struct {
	Kind       string         `json:"kind"`
	ResourceID string         `json:"resource_id"`
	Input      any            `json:"input,omitempty"`
	Config     map[string]any `json:"config"`
}

type Registry interface {
	Execute(context.Context, string, RegistryCall) (any, error)
}
type ViewWaiter interface {
	ShowAndWait(context.Context, string, any) (any, error)
}

type StepLog struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	Input      any       `json:"input,omitempty"`
	Output     any       `json:"output,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type Result struct {
	Status  string         `json:"status"`
	Steps   []StepLog      `json:"steps"`
	Outputs map[string]any `json:"outputs"`
}

type Executor struct {
	tokens   TokenSource
	registry Registry
	views    ViewWaiter
	now      func() time.Time
}

func New(tokens TokenSource, registry Registry, views ViewWaiter) *Executor {
	return &Executor{tokens: tokens, registry: registry, views: views, now: time.Now}
}

type definition struct {
	SchemaVersion string `json:"schema_version"`
	Trigger       struct {
		ID, Type string
		Config   map[string]any `json:"config"`
	} `json:"trigger"`
	Steps []struct {
		ID, Type string
		Config   map[string]any `json:"config"`
	} `json:"steps"`
}

func (e *Executor) Run(ctx context.Context, raw json.RawMessage, trigger any) (Result, error) {
	if e == nil || e.tokens == nil || e.registry == nil || e.views == nil {
		return Result{}, errors.New("app workflow executor is not fully configured")
	}
	var def definition
	if err := json.Unmarshal(raw, &def); err != nil {
		return Result{}, fmt.Errorf("workflow definition: %w", err)
	}
	if def.SchemaVersion != "1" || def.Trigger.ID == "" {
		return Result{}, errors.New("workflow definition must use schema_version 1 and include a trigger")
	}
	result := Result{Status: "succeeded", Steps: make([]StepLog, 0, len(def.Steps)), Outputs: map[string]any{def.Trigger.ID: trigger}}
	last := trigger
	for _, step := range def.Steps {
		log := StepLog{ID: step.ID, Type: step.Type, Status: "running", Input: last, StartedAt: e.now()}
		var output any
		var err error
		switch step.Type {
		case "registry.read", "registry.write":
			key, keyErr := e.tokens.Key(ctx)
			if keyErr != nil {
				err = fmt.Errorf("fresh registry key: %w", keyErr)
				break
			}
			kind := strings.TrimPrefix(step.Type, "registry.")
			output, err = e.registry.Execute(ctx, key, RegistryCall{Kind: kind, ResourceID: stringConfig(step.Config, "resource_id"), Input: last, Config: step.Config})
		case "filter":
			passed, filterErr := evaluateFilter(step.Config, result.Outputs)
			if filterErr != nil {
				err = filterErr
			} else {
				output = map[string]any{"passed": passed}
			}
			if !passed && err == nil {
				log.Status, log.Output, log.FinishedAt = "filtered", output, e.now()
				result.Steps = append(result.Steps, log)
				result.Outputs[step.ID] = output
				result.Status = "filtered"
				return result, nil
			}
		case "view.show_and_wait":
			output, err = e.views.ShowAndWait(ctx, stringConfig(step.Config, "view_id"), last)
		default:
			err = fmt.Errorf("unsupported workflow step %q", step.Type)
		}
		log.FinishedAt = e.now()
		if err != nil {
			log.Status = "failed"
			result.Steps = append(result.Steps, log)
			result.Status = "failed"
			return result, fmt.Errorf("step %s: %w", step.ID, err)
		}
		log.Status, log.Output = "succeeded", output
		result.Steps = append(result.Steps, log)
		result.Outputs[step.ID] = output
		last = output
	}
	return result, nil
}

func stringConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func evaluateFilter(config map[string]any, outputs map[string]any) (bool, error) {
	field := stringConfig(config, "field")
	parts := strings.SplitN(field, ".", 2)
	if len(parts) != 2 {
		return false, errors.New("filter field must reference a prior node field")
	}
	row, ok := outputs[parts[0]].(map[string]any)
	if !ok {
		return false, fmt.Errorf("filter source %q is unavailable", parts[0])
	}
	actual := row[parts[1]]
	expected := config["value"]
	switch stringConfig(config, "operator") {
	case "eq":
		return reflect.DeepEqual(actual, expected), nil
	case "neq":
		return !reflect.DeepEqual(actual, expected), nil
	case "gt", "gte", "lt", "lte":
		a, aok := number(actual)
		b, bok := number(expected)
		if !aok || !bok {
			return false, errors.New("numeric filter requires numbers")
		}
		switch stringConfig(config, "operator") {
		case "gt":
			return a > b, nil
		case "gte":
			return a >= b, nil
		case "lt":
			return a < b, nil
		default:
			return a <= b, nil
		}
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected)), nil
	default:
		return false, errors.New("filter operator is not supported")
	}
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}
