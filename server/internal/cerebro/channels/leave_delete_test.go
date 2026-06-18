package channels

// TECH-3758 — leave (remove own subscription) and delete (remove for everyone)
// channel handlers. Runs against the local dev DB; skipped when unreachable
// (see TestMain).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createTestChannel creates a kind='channel' issue authored by `creator` and
// subscribes every member in `members` (creator included if listed).
func createTestChannel(t *testing.T, creator pgtype.UUID, members ...pgtype.UUID) db.Issue {
	t.Helper()
	ctx := context.Background()
	q := db.New(testPool)

	number, err := q.IncrementIssueCounter(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("increment issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: testWorkspaceID,
		Title:       "leave-delete-test-channel",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   creator,
		Position:    0,
		Number:      number,
		Kind:        pgtype.Text{String: "channel", Valid: true},
	})
	if err != nil {
		t.Fatalf("create channel issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	for _, m := range members {
		if err := q.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: issue.ID, UserType: "member", UserID: m, Reason: "creator",
		}); err != nil {
			t.Fatalf("subscribe %v: %v", m, err)
		}
	}
	return issue
}

// channelRequest builds an http.Request carrying the X-User-ID header and the
// chi {id} URL param the channel handlers read.
func channelRequest(method string, channelID, userID pgtype.UUID) *http.Request {
	req := httptest.NewRequest(method, "/api/channels/"+uuidString(channelID), nil)
	req.Header.Set("X-User-ID", uuidString(userID))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidString(channelID))
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func isSubscribed(t *testing.T, channelID, userID pgtype.UUID) bool {
	t.Helper()
	ok, err := db.New(testPool).IsIssueSubscriber(context.Background(), db.IsIssueSubscriberParams{
		IssueID: channelID, UserType: "member", UserID: userID,
	})
	if err != nil {
		t.Fatalf("IsIssueSubscriber: %v", err)
	}
	return ok
}

func TestLeaveChannel_RemovesOwnSubscriptionOnly(t *testing.T) {
	creator := createTestUserAndMember(t, "leave-creator")
	leaver := createTestUserAndMember(t, "leave-member")
	channel := createTestChannel(t, creator, creator, leaver)

	w := httptest.NewRecorder()
	testSvc.LeaveChannelHandler(w, channelRequest(http.MethodPost, channel.ID, leaver))

	if w.Code != http.StatusNoContent {
		t.Fatalf("LeaveChannel: expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	if isSubscribed(t, channel.ID, leaver) {
		t.Fatalf("leaver is still subscribed after leaving")
	}
	if !isSubscribed(t, channel.ID, creator) {
		t.Fatalf("creator's subscription was wrongly removed")
	}
}

func TestDeleteChannel_NonPrivilegedForbidden(t *testing.T) {
	creator := createTestUserAndMember(t, "del-creator")
	other := createTestUserAndMember(t, "del-other")
	channel := createTestChannel(t, creator, creator, other)

	w := httptest.NewRecorder()
	testSvc.DeleteChannelHandler(w, channelRequest(http.MethodDelete, channel.ID, other))

	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteChannel by non-privileged member: expected 403, got %d", w.Code)
	}
	if _, err := db.New(testPool).GetIssue(context.Background(), channel.ID); err != nil {
		t.Fatalf("channel was deleted despite the 403: %v", err)
	}
}

func TestDeleteChannel_CreatorDeletesForEveryone(t *testing.T) {
	creator := createTestUserAndMember(t, "del2-creator")
	member := createTestUserAndMember(t, "del2-member")
	channel := createTestChannel(t, creator, creator, member)

	w := httptest.NewRecorder()
	testSvc.DeleteChannelHandler(w, channelRequest(http.MethodDelete, channel.ID, creator))

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChannel by creator: expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := db.New(testPool).GetIssue(context.Background(), channel.ID); err == nil {
		t.Fatalf("channel still exists after delete")
	}
}
