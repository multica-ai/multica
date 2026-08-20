package tagaccess_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type echoSessionWorkspaceClosePort struct {
	commands []tagaccess.ConnectionCloseCommand
	pending  bool
}

func (p *echoSessionWorkspaceClosePort) CloseConnections(_ context.Context, command tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, error) {
	p.commands = append(p.commands, command)
	if p.pending {
		return tagaccess.ConnectionCloseReceipt{}, context.DeadlineExceeded
	}
	return tagaccess.ConnectionCloseReceipt{
		ReceiptID: "close-" + command.DeliveryID, Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		AuthorityVersion: command.AuthorityVersion, IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		SessionWorkspaceGeneration: command.SessionWorkspaceGeneration, TargetDigest: command.TargetDigest,
		CompletedAt: time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC),
	}, nil
}

type sessionWorkspaceFixture struct {
	TestKeyBase64          string                                       `json:"testKeyBase64"`
	KeyID                  string                                       `json:"keyId"`
	UnsignedEnvelope       tagaccess.SessionWorkspaceSupersededEnvelope `json:"unsignedEnvelope"`
	UnsignedCanonical      string                                       `json:"unsignedCanonical"`
	PayloadSHA256          string                                       `json:"payloadSha256"`
	AuthenticatedCanonical string                                       `json:"authenticatedCanonical"`
	MACBase64              string                                       `json:"macBase64"`
}

