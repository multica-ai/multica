package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// ─────────────────────────────────────────────────────────────────────────────
// Security Test: Failover DoS Protection
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_FailoverDoSProtection_CompletedTaskOutputIgnored(t *testing.T) {
	// Simulated user output from a script testing rate-limiting or logging 429s:
	userCodeStdout := `HTTP 429 Too Many Requests
{
  "error": {
    "message": "Rate limit exceeded for model: opencode/nemotron-3-ultra-free",
    "type": "insufficient_quota",
    "param": null,
    "code": "rate_limit_exceeded"
  }
}`

	// 1. When the agent completes successfully (Status: "completed"), stdout MUST NOT trigger failover.
	completedResult := agent.Result{
		Status: "completed",
		Output: userCodeStdout,
		Error:  "",
	}

	isQuota, failMsg := isQuotaOrModelFailure(completedResult, nil)
	if isQuota {
		t.Fatalf("SECURITY VIOLATION: completed task stdout containing 429 triggered failover! failMsg: %q", failMsg)
	}

	// 2. When the agent session genuinely failed (Status: "failed"), provider quota stdout DOES trigger failover.
	failedResult := agent.Result{
		Status: "failed",
		Output: userCodeStdout,
		Error:  "",
	}

	isQuota, failMsg = isQuotaOrModelFailure(failedResult, nil)
	if !isQuota {
		t.Fatalf("expected genuine failed task with 429 output to trigger failover")
	}
	if failMsg == "" {
		t.Fatalf("expected failMsg to be populated for failed task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test: MCP Usage Detection with Agent Skills
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_MCPUsageDetectionWithSkills(t *testing.T) {
	// 1. Without skills, overlays, or apps, WillUseMCPTools is false
	if detectMCPUsage(nil, nil, 0, 0, 0) {
		t.Errorf("expected false when no MCP, apps, or skills configured")
	}

	// 2. With agent skills configured (skillsCount > 0), WillUseMCPTools is true
	if !detectMCPUsage(nil, nil, 0, 0, 1) {
		t.Errorf("expected true when agent has 1 skill configured")
	}

	// 3. With plugin hooks, WillUseMCPTools is true
	if !detectMCPUsage(nil, nil, 1, 0, 0) {
		t.Errorf("expected true when plugin hooks are present")
	}
}
