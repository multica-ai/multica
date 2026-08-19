package tagaccess_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

const (
	tagAccessTestDatabaseEnv        = "MULTICA_TAG_ACCESS_TEST_DATABASE_URL"
	defaultTagAccessTestDatabaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
)

func TestDisposableTagAccessDatabaseURLIgnoresAmbientApplicationDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://production.example.invalid/application")
	t.Setenv(tagAccessTestDatabaseEnv, "")

	databaseURL, err := disposableTagAccessDatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	if databaseURL != defaultTagAccessTestDatabaseURL {
		t.Fatalf("database URL = %q, want local test default", databaseURL)
	}
}

func TestDisposableTagAccessDatabaseURLRejectsRemoteHost(t *testing.T) {
	t.Setenv(tagAccessTestDatabaseEnv, "postgres://user:password@database.example.invalid/multica")

	if _, err := disposableTagAccessDatabaseURL(); err == nil {
		t.Fatal("expected remote test database URL to be rejected")
	}
}

func TestPostgresStoreMatchesAccessGateContract(t *testing.T) {
	conn := openDisposableTagAccessDatabase(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	verifier := tagaccess.NewFixtureDeliveryVerifier()
	gate := tagaccess.New(tagaccess.NewPostgresStore(conn), fixedClock{now: now}, verifier)
	event := tagaccess.ProjectionEvent{
		EventID:              "event-1",
		VIBESUserID:          "vibes-user-1",
		WorkspaceID:          "vibes-workspace-1",
		Role:                 tagaccess.RoleOwner,
		Status:               tagaccess.StatusActive,
		AccountEpoch:         4,
		MembershipGeneration: 9,
		AuthorityVersion:     1,
	}
	if result, err := gate.ApplyProjection(context.Background(), event); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("ApplyProjection() = %q, %v", result, err)
	}
	if result, err := gate.ApplyProjection(context.Background(), event); err != nil || result != tagaccess.ApplyDuplicate {
		t.Fatalf("duplicate ApplyProjection() = %q, %v", result, err)
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
	if !decision.Allowed || decision.Role != tagaccess.RoleOwner || decision.AuthorityVersion != 1 {
		t.Fatalf("Authorize() = %#v", decision)
	}

	version3 := event
	version3.EventID = "event-3"
	version3.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("out-of-order version 3 = %q, %v", result, err)
	}
	if decision := gate.Authorize(context.Background(), accessRequestForGrant(grant)); decision.Reason != tagaccess.DenyProjectionGap {
		t.Fatalf("gap Authorize() = %#v", decision)
	}
	version2 := event
	version2.EventID = "event-2"
	version2.AuthorityVersion = 2
	if result, err := gate.ApplyProjection(context.Background(), version2); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("version 2 before retried version 3 = %q, %v", result, err)
	}
	if result, err := gate.ApplyProjection(context.Background(), version3); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("retried version 3 = %q, %v", result, err)
	}
	grant.AuthorityVersion = 3
	if err := gate.GrantSession(context.Background(), grant); err != nil {
		t.Fatalf("refresh GrantSession() error = %v", err)
	}
	if decision := gate.Authorize(context.Background(), accessRequestForGrant(grant)); !decision.Allowed || decision.AuthorityVersion != 3 {
		t.Fatalf("refreshed Authorize() = %#v", decision)
	}

	snapshotProjection := event
	snapshotProjection.EventID = "snapshot-event-5"
	snapshotProjection.VIBESUserID = "vibes-user-snapshot"
	snapshotProjection.WorkspaceID = "vibes-workspace-snapshot"
	snapshotProjection.Role = tagaccess.RoleMember
	snapshotProjection.AuthorityVersion = 5
	snapshot := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 5,
		AuthorityAssertionID:     "snapshot-proof-5",
		Projections:              []tagaccess.ProjectionEvent{snapshotProjection},
	}
	verifier.Trust(snapshot)
	if result, err := gate.ApplyAuthorityDelivery(context.Background(), snapshot); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("snapshot bootstrap v5 = %q, %v", result, err)
	}
	snapshotGrant := grant
	snapshotGrant.TagSessionID = "tag-session-snapshot"
	snapshotGrant.VIBESSessionID = "vibes-session-snapshot"
	snapshotGrant.VIBESUserID = snapshotProjection.VIBESUserID
	snapshotGrant.WorkspaceID = snapshotProjection.WorkspaceID
	snapshotGrant.AuthorityVersion = 5
	if err := gate.GrantSession(context.Background(), snapshotGrant); err != nil {
		t.Fatalf("snapshot GrantSession() error = %v", err)
	}
	omittedMember := snapshotProjection
	omittedMember.EventID = "snapshot-member-b-6"
	omittedMember.VIBESUserID = "vibes-user-snapshot-b"
	omittedMember.AuthorityVersion = 6
	if result, err := gate.ApplyProjection(context.Background(), omittedMember); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("snapshot workspace member B v6 = %q, %v", result, err)
	}
	omittedGrant := snapshotGrant
	omittedGrant.TagSessionID = "tag-session-snapshot-b"
	omittedGrant.VIBESSessionID = "vibes-session-snapshot-b"
	omittedGrant.VIBESUserID = omittedMember.VIBESUserID
	omittedGrant.AuthorityVersion = 6
	if err := gate.GrantSession(context.Background(), omittedGrant); err != nil {
		t.Fatalf("omitted member GrantSession() error = %v", err)
	}
	snapshotProjection.EventID = "snapshot-event-7"
	snapshotProjection.AuthorityVersion = 7
	replacementSnapshot := tagaccess.ProjectionDelivery{
		Kind:                     tagaccess.DeliverySnapshot,
		BaselineAuthorityVersion: 7,
		AuthorityAssertionID:     "snapshot-proof-7",
		Projections:              []tagaccess.ProjectionEvent{snapshotProjection},
	}
	verifier.Trust(replacementSnapshot)
	if result, err := gate.ApplyAuthorityDelivery(context.Background(), replacementSnapshot); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("replacement snapshot v7 = %q, %v", result, err)
	}
	if decision := gate.Authorize(context.Background(), accessRequestForGrant(omittedGrant)); decision.Reason != tagaccess.DenyInactiveMembership {
		t.Fatalf("omitted PostgreSQL member Authorize() = %#v", decision)
	}
	omittedMember.EventID = "snapshot-member-b-8"
	omittedMember.AuthorityVersion = 8
	if result, err := gate.ApplyProjection(context.Background(), omittedMember); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("same-generation omitted PostgreSQL member rejoin = %q, %v, want conflict", result, err)
	}

	conflictBase := event
	conflictBase.EventID = "conflict-event-1"
	conflictBase.VIBESUserID = "vibes-user-conflict"
	conflictBase.WorkspaceID = "vibes-workspace-conflict"
	if result, err := gate.ApplyProjection(context.Background(), conflictBase); err != nil || result != tagaccess.ApplyApplied {
		t.Fatalf("conflict base = %q, %v", result, err)
	}
	conflictVersion4 := conflictBase
	conflictVersion4.EventID = "conflict-event-4"
	conflictVersion4.AuthorityVersion = 4
	if result, err := gate.ApplyProjection(context.Background(), conflictVersion4); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("conflict version 4 = %q, %v", result, err)
	}
	conflictVersion3A := conflictBase
	conflictVersion3A.EventID = "conflict-event-3-a"
	conflictVersion3A.Role = tagaccess.RoleAdmin
	conflictVersion3A.AuthorityVersion = 3
	if result, err := gate.ApplyProjection(context.Background(), conflictVersion3A); err != nil || result != tagaccess.ApplyGap {
		t.Fatalf("conflict version 3A = %q, %v", result, err)
	}
	conflictVersion3B := conflictVersion3A
	conflictVersion3B.EventID = "conflict-event-3-b"
	conflictVersion3B.Role = tagaccess.RoleMember
	if result, err := gate.ApplyProjection(context.Background(), conflictVersion3B); err != nil || result != tagaccess.ApplyConflict {
		t.Fatalf("conflict version 3B = %q, %v", result, err)
	}
}

