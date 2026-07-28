package service

import (
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agentroute"
)

func TestAdaptiveRouteFailureTransientOnlyForRecoverableSignals(t *testing.T) {
	t.Parallel()

	for _, reason := range []agentroute.RejectionReason{
		agentroute.RejectOffline,
		agentroute.RejectCapacityUnknown,
		agentroute.RejectReserveProtected,
	} {
		if !adaptiveRouteFailureTransient([]agentroute.Rejection{{Reason: reason}}) {
			t.Errorf("%q should be transient", reason)
		}
	}
	for _, reason := range []agentroute.RejectionReason{
		agentroute.RejectInvalidCandidate,
		agentroute.RejectProtectedRole,
		agentroute.RejectMissingSkill,
		agentroute.RejectMissingTool,
		agentroute.RejectMissingAuthority,
		agentroute.RejectForecastUnknown,
		agentroute.RejectCapacityInvalid,
	} {
		if adaptiveRouteFailureTransient([]agentroute.Rejection{{Reason: reason}}) {
			t.Errorf("%q should fail as permanent configuration", reason)
		}
	}
}

func TestAdaptiveCandidateProvidersDeduplicatesWithoutChangingOrder(t *testing.T) {
	t.Parallel()

	got := adaptiveCandidateProviders([]agentroute.Candidate{
		{Provider: " claude "},
		{Provider: "codex"},
		{Provider: "claude"},
		{Provider: ""},
	})
	if want := []string{"claude", "codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
}
