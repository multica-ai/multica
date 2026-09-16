package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/requestsecret"
	"github.com/multica-ai/multica/server/internal/util"
)

// stubVisitorCredentialProvider is a test double for the injectable issuer.
type stubVisitorCredentialProvider struct {
	secret string
	ok     bool
	err    error

	gotPrincipal pgtype.UUID
	gotTask      pgtype.UUID
	calls        int
}

func (p *stubVisitorCredentialProvider) VisitorCredential(_ context.Context, principal pgtype.UUID, task pgtype.UUID) (string, bool, error) {
	p.calls++
	p.gotPrincipal = principal
	p.gotTask = task
	return p.secret, p.ok, p.err
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

const (
	testPrincipalA = "11111111-1111-1111-1111-111111111111"
	testPrincipalB = "22222222-2222-2222-2222-222222222222"
	testTaskA      = "33333333-3333-3333-3333-333333333333"
)

// TestResolveRequestSecretRef_HappyPath_PrincipalIssueTaskRef is the core
// integration: an authenticated principal + a live provider + a real store
// yields an opaque handle that Consume returns ONLY for the same
// (principal, task) — the authenticated-principal → Issue → task-ref path.
func TestResolveRequestSecretRef_HappyPath_PrincipalIssueTaskRef(t *testing.T) {
	store := requestsecret.New(0)
	prov := &stubVisitorCredentialProvider{secret: "s3cr3t-jwt", ok: true}
	svc := &TaskService{VisitorCredentials: prov, RequestSecrets: store}

	principal := mustUUID(t, testPrincipalA)
	task := mustUUID(t, testTaskA)

	ref := svc.ResolveRequestSecretRef(context.Background(), principal, task)
	if ref == "" {
		t.Fatal("expected a non-empty opaque ref for an authenticated principal with a credential")
	}
	if ref == "s3cr3t-jwt" || strings.Contains(ref, "s3cr3t") {
		t.Fatalf("ref must be an opaque handle, not the secret: %q", ref)
	}
	// Provider must have been called with EXACTLY the authenticated principal
	// and the bound task — not any browser- or self-reported value.
	if prov.gotPrincipal != principal || prov.gotTask != task {
		t.Fatalf("provider bound to wrong (principal, task): got (%v,%v)", prov.gotPrincipal, prov.gotTask)
	}
	// The issuer is called EXACTLY once per claim — no double-mint, no probe.
	if prov.calls != 1 {
		t.Fatalf("provider must be called exactly once per claim, got %d", prov.calls)
	}

	// The minted handle resolves back to the secret ONLY for the same binding.
	got, err := store.Consume(ref, util.UUIDToString(principal), util.UUIDToString(task))
	if err != nil {
		t.Fatalf("Consume for the issuing principal/task should succeed: %v", err)
	}
	if got != "s3cr3t-jwt" {
		t.Fatalf("Consume returned wrong secret: %q", got)
	}
}

// TestResolveRequestSecretRef_CrossUserRejected proves the minted handle is
// bound to the authenticated principal: another user cannot consume it.
func TestResolveRequestSecretRef_CrossUserRejected(t *testing.T) {
	store := requestsecret.New(0)
	prov := &stubVisitorCredentialProvider{secret: "s3cr3t-jwt", ok: true}
	svc := &TaskService{VisitorCredentials: prov, RequestSecrets: store}

	ref := svc.ResolveRequestSecretRef(context.Background(),
		mustUUID(t, testPrincipalA), mustUUID(t, testTaskA))
	if ref == "" {
		t.Fatal("expected a ref")
	}
	// A different principal must be rejected WITHOUT burning the handle.
	if _, err := store.Consume(ref, testPrincipalB, testTaskA); !errors.Is(err, requestsecret.ErrPrincipal) {
		t.Fatalf("cross-user consume should be ErrPrincipal, got %v", err)
	}
	// The legitimate principal can still consume (probe did not burn it).
	if _, err := store.Consume(ref, testPrincipalA, testTaskA); err != nil {
		t.Fatalf("legitimate consume after cross-user probe should succeed: %v", err)
	}
}

// TestResolveRequestSecretRef_FailClosed covers every degraded input: nil
// provider, nil store, empty principal/task, no-credential (ok=false), and a
// provider error. All must yield "" (no ref) and none may fall back to any
// other identity.
func TestResolveRequestSecretRef_FailClosed(t *testing.T) {
	principal := mustUUID(t, testPrincipalA)
	task := mustUUID(t, testTaskA)

	cases := []struct {
		name string
		svc  *TaskService
		p    pgtype.UUID
		tk   pgtype.UUID
	}{
		{
			name: "nil provider",
			svc:  &TaskService{RequestSecrets: requestsecret.New(0)},
			p:    principal, tk: task,
		},
		{
			name: "nil store",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "x", ok: true}},
			p:    principal, tk: task,
		},
		{
			name: "empty principal",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "x", ok: true}, RequestSecrets: requestsecret.New(0)},
			p:    pgtype.UUID{}, tk: task,
		},
		{
			name: "empty task",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "x", ok: true}, RequestSecrets: requestsecret.New(0)},
			p:    principal, tk: pgtype.UUID{},
		},
		{
			name: "provider has no credential (ok=false)",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{ok: false}, RequestSecrets: requestsecret.New(0)},
			p:    principal, tk: task,
		},
		{
			name: "provider returns empty secret",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "", ok: true}, RequestSecrets: requestsecret.New(0)},
			p:    principal, tk: task,
		},
		{
			name: "provider error",
			svc:  &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{err: errors.New("issuer down")}, RequestSecrets: requestsecret.New(0)},
			p:    principal, tk: task,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ref := tc.svc.ResolveRequestSecretRef(context.Background(), tc.p, tc.tk); ref != "" {
				t.Fatalf("fail-closed case %q must yield no ref, got %q", tc.name, ref)
			}
		})
	}
}

