package daemon

// CEREBRO-PATCH(daemon-task-account-usage-wiring-guard): FIR-3118 regression
// guard. Upstream sync #4530 silently dropped the maybeReportAccountUsage
// call from the task-completion path, which stopped all account usage
// telemetry (token totals, throttle signals and the exact 5h/7d window
// percentages) without any test failing.

import (
	"os"
	"strings"
	"testing"
)

func TestTaskCompletionReportsAccountUsage(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "d.maybeReportAccountUsage(") {
		t.Fatal("task completion must report account usage; an upstream sync likely removed the Cerebro wiring")
	}
}

func TestHeartbeatAckCachesAccountID(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "d.accountIdentities.setAccountID(") {
		t.Fatal("heartbeat ack must cache the cerebro account id; an upstream sync likely removed the Cerebro wiring")
	}
}

func TestHeartbeatAckPollsProviderUsage(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "d.maybeReportProviderUsageWindows(d.recoveryContext(),") {
		t.Fatal("heartbeat ack must poll provider usage windows on the daemon root context (FIR-3118); a tick-scoped context cancels the provider request")
	}
}

func TestHeartbeatCarriesAccountIdentity(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	if !strings.Contains(string(src), "SendHeartbeatOpts{Account: d.heartbeatAccountFor(") {
		t.Fatal("HTTP heartbeat must piggyback the runtime's login identity; an upstream sync likely removed the Cerebro wiring")
	}
	if !strings.Contains(string(src), "d.refreshHeartbeatAccount(") {
		t.Fatal("heartbeat/register must refresh the identity-probe cache; an upstream sync likely removed the Cerebro wiring")
	}
}
