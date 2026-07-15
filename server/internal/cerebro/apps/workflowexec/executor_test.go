package workflowexec

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeTokens struct{ calls int }

func (f *fakeTokens) Key(context.Context) (string, error) { f.calls++; return "sk_personal", nil }

type fakeRegistry struct{ calls []RegistryCall }

func (f *fakeRegistry) Execute(_ context.Context, key string, call RegistryCall) (any, error) {
	if key != "sk_personal" {
		panic("registry received a non-personal key")
	}
	f.calls = append(f.calls, call)
	if call.Kind == "read" {
		return map[string]any{"count": float64(1), "sku": "A-1"}, nil
	}
	return map[string]any{"updated": float64(1)}, nil
}

type fakeViews struct{ calls int }

func (f *fakeViews) ShowAndWait(_ context.Context, _ string, viewID string, input any) (any, error) {
	f.calls++
	return map[string]any{"approved": true, "view_id": viewID, "input": input}, nil
}

func TestExecutorRunsLinearStepsWithFreshPersonalKeyAndLogs(t *testing.T) {
	definition := json.RawMessage(`{
		"schema_version":"1",
		"trigger":{"id":"trigger","type":"manual","config":{}},
		"steps":[
			{"id":"read","type":"registry.read","config":{"resource_id":"products"}},
			{"id":"filter","type":"filter","config":{"field":"read.count","operator":"gt","value":0}},
			{"id":"view","type":"view.show_and_wait","config":{"view_id":"approve"}},
			{"id":"write","type":"registry.write","config":{"resource_id":"products"}}
		]
	}`)
	tokens := &fakeTokens{}
	registry := &fakeRegistry{}
	views := &fakeViews{}
	result, err := New(tokens, registry, views).Run(context.Background(), definition, map[string]any{"source": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || len(result.Steps) != 4 {
		t.Fatalf("result=%+v", result)
	}
	if tokens.calls != 2 {
		t.Fatalf("registry steps must each request a fresh key; calls=%d", tokens.calls)
	}
	if len(registry.calls) != 2 || views.calls != 1 {
		t.Fatalf("registry=%d views=%d", len(registry.calls), views.calls)
	}
}

func TestExecutorStopsAtFalseFilterWithoutWriting(t *testing.T) {
	definition := json.RawMessage(`{"schema_version":"1","trigger":{"id":"trigger","type":"manual","config":{}},"steps":[{"id":"filter","type":"filter","config":{"field":"trigger.count","operator":"gt","value":10}},{"id":"write","type":"registry.write","config":{"resource_id":"products"}}]}`)
	registry := &fakeRegistry{}
	result, err := New(&fakeTokens{}, registry, &fakeViews{}).Run(context.Background(), definition, map[string]any{"count": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "filtered" || len(registry.calls) != 0 {
		t.Fatalf("result=%+v calls=%d", result, len(registry.calls))
	}
}
