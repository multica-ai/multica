package tagaccess_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type fixtureConnectionClosePort struct {
	receipt  tagaccess.ConnectionCloseReceipt
	err      error
	commands []tagaccess.ConnectionCloseCommand
}

type pointerClock struct{}

func (*pointerClock) Now() time.Time { return time.Time{} }

func (p *fixtureConnectionClosePort) CloseConnections(_ context.Context, command tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, error) {
	p.commands = append(p.commands, command)
	return p.receipt, p.err
}

func TestNewAuthenticatedAccessRejectsNilDependencies(t *testing.T) {
	keys := map[string][]byte{"vibes-primary": []byte("vibes-authority-test-key-32-bytes-minimum")}
	var nilStore *tagaccess.MemoryStore
	var nilClock *pointerClock
	var nilClosePort *fixtureConnectionClosePort
	tests := []struct {
		name      string
		construct func() error
	}{
		{name: "typed nil store", construct: func() error {
			_, err := tagaccess.NewAuthenticatedAccess(nilStore, tagaccess.SystemClock{}, keys, nil)
			return err
		}},
		{name: "typed nil clock", construct: func() error {
			_, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), nilClock, keys, nil)
			return err
		}},
		{name: "nil Postgres connection", construct: func() error {
			_, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewPostgresStore(nil), tagaccess.SystemClock{}, keys, nil)
			return err
		}},
		{name: "typed nil connection close port", construct: func() error {
			_, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, keys, nilClosePort)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.construct(); err == nil {
				t.Fatal("NewAuthenticatedAccess() error = nil, want invalid dependency")
			}
		})
	}
}

func TestAuthorityIngressRejectsUnsignedAndTamperedEnvelopes(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1,
		DeliveryID:    "vibes-outbox-1",
		CorrelationID: "correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind:                     tagaccess.DeliveryIncremental,
			BaselineAuthorityVersion: 0,
			Projections: []tagaccess.ProjectionEvent{{
				EventID:              "vibes-outbox-1",
				VIBESUserID:          "vibes-user-1",
				WorkspaceID:          "vibes-workspace-1",
				Role:                 tagaccess.RoleOwner,
				Status:               tagaccess.StatusActive,
				AccountEpoch:         7,
				MembershipGeneration: 3,
				AuthorityVersion:     1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{
			KeyID: "vibes-primary",
		},
	}
	if _, err := access.Gate.ApplyProjection(context.Background(), envelope.Delivery.Projections[0]); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) {
		t.Fatalf("direct production Gate apply error = %v, want unverified delivery", err)
	}

	if _, err := ingress.Deliver(context.Background(), envelope); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) {
		t.Fatalf("unsigned Deliver() error = %v, want unverified delivery", err)
	}

	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
	tampered := envelope
	tampered.Delivery.Projections = append([]tagaccess.ProjectionEvent(nil), envelope.Delivery.Projections...)
	tampered.Delivery.Projections[0].Role = tagaccess.RoleAdmin
	if _, err := ingress.Deliver(context.Background(), tampered); !errors.Is(err, tagaccess.ErrUnverifiedDelivery) {
		t.Fatalf("tampered Deliver() error = %v, want unverified delivery", err)
	}

	receipt, err := ingress.Deliver(context.Background(), envelope)
	if err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("authenticated Deliver() = %#v, %v, want applied", receipt, err)
	}
}

