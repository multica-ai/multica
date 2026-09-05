package sharecrm

// binding_test.go — minting a binding token is a write, and it used to happen
// once per inbound message from an unbound user. Someone who types six lines
// at a bot they have not linked yet wrote six rows and got six links, only the
// last of which they would ever click. Throttle port of wecom MUL-5880.

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

func TestMintReusesTheLinkAlreadySent(t *testing.T) {
	svc, fake, _ := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	first, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice")
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
		again, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice")
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

func TestMintIssuesAFreshLinkAfterTheWindow(t *testing.T) {
	svc, fake, clock := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	*clock = clock.Add(BindingTokenMintInterval + time.Second)

	again, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice")
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

func TestMintStillReusesPartWayThroughTheWindow(t *testing.T) {
	svc, fake, clock := newThrottledService()
	ws, inst := bindingUUID(1), bindingUUID(2)

	if _, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice"); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	*clock = clock.Add(BindingTokenMintInterval / 2)

	again, err := svc.Mint(context.Background(), ws, inst, "E.corp.alice")
	if err != nil {
		t.Fatalf("half-window mint: %v", err)
	}
	if !again.Reused {
		t.Fatal("halfway through the window the mint must reuse")
	}
	if fake.creates != 1 {
		t.Fatalf("%d rows written, want 1", fake.creates)
	}
}

func TestMintThrottleWindowIsShorterThanTheTTL(t *testing.T) {
	if BindingTokenMintInterval >= BindingTokenTTL {
		t.Fatalf("throttle %v must stay inside the %v token TTL", BindingTokenMintInterval, BindingTokenTTL)
	}
}

func TestMintDoesNotCrossUsersOrInstallations(t *testing.T) {
	svc, fake, _ := newThrottledService()
	ws, instA, instB := bindingUUID(1), bindingUUID(2), bindingUUID(3)

	if _, err := svc.Mint(context.Background(), ws, instA, "E.corp.alice"); err != nil {
		t.Fatalf("alice@A: %v", err)
	}
	bob, err := svc.Mint(context.Background(), ws, instA, "E.corp.bob")
	if err != nil {
		t.Fatalf("bob@A: %v", err)
	}
	if bob.Reused || bob.Raw == "" {
		t.Fatal("a different user must get their own link")
	}
	other, err := svc.Mint(context.Background(), ws, instB, "E.corp.alice")
	if err != nil {
		t.Fatalf("alice@B: %v", err)
	}
	if other.Reused || other.Raw == "" {
		t.Fatal("the same user on another installation must get a fresh link")
	}
	if fake.creates != 3 {
		t.Fatalf("%d rows written, want 3", fake.creates)
	}
}
