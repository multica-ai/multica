package db

import (
	"strings"
	"testing"
)

// TestRequeueExpiredClaimLeases_FiltersOfflineRuntimes is a regression test
// ensuring that the RequeueExpiredClaimLeases SQL only requeues tasks whose
// runtime is still online. Tasks on offline/dead runtimes must stay
// dispatched so FailTasksForOfflineRuntimes can properly fail+retry them.
func TestRequeueExpiredClaimLeases_FiltersOfflineRuntimes(t *testing.T) {
	sql := requeueExpiredClaimLeases

	if !strings.Contains(sql, "INNER JOIN agent_runtime") {
		t.Fatal("RequeueExpiredClaimLeases must JOIN agent_runtime to check liveness")
	}
	if !strings.Contains(sql, "ar.status = 'online'") {
		t.Fatal("RequeueExpiredClaimLeases must filter by ar.status = 'online'")
	}
}

// TestRequeueExpiredClaimLeases_RequiresHeartbeatFreshness is a regression test
// for the scenario where a daemon crashes, the 60s lease expires, but the
// runtime is still 'online' (150s stale threshold hasn't fired yet). The
// global sweeper must NOT requeue the task because the runtime's last_seen_at
// is stale — requeuing would put the task back to 'queued' on a dead runtime,
// creating a 2-hour black hole.
func TestRequeueExpiredClaimLeases_RequiresHeartbeatFreshness(t *testing.T) {
	sql := requeueExpiredClaimLeases

	// Must check last_seen_at freshness, not just ar.status = 'online'
	if !strings.Contains(sql, "ar.last_seen_at") {
		t.Fatal("RequeueExpiredClaimLeases must check ar.last_seen_at freshness to avoid requeuing to dead-but-online runtimes")
	}
	// Must use the stale_threshold_secs parameter
	if !strings.Contains(sql, "make_interval") {
		t.Fatal("RequeueExpiredClaimLeases must use make_interval with stale threshold to compute freshness window")
	}
}

// TestRequeueExpiredClaimLeasesForRuntime_NoLivenessCheck verifies that the
// per-runtime preflight requeue does NOT join agent_runtime or check
// last_seen_at — the runtime is proving liveness by actively calling claim.
func TestRequeueExpiredClaimLeasesForRuntime_NoLivenessCheck(t *testing.T) {
	sql := requeueExpiredClaimLeasesForRuntime

	if strings.Contains(sql, "agent_runtime") {
		t.Fatal("RequeueExpiredClaimLeasesForRuntime must NOT join agent_runtime (runtime proves liveness by claiming)")
	}
	if !strings.Contains(sql, "runtime_id = $1") {
		t.Fatal("RequeueExpiredClaimLeasesForRuntime must filter by the specific runtime_id")
	}
}

// TestFailAgentTask_TokenlessCannotBypassTokenedRow is a regression test
// ensuring that the FailAgentTask SQL uses strict token matching:
// tokenless requests can only fail rows where claim_token IS NULL.
func TestFailAgentTask_TokenlessCannotBypassTokenedRow(t *testing.T) {
	sql := failAgentTask

	// The old vulnerable pattern: ($6::uuid IS NULL OR claim_token = $6)
	// allows tokenless requests to match ANY row.
	if strings.Contains(sql, "IS NULL OR claim_token =") {
		t.Fatal("FailAgentTask must not use 'IS NULL OR claim_token =' pattern (tokenless bypass)")
	}

	// The correct pattern requires both conditions:
	// (param IS NULL AND claim_token IS NULL) OR claim_token = param
	if !strings.Contains(sql, "IS NULL AND claim_token IS NULL") {
		t.Fatal("FailAgentTask must require claim_token IS NULL for tokenless requests")
	}
}
