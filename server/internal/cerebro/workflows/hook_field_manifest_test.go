package workflows

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The field manifest is the only field vocabulary both sides of the product
// share: the editor's suggestions and the server gates' contexts. These
// tests fail the moment either side drifts (FIR-4933).

func TestHookFieldManifestCoversEveryEvent(t *testing.T) {
	for eventType := range HookEventCatalog {
		if HookFieldManifestPaths(eventType) == nil {
			t.Errorf("field manifest is missing event %s", eventType)
		}
	}
	for _, event := range hookFieldManifest.Events {
		if !isSupportedHookEventType(HookEventType(event.Type)) {
			t.Errorf("field manifest references unknown event %s", event.Type)
		}
	}
}

func TestHookFieldCatalogIsGeneratedFromServerManifest(t *testing.T) {
	want := GenerateHookFieldTypeScript()
	path := filepath.Join("..", "..", "..", "..", "packages", "cerebro-workflows", "core", "hook-field-catalog.generated.ts")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s has drifted; run go generate ./internal/cerebro/workflows", path)
	}
}

// Every condition a managed policy ever evaluates — and every $event.* value
// it references — must resolve against this manifest, or the hook editor back
// at FIR-4933 cannot show the rule the server enforces.
func TestManagedPoliciesMatchFieldManifest(t *testing.T) {
	for _, definition := range managedHookPolicies("workspace-test") {
		for _, eventType := range definition.Policy.Events {
			allowed := HookFieldManifestPaths(eventType)
			if allowed == nil {
				t.Errorf("%s: event %s has no field manifest", definition.Key, eventType)
				continue
			}
			for _, condition := range definition.Policy.Conditions {
				if !allowed[condition.Field] {
					t.Errorf("%s (%s) matches on %q, which the field manifest does not declare for %s", definition.Policy.Name, definition.Key, condition.Field, eventType)
				}
				for _, reference := range conditionEventReferences(condition) {
					if !allowed[reference] {
						t.Errorf("%s (%s) references $event.%s, which the field manifest does not declare for %s", definition.Policy.Name, definition.Key, reference, eventType)
					}
				}
			}
		}
	}
}

func conditionEventReferences(condition Condition) []string {
	var refs []string
	collect := func(value any) {
		text, ok := value.(string)
		if !ok {
			return
		}
		if path, ok := strings.CutPrefix(text, "$event."); ok {
			refs = append(refs, path)
		}
	}
	collect(condition.Value)
	for _, value := range condition.Values {
		collect(value)
	}
	return refs
}

// Conditions may only ever reference fields the engine actually builds: the
// base context plus the manifest. A field added to the base map without a
// manifest entry fails here.
func TestEngineBaseContextStaysInsideManifest(t *testing.T) {
	event := HookEvent{
		Type: HookBeforeMessageSend, WorkspaceID: "ws", ProjectID: "p",
		WorkflowID: "wf", AgentID: "a", Model: "m", IssueID: "i",
		SessionID: "s", Actor: HookActor{Type: "agent", ID: "a"},
		Attempt: 2, NoProgress: 1, HookDepth: 1,
	}
	allowed := HookFieldManifestPaths(HookBeforeMessageSend)
	for _, path := range flattenHookContextPaths(hookConditionContext(event), "") {
		if !allowed[path] {
			t.Errorf("engine base context exposes %q, which the field manifest does not declare", path)
		}
	}
}

func TestEngineActorReachesConditions(t *testing.T) {
	event := HookEvent{Type: HookBeforeMessageSend, Actor: HookActor{Type: "agent", ID: "agent-1"}}
	if !match(Condition{Field: "actor.type", Op: "eq", Value: "agent"}, hookConditionContext(event)) {
		t.Fatal("actor.type should match for an agent-actor event")
	}
	if match(Condition{Field: "actor.type", Op: "eq", Value: "member"}, hookConditionContext(event)) {
		t.Fatal("actor.type should not match member for an agent-actor event")
	}
}

// flattenHookContextPaths flattens nested string maps to dotted paths of
// scalar leaves — the same paths matchConditions can resolve.
func flattenHookContextPaths(value any, prefix string) []string {
	switch typed := value.(type) {
	case map[string]any:
		var out []string
		for key, nested := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			out = append(out, flattenHookContextPaths(nested, path)...)
		}
		return out
	case int, int32, int64, float64, bool, string:
		return []string{prefix}
	default:
		return nil
	}
}

func ExampleHookFieldManifestPaths() {
	paths := HookFieldManifestPaths(HookBeforeMessageSend)
	fmt.Println(paths["message.agent_authored"], paths["message.channel_id"])
	// Output: true false
}
