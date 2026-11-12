package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func seedIssueChannelCallback(t *testing.T, issueID string) db.IssueChannelCallback {
	t.Helper()
	cb, err := testHandler.Queries.CreateIssueChannelCallback(context.Background(), db.CreateIssueChannelCallbackParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID),
		InstallationID: parseUUID("00000000-0000-4000-8000-0000000000c1"), ChannelType: "feishu",
		ChatID: "oc_group", ChatType: "group", AnchorMessageID: "om_cmd",
		RegisteredBy: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create callback: %v", err)
	}
	dbfx.Cleanup(t, `DELETE FROM issue_channel_callback_delivery WHERE callback_id=$1`, uuidToString(cb.ID))
	dbfx.Cleanup(t, `DELETE FROM issue_channel_callback WHERE id=$1`, uuidToString(cb.ID))
	return cb
}

func TestIssueChannelCallbackListRevokeAndLedger(t *testing.T) {
	ctx := context.Background()
	issueID := dbfx.Issue(t, "Channel callback issue")
	cb := seedIssueChannelCallback(t, issueID)

	// The ledger claims each event once per callback.
	claim := db.ClaimIssueChannelCallbackDeliveryParams{CallbackID: cb.ID, DedupKey: "status:done:1", Event: "status:done"}
	if _, err := testHandler.Queries.ClaimIssueChannelCallbackDelivery(ctx, claim); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := testHandler.Queries.ClaimIssueChannelCallbackDelivery(ctx, claim); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second claim err = %v, want no row", err)
	}
	if err := testHandler.Queries.FinishIssueChannelCallbackDelivery(ctx, db.FinishIssueChannelCallbackDeliveryParams{
		CallbackID: cb.ID, DedupKey: claim.DedupKey, Status: "delivered", ProviderMessageID: "om_notice",
	}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	var listed []IssueChannelCallbackResponse
	testutil.Call(t, testHandler.ListIssueChannelCallbacks,
		withURLParam(newRequest("GET", "/api/issues/"+issueID+"/channel-callbacks", nil), "id", issueID),
	).Want(http.StatusOK).JSON(&listed)
	if len(listed) != 1 || listed[0].ID != uuidToString(cb.ID) || listed[0].RevokedAt != nil ||
		len(listed[0].Deliveries) != 1 || listed[0].Deliveries[0].Status != "delivered" || listed[0].Deliveries[0].ProviderMessageID != "om_notice" {
		t.Fatalf("listed = %+v", listed)
	}

	revokeReq := withURLParams(newRequest("DELETE", "/api/issues/"+issueID+"/channel-callbacks/"+uuidToString(cb.ID), nil),
		"id", issueID, "callbackID", uuidToString(cb.ID))
	var revoked IssueChannelCallbackResponse
	testutil.Call(t, testHandler.RevokeIssueChannelCallback, revokeReq).Want(http.StatusOK).JSON(&revoked)
	if revoked.RevokedAt == nil || len(revoked.Deliveries) != 1 {
		t.Fatalf("revoked = %+v", revoked)
	}
	active, err := testHandler.Queries.ListActiveIssueChannelCallbacks(ctx, db.ListActiveIssueChannelCallbacksParams{
		IssueID: cb.IssueID, ChannelType: "feishu",
	})
	if err != nil || len(active) != 0 {
		t.Fatalf("active after revoke = %+v, %v", active, err)
	}

	other := dbfx.Issue(t, "Other issue")
	wrongIssue := withURLParams(newRequest("DELETE", "/api/issues/"+other+"/channel-callbacks/"+uuidToString(cb.ID), nil),
		"id", other, "callbackID", uuidToString(cb.ID))
	testutil.Call(t, testHandler.RevokeIssueChannelCallback, wrongIssue).Want(http.StatusNotFound)
}

func TestDeleteIssueRemovesChannelCallbacks(t *testing.T) {
	ctx := context.Background()
	issueID := dbfx.Issue(t, "Deleted callback issue")
	cb := seedIssueChannelCallback(t, issueID)
	if _, err := testHandler.Queries.ClaimIssueChannelCallbackDelivery(ctx, db.ClaimIssueChannelCallbackDeliveryParams{
		CallbackID: cb.ID, DedupKey: "status:done:1", Event: "status:done",
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: cb.IssueID, WorkspaceID: cb.WorkspaceID}); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	var callbacks, deliveries int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_channel_callback WHERE id=$1`, cb.ID).Scan(&callbacks); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_channel_callback_delivery WHERE callback_id=$1`, cb.ID).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if callbacks != 0 || deliveries != 0 {
		t.Fatalf("after issue delete: %d callbacks, %d deliveries", callbacks, deliveries)
	}
}
