package handler

import (
	"testing"

	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

func TestRuntimeHookEventTypesCoverEveryRuntimeLifecycleEvent(t *testing.T) {
	want := []cerebroworkflows.HookEventType{
		cerebroworkflows.HookBeforeSessionStart,
		cerebroworkflows.HookAfterSessionStart,
		cerebroworkflows.HookBeforeSessionEnd,
		cerebroworkflows.HookAfterSessionEnd,
		cerebroworkflows.HookBeforePromptAssemble,
		cerebroworkflows.HookBeforeToolCall,
		cerebroworkflows.HookAfterToolCall,
		cerebroworkflows.HookOnToolFailure,
		cerebroworkflows.HookBeforeAgentStop,
		cerebroworkflows.HookBeforeSubagentStart,
		cerebroworkflows.HookAfterSubagentStop,
		cerebroworkflows.HookOnError,
	}
	for _, eventType := range want {
		if !runtimeHookEventTypes[eventType] {
			t.Errorf("runtime event channel rejects %q", eventType)
		}
	}
	if len(runtimeHookEventTypes) != len(want) {
		t.Fatalf("runtime event count = %d, want %d", len(runtimeHookEventTypes), len(want))
	}
}
