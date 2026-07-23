package rounds

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// names returns the caller's rounds in list order.
func (f *reviewFixture) names(t *testing.T) []string {
	t.Helper()
	rounds, err := f.svc.List(context.Background(), f.wsID, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rounds))
	for _, r := range rounds {
		out = append(out, r.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newRound creates an extra round for the fixture owner and returns its id.
func (f *reviewFixture) newRound(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	round, err := f.svc.Create(context.Background(), f.wsID, f.ownerID, name)
	if err != nil {
		t.Fatal(err)
	}
	return mustUUID(t, round.ID)
}

func TestReorderWritesTheCallersRoundOrder(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	first := f.roundID // "Review", created by the fixture
	second := f.newRound(t, "Second")
	third := f.newRound(t, "Third")

	if got := f.names(t); !equal(got, []string{"Review", "Second", "Third"}) {
		t.Fatalf("initial order = %v, want creation order", got)
	}

	rounds, err := f.svc.Reorder(ctx, f.wsID, f.ownerID, []pgtype.UUID{third, first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 3 || rounds[0].Name != "Third" || rounds[1].Name != "Review" || rounds[2].Name != "Second" {
		t.Fatalf("reorder returned %+v, want Third/Review/Second", rounds)
	}
	if got := f.names(t); !equal(got, []string{"Third", "Review", "Second"}) {
		t.Fatalf("persisted order = %v, want Third/Review/Second", got)
	}

	// A round created after a manual sort lands last, not first.
	f.newRound(t, "Fourth")
	if got := f.names(t); !equal(got, []string{"Third", "Review", "Second", "Fourth"}) {
		t.Fatalf("order after create = %v, want the new round last", got)
	}
}

func TestReorderRejectsRoundsTheCallerDoesNotOwn(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	other := f.newRound(t, "Second")

	var strangerID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Round Stranger', 'round-stranger-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&strangerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, strangerID) })
	stranger, err := f.svc.Create(ctx, f.wsID, strangerID, "Stranger round")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Reorder(ctx, f.wsID, f.ownerID, []pgtype.UUID{mustUUID(t, stranger.ID), f.roundID, other}); err == nil {
		t.Fatal("reorder accepted a round owned by someone else")
	}
	if got := f.names(t); !equal(got, []string{"Review", "Second"}) {
		t.Fatalf("order after rejected reorder = %v, want the original order", got)
	}

	if _, err := f.svc.Reorder(ctx, f.wsID, f.ownerID, []pgtype.UUID{f.roundID, f.roundID}); err == nil {
		t.Fatal("reorder accepted a duplicate round id")
	}
	if _, err := f.svc.Reorder(ctx, f.wsID, f.ownerID, nil); err == nil {
		t.Fatal("reorder accepted an empty order")
	}
}