// TestResolveRequestSecretRef_NoProviderCallWhenUnbound ensures we don't even
// invoke the issuer when there is no authenticated principal — no probing an
// upstream on an unauthenticated/owner-scoped run.
func TestResolveRequestSecretRef_NoProviderCallWhenUnbound(t *testing.T) {
	prov := &stubVisitorCredentialProvider{secret: "x", ok: true}
	svc := &TaskService{VisitorCredentials: prov, RequestSecrets: requestsecret.New(0)}

	if ref := svc.ResolveRequestSecretRef(context.Background(), pgtype.UUID{}, mustUUID(t, testTaskA)); ref != "" {
		t.Fatalf("unbound principal must yield no ref, got %q", ref)
	}
	if prov.calls != 0 {
		t.Fatalf("issuer must not be called for an unbound principal, calls=%d", prov.calls)
	}
}

// TestResolveRequestSecretRef_ReplayRejected proves the minted handle is
// one-time: a second Consume of the same handle is rejected (no DB, in-memory
// consume-on-first-use).
func TestResolveRequestSecretRef_ReplayRejected(t *testing.T) {
	store := requestsecret.New(0)
	svc := &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "s", ok: true}, RequestSecrets: store}

	ref := svc.ResolveRequestSecretRef(context.Background(), mustUUID(t, testPrincipalA), mustUUID(t, testTaskA))
	if ref == "" {
		t.Fatal("expected a ref")
	}
	if _, err := store.Consume(ref, testPrincipalA, testTaskA); err != nil {
		t.Fatalf("first consume should succeed: %v", err)
	}
	if _, err := store.Consume(ref, testPrincipalA, testTaskA); !errors.Is(err, requestsecret.ErrNotFound) {
		t.Fatalf("replay should be ErrNotFound (one-time), got %v", err)
	}
	// Nothing persisted: after the single consume the store is empty.
	if n := store.Len(); n != 0 {
		t.Fatalf("store must hold no entry after one-time consume, len=%d", n)
	}
}