func TestPostgresAuthorityIngressReceiptSurvivesAdapterRestart(t *testing.T) {
	conn := openDisposableTagAccessDatabase(t)
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: 1, DeliveryID: "postgres-outbox-1", CorrelationID: "postgres-correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental,
			Projections: []tagaccess.ProjectionEvent{{
				EventID: "postgres-outbox-1", VIBESUserID: "postgres-user-1", WorkspaceID: "postgres-workspace-1",
				Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 2,
				MembershipGeneration: 3, AuthorityVersion: 1,
			}},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: "vibes-primary"},
	}
	envelope.Authentication.MAC = signAuthorityEnvelope(t, key, envelope)
	newIngress := func() *tagaccess.AuthorityIngress {
		t.Helper()
		access, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewPostgresStore(conn), fixedClock{now: now}, map[string][]byte{"vibes-primary": key}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return access.Ingress
	}
	first, err := newIngress().Deliver(context.Background(), envelope)
	if err != nil || first.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("first Deliver() = %#v, %v", first, err)
	}
	restarted, err := newIngress().Deliver(context.Background(), envelope)
	if err != nil || restarted.Apply.Result != tagaccess.ApplyDuplicate || restarted.Apply.PayloadDigest != first.Apply.PayloadDigest {
		t.Fatalf("restarted Deliver() = %#v, %v, want durable duplicate receipt", restarted, err)
	}
}

