package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/binding"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// channelBindingTestProvider is the fixed provider value all binding
// integration tests use; we treat it as a magic string here so the
// CHECK (provider IN ('feishu')) constraint introduced in migration
// 070 is exercised end-to-end.
const channelBindingTestProvider = "feishu"

// freshBindToken seeds a unique (token_hash, plaintext) pair into the
// channel_bind_token table with the supplied lifetime. It returns the
// plaintext (caller passes it to Consume) and the inserted row's hash so
// the test can clean up deterministically.
//
// We bypass the production binding.TokenIssuer here on purpose: the
// integration tests must validate the SQL-level guarantees (RowsAffected,
// CASCADE, partial unique indexes) independently of the Go layer. If
// Issue() ever drifts away from the SQL contract we want the integration
// test to surface the discrepancy instead of hiding behind shared code.
func freshBindToken(t *testing.T, externalUserID string, ttl time.Duration) (plaintext string, hash []byte) {
	t.Helper()
	ctx := context.Background()

	rawBytes := make([]byte, 32)
	// Cheap deterministic seed: tests don't need cryptographic strength
	// here since we control both producer and consumer; what we DO need
	// is uniqueness across rows so the PK doesn't collide with a leaked
	// row from a sibling test. Time-based suffix gives us that without
	// pulling crypto/rand into the integration layer.
	now := time.Now().UnixNano()
	for i := range rawBytes {
		rawBytes[i] = byte(now >> (i % 8))
	}
	// Mix in the externalUserID so concurrent tests with the same clock
	// tick don't collide.
	h := sha256.New()
	h.Write([]byte(externalUserID))
	h.Write(rawBytes)
	rawBytes = h.Sum(nil)

	plaintext = base64.RawURLEncoding.EncodeToString(rawBytes)
	digest := sha256.Sum256([]byte(plaintext))
	hash = digest[:]

	queries := db.New(testPool)
	expires := pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true}
	if _, err := queries.CreateChannelBindToken(ctx, db.CreateChannelBindTokenParams{
		TokenHash:      hash,
		Purpose:        binding.PurposeUserIdentity,
		Provider:       channelBindingTestProvider,
		ConnectionID:   integrationTestChannelConnID,
		ExternalUserID: externalUserID,
		ExpiresAt:      expires,
	}); err != nil {
		t.Fatalf("seed channel_bind_token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_bind_token WHERE token_hash = $1`, hash)
	})
	return plaintext, hash
}

// TC-bind-4a · Token past expires_at is rejected at the SQL level.
//
// Per TestCase §3 TC-bind-4 sub-case 4a: insert a row with expires_at in
// the past, call ConsumeChannelBindToken, assert RowsAffected == 0. The
// production query has predicate "consumed_at IS NULL AND expires_at >
// now()" so an expired row never matches, regardless of consumed_at
// state.
func TestChannelBindToken_Consume_Expired_RejectedBySQL(t *testing.T) {
	if testPool == nil {
		t.Fatal("M1 acceptance test requires a real Postgres (DATABASE_URL); set up the dev DB before running T4 R1 / T8 tests")
	}
	plaintext, hash := freshBindToken(t, "ou_test_expired", -1*time.Second)

	queries := db.New(testPool)
	_, err := queries.ConsumeChannelBindToken(context.Background(), hash)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows for expired token, got %v", err)
	}

	// And the binding.TokenConsumer surfaces this as ErrTokenExpired
	// (the Go layer must distinguish "expired" from "already consumed"
	// by doing a follow-up SELECT — TestCase §3 TC-bind-4 explicitly
	// requires this).
	consumer := binding.NewTokenConsumer(queries)
	_, consumerErr := consumer.Consume(context.Background(), plaintext)
	if !errors.Is(consumerErr, binding.ErrTokenExpired) {
		t.Fatalf("TokenConsumer.Consume should return ErrTokenExpired for expired row, got %v", consumerErr)
	}
}

