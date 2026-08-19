package tagaccess_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestIdentityIngressSessionLogoutRevokesOnlyExactVIBESSession(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: now},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projectActiveMember(t, access, key, 7)
	grantSession(t, access.Gate, now, "tag-session-a", "vibes-session-a", 7)
	grantSession(t, access.Gate, now, "tag-session-b", "vibes-session-b", 7)

	delivery := tagaccess.IdentityRestrictionDelivery{
		Kind:                       tagaccess.IdentityRestrictionSessionLogout,
		EventID:                    "identity-event-1",
		CorrelationID:              "identity-correlation-1",
		IdempotencyKey:             "identity-idempotency-1",
		VIBESUserID:                "user-1",
		VIBESSessionID:             "vibes-session-a",
		AccountEpoch:               7,
		IdentityRestrictionVersion: 1,
		CloseTarget: tagaccess.ConnectionCloseTarget{
			Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "vibes-session-a",
		},
	}
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		Delivery:      delivery,
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{
			KeyID: "vibes-primary",
		},
	}
	envelope.Authentication.MAC = signIdentityAuthorityEnvelope(t, key, envelope)
	receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.Apply.VIBESUserID != "user-1" ||
		receipt.Apply.IdentityRestrictionVersion != 1 || receipt.ConnectionClose.Status != tagaccess.ConnectionClosePending {
		t.Fatalf("DeliverIdentity() receipt = %#v", receipt)
	}
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-a", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
	}); decision.Allowed || decision.Reason != tagaccess.DenyMissingGrant {
		t.Fatalf("logged-out session decision = %#v", decision)
	}
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-b", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
	}); !decision.Allowed {
		t.Fatalf("sibling session decision = %#v", decision)
	}
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: "tag-session-replayed", VIBESSessionID: "vibes-session-a",
		VIBESUserID: "user-1", WorkspaceID: "workspace-1", AccountEpoch: 7,
		MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != tagaccess.ErrGrantDenied {
		t.Fatalf("logged-out VIBES session GrantSession() error = %v, want grant denied", err)
	}
}

func TestIdentityIngressAccountBanRevokesAllUserSessionsAtHigherEpoch(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: now},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	projectActiveMember(t, access, key, 7)
	grantSession(t, access.Gate, now, "tag-session-a", "vibes-session-a", 7)
	grantSession(t, access.Gate, now, "tag-session-b", "vibes-session-b", 7)
	delivery := tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionAccountBan, EventID: "identity-ban-1",
		CorrelationID: "identity-ban-correlation-1", IdempotencyKey: "identity-ban-idempotency-1",
		VIBESUserID: "user-1", AccountEpoch: 8, IdentityRestrictionVersion: 1,
		CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"},
	}
	receipt := deliverIdentity(t, access, key, delivery)
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.Apply.AccountEpoch != 8 {
		t.Fatalf("account-ban receipt = %#v", receipt)
	}
	for _, tagSessionID := range []string{"tag-session-a", "tag-session-b"} {
		decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
			TagSessionID: tagSessionID, VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		})
		if decision.Allowed || decision.Reason != tagaccess.DenyMissingGrant {
			t.Fatalf("account-ban decision for %s = %#v", tagSessionID, decision)
		}
	}
	projectActiveMemberAt(t, access, key, 8, 2)
	for _, attempt := range []struct {
		epoch, authorityVersion uint64
	}{{epoch: 7, authorityVersion: 2}, {epoch: 8, authorityVersion: 2}} {
		if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
			TagSessionID: "tag-session-after-ban", VIBESSessionID: "vibes-session-after-ban",
			VIBESUserID: "user-1", WorkspaceID: "workspace-1", AccountEpoch: attempt.epoch,
			MembershipGeneration: 1, AuthorityVersion: attempt.authorityVersion,
			SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
		}); err != tagaccess.ErrGrantDenied {
			t.Fatalf("epoch %d GrantSession() error = %v, want grant denied", attempt.epoch, err)
		}
	}
}

