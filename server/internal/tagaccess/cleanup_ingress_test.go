package tagaccess_test

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type fixtureCleanupPort struct {
	receipt  tagaccess.CleanupReceipt
	err      error
	commands []tagaccess.CleanupCommand
}

func (p *fixtureCleanupPort) Cleanup(_ context.Context, command tagaccess.CleanupCommand) (tagaccess.CleanupReceipt, error) {
	p.commands = append(p.commands, command)
	return p.receipt, p.err
}

func TestAuthorityIngressCompletesCleanupOnlyForExactReceipt(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureCleanupPort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(),
		fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := access.AttachCleanupPort(port); err != nil {
		t.Fatal(err)
	}

	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		DeliveryID:    "remove-delivery-1",
		CorrelationID: "remove-correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "remove-event-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusRemoved, AccountEpoch: 7,
				MembershipGeneration: 4, AuthorityVersion: 1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)

	first, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cleanup.Status != tagaccess.CleanupPending || first.Cleanup.ReceiptID != "" {
		t.Fatalf("cleanup stage = %#v, want pending without fabricated receipt", first.Cleanup)
	}
	if len(port.commands) != 1 {
		t.Fatalf("cleanup commands = %#v, want one", port.commands)
	}
	command := port.commands[0]
	if command.Source != tagaccess.CleanupWorkspaceProjection || command.DeliveryID != envelope.DeliveryID ||
		command.CorrelationID != envelope.CorrelationID || command.WorkspaceID != "workspace-1" ||
		command.AuthorityVersion != 1 || len(command.PayloadDigest) != 64 || len(command.TargetDigest) != 64 ||
		len(command.Targets) != 1 || command.Targets[0].VIBESUserID != "user-1" ||
		command.Targets[0].MembershipGeneration != 4 || command.Targets[0].Status != tagaccess.StatusRemoved {
		t.Fatalf("cleanup command = %#v", command)
	}

	port.receipt = tagaccess.CleanupReceipt{
		ReceiptID: "wrong-cleanup-receipt", Source: command.Source, DeliveryID: "wrong-delivery",
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, PayloadDigest: command.PayloadDigest,
		TargetDigest: command.TargetDigest, CompletedAt: time.Date(2026, 8, 20, 12, 0, 1, 0, time.UTC),
	}
	wrong, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Apply.Result != tagaccess.ApplyDuplicate || wrong.Cleanup.Status != tagaccess.CleanupPending {
		t.Fatalf("mismatched cleanup receipt = %#v", wrong)
	}

	port.receipt.DeliveryID = command.DeliveryID
	exact, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if exact.Cleanup.Status != tagaccess.CleanupCompleted || exact.Cleanup.ReceiptID != "wrong-cleanup-receipt" || exact.Cleanup.CompletedAt == nil {
		t.Fatalf("exact cleanup receipt = %#v", exact.Cleanup)
	}
}

func TestAuthorityIngressRequestsCleanupOnlyForDurablyAppliedRestrictions(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureCleanupPort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := access.AttachCleanupPort(port); err != nil {
		t.Fatal(err)
	}
	deliver := func(version, baseline uint64, status tagaccess.Status, eventID string) tagaccess.TwoStageReceipt {
		t.Helper()
		envelope := tagaccess.AuthorityEnvelope{
			SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
			DeliveryID:    eventID, CorrelationID: "correlation-" + eventID,
			Delivery: tagaccess.ProjectionDelivery{
				Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: baseline,
				Projections: []tagaccess.ProjectionEvent{{
					EventID: eventID, VIBESUserID: "user-1", WorkspaceID: "workspace-1",
					Role: tagaccess.RoleMember, Status: status, AccountEpoch: 7,
					MembershipGeneration: 4, AuthorityVersion: version,
				}},
			},
			Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
		}
		envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
		receipt, err := access.Ingress.Deliver(context.Background(), envelope)
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}

	active := deliver(1, 0, tagaccess.StatusActive, "active-1")
	if active.Cleanup.Status != tagaccess.CleanupNotRequired || len(port.commands) != 0 {
		t.Fatalf("active cleanup = %#v, commands = %#v", active.Cleanup, port.commands)
	}
	gap := deliver(3, 2, tagaccess.StatusDisabled, "disabled-gap-3")
	if gap.Apply.Result != tagaccess.ApplyGap || gap.Cleanup.Status != tagaccess.CleanupNotRequired || len(port.commands) != 0 {
		t.Fatalf("gap cleanup = %#v, commands = %#v", gap, port.commands)
	}
	disabled := deliver(2, 1, tagaccess.StatusDisabled, "disabled-2")
	if disabled.Apply.Result != tagaccess.ApplyGap || disabled.Cleanup.Status != tagaccess.CleanupNotRequired || len(port.commands) != 0 {
		t.Fatalf("out-of-order cleanup = %#v, commands = %#v", disabled, port.commands)
	}
	retried := deliver(3, 2, tagaccess.StatusDisabled, "disabled-gap-3")
	if retried.Apply.Result != tagaccess.ApplyApplied || retried.Cleanup.Status != tagaccess.CleanupPending || len(port.commands) != 1 {
		t.Fatalf("repaired cleanup = %#v, commands = %#v", retried, port.commands)
	}
}