// TC-bind-4b · One-shot consumption: first consume succeeds, second
// fails with RowsAffected == 0.
//
// Per TestCase §3 TC-bind-4 sub-case 4b. Asserts at the SQL layer (the
// returned ChannelBindToken row's consumed_at is non-null after first
// consume) AND at the binding.TokenConsumer layer (second call returns
// ErrTokenAlreadyConsumed).
func TestChannelBindToken_Consume_OneShot(t *testing.T) {
	if testPool == nil {
		t.Fatal("M1 acceptance test requires a real Postgres (DATABASE_URL); set up the dev DB before running T4 R1 / T8 tests")
	}
	plaintext, hash := freshBindToken(t, "ou_test_oneshot", binding.DefaultTokenTTL)

	queries := db.New(testPool)

	// First consume: success path.
	row, err := queries.ConsumeChannelBindToken(context.Background(), hash)
	if err != nil {
		t.Fatalf("first ConsumeChannelBindToken: %v", err)
	}
	if !row.ConsumedAt.Valid {
		t.Fatalf("consumed_at must be Valid (NOT NULL) after successful consume")
	}
	if row.ExternalUserID != "ou_test_oneshot" {
		t.Fatalf("external_user_id mismatch: got %q", row.ExternalUserID)
	}

	// Second consume at the SQL level: query returns pgx.ErrNoRows
	// because the WHERE consumed_at IS NULL predicate filters out the
	// just-burned row.
	if _, err := queries.ConsumeChannelBindToken(context.Background(), hash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on second consume, got %v", err)
	}

	// Second consume through the binding.TokenConsumer: returns
	// ErrTokenAlreadyConsumed (the Go layer disambiguates by SELECTing
	// the row and checking consumed_at is non-null + expires_at is in
	// the future).
	consumer := binding.NewTokenConsumer(queries)
	_, err = consumer.Consume(context.Background(), plaintext)
	if !errors.Is(err, binding.ErrTokenAlreadyConsumed) {
		t.Fatalf("TokenConsumer.Consume second call should return ErrTokenAlreadyConsumed, got %v", err)
	}
}

// TC-risk-token-replay · 10 concurrent goroutines call Consume on the
// same token; exactly 1 succeeds.
//
// Per TestCase §11 TC-risk-token-replay. This is the strongest assertion
// in T4: it depends on the Postgres UPDATE ... WHERE consumed_at IS NULL
// being atomic at row level. If a future refactor accidentally splits
// the predicate from the UPDATE (e.g. "SELECT then UPDATE" race), this
// test will catch it.
func TestChannelBindToken_Consume_ReplayUnderConcurrency(t *testing.T) {
	if testPool == nil {
		t.Fatal("M1 acceptance test requires a real Postgres (DATABASE_URL); set up the dev DB before running T4 R1 / T8 tests")
	}
	plaintext, _ := freshBindToken(t, "ou_test_replay", binding.DefaultTokenTTL)

	const goroutines = 10
	queries := db.New(testPool)
	consumer := binding.NewTokenConsumer(queries)

	var (
		successes atomic.Int32
		alreadyC  atomic.Int32
		other     atomic.Int32
		ready     sync.WaitGroup
		start     = make(chan struct{})
		done      sync.WaitGroup
	)
	ready.Add(goroutines)
	done.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start // align all goroutines to fire as close as possible
			_, err := consumer.Consume(context.Background(), plaintext)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, binding.ErrTokenAlreadyConsumed),
				errors.Is(err, binding.ErrTokenInvalid):
				alreadyC.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected error from concurrent Consume: %v", err)
			}
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()

	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 success across %d concurrent consumes, got %d", goroutines, successes.Load())
	}
	if alreadyC.Load() != int32(goroutines-1) {
		t.Fatalf("expected %d ErrTokenAlreadyConsumed/ErrTokenInvalid, got %d (other=%d)",
			goroutines-1, alreadyC.Load(), other.Load())
	}
}
