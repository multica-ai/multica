package tagaccess_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func accessRequestForGrant(grant tagaccess.SessionGrant) tagaccess.AccessRequest {
	return tagaccess.AccessRequest{
		TagSessionID: grant.TagSessionID, VIBESSessionID: grant.VIBESSessionID,
		VIBESUserID: grant.VIBESUserID, WorkspaceID: grant.WorkspaceID,
		AccountEpoch: grant.AccountEpoch, SessionWorkspaceGeneration: grant.SessionWorkspaceGeneration,
		MembershipGeneration: grant.MembershipGeneration, AuthorityVersion: grant.AuthorityVersion,
	}
}

func TestAccessGateDeniesUnknownProjection(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})

	decision := gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-1", VIBESSessionID: "vibes-session-1",
		VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
		AccountEpoch: 1, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
	})

	if decision.Allowed || decision.Reason != tagaccess.DenyUnknownProjection {
		t.Fatalf("Authorize() = %#v, want unknown projection denial", decision)
	}
}

func TestProjectionApplyIsIdempotentForDuplicateDelivery(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         3,
		MembershipGeneration: 5,
		AuthorityVersion:     1,
	}

	first, err := gate.ApplyProjection(context.Background(), event)
	if err != nil || first != tagaccess.ApplyApplied {
		t.Fatalf("first ApplyProjection() = %q, %v, want applied", first, err)
	}
	redelivery := event
	redelivery.EventID = "event-1-redelivery"
	second, err := gate.ApplyProjection(context.Background(), redelivery)
	if err != nil || second != tagaccess.ApplyDuplicate {
		t.Fatalf("second ApplyProjection() = %q, %v, want duplicate", second, err)
	}
}

func TestProjectionRejectsIdentifiersPostgresCannotRepresent(t *testing.T) {
	gate, _ := newMemoryGate(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1\x00vibes-workspace-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}

	if _, err := gate.ApplyProjection(context.Background(), event); !errors.Is(err, tagaccess.ErrInvalidProjection) {
		t.Fatalf("ApplyProjection() error = %v, want invalid projection", err)
	}
}

func TestProjectionApplyHandlesOutOfOrderStaleAndConflictingDelivery(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	base := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         3,
		MembershipGeneration: 5,
		AuthorityVersion:     1,
	}
	if result, err := gate.ApplyProjection(context.Background(), base); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("version 1 = %q, %v", result, err)
	}

	version3 := base
	version3.EventID = "event-3"
	version3.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("out-of-order version 3 = %q, %v, want gap", result, err)
	}

	version2 := base
	version2.EventID = "event-2"
	version2.Role = tagaccess.RoleAdmin
	version2.AuthorityVersion = 2
	if result, err := gate.ApplyProjection(context.Background(), version2); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("version 2 while version 3 missing = %q, %v, want gap", result, err)
	}
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("retried version 3 = %q, %v, want applied", result, err)
	}
	if result, err := gate.ApplyProjection(context.Background(), version2); err != nil || result != tagaccess.ApplyStale {
		t.Fatalf("stale version 2 = %q, %v, want stale", result, err)
	}

	conflict := version3
	conflict.EventID = "event-3-conflict"
	conflict.Role = tagaccess.RoleMember
	if result, err := gate.ApplyProjection(context.Background(), conflict); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("conflicting version 3 = %q, %v, want conflict", result, err)
	}
	version4 := conflict
	version4.EventID = "event-4"
	version4.AuthorityVersion = 4
	if result, err := gate.ApplyProjection(context.Background(), version4); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("version after conflict = %q, %v, want conflict until reconciliation", result, err)
	}
}

func TestProjectionRequiresNewGenerationAfterRemoval(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	active := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 5,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	removed := active
	removed.EventID = "event-2"
	removed.Status = tagaccess.StatusRemoved
	removed.AuthorityVersion = 2
	if result, err := gate.ApplyProjection(context.Background(), removed); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("remove = %q, %v", result, err)
	}
	rejoinWithoutNewGeneration := removed
	rejoinWithoutNewGeneration.EventID = "event-3"
	rejoinWithoutNewGeneration.Status = tagaccess.StatusActive
	rejoinWithoutNewGeneration.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), rejoinWithoutNewGeneration); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("rejoin without higher generation = %q, %v, want conflict", result, err)
	}
}

