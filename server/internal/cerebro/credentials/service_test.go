package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

const (
	credentialsTestWorkspaceSlug = "credential-tests"
	credentialsTestUserEmail     = "credential-tests@multica.ai"
	credentialsTestUserName      = "Credential Tests"
	credentialsTestPrefix        = "CRD"
)

var (
	credentialsTestPool        *pgxpool.Pool
	credentialsTestWorkspaceID pgtype.UUID
	credentialsTestUserID      pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Common dev fallbacks; running tests without a DB is fine — every
		// integration test skips via newServiceTest when the pool is nil.
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("credentials: skipping integration tests, no DB: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("credentials: skipping integration tests, DB unreachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupCredentialFixture(ctx, pool); err != nil {
		fmt.Printf("credentials: pre-cleanup failed: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		credentialsTestUserName, credentialsTestUserEmail,
	).Scan(&credentialsTestUserID); err != nil {
		fmt.Printf("credentials: create user: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Credential Tests", credentialsTestWorkspaceSlug, "credentials test", credentialsTestPrefix,
	).Scan(&credentialsTestWorkspaceID); err != nil {
		fmt.Printf("credentials: create workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	credentialsTestPool = pool

	code := m.Run()

	if err := cleanupCredentialFixture(context.Background(), pool); err != nil {
		fmt.Printf("credentials: teardown failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func cleanupCredentialFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, credentialsTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, credentialsTestUserEmail); err != nil {
		return err
	}
	return nil
}

// newServiceTest builds a fresh Service with a real cipher and an isolated
// event bus. Each call wipes credential rows from the test workspace so
// tests start clean.
func newServiceTest(t *testing.T) (*Service, *eventRecorder) {
	t.Helper()
	if credentialsTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping credential integration test")
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
	rec := newEventRecorder(bus)
	// JEH-1197: existing tests focus on encryption/audit/bindings — they
	// pre-date policy enforcement and don't insert a member row, so we
	// give them AllowAllChecker. Policy-specific behaviour is covered in
	// policy_test.go (TestService_PolicyEnforces*).
	svc := NewService(cerebrodb.New(credentialsTestPool), cipher, bus).WithPolicy(AllowAllChecker)

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = credentialsTestPool.Exec(ctx,
			`DELETE FROM cerebro_credential WHERE workspace_id = $1`,
			credentialsTestWorkspaceID)
	})
	return svc, rec
}

type eventRecorder struct {
	mu     sync.Mutex
	events []events.Event
}

func newEventRecorder(bus *events.Bus) *eventRecorder {
	r := &eventRecorder{}
	bus.SubscribeAll(func(ev events.Event) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, ev)
	})
	return r
}

func (r *eventRecorder) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, ev := range r.events {
		out[i] = ev.Type
	}
	return out
}

func validCreateInput() CreateInput {
	return CreateInput{
		WorkspaceID: credentialsTestWorkspaceID,
		Type:        TypeAPIKey,
		Name:        "stripe-prod",
		Description: "stripe production key",
		Value:       "sk_live_secret_that_must_not_be_stored_plaintext",
		Metadata:    json.RawMessage(`{"env":"prod"}`),
		ActorType:   "member",
		ActorID:     credentialsTestUserID,
	}
}

// TestService_CreateStoresEncrypted_RetrievesMasked is the load-bearing test
// for JEH-1196: the plaintext must NEVER be observable in the DB after
// create. We hit the raw table to assert this directly.
func TestService_CreateStoresEncrypted_RetrievesMasked(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	in := validCreateInput()
	row, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.ValueHint == in.Value {
		t.Fatal("value_hint must not equal plaintext")
	}
	if !bytes.Equal(row.EncryptedValue, []byte(in.Value)) {
		// The byte arrays should differ — go on.
	} else {
		t.Fatal("encrypted_value bytes equal plaintext; encryption did not run")
	}

	// Raw DB probe: assert no row anywhere in the table carries the
	// plaintext. We cast the bytea column to text via encode(...,'escape')
	// so a single text-typed position() check covers every column.
	var hits int
	if err := credentialsTestPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM cerebro_credential
		WHERE workspace_id = $1
		  AND (position($2 in encode(encrypted_value, 'escape')) > 0
		       OR position($2 in value_hint) > 0
		       OR position($2 in description) > 0
		       OR position($2 in name) > 0)`,
		credentialsTestWorkspaceID, in.Value,
	).Scan(&hits); err != nil {
		t.Fatalf("raw probe: %v", err)
	}
	if hits != 0 {
		t.Fatalf("plaintext leaked into DB row(s): %d", hits)
	}
}

func TestService_CreateValidations(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	cases := []struct {
		name string
		mut  func(*CreateInput)
		want error
	}{
		{"bad-type", func(in *CreateInput) { in.Type = Type("nope") }, ErrInvalidType},
		{"empty-name", func(in *CreateInput) { in.Name = "  " }, ErrInvalidName},
		{"empty-value", func(in *CreateInput) { in.Value = "  " }, ErrInvalidValue},
		{"bad-metadata", func(in *CreateInput) { in.Metadata = json.RawMessage(`[1,2]`) }, ErrInvalidMetadata},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCreateInput()
			tc.mut(&in)
			if _, err := svc.Create(ctx, in); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestService_CreateDuplicateConflicts(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, validCreateInput()); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := svc.Create(ctx, validCreateInput()); !errors.Is(err, ErrCredentialExists) {
		t.Fatalf("expected ErrCredentialExists on duplicate, got %v", err)
	}
}

func TestService_AllTypesAccepted(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	all := []Type{
		TypeRepoDeployKey, TypeMCPBearer, TypeAPIKey, TypeGCPCredentials,
		TypeWebhookSecret, TypeSSOCert, TypeOAuthToken, TypeObjectStorageKey,
	}
	for _, ty := range all {
		in := validCreateInput()
		in.Type = ty
		in.Name = "cred-" + string(ty)
		if _, err := svc.Create(ctx, in); err != nil {
			t.Fatalf("Create(%s): %v", ty, err)
		}
	}
	list, err := svc.List(ctx, credentialsTestWorkspaceID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != len(all) {
		t.Fatalf("List: got %d, want %d", len(list), len(all))
	}
}

func TestService_RevealReturnsPlaintextAndAudits(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	in := validCreateInput()
	row, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, plaintext, err := svc.Reveal(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if plaintext != in.Value {
		t.Fatalf("Reveal: got %q want %q", plaintext, in.Value)
	}
	// Audit row must include both 'create' (from Create) and 'reveal'
	// (from Reveal). We don't pin ordering because Create's audit is
	// fire-and-forget on a goroutine-friendly path.
	audit, err := svc.ListAudit(ctx, credentialsTestWorkspaceID, row.ID, 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if !containsAction(audit, ActionCreate) {
		t.Fatalf("audit missing create: %+v", audit)
	}
	if !containsAction(audit, ActionReveal) {
		t.Fatalf("audit missing reveal: %+v", audit)
	}
}

func TestService_RevealCrossWorkspaceReturnsNotFound(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()
	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var alien pgtype.UUID
	alien.Valid = true
	for i := range alien.Bytes {
		alien.Bytes[i] = 0xee
	}
	if _, _, err := svc.Reveal(ctx, alien, row.ID, "member", credentialsTestUserID); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", err)
	}
}

func TestService_UpdateMetadataPreservesValue(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newName := "stripe-prod-renamed"
	newMeta := json.RawMessage(`{"env":"prod","note":"renamed"}`)
	updated, err := svc.Update(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID, UpdateInput{
		Name:     &newName,
		Metadata: newMeta,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("Update: name = %q want %q", updated.Name, newName)
	}
	if !bytes.Equal(updated.EncryptedValue, row.EncryptedValue) {
		t.Fatal("Update: encrypted_value changed; metadata update should preserve it")
	}

	// Reveal still returns the original plaintext.
	_, pt, err := svc.Reveal(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if pt != "sk_live_secret_that_must_not_be_stored_plaintext" {
		t.Fatalf("Reveal after Update: got %q", pt)
	}
}

func TestService_RotateReplacesValueAndBumpsTimestamp(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.LastRotatedAt.Valid {
		t.Fatal("freshly created row should have last_rotated_at NULL")
	}

	rotated, err := svc.Rotate(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID, "sk_live_replacement_value")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !rotated.LastRotatedAt.Valid {
		t.Fatal("Rotate: last_rotated_at should be set")
	}
	if bytes.Equal(rotated.EncryptedValue, row.EncryptedValue) {
		t.Fatal("Rotate: encrypted_value did not change")
	}
	_, pt, err := svc.Reveal(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if pt != "sk_live_replacement_value" {
		t.Fatalf("Reveal after Rotate: got %q", pt)
	}
}

func TestService_BindingsLifecycle(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	resourceID := credentialsTestWorkspaceID // bind to the workspace itself for the test
	binding, err := svc.CreateBinding(ctx, credentialsTestWorkspaceID, row.ID, "workspace", resourceID, "member", credentialsTestUserID)
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if binding.ResourceType != "workspace" {
		t.Fatalf("CreateBinding: resource_type = %q", binding.ResourceType)
	}
	// Re-binding the same (credential, resource) is idempotent — UPSERT.
	if _, err := svc.CreateBinding(ctx, credentialsTestWorkspaceID, row.ID, "workspace", resourceID, "member", credentialsTestUserID); err != nil {
		t.Fatalf("CreateBinding (repeat): %v", err)
	}

	list, err := svc.ListBindings(ctx, credentialsTestWorkspaceID, row.ID)
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListBindings: len = %d want 1", len(list))
	}

	if _, err := svc.DeleteBinding(ctx, credentialsTestWorkspaceID, row.ID, binding.ID, "member", credentialsTestUserID); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}

	list, err = svc.ListBindings(ctx, credentialsTestWorkspaceID, row.ID)
	if err != nil {
		t.Fatalf("ListBindings (post-delete): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListBindings (post-delete): len = %d want 0", len(list))
	}
}

func TestService_DeleteCascadesBindings(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()

	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.CreateBinding(ctx, credentialsTestWorkspaceID, row.ID, "workspace", credentialsTestWorkspaceID, "member", credentialsTestUserID); err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}

	if _, err := svc.Delete(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Bindings table should now be empty for this credential — ON DELETE
	// CASCADE in the migration is the contract.
	var n int
	if err := credentialsTestPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cerebro_credential_binding WHERE credential_id = $1`,
		row.ID,
	).Scan(&n); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected cascade to delete bindings; %d remain", n)
	}
}

