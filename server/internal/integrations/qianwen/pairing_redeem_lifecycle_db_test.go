package qianwen

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRedeemPairingCodeDoesNotReplaySuccessAfterTargetMembershipEnds(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var targetUserID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Qianwen Pairing Lifecycle Target', $1)
		RETURNING id
	`, "qianwen-pairing-lifecycle-"+uuid.NewString()+"@multica.test").Scan(&targetUserID); err != nil {
		t.Fatalf("insert pairing target: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, fixture.workspaceID, targetUserID); err != nil {
		t.Fatalf("add pairing target to workspace: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, targetUserID, fixture.agentID); err != nil {
		t.Fatalf("make target the private agent owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `UPDATE agent SET owner_id = $1 WHERE id = $2`, fixture.userID, fixture.agentID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1 AND multica_user_id = $2`, fixture.installation.Installation.ID, targetUserID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM qianwen_invocation_nonce WHERE installation_id = $1 AND multica_user_id = $2`, fixture.installation.Installation.ID, targetUserID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fixture.workspaceID, targetUserID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	minted, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, targetUserID)
	if err != nil {
		t.Fatalf("MintPairingCode() error = %v", err)
	}
	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }
	request := PairingRedeemRequest{
		Code: minted.Code,
		Identity: InvocationMetadata{
			OpenUserID: "opaque-lifecycle-user",
			OpenUUID:   "opaque-lifecycle-device",
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "lifecycle-retry-nonce-0000000001",
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); err != nil {
		t.Fatalf("initial RedeemPairingCode() error = %v", err)
	}

	if _, err := fixture.pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fixture.workspaceID, targetUserID); err != nil {
		t.Fatalf("remove paired target membership: %v", err)
	}
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request); !errors.Is(err, ErrPairingAccessDenied) {
		t.Fatalf("RedeemPairingCode() after membership removal error = %v, want ErrPairingAccessDenied", err)
	}
}