func TestAuthorityIngressReturnsDurableApplyReceiptWhileConnectionCloseIsPending(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1,
		DeliveryID:    "vibes-outbox-remove-9",
		CorrelationID: "correlation-remove-9",
		Delivery: tagaccess.ProjectionDelivery{
			Kind:                     tagaccess.DeliverySnapshot,
			BaselineAuthorityVersion: 9,
			AuthorityAssertionID:     "vibes-snapshot-assertion-9",
			Projections: []tagaccess.ProjectionEvent{{
				EventID:              "vibes-outbox-remove-9",
				VIBESUserID:          "vibes-user-1",
				WorkspaceID:          "vibes-workspace-1",
				Role:                 tagaccess.RoleMember,
				Status:               tagaccess.StatusRemoved,
				AccountEpoch:         7,
				MembershipGeneration: 3,
				AuthorityVersion:     9,
			}},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{{
			Scope:       tagaccess.ConnectionCloseMembership,
			WorkspaceID: "vibes-workspace-1",
			VIBESUserID: "vibes-user-1",
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)

	receipt, err := ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.Apply.DeliveryID != envelope.DeliveryID ||
		receipt.Apply.CorrelationID != envelope.CorrelationID || receipt.Apply.WorkspaceID != "vibes-workspace-1" ||
		receipt.Apply.AuthorityVersion != 9 || len(receipt.Apply.PayloadDigest) != 64 {
		t.Fatalf("Deliver() receipt = %#v, want durable apply receipt bound to envelope", receipt)
	}
	if receipt.ConnectionClose.Status != tagaccess.ConnectionClosePending || receipt.ConnectionClose.ReceiptID != "" {
		t.Fatalf("connection-close stage = %#v, want pending without receipt", receipt.ConnectionClose)
	}
	closeJSON, err := json.Marshal(receipt.ConnectionClose)
	if err != nil {
		t.Fatal(err)
	}
	if string(closeJSON) != `{"status":"pending"}` {
		t.Fatalf("pending close JSON = %s, want no fabricated receipt fields", closeJSON)
	}

	retry, err := ingress.Deliver(context.Background(), envelope)
	if err != nil || retry.Apply.Result != tagaccess.ApplyDuplicate || retry.Apply.PayloadDigest != receipt.Apply.PayloadDigest {
		t.Fatalf("retry Deliver() = %#v, %v, want idempotent duplicate with same durable digest", retry, err)
	}
}

func TestAuthorityIngressDoesNotCloseConnectionsForIgnoredOrBlockedDelivery(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	port := &fixtureConnectionClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"vibes-primary": key}, port,
	)
	if err != nil {
		t.Fatal(err)
	}
	projection := tagaccess.ProjectionEvent{
		EventID: "outbox-2", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		Role: tagaccess.RoleMember, Status: tagaccess.StatusRemoved, AccountEpoch: 2,
		MembershipGeneration: 3, AuthorityVersion: 2,
	}
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "outbox-2", CorrelationID: "correlation-2",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: 1,
			Projections: []tagaccess.ProjectionEvent{projection},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{{
			Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-1", VIBESUserID: "user-1",
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)

	receipt, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil || receipt.Apply.Result != tagaccess.ApplyGap {
		t.Fatalf("gap Deliver() = %#v, %v", receipt, err)
	}
	if receipt.ConnectionClose.Status != tagaccess.ConnectionCloseNotRequired || len(port.commands) != 0 {
		t.Fatalf("blocked delivery close stage = %#v, commands = %#v", receipt.ConnectionClose, port.commands)
	}
}

func TestAuthorityIngressReportsConnectionCloseCompleteOnlyForExactDurableReceipt(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	store := tagaccess.NewMemoryStore()
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1,
		DeliveryID:    "vibes-outbox-remove-1",
		CorrelationID: "correlation-remove-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "vibes-outbox-remove-1", VIBESUserID: "vibes-user-1", WorkspaceID: "vibes-workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusRemoved, AccountEpoch: 2,
				MembershipGeneration: 4, AuthorityVersion: 1,
			}},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{{
			Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "vibes-workspace-1", VIBESUserID: "vibes-user-1",
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)

	port := &fixtureConnectionClosePort{receipt: tagaccess.ConnectionCloseReceipt{
		ReceiptID: "close-receipt-wrong", DeliveryID: "another-delivery", TargetDigest: "wrong-digest",
		CompletedAt: time.Date(2026, 8, 19, 12, 0, 1, 0, time.UTC),
	}}
	access, err := tagaccess.NewAuthenticatedAccess(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, port)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	receipt, err := ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ConnectionClose.Status != tagaccess.ConnectionClosePending || receipt.ConnectionClose.ReceiptID != "" {
		t.Fatalf("mismatched close receipt = %#v, want pending", receipt.ConnectionClose)
	}
	if len(port.commands) != 1 || port.commands[0].DeliveryID != envelope.DeliveryID || len(port.commands[0].TargetDigest) != 64 {
		t.Fatalf("close command = %#v, want exact envelope identity and target digest", port.commands)
	}

	command := port.commands[0]
	port.receipt = tagaccess.ConnectionCloseReceipt{
		ReceiptID: "close-receipt-1", Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		TargetDigest: command.TargetDigest,
		CompletedAt:  time.Date(2026, 8, 19, 12, 0, 2, 0, time.UTC),
	}
	retry, err := ingress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted || retry.ConnectionClose.ReceiptID != "close-receipt-1" {
		t.Fatalf("matching close receipt = %#v, want completed", retry.ConnectionClose)
	}
}