func TestSessionWorkspaceSupersessionCanonicalMatchesExactVIBESFixture(t *testing.T) {
	fixture := loadSessionWorkspaceFixture(t)
	unsigned, err := tagaccess.CanonicalSessionWorkspaceSupersessionPayload(fixture.UnsignedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(unsigned) != fixture.UnsignedCanonical {
		t.Fatalf("unsigned canonical mismatch\n got: %s\nwant: %s", unsigned, fixture.UnsignedCanonical)
	}
	digest := sha256.Sum256(unsigned)
	if hex.EncodeToString(digest[:]) != fixture.PayloadSHA256 {
		t.Fatalf("payload digest = %x, want %s", digest, fixture.PayloadSHA256)
	}
	key, err := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	macBytes, err := base64.StdEncoding.DecodeString(fixture.MACBase64)
	if err != nil {
		t.Fatal(err)
	}
	fixture.UnsignedEnvelope.Authentication = tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID, MAC: macBytes}
	authenticated, err := tagaccess.CanonicalSessionWorkspaceSupersessionEnvelope(fixture.UnsignedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(authenticated) != fixture.AuthenticatedCanonical {
		t.Fatalf("authenticated canonical mismatch\n got: %s\nwant: %s", authenticated, fixture.AuthenticatedCanonical)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(authenticated)
	if !hmac.Equal(mac.Sum(nil), macBytes) {
		t.Fatal("exact VIBES fixture HMAC did not verify")
	}
}

func loadSessionWorkspaceFixture(t *testing.T) sessionWorkspaceFixture {
	t.Helper()
	body, err := os.ReadFile("testdata/session-workspace-supersession-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sessionWorkspaceFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestSessionWorkspaceSupersessionExactFixtureFencesOldGrantAndCompletesExactClose(t *testing.T) {
	fixture := loadSessionWorkspaceFixture(t)
	key, _ := base64.StdEncoding.DecodeString(fixture.TestKeyBase64)
	macBytes, _ := base64.StdEncoding.DecodeString(fixture.MACBase64)
	fixture.UnsignedEnvelope.Authentication = tagaccess.AuthorityEnvelopeAuthentication{KeyID: fixture.KeyID, MAC: macBytes}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closePort := &echoSessionWorkspaceClosePort{}
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{fixture.KeyID: key}, closePort)
	if err != nil {
		t.Fatal(err)
	}
	projectSessionWorkspaceMember(t, access, key, fixture.KeyID, "workspace-alpha", 7, 5, 1)
	projectSessionWorkspaceMember(t, access, key, fixture.KeyID, "workspace-beta", 7, 11, 3)
	projectSessionWorkspaceMember(t, access, key, fixture.KeyID, "workspace-gamma", 7, 12, 4)
	for version := uint64(1); version <= 4; version++ {
		deliverIdentityWithKey(t, access, key, fixture.KeyID, tagaccess.IdentityRestrictionDelivery{
			Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: fmt.Sprintf("sibling-event-%d", version),
			CorrelationID: fmt.Sprintf("sibling-correlation-%d", version), IdempotencyKey: fmt.Sprintf("sibling-idempotency-%d", version),
			VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("sibling-%d", version), AccountEpoch: 7,
			IdentityRestrictionVersion: version,
			CloseTarget:                tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("sibling-%d", version)},
		})
	}
	tagSessionID := tagaccess.BrowserTagSessionID("user-1", "session-1")
	grantSessionWorkspace(t, access.Gate, now, tagSessionID, "session-1", "workspace-alpha", 7, 1, 1, 5)
	siblingTagSessionID := tagaccess.BrowserTagSessionID("user-1", "sibling-live")
	grantSessionWorkspace(t, access.Gate, now, siblingTagSessionID, "sibling-live", "workspace-alpha", 7, 1, 1, 5)

	receipt, err := access.SessionWorkspaceIngress.Deliver(context.Background(), fixture.UnsignedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Apply.Result != tagaccess.ApplyApplied || receipt.Apply.PayloadDigest != fixture.PayloadSHA256 ||
		receipt.Apply.DeliveryID != "delivery-switch-0002" || receipt.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted {
		t.Fatalf("fixture receipt = %#v", receipt)
	}
	var sessionWorkspaceCommands []tagaccess.ConnectionCloseCommand
	for _, command := range closePort.commands {
		if command.Source == tagaccess.ConnectionCloseSessionWorkspaceSupersession {
			sessionWorkspaceCommands = append(sessionWorkspaceCommands, command)
		}
	}
	if len(sessionWorkspaceCommands) != 1 {
		t.Fatalf("session Workspace close commands = %d, want 1", len(sessionWorkspaceCommands))
	}
	command := sessionWorkspaceCommands[0]
	if command.Source != tagaccess.ConnectionCloseSessionWorkspaceSupersession || command.SessionWorkspaceGeneration != 2 ||
		len(command.Targets) != 1 || command.Targets[0] != fixture.UnsignedEnvelope.Delivery.CloseTarget {
		t.Fatalf("close command = %#v", command)
	}
	oldRequest := tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-alpha",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 5,
	}
	if decision := access.Gate.Authorize(context.Background(), oldRequest); decision.Allowed {
		t.Fatalf("old Workspace grant revived after durable apply: %#v", decision)
	}
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-alpha",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 5,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != tagaccess.ErrGrantDenied {
		t.Fatalf("old handoff grant error = %v, want denied", err)
	}
	grantSessionWorkspace(t, access.Gate, now, tagSessionID, "session-1", "workspace-beta", 7, 2, 3, 11)
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-beta",
		AccountEpoch: 7, SessionWorkspaceGeneration: 2, MembershipGeneration: 3, AuthorityVersion: 11,
	}); !decision.Allowed {
		t.Fatalf("new Workspace grant denied: %#v", decision)
	}
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: siblingTagSessionID, VIBESSessionID: "sibling-live", VIBESUserID: "user-1", WorkspaceID: "workspace-alpha",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 5,
	}); !decision.Allowed {
		t.Fatalf("unaffected sibling session denied: %#v", decision)
	}
	gen3 := sessionWorkspaceDelivery("switch-0003", "workspace-beta", "workspace-gamma", 3, 12, 4)
	gen3.IdentityRestrictionVersion = 4
	if receipt := deliverSessionWorkspace(t, access, key, fixture.KeyID, gen3); receipt.Apply.Result != tagaccess.ApplyApplied || receipt.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted {
		t.Fatalf("A→B→C generation 3 receipt = %#v", receipt)
	}
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-beta",
		AccountEpoch: 7, SessionWorkspaceGeneration: 2, MembershipGeneration: 3, AuthorityVersion: 11,
	}); decision.Allowed {
		t.Fatalf("generation 2 grant survived generation 3 apply: %#v", decision)
	}
	grantSessionWorkspace(t, access.Gate, now, tagSessionID, "session-1", "workspace-gamma", 7, 3, 4, 12)
}