// TestResolveRequestSecretRef_ExpiryRejected proves an expired handle is
// rejected and swept — the TTL-cleanup path.
func TestResolveRequestSecretRef_ExpiryRejected(t *testing.T) {
	store := requestsecret.New(time.Minute)
	now := time.Unix(1_000_000, 0)
	store.SetClock(func() time.Time { return now })
	svc := &TaskService{VisitorCredentials: &stubVisitorCredentialProvider{secret: "s", ok: true}, RequestSecrets: store}

	ref := svc.ResolveRequestSecretRef(context.Background(), mustUUID(t, testPrincipalA), mustUUID(t, testTaskA))
	if ref == "" {
		t.Fatal("expected a ref")
	}
	now = now.Add(2 * time.Minute) // past TTL
	if _, err := store.Consume(ref, testPrincipalA, testTaskA); !errors.Is(err, requestsecret.ErrExpired) {
		t.Fatalf("expired consume should be ErrExpired, got %v", err)
	}
}

// TestClaimTimeMint_Semantics reproduces exactly what the claim path
// (buildClaimedTaskResponse) does — resp.RequestSecretRef =
// TaskService.ResolveRequestSecretRef(ctx, task.OriginatorUserID, task.ID) —
// asserting the two load-bearing outcomes without a DB:
//   - an identity task (provider yields a credential) gets an opaque handle
//     bound to the ORIGINATOR (not runtime owner), issuer called once;
//   - an ordinary non-identity task (provider says ok=false) gets an empty
//     ref and is otherwise untouched (zero regression).
func TestClaimTimeMint_Semantics(t *testing.T) {
	t.Run("identity task mints originator-bound handle", func(t *testing.T) {
		store := requestsecret.New(0)
		prov := &stubVisitorCredentialProvider{secret: "user-jwt", ok: true}
		svc := &TaskService{VisitorCredentials: prov, RequestSecrets: store}

		originator := mustUUID(t, testPrincipalA)
		taskID := mustUUID(t, testTaskA)

		// Mirror the claim-path call site exactly.
		ref := svc.ResolveRequestSecretRef(context.Background(), originator, taskID)

		if ref == "" || strings.Contains(ref, "user-jwt") {
			t.Fatalf("expected opaque non-secret handle, got %q", ref)
		}
		if prov.calls != 1 || prov.gotPrincipal != originator || prov.gotTask != taskID {
			t.Fatalf("issuer must be called once, bound to originator+task: calls=%d p=%v t=%v", prov.calls, prov.gotPrincipal, prov.gotTask)
		}
		if _, err := store.Consume(ref, testPrincipalA, testTaskA); err != nil {
			t.Fatalf("originator must be able to consume its handle: %v", err)
		}
	})

	t.Run("non-identity task: zero regression", func(t *testing.T) {
		store := requestsecret.New(0)
		prov := &stubVisitorCredentialProvider{ok: false} // no credential for this run
		svc := &TaskService{VisitorCredentials: prov, RequestSecrets: store}

		ref := svc.ResolveRequestSecretRef(context.Background(), mustUUID(t, testPrincipalA), mustUUID(t, testTaskA))
		if ref != "" {
			t.Fatalf("non-identity task must get empty ref, got %q", ref)
		}
		if store.Len() != 0 {
			t.Fatalf("non-identity task must mint nothing, store len=%d", store.Len())
		}
	})

	t.Run("no provider wired (default): every task empty ref", func(t *testing.T) {
		// The production default until an issuer is wired: VisitorCredentials
		// nil, store present. Must be a pure no-op for all tasks.
		svc := &TaskService{RequestSecrets: requestsecret.New(0)}
		ref := svc.ResolveRequestSecretRef(context.Background(), mustUUID(t, testPrincipalA), mustUUID(t, testTaskA))
		if ref != "" {
			t.Fatalf("nil provider must yield empty ref, got %q", ref)
		}
	})
}