func TestProjectionConflictingVariantBehindGapRemainsBlocked(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	version1 := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), version1); err != nil {
		t.Fatal(err)
	}
	version3A := version1
	version3A.EventID = "event-3-a"
	version3A.Role = tagaccess.RoleAdmin
	version3A.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3A); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("version 3A = %q, %v", result, err)
	}
	version3B := version3A
	version3B.EventID = "event-3-b"
	version3B.Role = tagaccess.RoleMember
	if result, err := gate.ApplyProjection(context.Background(), version3B); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("version 3B = %q, %v, want conflict", result, err)
	}
	version2 := version1
	version2.EventID = "event-2"
	version2.AuthorityVersion = 2
	if result, err := gate.ApplyProjection(context.Background(), version2); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("version 2 after conflict = %q, %v, want conflict", result, err)
	}
}

func TestProjectionConflictAtAnyObservedGapVersionRemainsBlocked(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	version1 := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), version1); err != nil {
		t.Fatal(err)
	}
	version4 := version1
	version4.EventID = "event-4"
	version4.AuthorityVersion = 4
	if result, err := gate.ApplyProjection(context.Background(), version4); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("version 4 = %q, %v, want gap", result, err)
	}
	version3A := version1
	version3A.EventID = "event-3-a"
	version3A.Role = tagaccess.RoleAdmin
	version3A.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3A); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("version 3A = %q, %v, want gap", result, err)
	}
	version3B := version3A
	version3B.EventID = "event-3-b"
	version3B.Role = tagaccess.RoleMember
	if result, err := gate.ApplyProjection(context.Background(), version3B); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("version 3B = %q, %v, want conflict", result, err)
	}
	version2 := version1
	version2.EventID = "event-2"
	version2.AuthorityVersion = 2
	if result, err := gate.ApplyProjection(context.Background(), version2); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("version 2 after conflict = %q, %v, want conflict", result, err)
	}
}

func TestAccessGateAuthorizesOnlyAProjectionBoundSessionGrant(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: now})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleAdmin,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         3,
		MembershipGeneration: 5,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	grant := tagaccess.SessionGrant{
		TagSessionID:               "tag-session-1",
		VIBESSessionID:             "vibes-session-1",
		VIBESUserID:                event.VIBESUserID,
		WorkspaceID:                event.WorkspaceID,
		AccountEpoch:               event.AccountEpoch,
		SessionWorkspaceGeneration: 1,
		MembershipGeneration:       event.MembershipGeneration,
		AuthorityVersion:           event.AuthorityVersion,
		SessionExpiresAt:           now.Add(time.Hour),
		GrantExpiresAt:             now.Add(30 * time.Minute),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatalf("GrantSession() error = %v", err)
	}

	decision := gate.Authorize(context.Background(), accessRequestForGrant(grant))
	if !decision.Allowed || decision.Role != tagaccess.RoleAdmin || decision.AuthorityVersion != 1 {
		t.Fatalf("Authorize() = %#v, want current admin projection", decision)
	}
}

func TestAccessGateRejectsLegacyCredentialWithoutSessionGrant(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	decision := gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "legacy-30-day-jwt-subject", VIBESSessionID: "legacy-vibes-session",
		VIBESUserID: event.VIBESUserID, WorkspaceID: event.WorkspaceID,
		AccountEpoch: event.AccountEpoch, SessionWorkspaceGeneration: 1,
		MembershipGeneration: event.MembershipGeneration, AuthorityVersion: event.AuthorityVersion,
	})
	if decision.Allowed || decision.Reason != tagaccess.DenyMissingGrant {
		t.Fatalf("Authorize() = %#v, want missing grant denial", decision)
	}
}

