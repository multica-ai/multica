package mobilepush

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMobilePushTargetsRespectRecipientPreferencesMembershipAndExpiry(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture := testutil.New(pool, "", "")
	suffix := fmt.Sprint(time.Now().UnixNano())
	user := fixture.User(t, "Push test", "push-"+suffix+"@example.test")
	other := fixture.User(t, "Other push test", "other-push-"+suffix+"@example.test")
	ws := fixture.Workspace(t, "Push test", "push-"+suffix)
	member := fixture.Member(t, ws, user, "member")
	item := fixture.Insert(t, "inbox_item", testutil.Cols{"workspace_id": ws, "recipient_type": "member", "recipient_id": user, "type": "mentioned", "title": "Private title"})
	id, _ := util.ParseUUID(item)
	uid, _ := util.ParseUUID(user)
	otherID, _ := util.ParseUUID(other)
	wsID, _ := util.ParseUUID(ws)
	q := db.New(pool)
	token := "ExpoPushToken[test_" + suffix + "]"
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM mobile_push_subscription WHERE token=$1", token) })
	upsert := func(userID pgtype.UUID, expiry time.Time) {
		t.Helper()
		if err := q.UpsertMobilePushSubscription(ctx, db.UpsertMobilePushSubscriptionParams{Token: token, UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: expiry, Valid: true}}); err != nil {
			t.Fatal(err)
		}
	}
	wantTargets := func(count int) {
		t.Helper()
		targets, err := q.ListMobilePushTargets(ctx, id)
		if err != nil || len(targets) != count {
			t.Fatalf("targets=%v err=%v want=%d", targets, err, count)
		}
	}
	upsert(uid, time.Now().Add(time.Hour))
	wantTargets(1)
	if err := q.DeleteMobilePushSubscription(ctx, db.DeleteMobilePushSubscriptionParams{Token: token, UserID: otherID}); err != nil {
		t.Fatal(err)
	}
	wantTargets(1)
	upsert(uid, time.Now().Add(-time.Hour))
	wantTargets(0)
	upsert(uid, time.Now().Add(time.Hour))
	wantTargets(1)
	fixture.Insert(t, "notification_preference", testutil.Cols{"workspace_id": ws, "user_id": user, "preferences": `{"system_notifications":"muted"}`})
	wantTargets(0)
	if _, err := q.UpsertNotificationPreference(ctx, db.UpsertNotificationPreferenceParams{WorkspaceID: wsID, UserID: uid, Preferences: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	wantTargets(1)
	for _, column := range []string{"read", "archived"} {
		_, err = pool.Exec(ctx, "UPDATE inbox_item SET "+column+"=true WHERE id=$1", item)
		if err != nil {
			t.Fatal(err)
		}
		wantTargets(0)
		_, _ = pool.Exec(ctx, "UPDATE inbox_item SET "+column+"=false WHERE id=$1", item)
	}
	targets, _ := q.ListMobilePushTargets(ctx, id)
	previous := targets[0].UpdatedAt
	upsert(uid, time.Now().Add(2*time.Hour))
	if err := q.InvalidateMobilePushSubscription(ctx, db.InvalidateMobilePushSubscriptionParams{Token: token, UpdatedAt: previous}); err != nil {
		t.Fatal(err)
	}
	wantTargets(1)
	upsert(otherID, time.Now().Add(time.Hour))
	wantTargets(0)
	upsert(uid, time.Now().Add(time.Hour))
	wantTargets(1)
	_, err = pool.Exec(ctx, "DELETE FROM member WHERE id=$1", member)
	if err != nil {
		t.Fatal(err)
	}
	wantTargets(0)
}