func TestService_PublishesEvents(t *testing.T) {
	svc, rec := newServiceTest(t)
	ctx := context.Background()

	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := svc.Reveal(ctx, credentialsTestWorkspaceID, row.ID, "member", credentialsTestUserID); err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	// Bus is synchronous — events.Bus.Publish calls subscribers inline
	// (events/bus.go). A short wait is still polite in case that
	// assumption ever changes.
	time.Sleep(10 * time.Millisecond)

	got := rec.types()
	wantContains := []string{EventCredentialCreated, EventCredentialRevealed}
	for _, want := range wantContains {
		found := false
		for _, t := range got {
			if t == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing event %q; got %v", want, got)
		}
	}
}

func TestService_GetReturnsNotFoundForWrongWorkspace(t *testing.T) {
	svc, _ := newServiceTest(t)
	ctx := context.Background()
	row, err := svc.Create(ctx, validCreateInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var other pgtype.UUID
	other.Valid = true
	for i := range other.Bytes {
		other.Bytes[i] = 0xaa
	}
	if _, err := svc.Get(ctx, other, row.ID); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", err)
	}
}

func TestService_CipherMissingSurfacesError(t *testing.T) {
	if credentialsTestPool == nil {
		t.Skip("DATABASE_URL not configured")
	}
	bus := events.New()
	svc := NewService(cerebrodb.New(credentialsTestPool), nil, bus)
	_, err := svc.Create(context.Background(), validCreateInput())
	if !errors.Is(err, ErrCipherMissing) {
		t.Fatalf("expected ErrCipherMissing, got %v", err)
	}
}

func containsAction(audit []cerebrodb.CerebroCredentialAudit, action Action) bool {
	for _, a := range audit {
		if a.Action == string(action) {
			return true
		}
	}
	return false
}