func TestAccessGateFailsClosedOnVersionGapAndStoreFailure(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: now})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	grant := tagaccess.SessionGrant{
		TagSessionID:               "tag-session-1",
		VIBESSessionID:             "vibes-session-1",
		VIBESUserID:                event.VIBESUserID,
		WorkspaceID:                event.WorkspaceID,
		AccountEpoch:               1,
		SessionWorkspaceGeneration: 1,
		MembershipGeneration:       1,
		AuthorityVersion:           1,
		SessionExpiresAt:           now.Add(time.Hour),
		GrantExpiresAt:             now.Add(time.Hour),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	version3 := event
	version3.EventID = "event-3"
	version3.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("ApplyProjection(version 3) = %q, %v", result, err)
	}
	request := accessRequestForGrant(grant)
	if decision := gate.Authorize(context.Background(), request); decision.Allowed || decision.Reason != tagaccess.DenyProjectionGap {
		t.Fatalf("gap decision = %#v", decision)
	}

	store.SetFailure(errors.New("database unavailable"))
	if decision := gate.Authorize(context.Background(), request); decision.Allowed || decision.Reason != tagaccess.DenyStoreUnavailable {
		t.Fatalf("store failure decision = %#v", decision)
	}
}

func TestAccessGateRejectsStaleVersionAndMembershipGeneration(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: now})
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         2,
		MembershipGeneration: 7,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	grant := tagaccess.SessionGrant{
		TagSessionID:               "tag-session-1",
		VIBESSessionID:             "vibes-session-1",
		VIBESUserID:                event.VIBESUserID,
		WorkspaceID:                event.WorkspaceID,
		AccountEpoch:               event.AccountEpoch,
		SessionWorkspaceGeneration: 1,
		MembershipGeneration:       event.MembershipGeneration,
		AuthorityVersion:           event.AuthorityVersion,
		SessionExpiresAt:           now.Add(time.Hour),
		GrantExpiresAt:             now.Add(time.Hour),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	request := accessRequestForGrant(grant)

	roleChange := event
	roleChange.EventID = "event-2"
	roleChange.Role = tagaccess.RoleAdmin
	roleChange.AuthorityVersion = 2
	if _, err := gate.ApplyProjection(context.Background(), roleChange); err != nil {
		t.Fatal(err)
	}
	if decision := gate.Authorize(context.Background(), request); decision.Reason != tagaccess.DenyStaleVersion {
		t.Fatalf("role change decision = %#v, want stale version", decision)
	}

	rejoin := roleChange
	rejoin.EventID = "event-3"
	rejoin.MembershipGeneration = 8
	rejoin.AuthorityVersion = 3
	if _, err := gate.ApplyProjection(context.Background(), rejoin); err != nil {
		t.Fatal(err)
	}
	if decision := gate.Authorize(context.Background(), request); decision.Reason != tagaccess.DenyStaleGeneration {
		t.Fatalf("rejoin decision = %#v, want stale generation", decision)
	}
	newGenerationGrant := grant
	newGenerationGrant.MembershipGeneration = 8
	newGenerationGrant.AuthorityVersion = 3
	if err := gate.GrantSession(context.Background(), newGenerationGrant); !errors.Is(err, tagaccess.ErrGrantDenied) {
		t.Fatalf("reusing old Tag session across generation error = %v, want denied", err)
	}
}

