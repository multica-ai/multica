package apps

import (
	"context"
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/cerebro/apps/workflowexec"
)

type workflowTestTokens struct{}

func (workflowTestTokens) Key(context.Context) (string, error) { return "workflow-test-key", nil }

type workflowTestRegistry struct{}

func (workflowTestRegistry) Execute(_ context.Context, _ string, call workflowexec.RegistryCall) (any, error) {
	if call.Kind == "write" {
		return call.Input, nil
	}
	output := map[string]any{"count": float64(1), "resource_id": call.ResourceID}
	if input, ok := call.Input.(map[string]any); ok {
		for key, value := range input {
			output[key] = value
		}
	}
	return output, nil
}

type workflowTestViews struct{}

func (workflowTestViews) ShowAndWait(_ context.Context, viewID string, input any) (any, error) {
	return map[string]any{"view_id": viewID, "submitted": true, "value": input}, nil
}

func runWorkflowTest(ctx context.Context, definition json.RawMessage, trigger any) (workflowexec.Result, error) {
	return workflowexec.New(workflowTestTokens{}, workflowTestRegistry{}, workflowTestViews{}).Run(ctx, definition, trigger)
}
