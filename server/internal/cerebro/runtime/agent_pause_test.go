package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestFormatAgentPauseWaitReason(t *testing.T) {
	at := time.Date(2026, 8, 4, 15, 0, 0, 0, time.UTC)
	got := FormatAgentPauseWaitReason("auth_error", at, false)
	want := "agent_paused|auth_error|2026-08-04T15:00:00Z"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !IsAgentPauseWaitReason(got) {
		t.Fatal("IsAgentPauseWaitReason should be true")
	}
	if IsRuntimePauseWaitReason(got) {
		t.Fatal("agent wait_reason must not match runtime prefix")
	}

	manual := FormatAgentPauseWaitReason("rate_limit", time.Time{}, true)
	if !strings.HasSuffix(manual, "|") {
		t.Fatalf("circuit-open wait_reason should end with empty unpause: %q", manual)
	}
	if !IsAgentPauseWaitReason(manual) {
		t.Fatal("expected agent pause prefix")
	}
}