func TestSessionWorkspaceSupersessionOrdersRapidSwitchesAndRejectsConflict(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, &echoSessionWorkspaceClosePort{})
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range []struct {
		workspace  string
		version    uint64
		generation uint64
	}{{"workspace-a", 1, 1}, {"workspace-b", 2, 1}, {"workspace-c", 3, 1}} {
		projectSessionWorkspaceMember(t, access, key, "vibes-primary", projection.workspace, 7, projection.version, projection.generation)
	}
	gen2 := sessionWorkspaceDelivery("event-2", "workspace-a", "workspace-b", 2, 2, 1)
	gen3 := sessionWorkspaceDelivery("event-3", "workspace-b", "workspace-c", 3, 3, 1)
	if got := deliverSessionWorkspace(t, access, key, "vibes-primary", gen3); got.Apply.Result != tagaccess.ApplyGap {
		t.Fatalf("generation 3 first = %s, want gap", got.Apply.Result)
	}
	if got := deliverSessionWorkspace(t, access, key, "vibes-primary", gen2); got.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("generation 2 = %s, want applied", got.Apply.Result)
	}
	if got := deliverSessionWorkspace(t, access, key, "vibes-primary", gen3); got.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("generation 3 retry = %s, want applied", got.Apply.Result)
	}
	if got := deliverSessionWorkspace(t, access, key, "vibes-primary", gen2); got.Apply.Result != tagaccess.ApplyStale {
		t.Fatalf("generation 2 replay after generation 3 = %s, want stale", got.Apply.Result)
	}
	conflict := gen3
	conflict.NewWorkspaceID = "workspace-a"
	conflict.CloseTarget.WorkspaceID = "workspace-b"
	if got := deliverSessionWorkspace(t, access, key, "vibes-primary", conflict); got.Apply.Result != tagaccess.ApplyConflict {
		t.Fatalf("different binding at generation 3 = %s, want conflict", got.Apply.Result)
	}
}

func TestSessionWorkspaceSupersessionIdentityRaceFencesExactSessionButNotSibling(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name        string
		restriction tagaccess.IdentityRestrictionDelivery
		want        tagaccess.ApplyResult
	}{
		{
			name: "exact session logout",
			restriction: tagaccess.IdentityRestrictionDelivery{
				Kind: tagaccess.IdentityRestrictionSessionLogout, VIBESSessionID: "session-1", AccountEpoch: 7,
				CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "session-1"},
			},
			want: tagaccess.ApplyConflict,
		},
		{
			name: "sibling session logout",
			restriction: tagaccess.IdentityRestrictionDelivery{
				Kind: tagaccess.IdentityRestrictionSessionLogout, VIBESSessionID: "sibling-race", AccountEpoch: 7,
				CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "sibling-race"},
			},
			want: tagaccess.ApplyApplied,
		},
		{
			name: "account ban epoch advance",
			restriction: tagaccess.IdentityRestrictionDelivery{
				Kind: tagaccess.IdentityRestrictionAccountBan, AccountEpoch: 8,
				CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"},
			},
			want: tagaccess.ApplyConflict,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			closePort := &echoSessionWorkspaceClosePort{}
			access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, closePort)
			if err != nil {
				t.Fatal(err)
			}
			projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-a", 7, 1, 1)
			projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-b", 7, 2, 1)
			for version := uint64(1); version <= 4; version++ {
				deliverIdentityWithKey(t, access, key, "vibes-primary", tagaccess.IdentityRestrictionDelivery{
					Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: fmt.Sprintf("seed-event-%d", version),
					CorrelationID: fmt.Sprintf("seed-correlation-%d", version), IdempotencyKey: fmt.Sprintf("seed-idempotency-%d", version),
					VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("seed-sibling-%d", version), AccountEpoch: 7,
					IdentityRestrictionVersion: version,
					CloseTarget:                tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: fmt.Sprintf("seed-sibling-%d", version)},
				})
			}
			restriction := test.restriction
			restriction.EventID = "race-event-5"
			restriction.CorrelationID = "race-correlation-5"
			restriction.IdempotencyKey = "race-idempotency-5"
			restriction.VIBESUserID = "user-1"
			restriction.IdentityRestrictionVersion = 5
			deliverIdentityWithKey(t, access, key, "vibes-primary", restriction)
			delivery := sessionWorkspaceDelivery("switch-race", "workspace-a", "workspace-b", 2, 2, 1)
			delivery.IdentityRestrictionVersion = 4
			before := len(closePort.commands)
			receipt := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery)
			if receipt.Apply.Result != test.want {
				t.Fatalf("result = %s, want %s", receipt.Apply.Result, test.want)
			}
			if test.want == tagaccess.ApplyConflict && len(closePort.commands) != before {
				t.Fatal("rejected supersession dispatched a close")
			}
		})
	}
}