func TestIdentityIngressVersionGapFailsClosedWithoutRevokingBeforeApply(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	port := &fixtureConnectionClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: now},
		map[string][]byte{"vibes-primary": key}, port,
	)
	if err != nil {
		t.Fatal(err)
	}
	projectActiveMember(t, access, key, 7)
	grantSession(t, access.Gate, now, "tag-session-a", "vibes-session-a", 7)
	receipt := deliverIdentity(t, access, key, tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "identity-event-2",
		CorrelationID: "identity-correlation-2", IdempotencyKey: "identity-idempotency-2",
		VIBESUserID: "user-1", VIBESSessionID: "vibes-session-a", AccountEpoch: 7,
		IdentityRestrictionVersion: 2,
		CloseTarget: tagaccess.ConnectionCloseTarget{
			Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "vibes-session-a",
		},
	})
	if receipt.Apply.Result != tagaccess.ApplyGap || receipt.ConnectionClose.Status != tagaccess.ConnectionCloseNotRequired {
		t.Fatalf("gap receipt = %#v", receipt)
	}
	if len(port.commands) != 0 {
		t.Fatalf("gap close commands = %#v", port.commands)
	}
	decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-a", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
	})
	if decision.Allowed || decision.Reason != tagaccess.DenyIdentityRestrictionGap {
		t.Fatalf("gap decision = %#v", decision)
	}
	missing := identityLogoutDelivery(1, "identity-event-1")
	missing.VIBESSessionID = "vibes-session-other"
	missing.CloseTarget.VIBESSessionID = "vibes-session-other"
	if got := deliverIdentity(t, access, key, missing); got.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("durably applied missing version = %#v", got)
	}
	decision = access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-a", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
	})
	if decision.Allowed || decision.Reason != tagaccess.DenyIdentityRestrictionGap {
		t.Fatalf("remaining observed gap decision = %#v", decision)
	}
}

func TestIdentityIngressOrderingAndDigestAreDeterministic(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	newAccess := func(t *testing.T) *tagaccess.AuthenticatedAccess {
		t.Helper()
		access, err := tagaccess.NewAuthenticatedAccess(
			tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
			map[string][]byte{"vibes-primary": key}, nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		return access
	}
	delivery := func(version uint64, eventID string) tagaccess.IdentityRestrictionDelivery {
		return tagaccess.IdentityRestrictionDelivery{
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: eventID,
			CorrelationID: "correlation-" + eventID, IdempotencyKey: "idempotency-" + eventID,
			VIBESUserID: "user-1", VIBESSessionID: "vibes-session-1", AccountEpoch: 7,
			IdentityRestrictionVersion: version,
			CloseTarget: tagaccess.ConnectionCloseTarget{
				Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "vibes-session-1",
			},
		}
	}

	t.Run("duplicate and stale", func(t *testing.T) {
		access := newAccess(t)
		version1 := delivery(1, "event-1")
		first := deliverIdentity(t, access, key, version1)
		duplicate := deliverIdentity(t, access, key, version1)
		if first.Apply.Result != tagaccess.ApplyApplied || duplicate.Apply.Result != tagaccess.ApplyDuplicate ||
			duplicate.Apply.PayloadDigest != first.Apply.PayloadDigest {
			t.Fatalf("first/duplicate = %#v / %#v", first, duplicate)
		}
		if got := deliverIdentity(t, access, key, delivery(2, "event-2")); got.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("version 2 = %#v", got)
		}
		if got := deliverIdentity(t, access, key, version1); got.Apply.Result != tagaccess.ApplyStale {
			t.Fatalf("stale version 1 = %#v", got)
		}
	})

	t.Run("ordered missing delivery repairs gap", func(t *testing.T) {
		access := newAccess(t)
		version2 := delivery(2, "event-2")
		if got := deliverIdentity(t, access, key, version2); got.Apply.Result != tagaccess.ApplyGap {
			t.Fatalf("initial version 2 = %#v", got)
		}
		if got := deliverIdentity(t, access, key, delivery(1, "event-1")); got.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("missing version 1 = %#v", got)
		}
		if got := deliverIdentity(t, access, key, version2); got.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("retried version 2 = %#v", got)
		}
	})

	t.Run("same version different digest is terminal conflict", func(t *testing.T) {
		access := newAccess(t)
		version1 := delivery(1, "event-1")
		if got := deliverIdentity(t, access, key, version1); got.Apply.Result != tagaccess.ApplyApplied {
			t.Fatalf("version 1 = %#v", got)
		}
		conflict := version1
		conflict.IdempotencyKey = "changed-idempotency"
		if got := deliverIdentity(t, access, key, conflict); got.Apply.Result != tagaccess.ApplyConflict {
			t.Fatalf("conflicting version 1 = %#v", got)
		}
		if got := deliverIdentity(t, access, key, delivery(2, "event-2")); got.Apply.Result != tagaccess.ApplyConflict {
			t.Fatalf("version 2 after conflict = %#v", got)
		}
	})
}

