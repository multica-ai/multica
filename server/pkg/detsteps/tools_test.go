package detsteps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

func TestToolsRunsStepSource(t *testing.T) {
	steps := []StepDef{{
		Name:        "greet",
		Description: "greets",
		Source: `package step
import "strings"
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	return map[string]any{"status": "ok", "summary": "hi " + strings.ToUpper(name)}
}`,
	}}

	tools := Tools(os.Args[0], steps)
	if len(tools) != 1 || tools[0].Name != "greet" {
		t.Fatalf("Tools() = %+v, want one tool named greet", tools)
	}

	env := dettools.ToolEnv{Timeout: 5 * time.Second}
	res := tools[0].Handler(context.Background(), json.RawMessage(`{"name":"world"}`), env)
	if res.Status != dettools.StatusOK || res.Summary != "hi WORLD" {
		t.Fatalf("handler result = %+v, want ok/hi WORLD", res)
	}
}

func TestToolsHandlerRejectsBadInput(t *testing.T) {
	tools := Tools(os.Args[0], []StepDef{{Name: "x", Source: "package step\nfunc Run(i map[string]any) map[string]any { return nil }"}})
	res := tools[0].Handler(context.Background(), json.RawMessage(`{not json`), dettools.ToolEnv{Timeout: time.Second})
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInvalidInput {
		t.Fatalf("result = %+v, want error/INVALID_INPUT for malformed args", res)
	}
}

func TestLoadStepsFile(t *testing.T) {
	if steps, err := LoadStepsFile(""); err != nil || steps != nil {
		t.Fatalf("empty path: got %v, %v; want nil, nil", steps, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "steps.json")
	if err := os.WriteFile(path, []byte(`[{"name":"a","description":"d","source":"package step"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	steps, err := LoadStepsFile(path)
	if err != nil {
		t.Fatalf("LoadStepsFile: %v", err)
	}
	if len(steps) != 1 || steps[0].Name != "a" || steps[0].Source != "package step" {
		t.Fatalf("steps = %+v, want one step a", steps)
	}
}

// A step name that collides with a built-in must not shadow it in the registry.
func TestRegistryAddRefusesBuiltinCollision(t *testing.T) {
	reg := dettools.NewRegistry(dettools.AllToolNames())
	tools := Tools("", []StepDef{{Name: "repo_facts", Source: "package step\nfunc Run(i map[string]any) map[string]any { return nil }"}})
	if reg.Add(tools[0]) {
		t.Fatal("Add returned true for a name colliding with built-in repo_facts; want false")
	}
}
