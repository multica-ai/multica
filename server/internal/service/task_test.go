package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestSelectRuntimeForAgent_PrefersOnline: two assigned runtimes (one online, one offline).
// No usage seeded. Expect the online one.
func TestSelectRuntimeForAgent_PrefersOnline(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtOnline := tc.createRuntime(t, "online")
	rtOffline := tc.createRuntime(t, "offline")
	tc.assign(t, agentID, rtOnline)
	tc.assign(t, agentID, rtOffline)

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got != rtOnline {
		t.Errorf("expected online runtime, got different UUID")
	}
}

// TestSelectRuntimeForAgent_LeastTokensWins: two online runtimes.
// Seed heavy usage on one (1M tokens) and light (100 tokens) on the other, both 1 day ago.
// Expect the light-usage one.
func TestSelectRuntimeForAgent_LeastTokensWins(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtHeavy := tc.createRuntime(t, "online")
	rtLight := tc.createRuntime(t, "online")
	tc.assign(t, agentID, rtHeavy)
	tc.assign(t, agentID, rtLight)

	oneDayAgo := time.Now().UTC().Add(-24 * time.Hour)
	tc.seedUsage(t, rtHeavy, 1_000_000, oneDayAgo)
	tc.seedUsage(t, rtLight, 100, oneDayAgo)

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got != rtLight {
		t.Errorf("expected light-usage runtime, got different UUID")
	}
}

// TestSelectRuntimeForAgent_UsageOutsideWindowIgnored: two online runtimes.
// Heavy usage 10 days ago on rtA, light recent (1 hour ago) on rtB.
// Expect rtA because its old usage is outside the 7-day window (7-day total = 0).
func TestSelectRuntimeForAgent_UsageOutsideWindowIgnored(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtA := tc.createRuntime(t, "online")
	rtB := tc.createRuntime(t, "online")
	tc.assign(t, agentID, rtA)
	tc.assign(t, agentID, rtB)

	tenDaysAgo := time.Now().UTC().Add(-10 * 24 * time.Hour)
	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)

	tc.seedUsage(t, rtA, 5_000_000, tenDaysAgo) // outside 7-day window: counts as 0
	tc.seedUsage(t, rtB, 100, oneHourAgo)        // inside window: 100 tokens

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	// rtA has 0 7-day tokens (old usage outside window), rtB has 100.
	// Expect rtA (lower 7-day total).
	if got != rtA {
		t.Errorf("expected rtA (usage outside window = 0 effective tokens), got different UUID")
	}
}

// TestSelectRuntimeForAgent_NeverUsedBeatsEverUsedOnTie: two online runtimes,
// both with zero 7-day tokens. rtUsed has a task row within the 7-day window
// (2 days ago, no usage row so tokens = 0), rtFresh has nothing.
// Expect rtFresh because NULL last_used_at sorts before any timestamp (NULLS FIRST).
func TestSelectRuntimeForAgent_NeverUsedBeatsEverUsedOnTie(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtUsed := tc.createRuntime(t, "online")
	rtFresh := tc.createRuntime(t, "online")
	tc.assign(t, agentID, rtUsed)
	tc.assign(t, agentID, rtFresh)

	// Seed a task within the 7-day window (2 days ago) but with no usage tokens.
	// The runtime_load CTE will pick it up and assign tokens_7d=0 but a non-NULL
	// last_used_at. rtFresh gets no CTE entry → last_used_at=NULL → sorts first.
	twoDaysAgo := time.Now().UTC().Add(-2 * 24 * time.Hour)
	tc.seedTaskRow(t, rtUsed, twoDaysAgo)

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	// Both have 0 7-day tokens. rtFresh has NULL last_used_at (never used);
	// rtUsed has a non-NULL last_used_at. NULLS FIRST → rtFresh wins.
	if got != rtFresh {
		t.Errorf("expected rtFresh (NULL last_used_at beats non-NULL), got different UUID")
	}
}

