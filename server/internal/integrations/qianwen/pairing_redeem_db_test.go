package qianwen

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type pairingRedeemLedgerCounts struct {
	attempts int
	nonces   int
	terminal int
}

func TestRedeemPairingCodeInvalidOutcomeIsStableAcrossProviderRetries(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	cleanupPairingRedeemLedger(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture.service.now = func() time.Time { return now }

	request := newInvalidPairingRedeemRequest(now, 1, 1)
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingCodeInvalid) {
		t.Fatalf("first RedeemPairingCode() error = %v, want ErrPairingCodeInvalid", err)
	}

	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingCodeInvalid) {
		t.Fatalf("exact retry RedeemPairingCode() error = %v, want stable ErrPairingCodeInvalid outcome", err)
	}

	request.Identity.Timestamp = fmt.Sprint(now.Add(time.Second).UnixMilli())
	request.Identity.Nonce = fmt.Sprintf("%032d", 2)
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingCodeInvalid) {
		t.Fatalf("fresh timestamp/nonce retry RedeemPairingCode() error = %v, want stable ErrPairingCodeInvalid outcome", err)
	}

	assertPairingRedeemLedgerCounts(t, ctx, fixture, pairingRedeemLedgerCounts{
		attempts: 1,
		nonces:   1,
		terminal: 1,
	})
}

func TestRedeemPairingCodeIdentityLimitPersistsFifthFailureAndRejectsSixthWithoutWrites(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	cleanupPairingRedeemLedger(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture.service.now = func() time.Time { return now }

	for attempt := 1; attempt <= 5; attempt++ {
		request := newInvalidPairingRedeemRequest(now, 1, attempt)
		signPairingRedeemRequest(fixture.installation.AccessToken, &request)
		if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingCodeInvalid) {
			t.Fatalf("RedeemPairingCode() identity attempt %d error = %v, want ErrPairingCodeInvalid", attempt, err)
		}
	}

	boundary := pairingRedeemLedgerCounts{attempts: 5, nonces: 5, terminal: 5}
	assertPairingRedeemLedgerCounts(t, ctx, fixture, boundary)

	sixth := newInvalidPairingRedeemRequest(now, 1, 6)
	signPairingRedeemRequest(fixture.installation.AccessToken, &sixth)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, sixth); !errors.Is(err, ErrPairingRateLimited) {
		t.Fatalf("RedeemPairingCode() identity attempt 6 error = %v, want ErrPairingRateLimited", err)
	}
	assertPairingRedeemLedgerCounts(t, ctx, fixture, boundary)
}

func TestRedeemPairingCodeInstallationLimitPersistsTwentiethFailureAndRejectsTwentyFirstWithoutWrites(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	cleanupPairingRedeemLedger(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture.service.now = func() time.Time { return now }

	for attempt := 1; attempt <= 20; attempt++ {
		request := newInvalidPairingRedeemRequest(now, attempt, attempt)
		signPairingRedeemRequest(fixture.installation.AccessToken, &request)
		if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingCodeInvalid) {
			t.Fatalf("RedeemPairingCode() installation attempt %d error = %v, want ErrPairingCodeInvalid", attempt, err)
		}
	}

	boundary := pairingRedeemLedgerCounts{attempts: 20, nonces: 20, terminal: 20}
	assertPairingRedeemLedgerCounts(t, ctx, fixture, boundary)

	twentyFirst := newInvalidPairingRedeemRequest(now, 21, 21)
	signPairingRedeemRequest(fixture.installation.AccessToken, &twentyFirst)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, twentyFirst); !errors.Is(err, ErrPairingRateLimited) {
		t.Fatalf("RedeemPairingCode() installation attempt 21 error = %v, want ErrPairingRateLimited", err)
	}
	assertPairingRedeemLedgerCounts(t, ctx, fixture, boundary)
}

func newInvalidPairingRedeemRequest(now time.Time, identity, attempt int) PairingRedeemRequest {
	return PairingRedeemRequest{
		Code: fmt.Sprintf("%08d", 70_000_000+attempt),
		Identity: InvocationMetadata{
			OpenUserID: fmt.Sprintf("opaque-user-%d", identity),
			OpenUUID:   fmt.Sprintf("opaque-uuid-%d", identity),
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      fmt.Sprintf("%032d", attempt),
		},
	}
}

func cleanupPairingRedeemLedger(t *testing.T, fixture *qianwenServiceDBFixture) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := fixture.pool.Exec(ctx,
			`DELETE FROM qianwen_invocation_nonce WHERE installation_id = $1`,
			fixture.installation.Installation.ID,
		); err != nil {
			t.Errorf("cleanup qianwen invocation nonces: %v", err)
		}
		if _, err := fixture.pool.Exec(ctx,
			`DELETE FROM qianwen_pairing_attempt WHERE installation_id = $1`,
			fixture.installation.Installation.ID,
		); err != nil {
			t.Errorf("cleanup qianwen pairing attempts: %v", err)
		}
	})
}

func assertPairingRedeemLedgerCounts(
	t *testing.T,
	ctx context.Context,
	fixture *qianwenServiceDBFixture,
	want pairingRedeemLedgerCounts,
) {
	t.Helper()
	var got pairingRedeemLedgerCounts
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM qianwen_pairing_attempt WHERE installation_id = $1),
			count(*),
			count(*) FILTER (WHERE outcome IS NOT NULL)
		FROM qianwen_invocation_nonce
		WHERE installation_id = $1
	`, fixture.installation.Installation.ID).Scan(&got.attempts, &got.nonces, &got.terminal); err != nil {
		t.Fatalf("count Qianwen pairing failure ledger: %v", err)
	}
	if got != want {
		t.Fatalf("pairing failure ledger counts = %+v, want %+v", got, want)
	}
}