func TestIdentityIngressRejectsUnknownUnsignedTamperedAndForgedDeliveries(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	store := tagaccess.NewMemoryStore()
	port := &fixtureConnectionClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(store, fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, port)
	if err != nil {
		t.Fatal(err)
	}
	delivery := identityLogoutDelivery(1, "identity-event-1")
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, Delivery: delivery,
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) || receipt != (tagaccess.IdentityTwoStageReceipt{}) {
		t.Fatalf("unsigned Deliver() = %#v, %v", receipt, err)
	}
	envelope.Authentication.KeyID = "unknown-key"
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) || receipt != (tagaccess.IdentityTwoStageReceipt{}) {
		t.Fatalf("unknown-key Deliver() = %#v, %v", receipt, err)
	}
	envelope.Authentication.KeyID = "vibes-primary"
	envelope.Authentication.MAC = signIdentityAuthorityEnvelope(t, key, envelope)
	tampered := envelope
	tampered.Delivery.AccountEpoch++
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), tampered); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) || receipt != (tagaccess.IdentityTwoStageReceipt{}) {
		t.Fatalf("tampered Deliver() = %#v, %v", receipt, err)
	}
	forged := envelope
	forged.Delivery.CloseTarget.VIBESSessionID = "sibling-session"
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), forged); !errors.Is(err, tagaccess.ErrInvalidProjection) || receipt != (tagaccess.IdentityTwoStageReceipt{}) {
		t.Fatalf("forged target Deliver() = %#v, %v", receipt, err)
	}
	if len(port.commands) != 0 {
		t.Fatalf("unauthenticated close commands = %#v", port.commands)
	}
}

func TestIdentityIngressBindsCloseReceiptToExactDeliveryAndTarget(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureConnectionClosePort{receipt: tagaccess.ConnectionCloseReceipt{
		ReceiptID: "wrong-receipt", DeliveryID: "wrong-delivery", TargetDigest: "wrong-digest",
		CompletedAt: time.Date(2026, 8, 20, 12, 0, 1, 0, time.UTC),
	}}
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, port)
	if err != nil {
		t.Fatal(err)
	}
	delivery := identityLogoutDelivery(1, "identity-event-1")
	first := deliverIdentity(t, access, key, delivery)
	if first.ConnectionClose.Status != tagaccess.ConnectionClosePending || len(port.commands) != 1 {
		t.Fatalf("mismatched receipt = %#v, commands = %#v", first, port.commands)
	}
	command := port.commands[0]
	if command.Source != tagaccess.ConnectionCloseIdentityRestriction || command.DeliveryID != delivery.EventID ||
		command.IdentityRestrictionVersion != delivery.IdentityRestrictionVersion || len(command.Targets) != 1 || command.Targets[0] != delivery.CloseTarget {
		t.Fatalf("identity close command = %#v", command)
	}
	port.receipt = tagaccess.ConnectionCloseReceipt{
		ReceiptID: "wrong-version-receipt", Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion + 1,
		TargetDigest: command.TargetDigest, CompletedAt: time.Date(2026, 8, 20, 12, 0, 2, 0, time.UTC),
	}
	if retry := deliverIdentity(t, access, key, delivery); retry.ConnectionClose.Status != tagaccess.ConnectionClosePending {
		t.Fatalf("wrong-version close receipt = %#v, want pending", retry.ConnectionClose)
	}
	port.receipt = tagaccess.ConnectionCloseReceipt{
		ReceiptID: "exact-receipt", Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		TargetDigest: command.TargetDigest,
		CompletedAt:  time.Date(2026, 8, 20, 12, 0, 3, 0, time.UTC),
	}
	retry := deliverIdentity(t, access, key, delivery)
	if retry.Apply.Result != tagaccess.ApplyDuplicate || retry.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted || retry.ConnectionClose.ReceiptID != "exact-receipt" {
		t.Fatalf("exact receipt retry = %#v", retry)
	}
}