func TestAccessGateBindsAndSupersedesSessionWorkspaceGeneration(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	gate, _ := newMemoryGate(tagaccess.NewMemoryStore(), fixedClock{now: now})
	for _, event := range []tagaccess.ProjectionEvent{
		{
			EventID: "workspace-a-1", VIBESUserID: "vibes-user-1", WorkspaceID: "workspace-a",
			Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 1,
			MembershipGeneration: 1, AuthorityVersion: 1,
		},
		{
			EventID: "workspace-b-1", VIBESUserID: "vibes-user-1", WorkspaceID: "workspace-b",
			Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 1,
			MembershipGeneration: 1, AuthorityVersion: 1,
		},
	} {
		if _, err := gate.ApplyProjection(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	tagSessionID := tagaccess.BrowserTagSessionID("vibes-user-1", "vibes-session-1")
	grant := tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: "vibes-session-1", VIBESUserID: "vibes-user-1",
		WorkspaceID: "workspace-a", AccountEpoch: 1, SessionWorkspaceGeneration: 1,
		MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(time.Hour),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	request := tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: grant.VIBESSessionID, VIBESUserID: grant.VIBESUserID,
		WorkspaceID: grant.WorkspaceID, AccountEpoch: 1, SessionWorkspaceGeneration: 1,
		MembershipGeneration: 1, AuthorityVersion: 1,
	}
	if decision := gate.Authorize(context.Background(), request); !decision.Allowed {
		t.Fatalf("generation 1 decision = %#v", decision)
	}

	newWorkspaceGrant := grant
	newWorkspaceGrant.WorkspaceID = "workspace-b"
	if err := gate.GrantSession(context.Background(), newWorkspaceGrant); !errors.Is(err, tagaccess.ErrGrantDenied) {
		t.Fatalf("same-generation Workspace switch error = %v, want denied", err)
	}
	newWorkspaceGrant.SessionWorkspaceGeneration = 2
	if err := gate.GrantSession(context.Background(), newWorkspaceGrant); err != nil {
		t.Fatal(err)
	}
	if decision := gate.Authorize(context.Background(), request); decision.Allowed {
		t.Fatalf("superseded Workspace grant remained active: %#v", decision)
	}
	request.WorkspaceID = "workspace-b"
	request.SessionWorkspaceGeneration = 2
	if decision := gate.Authorize(context.Background(), request); !decision.Allowed {
		t.Fatalf("generation 2 decision = %#v", decision)
	}
	request.SessionWorkspaceGeneration = 1
	if decision := gate.Authorize(context.Background(), request); decision.Allowed || decision.Reason != tagaccess.DenyStaleSessionWorkspaceGeneration {
		t.Fatalf("stale session Workspace generation decision = %#v", decision)
	}
	if err := gate.GrantSession(context.Background(), grant); !errors.Is(err, tagaccess.ErrGrantDenied) {
		t.Fatalf("stale handoff grant error = %v, want denied", err)
	}
}

func TestAccessGateNeverRevivesOldSessionAfterReinvite(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: now})
	active := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 5,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	oldGrant := tagaccess.SessionGrant{
		TagSessionID:               "tag-session-old",
		VIBESSessionID:             "vibes-session-1",
		VIBESUserID:                active.VIBESUserID,
		WorkspaceID:                active.WorkspaceID,
		AccountEpoch:               1,
		SessionWorkspaceGeneration: 1,
		MembershipGeneration:       5,
		AuthorityVersion:           1,
		SessionExpiresAt:           now.Add(time.Hour),
		GrantExpiresAt:             now.Add(time.Hour),
	}
	if err := gate.GrantSession(context.Background(), oldGrant); err != nil {
		t.Fatal(err)
	}
	removed := active
	removed.EventID = "event-2"
	removed.Status = tagaccess.StatusRemoved
	removed.AuthorityVersion = 2
	if _, err := gate.ApplyProjection(context.Background(), removed); err != nil {
		t.Fatal(err)
	}
	rejoined := removed
	rejoined.EventID = "event-3"
	rejoined.Status = tagaccess.StatusActive
	rejoined.MembershipGeneration = 6
	rejoined.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), rejoined); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("rejoin = %q, %v", result, err)
	}
	request := accessRequestForGrant(oldGrant)
	if decision := gate.Authorize(context.Background(), request); decision.Reason != tagaccess.DenyStaleGeneration {
		t.Fatalf("old session after rejoin = %#v", decision)
	}
	refreshedOldGrant := oldGrant
	refreshedOldGrant.MembershipGeneration = 6
	refreshedOldGrant.AuthorityVersion = 3
	if err := gate.GrantSession(context.Background(), refreshedOldGrant); !errors.Is(err, tagaccess.ErrGrantDenied) {
		t.Fatalf("old session refreshed across generation error = %v, want denied", err)
	}
	freshGrant := refreshedOldGrant
	freshGrant.TagSessionID = "tag-session-fresh"
	if err := gate.GrantSession(context.Background(), freshGrant); err != nil {
		t.Fatalf("fresh post-reinvite grant error = %v", err)
	}
}

