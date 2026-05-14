package credentials

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

// newPolicyServiceTest builds a Service whose Policy is supplied per test.
// Mirrors newServiceTest but lets the caller decide what the checker does.
func newPolicyServiceTest(t *testing.T, p PolicyChecker) *Service {
	t.Helper()
	if credentialsTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping credential policy integration test")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	bus := events.New()
	svc := NewService(cerebrodb.New(credentialsTestPool), cipher, bus).WithPolicy(p)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = credentialsTestPool.Exec(ctx,
			`DELETE FROM cerebro_credential WHERE workspace_id = $1`,
			credentialsTestWorkspaceID)
	})
	return svc
}

// seedCredential creates one credential with AllowAllChecker so the test
// can later swap in a deny policy without having to bypass policy on
// the creation step itself (Create isn't part of the JEH-1197 governance
// set anyway).
func seedCredential(t *testing.T, svc *Service) cerebrodb.CerebroCredential {
	t.Helper()
	row, err := svc.Create(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	return row
}

// recordingChecker captures every PolicyRequest the service sends so
// tests can assert the audit metadata and request shape.
type recordingChecker struct {
	allow    bool
	reason   string
	requests []PolicyRequest
}

func (r *recordingChecker) Check(_ context.Context, req PolicyRequest) PolicyDecision {
	r.requests = append(r.requests, req)
	if r.allow {
		return Allow(r.reason)
	}
	return Deny(r.reason)
}

func TestService_PolicyEnforces_Reveal(t *testing.T) {
	allow := &recordingChecker{allow: true, reason: "test allow"}
	svc := newPolicyServiceTest(t, allow)
	row := seedCredential(t, svc)

	// Allow path: reveal succeeds.
	_, plaintext, err := svc.Reveal(context.Background(), row.WorkspaceID, row.ID, "member", credentialsTestUserID)
	if err != nil {
		t.Fatalf("Reveal (allow): %v", err)
	}
	if plaintext == "" {
		t.Fatalf("Reveal returned empty plaintext")
	}
	// Audit should now have an allow row for reveal.
	got := fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionReveal) || got.Result != AuditResultAllow {
		t.Fatalf("expected reveal allow audit row, got action=%q result=%q reason=%q", got.Action, got.Result, got.Reason)
	}

	// Swap to deny policy.
	deny := &recordingChecker{allow: false, reason: "no grant"}
	svc.Policy = deny
	_, _, err = svc.Reveal(context.Background(), row.WorkspaceID, row.ID, "member", credentialsTestUserID)
	if err == nil {
		t.Fatalf("Reveal under deny policy should have errored")
	}
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("expected ErrPolicyDenied, got %v", err)
	}

	// Deny audit row was written.
	got = fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionReveal) || got.Result != AuditResultDeny {
		t.Fatalf("expected reveal deny audit, got action=%q result=%q", got.Action, got.Result)
	}
	if got.Reason != deny.reason {
		t.Fatalf("expected deny reason %q in audit, got %q", deny.reason, got.Reason)
	}

	// PolicyRequest carried the correct shape.
	if len(deny.requests) != 1 {
		t.Fatalf("expected 1 deny check, got %d", len(deny.requests))
	}
	req := deny.requests[0]
	if req.Permission != PermReveal {
		t.Errorf("expected permission %q, got %q", PermReveal, req.Permission)
	}
	if Type(row.Type) != req.CredentialType {
		t.Errorf("expected credential_type %q, got %q", row.Type, req.CredentialType)
	}
}

func TestService_PolicyEnforces_Rotate(t *testing.T) {
	allow := &recordingChecker{allow: true}
	svc := newPolicyServiceTest(t, allow)
	row := seedCredential(t, svc)

	svc.Policy = &recordingChecker{allow: false, reason: "rotate denied"}
	_, err := svc.Rotate(context.Background(), row.WorkspaceID, row.ID, "member", credentialsTestUserID, "new-secret-value-x")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Rotate: expected ErrPolicyDenied, got %v", err)
	}
	got := fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionRotate) || got.Result != AuditResultDeny {
		t.Fatalf("expected rotate deny audit, got action=%q result=%q", got.Action, got.Result)
	}
}