func TestIdentityIngressStoreFailureReturnsNoReceiptAndDoesNotClose(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	store := tagaccess.NewMemoryStore()
	store.SetFailure(errors.New("identity projection store unavailable"))
	port := &fixtureConnectionClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(store, fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, port)
	if err != nil {
		t.Fatal(err)
	}
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, Delivery: identityLogoutDelivery(1, "identity-event-1"),
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signIdentityAuthorityEnvelope(t, key, envelope)
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope); err == nil || receipt != (tagaccess.IdentityTwoStageReceipt{}) {
		t.Fatalf("store failure Deliver() = %#v, %v", receipt, err)
	}
	if len(port.commands) != 0 {
		t.Fatalf("store failure close commands = %#v", port.commands)
	}
}

func TestCanonicalIdentityRestrictionEnvelopeHasStableCrossServiceBytes(t *testing.T) {
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		Delivery:      identityLogoutDelivery(1, "identity-event-1"),
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{
			KeyID: "vibes-primary",
		},
	}
	payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":1,"delivery":{"kind":"session_logged_out","eventId":"identity-event-1","correlationId":"correlation-identity-event-1","idempotencyKey":"idempotency-identity-event-1","vibesUserId":"user-1","vibesSessionId":"vibes-session-1","accountEpoch":7,"identityRestrictionVersion":1,"closeTarget":{"scope":"session","vibesUserId":"user-1","vibesSessionId":"vibes-session-1"}},"authentication":{"keyId":"vibes-primary"}}`
	if string(payload) != want {
		t.Fatalf("canonical identity envelope = %s, want %s", payload, want)
	}
	if strings.Contains(string(payload), "mac") {
		t.Fatalf("canonical signing payload includes MAC: %s", payload)
	}
}

func identityLogoutDelivery(version uint64, eventID string) tagaccess.IdentityRestrictionDelivery {
	return tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: eventID,
		CorrelationID: "correlation-" + eventID, IdempotencyKey: "idempotency-" + eventID,
		VIBESUserID: "user-1", VIBESSessionID: "vibes-session-1", AccountEpoch: 7,
		IdentityRestrictionVersion: version,
		CloseTarget: tagaccess.ConnectionCloseTarget{
			Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "vibes-session-1",
		},
	}
}

func projectActiveMember(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, accountEpoch uint64) {
	t.Helper()
	projectActiveMemberAt(t, access, key, accountEpoch, 1)
}

func projectActiveMemberAt(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, accountEpoch, authorityVersion uint64) {
	t.Helper()
	eventID := "workspace-event-" + string(rune('0'+authorityVersion))
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		DeliveryID:    eventID,
		CorrelationID: "workspace-correlation-" + eventID,
		Delivery: tagaccess.ProjectionDelivery{
			Kind:                     tagaccess.DeliveryIncremental,
			BaselineAuthorityVersion: authorityVersion - 1,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: eventID, VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: accountEpoch,
				MembershipGeneration: 1, AuthorityVersion: authorityVersion,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
	if receipt, err := access.Ingress.Deliver(context.Background(), envelope); err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("project active member = %#v, %v", receipt, err)
	}
}

func grantSession(t *testing.T, gate *tagaccess.Gate, now time.Time, tagSessionID, vibesSessionID string, accountEpoch uint64) {
	t.Helper()
	if err := gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: vibesSessionID, VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		AccountEpoch: accountEpoch, MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
}

func signIdentityAuthorityEnvelope(t *testing.T, key []byte, envelope tagaccess.IdentityRestrictionEnvelope) []byte {
	t.Helper()
	payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func deliverIdentity(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, delivery tagaccess.IdentityRestrictionDelivery) tagaccess.IdentityTwoStageReceipt {
	t.Helper()
	envelope := tagaccess.IdentityRestrictionEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, Delivery: delivery,
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signIdentityAuthorityEnvelope(t, key, envelope)
	receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