// TestSelectRuntimeForAgent_BurstEnqueueDistributesByLRU: three online runtimes,
// all zero-usage. Call SelectRuntimeForAgent three times, each time seeding a task
// on the chosen runtime to simulate the enqueue side effect. All three distinct
// runtimes should be chosen.
func TestSelectRuntimeForAgent_BurstEnqueueDistributesByLRU(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rt1 := tc.createRuntime(t, "online")
	rt2 := tc.createRuntime(t, "online")
	rt3 := tc.createRuntime(t, "online")
	tc.assign(t, agentID, rt1)
	tc.assign(t, agentID, rt2)
	tc.assign(t, agentID, rt3)

	chosen := make(map[[16]byte]bool)
	for i := 0; i < 3; i++ {
		got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
		if err != nil {
			t.Fatalf("SelectRuntimeForAgent (call %d): %v", i+1, err)
		}
		// Simulate the enqueue side effect by seeding a task on the chosen runtime.
		tc.seedTaskRow(t, got, time.Now().UTC())
		chosen[got.Bytes] = true
	}

	if len(chosen) != 3 {
		t.Errorf("expected 3 distinct runtimes chosen, got %d", len(chosen))
	}
}

// TestSelectRuntimeForAgent_OnlineHeavyBeatsOfflineIdle: one online runtime with
// 5M tokens in the last hour, one offline runtime with zero usage.
// Expect the online-heavy one.
func TestSelectRuntimeForAgent_OnlineHeavyBeatsOfflineIdle(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtOnlineHeavy := tc.createRuntime(t, "online")
	rtOfflineIdle := tc.createRuntime(t, "offline")
	tc.assign(t, agentID, rtOnlineHeavy)
	tc.assign(t, agentID, rtOfflineIdle)

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	tc.seedUsage(t, rtOnlineHeavy, 5_000_000, oneHourAgo)

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got != rtOnlineHeavy {
		t.Errorf("expected online-heavy runtime (online preferred over offline), got different UUID")
	}
}