func TestAuthorityIngressRequestsCleanupForMemberOmittedBySnapshot(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureCleanupPort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	seed := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, DeliveryID: "snapshot-seed-1", CorrelationID: "snapshot-seed-correlation-1",
		Delivery: tagaccess.ProjectionDelivery{Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 1, AuthorityAssertionID: "snapshot-seed-assertion-1", Projections: []tagaccess.ProjectionEvent{
			{EventID: "seed-user-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1", Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 1},
			{EventID: "seed-user-2", VIBESUserID: "user-2", WorkspaceID: "workspace-1", Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 1},
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	seed.Authentication.MAC = signAuthorityEnvelope(t, key, seed)
	if _, err := access.Ingress.Deliver(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	if err := access.AttachCleanupPort(port); err != nil {
		t.Fatal(err)
	}
	snapshot := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion, DeliveryID: "snapshot-remove-2", CorrelationID: "snapshot-remove-correlation-2",
		Delivery: tagaccess.ProjectionDelivery{Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 2, AuthorityAssertionID: "snapshot-remove-assertion-2", Projections: []tagaccess.ProjectionEvent{
			{EventID: "snapshot-user-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1", Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 7, MembershipGeneration: 1, AuthorityVersion: 2},
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	snapshot.Authentication.MAC = signAuthorityEnvelope(t, key, snapshot)
	receipt, err := access.Ingress.Deliver(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.Cleanup.Status != tagaccess.CleanupPending || len(port.commands) != 1 {
		t.Fatalf("snapshot omission cleanup = %#v, commands = %#v", receipt, port.commands)
	}
	if command := port.commands[0]; command.DeliveryID != snapshot.DeliveryID || command.AuthorityVersion != 2 || len(command.Targets) != 0 {
		t.Fatalf("snapshot omission cleanup command = %#v", command)
	}
}

func TestIdentityIngressRequestsCleanupForAccountBanButNotSessionLogout(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureCleanupPort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := access.AttachCleanupPort(port); err != nil {
		t.Fatal(err)
	}

	logout := deliverIdentity(t, access, key, identityLogoutDelivery(1, "logout-1"))
	if logout.Cleanup.Status != tagaccess.CleanupNotRequired || len(port.commands) != 0 {
		t.Fatalf("session logout cleanup = %#v, commands = %#v", logout.Cleanup, port.commands)
	}
	ban := tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionAccountBan, EventID: "ban-2", CorrelationID: "ban-correlation-2",
		IdempotencyKey: "ban-idempotency-2", VIBESUserID: "user-1", AccountEpoch: 8,
		IdentityRestrictionVersion: 2,
		CloseTarget:                tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"},
	}
	receipt := deliverIdentity(t, access, key, ban)
	if receipt.Cleanup.Status != tagaccess.CleanupPending || len(port.commands) != 1 {
		t.Fatalf("account ban cleanup = %#v, commands = %#v", receipt.Cleanup, port.commands)
	}
	command := port.commands[0]
	if command.Source != tagaccess.CleanupIdentityRestriction || command.DeliveryID != ban.EventID ||
		command.CorrelationID != ban.CorrelationID || command.VIBESUserID != ban.VIBESUserID ||
		command.AccountEpoch != ban.AccountEpoch || command.IdentityRestrictionVersion != ban.IdentityRestrictionVersion ||
		len(command.PayloadDigest) != 64 {
		t.Fatalf("account ban cleanup command = %#v", command)
	}
}
