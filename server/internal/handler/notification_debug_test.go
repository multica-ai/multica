package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListNotificationDebugRows_ChannelFilterShowsMissingDelivery(t *testing.T) {
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'debug notification issue', 'todo', 'none', 'member', $2,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	event, err := testHandler.Queries.CreateNotificationEvent(context.Background(), db.CreateNotificationEventParams{
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		RecipientUserID: util.MustParseUUID(testUserID),
		Type:            "new_comment",
		Severity:        "info",
		IssueID:         util.MustParseUUID(issueID),
		ActorType:       util.StrToText("agent"),
		ActorID:         util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		Title:           "debug notification",
		Body:            util.StrToText("agent comment"),
		Link:            util.StrToText("https://multica.test/handler-tests/issues/HAN-1"),
		Details:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateNotificationEvent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM notification_delivery WHERE notification_event_id = $1`, util.UUIDToString(event.ID))
		_, _ = testPool.Exec(context.Background(), `DELETE FROM notification_event WHERE id = $1`, util.UUIDToString(event.ID))
	})
	if _, err := testHandler.Queries.CreateNotificationDelivery(context.Background(), db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "inbox",
		Status:              "sent",
		AttemptCount:        1,
		PayloadSnapshot:     []byte(`{}`),
	}); err != nil {
		t.Fatalf("CreateNotificationDelivery: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/notification-debug/deliveries?issue_id="+issueID+"&channel=openclaw_weixin", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListNotificationDebugRows(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListNotificationDebugRows: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ListNotificationDebugRowsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected one debug row, got %d", resp.Total)
	}
	if resp.Rows[0].NotificationEvent.ID != uuidToString(event.ID) {
		t.Fatalf("unexpected notification_event id %q", resp.Rows[0].NotificationEvent.ID)
	}
	if resp.Rows[0].Delivery != nil {
		t.Fatalf("expected nil delivery for missing openclaw_weixin delivery, got %#v", resp.Rows[0].Delivery)
	}
}
