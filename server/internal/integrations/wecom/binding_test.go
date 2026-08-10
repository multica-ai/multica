package wecom

// binding_test.go — minting a binding token is a write, and it happened once
// per inbound message from an unbound user. Someone who types six lines at a
// bot they have not linked yet wrote six rows and got six links, only the
// last of which they would ever click.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func bindingUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

// fakeMintQueries stands in for channel_binding_token, applying the same
// predicates FindLiveChannelBindingToken does so the throttle is exercised
// against the real filter rather than a stub that always answers.
type fakeMintQueries struct {
	rows    []db.ChannelBindingToken
	creates int
	now     func() time.Time
}

func (f *fakeMintQueries) CreateChannelBindingToken(_ context.Context, arg db.CreateChannelBindingTokenParams) (db.ChannelBindingToken, error) {
	f.creates++
	row := db.ChannelBindingToken{
		TokenHash:      arg.TokenHash,
		WorkspaceID:    arg.WorkspaceID,
		InstallationID: arg.InstallationID,
		ChannelType:    arg.ChannelType,
		ChannelUserID:  arg.ChannelUserID,
		ExpiresAt:      arg.ExpiresAt,
		CreatedAt:      pgtype.Timestamptz{Time: f.now(), Valid: true},
	}
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeMintQueries) FindLiveChannelBindingToken(_ context.Context, arg db.FindLiveChannelBindingTokenParams) (db.ChannelBindingToken, error) {
	var best db.ChannelBindingToken
	found := false
	for _, r := range f.rows {
		if r.InstallationID != arg.InstallationID || r.ChannelType != arg.ChannelType || r.ChannelUserID != arg.ChannelUserID {
			continue
		}
		if r.ConsumedAt.Valid {
			continue
		}
		if !r.ExpiresAt.Time.After(f.now()) {
			continue
		}
		// The query derives its cutoff from now() on the database side, so
		// the fake resolves the window against the same clock it stamps
		// created_at with rather than against a caller-supplied timestamp.
		cutoff := f.now().Add(-time.Duration(arg.MintInterval.Microseconds) * time.Microsecond)
		if r.CreatedAt.Time.Before(cutoff) {
			continue
		}
		if !found || r.CreatedAt.Time.After(best.CreatedAt.Time) {
			best, found = r, true
		}
	}
	if !found {
		return db.ChannelBindingToken{}, pgx.ErrNoRows
	}
	return best, nil
}

func newThrottledService() (*BindingTokenService, *fakeMintQueries, *time.Time) {
	clock := time.Now()
	fake := &fakeMintQueries{now: func() time.Time { return clock }}
	svc := &BindingTokenService{mint: fake, now: func() time.Time { return clock }}
	return svc, fake, &clock
}

// TestMintReusesTheLinkAlreadySent: the second message inside the window must
// not write another row. Fails on the pre-change Mint, which inserted every
// time it was called.
func TestMintReusesTheLinkAlreadySent(t *testing.T) {
	svc, fake, _ := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	first, err := svc.Mint(context.Background(), ws, inst, "T-alex")
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}
	if first.Reused {
		t.Fatal("the first mint cannot be a reuse")
	}
	if first.Raw == "" {
		t.Fatal("the first mint must return a raw token")
	}

	for i := 0; i < 5; i++ {
		again, err := svc.Mint(context.Background(), ws, inst, "T-alex")
		if err != nil {
			t.Fatalf("mint #%d: %v", i+2, err)
		}
		if !again.Reused {
			t.Fatalf("mint #%d minted a second token inside the throttle window", i+2)
		}
		if again.Raw != "" {
			t.Fatal("a reused token cannot hand back a raw secret it never stored")
		}
		if !again.ExpiresAt.Equal(first.ExpiresAt) {
			t.Fatalf("reuse reported expiry %v, want the live token's %v", again.ExpiresAt, first.ExpiresAt)
		}
	}
	if fake.creates != 1 {
		t.Fatalf("six messages wrote %d token rows, want 1", fake.creates)
	}
}

// TestMintIssuesAFreshLinkAfterTheWindow: the throttle must not lock a user
// out — once the window passes they get a new link.
func TestMintIssuesAFreshLinkAfterTheWindow(t *testing.T) {
	svc, fake, clock := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "T-alex"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	*clock = clock.Add(BindingTokenMintInterval + time.Second)

	again, err := svc.Mint(context.Background(), ws, inst, "T-alex")
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if again.Reused || again.Raw == "" {
		t.Fatal("past the throttle window the user must get a fresh link")
	}
	if fake.creates != 2 {
		t.Fatalf("%d rows written, want 2", fake.creates)
	}
}

// TestMintStillReusesPartWayThroughTheWindow pins the width of the window
// itself. The two tests above only exercise its edges — same instant, and
// past it — which a zero-length interval satisfies just as well, so neither
// would notice if Mint stopped passing a real one. Halfway through, a zero
// interval reads as expired and mints again.
func TestMintStillReusesPartWayThroughTheWindow(t *testing.T) {
	svc, fake, clock := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "T-alex"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	*clock = clock.Add(BindingTokenMintInterval / 2)

	again, err := svc.Mint(context.Background(), ws, inst, "T-alex")
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if !again.Reused {
		t.Fatal("a link half a window old is still the one to point back at")
	}
	if fake.creates != 1 {
		t.Fatalf("%d rows written, want 1", fake.creates)
	}
}

// TestMintIsPerUser: one user's pending link must not silence another's.
func TestMintIsPerUser(t *testing.T) {
	svc, fake, _ := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "T-alex"); err != nil {
		t.Fatalf("mint alex: %v", err)
	}
	dana, err := svc.Mint(context.Background(), ws, inst, "T-dana")
	if err != nil {
		t.Fatalf("mint dana: %v", err)
	}
	if dana.Reused || dana.Raw == "" {
		t.Fatal("a second user must get their own link")
	}
	if fake.creates != 2 {
		t.Fatalf("%d rows written, want 2", fake.creates)
	}
}

// TestMintIgnoresAConsumedToken: they bound, then unbound and came back.
func TestMintIgnoresAConsumedToken(t *testing.T) {
	svc, fake, _ := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "T-alex"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	fake.rows[0].ConsumedAt = pgtype.Timestamptz{Time: fake.now(), Valid: true}

	again, err := svc.Mint(context.Background(), ws, inst, "T-alex")
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if again.Reused || again.Raw == "" {
		t.Fatal("a consumed token must not suppress a new one")
	}
	if fake.creates != 2 {
		t.Fatalf("%d rows written, want 2", fake.creates)
	}
}

// TestMintThrottleWindowIsShorterThanTheTTL — a throttle at or past the TTL
// would leave a user with no valid link and no way to get one.
func TestMintThrottleWindowIsShorterThanTheTTL(t *testing.T) {
	if BindingTokenMintInterval >= BindingTokenTTL {
		t.Fatalf("throttle %v must stay inside the %v token TTL", BindingTokenMintInterval, BindingTokenTTL)
	}
}