func TestService_PolicyEnforces_Delete(t *testing.T) {
	allow := &recordingChecker{allow: true}
	svc := newPolicyServiceTest(t, allow)
	row := seedCredential(t, svc)

	svc.Policy = &recordingChecker{allow: false, reason: "revoke denied"}
	_, err := svc.Delete(context.Background(), row.WorkspaceID, row.ID, "member", credentialsTestUserID)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Delete: expected ErrPolicyDenied, got %v", err)
	}
	// Confirm the credential is still alive — deny must short-circuit before delete.
	if _, err := svc.Get(context.Background(), row.WorkspaceID, row.ID); err != nil {
		t.Fatalf("credential should still exist after denied revoke, Get err = %v", err)
	}
	got := fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionDelete) || got.Result != AuditResultDeny {
		t.Fatalf("expected delete deny audit, got action=%q result=%q", got.Action, got.Result)
	}
}

func TestService_PolicyEnforces_AttachBinding(t *testing.T) {
	allow := &recordingChecker{allow: true}
	svc := newPolicyServiceTest(t, allow)
	row := seedCredential(t, svc)

	svc.Policy = &recordingChecker{allow: false, reason: "attach denied"}
	_, err := svc.CreateBinding(context.Background(), row.WorkspaceID, row.ID, "project", row.WorkspaceID, "member", credentialsTestUserID)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("CreateBinding: expected ErrPolicyDenied, got %v", err)
	}
	got := fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionBind) || got.Result != AuditResultDeny {
		t.Fatalf("expected bind deny audit, got action=%q result=%q", got.Action, got.Result)
	}
}

func TestService_PolicyEnforces_AuthorizeRead(t *testing.T) {
	allow := &recordingChecker{allow: true}
	svc := newPolicyServiceTest(t, allow)
	row := seedCredential(t, svc)

	if err := svc.AuthorizeRead(context.Background(), row.WorkspaceID, row.ID, Type(row.Type), "member", credentialsTestUserID); err != nil {
		t.Fatalf("AuthorizeRead under allow: %v", err)
	}
	// Allow read does NOT write an audit row — confirm latest audit is
	// still the create row from seedCredential.
	got := fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionCreate) {
		t.Fatalf("AuthorizeRead allow should not write an audit row; latest was %q", got.Action)
	}

	svc.Policy = &recordingChecker{allow: false, reason: "no read grant"}
	err := svc.AuthorizeRead(context.Background(), row.WorkspaceID, row.ID, Type(row.Type), "member", credentialsTestUserID)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("AuthorizeRead under deny: expected ErrPolicyDenied, got %v", err)
	}
	got = fetchLatestAudit(t, row.ID)
	if got.Action != string(ActionRead) || got.Result != AuditResultDeny {
		t.Fatalf("expected read deny audit, got action=%q result=%q", got.Action, got.Result)
	}
}

// TestService_PolicyDefault_IsDeny verifies that a freshly-constructed
// Service without WithPolicy denies-by-default — the safe wiring posture
// required by JEH-1197's acceptance criteria.
func TestService_PolicyDefault_IsDeny(t *testing.T) {
	if credentialsTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping credential policy integration test")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	svc := NewService(cerebrodb.New(credentialsTestPool), cipher, events.New())
	// Seed using AllowAllChecker scope, then revert to default (DenyAll).
	svcAllow := svc.WithPolicy(AllowAllChecker)
	row, err := svcAllow.Create(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = credentialsTestPool.Exec(context.Background(),
			`DELETE FROM cerebro_credential WHERE workspace_id = $1`,
			credentialsTestWorkspaceID)
	})

	_, _, err = svc.Reveal(context.Background(), row.WorkspaceID, row.ID, "member", credentialsTestUserID)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("default policy should deny reveal, got err=%v", err)
	}
}

// fetchLatestAudit returns the most-recent audit row for a credential.
// Used by the policy integration tests to verify allow/deny rows and
// reasons land on the right schema columns.
func fetchLatestAudit(t *testing.T, credentialID pgtype.UUID) cerebrodb.CerebroCredentialAudit {
	t.Helper()
	rows, err := cerebrodb.New(credentialsTestPool).ListCerebroCredentialAudit(context.Background(), cerebrodb.ListCerebroCredentialAuditParams{
		CredentialID: credentialID,
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("ListCerebroCredentialAudit: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("no audit rows for credential %v", credentialID)
	}
	return rows[0]
}
