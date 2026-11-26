package service

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestRunFailureNotice(t *testing.T) {
	taskID := util.MustParseUUID("0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")

	got := RunFailureNotice(taskID, "agent_error.provider_network")
	if !strings.HasPrefix(got, "Run `0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b` failed (`agent_error.provider_network`).") {
		t.Fatalf("notice = %q, want the run ID and failure reason up front", got)
	}
	if !strings.Contains(got, "run record") {
		t.Fatalf("notice = %q, want a pointer to the run record", got)
	}

	if got := RunFailureNotice(taskID, ""); !strings.Contains(got, "(`agent_error`)") {
		t.Fatalf("notice without a reason = %q, want the agent_error fallback", got)
	}
}
