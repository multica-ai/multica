package failrouter

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestRouteCoversEveryCanonicalFailureReason(t *testing.T) {
	for _, reason := range taskfailure.AllReasons() {
		route, ok := Lookup(reason.String())
		if !ok {
			t.Errorf("missing route for %q", reason)
		}
		if route.Action == "" {
			t.Errorf("empty action for %q", reason)
		}
	}
}

func TestNewFailureRoutes(t *testing.T) {
	tests := []struct {
		reason     taskfailure.Reason
		action     Action
		fresh      bool
		retryLimit int32
	}{
		{taskfailure.ReasonAgentContextOverflow, ActionRetry, true, 1},
		{taskfailure.ReasonAgentEmptyOrUnparseableOutput, ActionRetry, true, 1},
		{taskfailure.ReasonAgentProviderServerError, ActionRetry, false, 0},
		{taskfailure.ReasonAgentProviderNetwork, ActionRetry, false, 0},
		{taskfailure.ReasonAgentProcessFailure, ActionRetry, false, 1},
		{taskfailure.ReasonAgentRuntimeMissingExecutable, ActionAlert, false, 0},
		{taskfailure.ReasonAgentRuntimeVersionUnsupported, ActionAlert, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			route, ok := Lookup(tt.reason.String())
			if !ok {
				t.Fatal("route missing")
			}
			if route.Action != tt.action || route.FreshSession != tt.fresh || route.RetryLimit != tt.retryLimit {
				t.Fatalf("route = %+v, want action=%q fresh=%t retryLimit=%d", route, tt.action, tt.fresh, tt.retryLimit)
			}
		})
	}
}

func TestExistingRoutesRemainStable(t *testing.T) {
	tests := map[taskfailure.Reason]Action{
		taskfailure.ReasonRuntimeOffline:                   ActionRetry,
		taskfailure.ReasonRuntimeRecovery:                  ActionRetry,
		taskfailure.ReasonTimeout:                          ActionRetry,
		taskfailure.ReasonAgentProviderAuthOrAccess:        ActionPause,
		taskfailure.ReasonAgentProviderQuotaLimit:          ActionPause,
		taskfailure.ReasonAgentProviderCapacityOrRateLimit: ActionPause,
		taskfailure.ReasonIterationLimit:                   ActionSurface,
		taskfailure.ReasonAgentBlocked:                     ActionSurface,
		taskfailure.ReasonAPIInvalidRequest:                ActionSurface,
		taskfailure.ReasonAgentTimeout:                     ActionSurface,
		taskfailure.ReasonAgentMissingConfig:               ActionSurface,
		taskfailure.ReasonAgentModelNotFoundOrUnavailable:  ActionSurface,
		taskfailure.ReasonQueuedExpired:                    ActionSurface,
		taskfailure.ReasonAgentUnknown:                     ActionSurface,
	}
	for reason, want := range tests {
		route, ok := Lookup(reason.String())
		if !ok || route.Action != want {
			t.Errorf("Lookup(%q) = %+v, %t; want action %q", reason, route, ok, want)
		}
	}
}

func TestActionableUserMessage(t *testing.T) {
	for _, reason := range []taskfailure.Reason{
		taskfailure.ReasonAgentRuntimeMissingExecutable,
		taskfailure.ReasonAgentRuntimeVersionUnsupported,
	} {
		if got := UserMessage(reason.String()); got == "" {
			t.Errorf("UserMessage(%q) is empty", reason)
		} else if strings.Contains(got, "has been notified") {
			t.Errorf("UserMessage(%q) claims an alert succeeded before side effects finish: %q", reason, got)
		}
	}
	if got := UserMessage(taskfailure.ReasonAgentUnknown.String()); got != "" {
		t.Errorf("UserMessage(unknown) = %q, want empty", got)
	}
}