func TestSessionWorkspaceSupersessionKeepsCompletionPendingUntilExactCloseReceipt(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closePort := &echoSessionWorkspaceClosePort{pending: true}
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, closePort)
	if err != nil {
		t.Fatal(err)
	}
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-a", 7, 1, 1)
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-b", 7, 2, 1)
	tagSessionID := tagaccess.BrowserTagSessionID("user-1", "session-1")
	grantSessionWorkspace(t, access.Gate, now, tagSessionID, "session-1", "workspace-a", 7, 1, 1, 1)
	delivery := sessionWorkspaceDelivery("pending-switch", "workspace-a", "workspace-b", 2, 2, 1)
	first := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery)
	if first.Apply.Result != tagaccess.ApplyApplied || first.ConnectionClose.Status != tagaccess.ConnectionClosePending || first.ConnectionClose.ReceiptID != "" {
		t.Fatalf("first receipt = %#v, want durable apply with pending close", first)
	}
	if decision := access.Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
	}); decision.Allowed {
		t.Fatalf("old grant remained live while close receipt was pending: %#v", decision)
	}
	closePort.pending = false
	retry := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery)
	if retry.Apply.Result != tagaccess.ApplyDuplicate || retry.ConnectionClose.Status != tagaccess.ConnectionCloseCompleted || retry.ConnectionClose.ReceiptID == "" {
		t.Fatalf("retry receipt = %#v, want exact close completion", retry)
	}
}

func TestSessionWorkspaceSupersessionFencesUnconsumedOldHandoffBeforeNewSocketAdmission(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, &echoSessionWorkspaceClosePort{})
	if err != nil {
		t.Fatal(err)
	}
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-a", 7, 1, 1)
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-b", 7, 2, 1)
	delivery := sessionWorkspaceDelivery("unconsumed-switch", "workspace-a", "workspace-b", 2, 2, 1)
	if receipt := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery); receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("apply = %#v", receipt)
	}
	tagSessionID := tagaccess.BrowserTagSessionID("user-1", "session-1")
	if err := access.Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: "session-1", VIBESUserID: "user-1", WorkspaceID: "workspace-a",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != tagaccess.ErrGrantDenied {
		t.Fatalf("old unconsumed handoff error = %v, want denied", err)
	}
	grantSessionWorkspace(t, access.Gate, now, tagSessionID, "session-1", "workspace-b", 7, 2, 1, 2)
}

func TestSessionWorkspaceSupersessionPendingCloseCannotCompleteAfterExactLogoutWinsRace(t *testing.T) {
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closePort := &echoSessionWorkspaceClosePort{pending: true}
	access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, closePort)
	if err != nil {
		t.Fatal(err)
	}
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-a", 7, 1, 1)
	projectSessionWorkspaceMember(t, access, key, "vibes-primary", "workspace-b", 7, 2, 1)
	delivery := sessionWorkspaceDelivery("logout-race-switch", "workspace-a", "workspace-b", 2, 2, 1)
	if first := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery); first.Apply.Result != tagaccess.ApplyApplied || first.ConnectionClose.Status != tagaccess.ConnectionClosePending {
		t.Fatalf("first = %#v", first)
	}
	deliverIdentityWithKey(t, access, key, "vibes-primary", tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "logout-race-event",
		CorrelationID: "logout-race-correlation", IdempotencyKey: "logout-race-idempotency",
		VIBESUserID: "user-1", VIBESSessionID: "session-1", AccountEpoch: 7, IdentityRestrictionVersion: 1,
		CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "session-1"},
	})
	closePort.pending = false
	retry := deliverSessionWorkspace(t, access, key, "vibes-primary", delivery)
	if retry.Apply.Result != tagaccess.ApplyConflict || retry.ConnectionClose.Status != tagaccess.ConnectionCloseNotRequired {
		t.Fatalf("retry after exact logout = %#v, want irrevocable conflict without supersession completion", retry)
	}
}