// TestSelectRuntimeForAgent_AllOfflineStillReturnsOne: two offline runtimes.
// Seed rt1 with 1000 tokens and rt2 with 10 tokens (both 1 hour ago). Expect rt2.
func TestSelectRuntimeForAgent_AllOfflineStillReturnsOne(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rt1 := tc.createRuntime(t, "offline")
	rt2 := tc.createRuntime(t, "offline")
	tc.assign(t, agentID, rt1)
	tc.assign(t, agentID, rt2)

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	tc.seedUsage(t, rt1, 1000, oneHourAgo)
	tc.seedUsage(t, rt2, 10, oneHourAgo)

	got, err := svc.Queries.SelectRuntimeForAgent(tc.ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got != rt2 {
		t.Errorf("expected rt2 (lower token usage), got different UUID")
	}
}

// TestSelectRuntimeForAgent_UnionsAssignmentsAndGroups: agent has one directly-assigned
// runtime and two runtimes via a group. All three should be returned across three calls
// as the LRU tiebreak advances.
func TestSelectRuntimeForAgent_UnionsAssignmentsAndGroups(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	ctx := context.Background()
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtDirect := tc.createRuntime(t, "online")
	rtGroupA := tc.createRuntime(t, "online")
	rtGroupB := tc.createRuntime(t, "online")
	tc.assign(t, agentID, rtDirect)
	groupID := tc.createGroup(t, []pgtype.UUID{rtGroupA, rtGroupB})
	tc.linkAgentToGroup(t, agentID, groupID)

	// Burst-enqueue 3 times; each call should pick a distinct runtime because
	// all three candidates start at zero usage and the LRU tiebreak advances
	// as we seed task rows.
	seen := map[[16]byte]bool{}
	for i := 0; i < 3; i++ {
		got, err := svc.Queries.SelectRuntimeForAgent(ctx, agentID)
		if err != nil {
			t.Fatalf("SelectRuntimeForAgent: %v", err)
		}
		tc.seedTaskRow(t, got, time.Now())
		seen[got.Bytes] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 distinct runtimes across union, got %d", len(seen))
	}
}

// TestSelectRuntimeForAgent_DedupesUnion: same runtime assigned directly AND via group.
// The query must not return it twice or fail due to duplicates in the candidate set.
func TestSelectRuntimeForAgent_DedupesUnion(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	ctx := context.Background()
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtBoth := tc.createRuntime(t, "online")
	// Assign directly AND via group.
	tc.assign(t, agentID, rtBoth)
	groupID := tc.createGroup(t, []pgtype.UUID{rtBoth})
	tc.linkAgentToGroup(t, agentID, groupID)

	got, err := svc.Queries.SelectRuntimeForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got.Bytes != rtBoth.Bytes {
		t.Fatalf("expected rtBoth, got different runtime")
	}
	// Also guard against the query returning nothing due to duplicate candidates
	// confusing the GROUP BY. If this test reaches here without error, UNION
	// dedup is working.
}

// TestSelectRuntimeForAgent_OverrideBeatsLeastLoaded: group has two runtimes.
// rtHeavy has heavy usage but is overridden — it should still win because the
// override takes priority over the load-based ranking.
func TestSelectRuntimeForAgent_OverrideBeatsLeastLoaded(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	ctx := context.Background()
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtHeavy := tc.createRuntime(t, "online")
	rtLight := tc.createRuntime(t, "online")
	groupID := tc.createGroup(t, []pgtype.UUID{rtHeavy, rtLight})
	tc.linkAgentToGroup(t, agentID, groupID)

	// rtHeavy has 10M tokens in the last hour; rtLight has 100 tokens.
	tc.seedUsage(t, rtHeavy, 10_000_000, time.Now().Add(-1*time.Hour))
	tc.seedUsage(t, rtLight, 100, time.Now().Add(-1*time.Hour))

	// Override the group to rtHeavy — it should win because online + overridden
	// beats online + not-overridden regardless of load.
	tc.setOverride(t, groupID, rtHeavy, time.Now().Add(1*time.Hour))

	got, err := svc.Queries.SelectRuntimeForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got.Bytes != rtHeavy.Bytes {
		t.Fatalf("expected override runtime (rtHeavy) to win, got rtLight")
	}
}

// TestSelectRuntimeForAgent_OfflineOverrideFallsThrough: override points at an
// offline runtime; the online group member should win instead.
func TestSelectRuntimeForAgent_OfflineOverrideFallsThrough(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	ctx := context.Background()
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtOfflineOverride := tc.createRuntime(t, "offline")
	rtOnline := tc.createRuntime(t, "online")
	groupID := tc.createGroup(t, []pgtype.UUID{rtOfflineOverride, rtOnline})
	tc.linkAgentToGroup(t, agentID, groupID)

	// Override points at the offline runtime; online one must win.
	tc.setOverride(t, groupID, rtOfflineOverride, time.Now().Add(1*time.Hour))

	got, err := svc.Queries.SelectRuntimeForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got.Bytes != rtOnline.Bytes {
		t.Fatalf("expected online runtime when override is offline, got different")
	}
}

// TestSelectRuntimeForAgent_ExpiredOverrideIgnored: an expired override on rtExpired
// must not give it priority; rtNormal should win on lower token usage.
func TestSelectRuntimeForAgent_ExpiredOverrideIgnored(t *testing.T) {
	svc, tc := setupTaskServiceTest(t)
	ctx := context.Background()
	defer tc.cleanup()

	agentID := tc.createAgent(t)
	rtExpired := tc.createRuntime(t, "online")
	rtNormal := tc.createRuntime(t, "online")
	groupID := tc.createGroup(t, []pgtype.UUID{rtExpired, rtNormal})
	tc.linkAgentToGroup(t, agentID, groupID)

	// Insert an expired override via direct SQL (setOverride helper would
	// require ends_at in the future to satisfy CHECK constraint).
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO runtime_group_override (group_id, runtime_id, starts_at, ends_at) VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')`,
		groupID, rtExpired,
	)
	if err != nil {
		t.Fatalf("insert expired override: %v", err)
	}

	// Seed rtExpired with heavy usage so it would lose on tokens if the
	// override were honored. If the expired override is correctly ignored,
	// rtNormal wins on tokens.
	tc.seedUsage(t, rtExpired, 1_000_000, time.Now().Add(-1*time.Hour))

	got, err := svc.Queries.SelectRuntimeForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("SelectRuntimeForAgent: %v", err)
	}
	if got.Bytes != rtNormal.Bytes {
		t.Fatalf("expected rtNormal (expired override ignored), got different")
	}
}