func TestVerifiedSnapshotBootstrapsNewMemberAtCurrentWorkspaceVersion(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	verifier := tagaccess.NewFixtureDeliveryVerifier()
	gate := tagaccess.New(store, fixedClock{now: now}, verifier)
	event := tagaccess.ProjectionEvent{
		EventID:              "snapshot-event-5",
		VIBESUserID:          "vibes-user-new",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     5,
	}
	delivery := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 5,
		AuthorityAssertionID:     "snapshot-proof-5",
		Projections:              []tagaccess.ProjectionEvent{event},
	}
	verifier.Trust(delivery)

	if result, err := gate.ApplyAuthorityDelivery(context.Background(), delivery); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("ApplyProjection(snapshot v5) = %q, %v, want applied", result, err)
	}
	grant := tagaccess.SessionGrant{
		TagSessionID:               "tag-session-new",
		VIBESSessionID:             "vibes-session-new",
		VIBESUserID:                event.VIBESUserID,
		WorkspaceID:                event.WorkspaceID,
		AccountEpoch:               event.AccountEpoch,
		SessionWorkspaceGeneration: 1,
		MembershipGeneration:       event.MembershipGeneration,
		AuthorityVersion:           event.AuthorityVersion,
		SessionExpiresAt:           now.Add(time.Hour),
		GrantExpiresAt:             now.Add(30 * time.Minute),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatalf("GrantSession() error = %v", err)
	}
	decision := gate.Authorize(context.Background(), accessRequestForGrant(grant))
	if !decision.Allowed || decision.AuthorityVersion != 5 {
		t.Fatalf("Authorize() = %#v, want allowed snapshot at v5", decision)
	}
}

func TestOrdinaryIncrementCannotBootstrapOrSkipWorkspaceAuthorityVersion(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	version5 := tagaccess.ProjectionEvent{
		EventID:              "event-5",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     5,
	}

	if result, err := gate.ApplyProjection(context.Background(), version5); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("empty + incremental v5 = %q, %v, want gap", result, err)
	}
}

func TestIncrementalVersionFiveGapsAfterWorkspaceVersionThree(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, verifier := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	version3 := tagaccess.ProjectionEvent{
		EventID:              "snapshot-event-3",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     3,
	}
	bootstrap := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 3,
		AuthorityAssertionID:     "snapshot-proof-3",
		Projections:              []tagaccess.ProjectionEvent{version3},
	}
	verifier.Trust(bootstrap)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	version5 := version3
	version5.EventID = "event-5"
	version5.AuthorityVersion = 5
	if result, err := gate.ApplyProjection(context.Background(), version5); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("workspace v3 + incremental v5 = %q, %v, want gap", result, err)
	}
}

func TestVerifiedAuthorityDeliveryRepairsGapAndOnlyReconcileRepairsConflict(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, verifier := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	version1 := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleMember,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     1,
	}
	if _, err := gate.ApplyProjection(context.Background(), version1); err != nil {
		t.Fatal(err)
	}
	version3 := version1
	version3.EventID = "event-3"
	version3.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("incremental v3 = %q, %v, want gap", result, err)
	}

	snapshotProjection := version3
	snapshotProjection.EventID = "snapshot-event-4"
	snapshotProjection.AuthorityVersion = 4
	snapshot := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 4,
		AuthorityAssertionID:     "snapshot-proof-4",
		Projections:              []tagaccess.ProjectionEvent{snapshotProjection},
	}
	verifier.Trust(snapshot)
	if result, err := gate.ApplyAuthorityDelivery(context.Background(), snapshot); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("verified snapshot v4 = %q, %v, want applied", result, err)
	}

	version6A := snapshotProjection
	version6A.EventID = "event-6-a"
	version6A.Role = tagaccess.RoleAdmin
	version6A.AuthorityVersion = 6
	if result, err := gate.ApplyProjection(context.Background(), version6A); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("incremental v6A = %q, %v, want gap", result, err)
	}
	version6B := version6A
	version6B.EventID = "event-6-b"
	version6B.Role = tagaccess.RoleOwner
	if result, err := gate.ApplyProjection(context.Background(), version6B); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("incremental v6B = %q, %v, want conflict", result, err)
	}
	version5 := snapshotProjection
	version5.EventID = "event-5"
	version5.AuthorityVersion = 5
	if result, err := gate.ApplyProjection(context.Background(), version5); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("ordinary incremental after conflict = %q, %v, want conflict", result, err)
	}

	reconciledProjection := snapshotProjection
	reconciledProjection.EventID = "reconcile-event-7"
	reconciledProjection.AuthorityVersion = 7
	reconcile := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliveryReconcile,
		BaselineAuthorityVersion: 7,
		AuthorityAssertionID:     "reconcile-proof-7",
		Projections:              []tagaccess.ProjectionEvent{reconciledProjection},
	}
	verifier.Trust(reconcile)
	if result, err := gate.ApplyAuthorityDelivery(context.Background(), reconcile); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("verified reconcile v7 = %q, %v, want applied", result, err)
	}
}