func TestPostgresIdentityRestrictionReceiptSurvivesAdapterRestart(t *testing.T) {
	conn := openDisposableTagAccessDatabase(t)
	key := []byte("vibes-authority-test-key-32-bytes-minimum")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	newAccess := func() *tagaccess.AuthenticatedAccess {
		t.Helper()
		access, err := tagaccess.NewAuthenticatedAccess(
			tagaccess.NewPostgresStore(conn), fixedClock{now: now},
			map[string][]byte{"vibes-primary": key}, nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		return access
	}
	access := newAccess()
	projectActiveMember(t, access, key, 7)
	grantSession(t, access.Gate, now, "tag-session-a", "vibes-session-a", 7)
	grantSession(t, access.Gate, now, "tag-session-b", "vibes-session-b", 7)
	delivery := tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionSessionLogout, EventID: "identity-event-1",
		CorrelationID: "identity-correlation-1", IdempotencyKey: "identity-idempotency-1",
		VIBESUserID: "user-1", VIBESSessionID: "vibes-session-a", AccountEpoch: 7,
		IdentityRestrictionVersion: 1,
		CloseTarget: tagaccess.ConnectionCloseTarget{
			Scope: tagaccess.ConnectionCloseSession, VIBESUserID: "user-1", VIBESSessionID: "vibes-session-a",
		},
	}
	first := deliverIdentity(t, access, key, delivery)
	if first.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("first identity receipt = %#v", first)
	}
	restarted := deliverIdentity(t, newAccess(), key, delivery)
	if restarted.Apply.Result != tagaccess.ApplyDuplicate || restarted.Apply.PayloadDigest != first.Apply.PayloadDigest {
		t.Fatalf("restarted identity receipt = %#v", restarted)
	}
	if decision := newAccess().Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-a", VIBESSessionID: "vibes-session-a", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
	}); decision.Allowed || decision.Reason != tagaccess.DenyMissingGrant {
		t.Fatalf("revoked session decision = %#v", decision)
	}
	if decision := newAccess().Gate.Authorize(context.Background(), tagaccess.AccessRequest{
		TagSessionID: "tag-session-b", VIBESSessionID: "vibes-session-b", VIBESUserID: "user-1", WorkspaceID: "workspace-1",
		AccountEpoch: 7, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
	}); !decision.Allowed {
		t.Fatalf("sibling session decision = %#v", decision)
	}
	ban := tagaccess.IdentityRestrictionDelivery{
		Kind: tagaccess.IdentityRestrictionAccountBan, EventID: "identity-event-2",
		CorrelationID: "identity-correlation-2", IdempotencyKey: "identity-idempotency-2",
		VIBESUserID: "user-1", AccountEpoch: 8, IdentityRestrictionVersion: 2,
		CloseTarget: tagaccess.ConnectionCloseTarget{Scope: tagaccess.ConnectionCloseAccount, VIBESUserID: "user-1"},
	}
	if receipt := deliverIdentity(t, newAccess(), key, ban); receipt.Apply.Result != tagaccess.ApplyApplied {
		t.Fatalf("account ban receipt = %#v", receipt)
	}
	projectActiveMemberAt(t, newAccess(), key, 8, 2)
	if err := newAccess().Gate.GrantSession(context.Background(), tagaccess.SessionGrant{
		TagSessionID: "tag-session-after-ban", VIBESSessionID: "vibes-session-after-ban",
		VIBESUserID: "user-1", WorkspaceID: "workspace-1", AccountEpoch: 8,
		SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 2,
		SessionExpiresAt: now.Add(time.Hour), GrantExpiresAt: now.Add(30 * time.Minute),
	}); err != tagaccess.ErrGrantDenied {
		t.Fatalf("same banned epoch GrantSession() error = %v, want grant denied", err)
	}
}

func openDisposableTagAccessDatabase(t *testing.T) *pgx.Conn {
	t.Helper()
	databaseURL, err := disposableTagAccessDatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(context.Background())
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	schema := fmt.Sprintf("tag_access_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema); err != nil {
		t.Fatal(err)
	}

	for _, migration := range []string{
		"348_tag_access_projection.up.sql",
		"349_tag_access_projection_identity_index.up.sql",
		"350_tag_access_session.up.sql",
		"351_tag_access_session_identity_index.up.sql",
		"352_tag_session_workspace_grant.up.sql",
		"353_tag_session_workspace_grant_identity_index.up.sql",
		"354_tag_access_projection_delivery.up.sql",
		"355_tag_access_projection_delivery_identity_index.up.sql",
		"356_tag_access_workspace_state.up.sql",
		"357_tag_access_workspace_state_identity_index.up.sql",
		"358_tag_access_identity_restriction.up.sql",
		"359_tag_access_identity_state_index.up.sql",
		"360_tag_access_identity_restriction_delivery.up.sql",
		"361_tag_access_identity_delivery_key_index.up.sql",
		"362_tag_access_identity_event_index.up.sql",
		"363_tag_access_identity_idempotency_index.up.sql",
		"364_tag_access_session_workspace_generation.up.sql",
		"365_tag_http_assertion_replay.up.sql",
		"366_tag_http_assertion_replay_identity_index.up.sql",
		"367_tag_http_assertion_replay_expiry_index.up.sql",
	} {
		body, err := os.ReadFile(filepath.Join(migrationsDir(t), migration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return conn
}

func disposableTagAccessDatabaseURL() (string, error) {
	databaseURL := os.Getenv(tagAccessTestDatabaseEnv)
	if databaseURL == "" {
		databaseURL = defaultTagAccessTestDatabaseURL
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", errors.New("Tag access test database URL must be a PostgreSQL URL")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("Tag access test database host %q is not loopback", hostname)
	}
	return databaseURL, nil
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
}
