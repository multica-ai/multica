package lark

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMediaDeliveryDatabaseOwnershipAndRetryRecovery(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Media delivery test", "media-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Media delivery test", "media-"+suffix)
	runtimeID := fx.Runtime(t, "Media delivery test")
	agentID := fx.Agent(t, "Media delivery test", runtimeID)
	root := fx.Task(t, agentID, testutil.Cols{"status": "completed", "runtime_id": runtimeID})
	retry := fx.Task(t, agentID, testutil.Cols{"status": "completed", "runtime_id": runtimeID, "retry_of_task_id": root})
	rootID := util.MustParseUUID(root)
	fx.Cleanup(t, "DELETE FROM channel_reply_delivery WHERE turn_id=$1", rootID)
	q := NewChannelStore(db.New(pool))
	api := &fakeMediaAPIClient{fakeAPIClient: &fakeAPIClient{}}
	first := NewPatcher(q, fakeCredentials{secret: "unused"}, api, PatcherConfig{Logger: newDiscardLogger()})
	second := NewPatcher(q, fakeCredentials{secret: "unused"}, api, PatcherConfig{Logger: newDiscardLogger()})
	binding := ChatSessionBinding{ID: util.MustParseUUID(uuid.NewString()), InstallationID: util.MustParseUUID(uuid.NewString()), ChannelChatID: "test-chat"}
	ctx, finish, ok := first.claimMediaDelivery(context.Background(), rootID, binding)
	if !ok || !first.markMediaSending(ctx) {
		t.Fatal("first owner did not acquire and publish send")
	}
	if _, _, ok := second.claimMediaDelivery(context.Background(), rootID, binding); ok {
		t.Fatal("two live replicas owned one delivery")
	}
	fx.Exec(t, "UPDATE channel_reply_delivery SET owner_expires_at=now()-interval '1 second' WHERE turn_id=$1", rootID)
	successor, settle, ok := second.claimMediaDelivery(context.Background(), util.MustParseUUID(retry), binding)
	if !ok {
		t.Fatal("retry could not take an expired lease")
	}
	if second.markMediaSending(successor) {
		t.Fatal("takeover silently replayed an in-flight attachment batch")
	}
	finish()
	row, err := q.GetChannelReplyDelivery(context.Background(), rootID)
	if err != nil || row.Phase == "settled" || row.AttemptDepth != 1 {
		t.Fatalf("stale owner overwrote retry: %+v err=%v", row, err)
	}
	settle()
	if _, _, ok := first.claimMediaDelivery(context.Background(), rootID, binding); ok {
		t.Fatal("settled delivery accepted a replay")
	}
	if _, _, ok := second.claimMediaDelivery(context.Background(), util.MustParseUUID(retry), binding); ok {
		t.Fatal("retry repeated a settled delivery")
	}
}