func TestCanonicalAuthorityEnvelopeHasStableCrossServiceBytes(t *testing.T) {
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1,
		DeliveryID:    "outbox-1",
		CorrelationID: "correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "outbox-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 2,
				MembershipGeneration: 3, AuthorityVersion: 1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "key-1"},
	}

	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"schemaVersion":1,"deliveryId":"outbox-1","correlationId":"correlation-1","delivery":{"kind":"incremental","baselineAuthorityVersion":0,"authorityAssertionId":"","projections":[{"eventId":"outbox-1","vibesUserId":"user-1","workspaceId":"workspace-1","role":"member","status":"active","accountEpoch":2,"membershipGeneration":3,"authorityVersion":1}]},"connectionCloseTargets":[],"authentication":{"keyId":"key-1"}}`
	if string(payload) != expected {
		t.Fatalf("canonical envelope = %s, want %s", payload, expected)
	}
}

func TestCanonicalAuthorityEnvelopeNormalizesSnapshotAndCloseTargetOrder(t *testing.T) {
	memberA := tagaccess.ProjectionEvent{
		EventID: "snapshot-a", VIBESUserID: "user-a", WorkspaceID: "workspace-1",
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 2,
		MembershipGeneration: 3, AuthorityVersion: 5,
	}
	memberB := memberA
	memberB.EventID = "snapshot-b"
	memberB.VIBESUserID = "user-b"
	memberB.Role = tagaccess.RoleMember
	targetA := tagaccess.ConnectionCloseTarget{
		Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-1", VIBESUserID: "user-a",
	}
	targetB := targetA
	targetB.VIBESUserID = "user-b"
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "snapshot-5", CorrelationID: "correlation-5",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: 5,
			AuthorityAssertionID: "snapshot-assertion-5", Projections: []tagaccess.ProjectionEvent{memberB, memberA},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{targetB, targetA},
		Authentication:         tagaccess.AuthorityEnvelopeAuthentication{KeyID: "key-1"},
	}
	reordered := envelope
	reordered.Delivery.Projections = []tagaccess.ProjectionEvent{memberA, memberB}
	reordered.ConnectionCloseTargets = []tagaccess.ConnectionCloseTarget{targetA, targetB}

	first, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tagaccess.CanonicalAuthorityEnvelope(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical envelope changed with set order:\n%s\n%s", first, second)
	}
}

func TestAuthorityIngressDuplicateIsIdempotentAndSameVersionDifferentDigestConflicts(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "outbox-1", CorrelationID: "correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "outbox-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 2,
				MembershipGeneration: 3, AuthorityVersion: 1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
	first, err := ingress.Deliver(context.Background(), envelope)
	if err != nil || first.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("first Deliver() = %#v, %v", first, err)
	}
	duplicate, err := ingress.Deliver(context.Background(), envelope)
	if err != nil || duplicate.Apply.Result != tagaccess.ApplyDuplicate || duplicate.Apply.PayloadDigest != first.Apply.PayloadDigest {
		t.Fatalf("duplicate Deliver() = %#v, %v, want same digest duplicate", duplicate, err)
	}

	conflict := envelope
	conflict.ConnectionCloseTargets = []tagaccess.ConnectionCloseTarget{{
		Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-1", VIBESUserID: "user-1",
	}}
	conflict.Authentication.MAC = signAuthorityEnvelope(t, key, conflict)
	conflictingReceipt, err := ingress.Deliver(context.Background(), conflict)
	if err != nil || conflictingReceipt.Apply.Result != tagaccess.ApplyConflict || conflictingReceipt.Apply.PayloadDigest == first.Apply.PayloadDigest {
		t.Fatalf("same-version changed envelope = %#v, %v, want conflict with different digest", conflictingReceipt, err)
	}
}

func TestAuthorityIngressKeyRotationDoesNotChangeAuthorityPayloadDigest(t *testing.T) {
	keyOne := []byte("vibes-authority-test-key-one-32-bytes-minimum")
	keyTwo := []byte("vibes-authority-test-key-two-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)},
		map[string][]byte{"key-one": keyOne, "key-two": keyTwo}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "outbox-1", CorrelationID: "correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "outbox-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: 2,
				MembershipGeneration: 3, AuthorityVersion: 1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "key-one"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, keyOne, envelope)
	first, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil || first.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("first key Deliver() = %#v, %v", first, err)
	}

	envelope.Authentication = tagaccess.AuthorityEnvelopeAuthentication{KeyID: "key-two"}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, keyTwo, envelope)
	rotated, err := access.Ingress.Deliver(context.Background(), envelope)
	if err != nil || rotated.Apply.Result != tagaccess.ApplyDuplicate || rotated.Apply.PayloadDigest != first.Apply.PayloadDigest {
		t.Fatalf("rotated key Deliver() = %#v, %v, want same-payload duplicate", rotated, err)
	}
}

func TestAuthorityIngressDoesNotRequestConnectionCloseBeforeDurableApply(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	store := tagaccess.NewMemoryStore()
	store.SetFailure(errors.New("projection store unavailable"))
	port := &fixtureConnectionClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(store, fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, port)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "outbox-1", CorrelationID: "correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "outbox-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
				Role: tagaccess.RoleMember, Status: tagaccess.StatusRemoved, AccountEpoch: 2,
				MembershipGeneration: 3, AuthorityVersion: 1,
			}},
		},
		ConnectionCloseTargets: []tagaccess.ConnectionCloseTarget{{
			Scope: tagaccess.ConnectionCloseMembership, WorkspaceID: "workspace-1", VIBESUserID: "user-1",
		}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)

	receipt, err := ingress.Deliver(context.Background(), envelope)
	if err == nil || receipt != (tagaccess.TwoStageReceipt{}) {
		t.Fatalf("store failure Deliver() = %#v, %v, want no receipt", receipt, err)
	}
	if len(port.commands) != 0 {
		t.Fatalf("close commands = %#v, want none before durable apply", port.commands)
	}
}

func TestAuthorityIngressUsesAuthenticatedReconcileToRepairGap(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}, map[string][]byte{"vibes-primary": key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := access.Ingress
	projection := tagaccess.ProjectionEvent{
		EventID: "outbox-1", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 2,
		MembershipGeneration: 3, AuthorityVersion: 1,
	}
	deliver := func(envelope tagaccess.AuthorityEnvelope) tagaccess.TwoStageReceipt {
		t.Helper()
		envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
		receipt, err := ingress.Deliver(context.Background(), envelope)
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	base := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "outbox-1", CorrelationID: "correlation-1",
		Delivery:       tagaccess.ProjectionDelivery{Kind: tagaccess.DeliveryIncremental, Projections: []tagaccess.ProjectionEvent{projection}},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	if receipt := deliver(base); receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("base receipt = %#v", receipt)
	}
	projection.EventID = "outbox-3"
	projection.AuthorityVersion = 3
	gap := base
	gap.DeliveryID = "outbox-3"
	gap.CorrelationID = "correlation-3"
	gap.Delivery = tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: 2,
		Projections: []tagaccess.ProjectionEvent{projection},
	}
	if receipt := deliver(gap); receipt.Apply.Result != tagaccess.ApplyGap {
		t.Fatalf("gap receipt = %#v", receipt)
	}
	projection.EventID = "reconcile-4"
	projection.AuthorityVersion = 4
	reconcile := base
	reconcile.DeliveryID = "reconcile-4"
	reconcile.CorrelationID = "correlation-reconcile-4"
	reconcile.Delivery = tagaccess.ProjectionDelivery{
		Kind: tagaccess.DeliveryReconcile, BaselineAuthorityVersion: 4,
		AuthorityAssertionID: "vibes-reconcile-assertion-4", Projections: []tagaccess.ProjectionEvent{projection},
	}
	if receipt := deliver(reconcile); receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("reconcile receipt = %#v, want repaired apply", receipt)
	}
}

func signAuthorityEnvelope(t *testing.T, key []byte, envelope tagaccess.AuthorityEnvelope) []byte {
	t.Helper()
	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
