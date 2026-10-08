//go:build agentintegration

package agent

import (
	"os"
	"testing"
)

func requireRealAgentSmoke(t *testing.T) {
	t.Helper()
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real agent CLI and account access")
	}
	t.Log("REAL AGENT SMOKE TEST: this test may access an authenticated account and consume quota")
}