func projectSessionWorkspaceMember(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, keyID, workspaceID string, accountEpoch, authorityVersion, membershipGeneration uint64) {
	t.Helper()
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "projection-" + workspaceID, CorrelationID: "projection-correlation-" + workspaceID,
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliverySnapshot, BaselineAuthorityVersion: authorityVersion, AuthorityAssertionID: "assertion-" + workspaceID,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "projection-" + workspaceID, VIBESUserID: "user-1", WorkspaceID: workspaceID,
				Role: tagaccess.RoleMember, Status: tagaccess.StatusActive, AccountEpoch: accountEpoch,
				MembershipGeneration: membershipGeneration, AuthorityVersion: authorityVersion,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: keyID},
	}
	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	if receipt, err := access.Ingress.Deliver(context.Background(), envelope); err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("project %s = %#v, %v", workspaceID, receipt, err)
	}
}

func deliverIdentityWithKey(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, keyID string, delivery tagaccess.IdentityRestrictionDelivery) {
	t.Helper()
	envelope := tagaccess.IdentityRestrictionEnvelope{SchemaVersion: 1, Delivery: delivery, Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: keyID}}
	payload, err := tagaccess.CanonicalIdentityRestrictionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	if receipt, err := access.IdentityIngress.Deliver(context.Background(), envelope); err != nil || receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("identity delivery = %#v, %v", receipt, err)
	}
}

func grantSessionWorkspace(t *testing.T, gate *tagaccess.Gate, now time.Time, tagSessionID, vibesSessionID, workspaceID string, accountEpoch, sessionGeneration, membershipGeneration, authorityVersion uint64) {
	t.Helper()
	if err := gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: vibesSessionID, VIBESUserID: "user-1", WorkspaceID: workspaceID,
		AccountEpoch: accountEpoch, SessionWorkspaceGeneration: sessionGeneration,
		MembershipGeneration: membershipGeneration, AuthorityVersion: authorityVersion,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
}

func sessionWorkspaceDelivery(eventID, previous, next string, generation, authorityVersion, membershipGeneration uint64) tagaccess.SessionWorkspaceSupersededDelivery {
	return tagaccess.SessionWorkspaceSupersededDelivery{
		Kind: tagaccess.SessionWorkspaceSuperseded, EventID: eventID, DeliveryID: "delivery-" + eventID,
		CorrelationID: "correlation-" + eventID, IdempotencyKey: "idempotency-" + eventID,
		VIBESUserID: "user-1", VIBESSessionID: "session-1", PreviousWorkspaceID: previous, NewWorkspaceID: next,
		SessionWorkspaceGeneration: generation, AccountEpoch: 7, IdentityRestrictionVersion: 0,
		AuthorityVersion: authorityVersion, MembershipGeneration: membershipGeneration,
		CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseSessionWorkspace, VIBESUserID: "user-1", VIBESSessionID: "session-1", WorkspaceID: previous},
	}
}

func deliverSessionWorkspace(t *testing.T, access *tagaccess.AuthenticatedAccess, key []byte, keyID string, delivery tagaccess.SessionWorkspaceSupersededDelivery) tagaccess.SessionWorkspaceTwoStageReceipt {
	t.Helper()
	envelope := tagaccess.SessionWorkspaceSupersededEnvelope{SchemaVersion: 1, Delivery: delivery, Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: keyID}}
	payload, err := tagaccess.CanonicalSessionWorkspaceSupersessionEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	receipt, err := access.SessionWorkspaceIngress.Deliver(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