func TestUnverifiedSnapshotCannotForgeBootstrap(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, _ := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	projection := tagaccess.ProjectionEvent{
		EventID:              "forged-snapshot-5",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     5,
	}
	delivery := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 5,
		AuthorityAssertionID:     "attacker-assertion",
		Projections:              []tagaccess.ProjectionEvent{projection},
	}

	if _, err := gate.ApplyAuthorityDelivery(context.Background(), delivery); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) {
		t.Fatalf("forged snapshot error = %v, want unverified delivery", err)
	}
}

func TestWorkspaceCursorAcceptsNextIncrementAfterUnrelatedMemberChange(t *testing.T) {
	store := tagaccess.NewMemoryStore()
	gate, verifier := newMemoryGate(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)})
	memberA := tagaccess.ProjectionEvent{
		EventID:              "snapshot-a-3",
		VIBESUserID:          "vibes-user-a",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         1,
		MembershipGeneration: 1,
		AuthorityVersion:     3,
	}
	bootstrap := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 3,
		AuthorityAssertionID:     "snapshot-proof-3",
		Projections:              []tagaccess.ProjectionEvent{memberA},
	}
	verifier.Trust(bootstrap)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	memberB := memberA
	memberB.EventID = "event-b-4"
	memberB.VIBESUserID = "vibes-user-b"
	memberB.Role = tagaccess.RoleMember
	memberB.AuthorityVersion = 4
	if result, err := gate.ApplyProjection(context.Background(), memberB); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("unrelated member v4 = %q, %v", result, err)
	}
	memberA.EventID = "event-a-5"
	memberA.Role = tagaccess.RoleAdmin
	memberA.AuthorityVersion = 5
	if result, err := gate.ApplyProjection(context.Background(), memberA); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("member A v5 after global v4 = %q, %v, want applied", result, err)
	}
}

func TestCompleteSnapshotCannotLeaveAnOmittedMemberAuthorized(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, verifier := newMemoryGate(store, fixedClock{now: now})
	memberA := tagaccess.ProjectionEvent{
		EventID: "snapshot-a-3", VIBESUserID: "vibes-user-a", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 1,
		MembershipGeneration: 1, AuthorityVersion: 3,
	}
	memberB := memberA
	memberB.EventID = "snapshot-b-3"
	memberB.VIBESUserID = "vibes-user-b"
	memberB.Role = tagaccess.RoleMember
	initial := tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 3,
		AuthorityAssertionID: "snapshot-proof-3", Projections: []tagaccess.ProjectionEvent{memberA, memberB},
	}
	verifier.Trust(initial)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	grant := tagaccess.SessionGrant{
		TagSessionID: "tag-session-b", VIBESSessionID: "vibes-session-b",
		VIBESUserID: memberB.VIBESUserID, WorkspaceID: memberB.WorkspaceID,
		AccountEpoch: 1, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 3,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	memberA.EventID = "snapshot-a-5"
	memberA.AuthorityVersion = 5
	replacement := tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 5,
		AuthorityAssertionID: "snapshot-proof-5", Projections: []tagaccess.ProjectionEvent{memberA},
	}
	verifier.Trust(replacement)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	decision := gate.Authorize(context.Background(), accessRequestForGrant(grant))
	if decision.Allowed || decision.Reason != tagaccess.DenyInactiveMembership {
		t.Fatalf("omitted member Authorize() = %#v, want inactive membership denial", decision)
	}
	sameGenerationRejoin := memberB
	sameGenerationRejoin.EventID = "event-b-6"
	sameGenerationRejoin.AuthorityVersion = 6
	if result, err := gate.ApplyProjection(context.Background(), sameGenerationRejoin); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("same-generation rejoin after omission = %q, %v, want conflict", result, err)
	}
}

func TestSnapshotOmissionRequiresHigherGenerationAndNeverRevivesOldSession(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := tagaccess.NewMemoryStore()
	gate, verifier := newMemoryGate(store, fixedClock{now: now})
	memberA := tagaccess.ProjectionEvent{
		EventID: "snapshot-a-3", VIBESUserID: "vibes-user-a", WorkspaceID: "vibes-workspace-1",
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 1,
		MembershipGeneration: 1, AuthorityVersion: 3,
	}
	memberB := memberA
	memberB.EventID = "snapshot-b-3"
	memberB.VIBESUserID = "vibes-user-b"
	memberB.Role = tagaccess.RoleMember
	initial := tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 3,
		AuthorityAssertionID: "snapshot-proof-3", Projections: []tagaccess.ProjectionEvent{memberA, memberB},
	}
	verifier.Trust(initial)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	oldGrant := tagaccess.SessionGrant{
		TagSessionID: "tag-session-old", VIBESSessionID: "vibes-session-old",
		VIBESUserID: memberB.VIBESUserID, WorkspaceID: memberB.WorkspaceID,
		AccountEpoch: 1, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 3,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}
	if err := gate.GrantSession(context.Background(), oldGrant); err != nil {
		t.Fatal(err)
	}
	memberA.EventID = "snapshot-a-5"
	memberA.AuthorityVersion = 5
	omission := tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 5,
		AuthorityAssertionID: "snapshot-proof-5", Projections: []tagaccess.ProjectionEvent{memberA},
	}
	verifier.Trust(omission)
	if _, err := gate.ApplyAuthorityDelivery(context.Background(), omission); err != nil {
		t.Fatal(err)
	}
	rejoined := memberB
	rejoined.EventID = "event-b-6"
	rejoined.Status = tagaccess.StatusActive
	rejoined.MembershipGeneration = 2
	rejoined.AuthorityVersion = 6
	if result, err := gate.ApplyProjection(context.Background(), rejoined); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("higher-generation rejoin = %q, %v", result, err)
	}
	request := accessRequestForGrant(oldGrant)
	if decision := gate.Authorize(context.Background(), request); decision.Reason != tagaccess.DenyStaleGeneration {
		t.Fatalf("old session after higher-generation rejoin = %#v", decision)
	}
	refreshedOldGrant := oldGrant
	refreshedOldGrant.MembershipGeneration = 2
	refreshedOldGrant.AuthorityVersion = 6
	if err := gate.GrantSession(context.Background(), refreshedOldGrant); !errors.Is(err, tagaccess.ErrGrantDenied) {
		t.Fatalf("old session refreshed across omission generation = %v, want denied", err)
	}
	freshGrant := refreshedOldGrant
	freshGrant.TagSessionID = "tag-session-fresh"
	if err := gate.GrantSession(context.Background(), freshGrant); err != nil {
		t.Fatalf("fresh post-omission grant error = %v", err)
	}
}

func newMemoryGate(store *tagaccess.MemoryStore, clock tagaccess.Clock) (*tagaccess.Gate, *tagaccess.FixtureDeliveryVerifier) {
	verifier := tagaccess.NewFixtureDeliveryVerifier()
	return tagaccess.New(store, clock, verifier), verifier
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }
