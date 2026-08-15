package qianwen

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

type qianwenBindingManagementCounts struct {
	bindings      int
	pendingCodes  int
	pairedReplays int
}

func TestUnbindCurrentUserIsIdempotentAndDoesNotAffectAnotherUser(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	cleanupQianwenBindingManagementState(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture.service.now = func() time.Time { return now }
	otherUserID := newQianwenBindingManagementUser(t, fixture)

	pairQianwenBindingManagementUser(t, ctx, fixture, fixture.userID, now, 1)
	pairQianwenBindingManagementUser(t, ctx, fixture, otherUserID, now, 2)
	if _, err := fixture.service.MintPairingCode(
		ctx,
		fixture.workspaceID,
		fixture.installation.Installation.ID,
		fixture.userID,
	); err != nil {
		t.Fatalf("mint current-user pending code: %v", err)
	}
	if _, err := fixture.service.MintPairingCode(
		ctx,
		fixture.workspaceID,
		fixture.installation.Installation.ID,
		otherUserID,
	); err != nil {
		t.Fatalf("mint other-user pending code: %v", err)
	}

	assertQianwenBindingManagementCounts(t, ctx, fixture, fixture.userID, qianwenBindingManagementCounts{
		bindings:      1,
		pendingCodes:  1,
		pairedReplays: 1,
	})
	assertQianwenBindingManagementCounts(t, ctx, fixture, otherUserID, qianwenBindingManagementCounts{
		bindings:      1,
		pendingCodes:  1,
		pairedReplays: 1,
	})

	for attempt := 1; attempt <= 2; attempt++ {
		if err := fixture.service.UnbindCurrentUser(
			ctx,
			fixture.workspaceID,
			fixture.installation.Installation.ID,
			fixture.userID,
		); err != nil {
			t.Fatalf("UnbindCurrentUser() attempt %d error = %v, want idempotent success", attempt, err)
		}
	}

	assertQianwenBindingManagementCounts(t, ctx, fixture, fixture.userID, qianwenBindingManagementCounts{})
	assertQianwenBindingManagementCounts(t, ctx, fixture, otherUserID, qianwenBindingManagementCounts{
		bindings:      1,
		pendingCodes:  1,
		pairedReplays: 1,
	})
}

func TestUnbindCurrentUserSerializesAfterConcurrentRedeem(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	cleanupQianwenBindingManagementState(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture.service.now = func() time.Time { return now }

	minted, err := fixture.service.MintPairingCode(
		ctx,
		fixture.workspaceID,
		fixture.installation.Installation.ID,
		fixture.userID,
	)
	if err != nil {
		t.Fatalf("MintPairingCode() error = %v", err)
	}
	redeemRequest := PairingRedeemRequest{
		Code: minted.Code,
		Identity: InvocationMetadata{
			OpenUserID: "opaque-concurrent-unbind-user",
			OpenUUID:   "opaque-concurrent-unbind-device",
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "concurrent-unbind-nonce-00000001",
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &redeemRequest)

	// Park redeem after it owns the (workspace,current-user) advisory lock but
	// before it can lock the workspace. Unbind must wait behind redeem on that
	// same advisory lock; taking the workspace lock first would reopen the race.
	holder, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace-lock holder: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := fixture.queries.WithTx(holder).LockWorkspaceForDelete(ctx, fixture.workspaceID); err != nil {
		t.Fatalf("lock workspace ahead of redeem: %v", err)
	}

	redeemDone := make(chan error, 1)
	go func() {
		_, redeemErr := fixture.service.RedeemPairingCode(
			ctx,
			fixture.installation.ConnectionID,
			fixture.installation.AccessToken,
			redeemRequest,
		)
		redeemDone <- redeemErr
	}()
	redeemPID := waitForQianwenBindingManagementBlockedQuery(
		t,
		fixture,
		"LockWorkspaceForChatSessionCreate",
	)

	unbindDone := make(chan error, 1)
	go func() {
		unbindDone <- fixture.service.UnbindCurrentUser(
			ctx,
			fixture.workspaceID,
			fixture.installation.Installation.ID,
			fixture.userID,
		)
	}()
	waitForQianwenBindingManagementWaiterBlockedBy(t, fixture, redeemPID)

	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release workspace lock: %v", err)
	}
	if err := awaitQianwenBindingManagementResult(t, "concurrent redeem", redeemDone); err != nil {
		t.Fatalf("concurrent RedeemPairingCode() error = %v", err)
	}
	if err := awaitQianwenBindingManagementResult(t, "unbind", unbindDone); err != nil {
		t.Fatalf("UnbindCurrentUser() error = %v", err)
	}

	// Redeem committed first; unbind ran immediately after it under the same
	// lifecycle fence and therefore owns the final state.
	assertQianwenBindingManagementCounts(t, ctx, fixture, fixture.userID, qianwenBindingManagementCounts{})
}

func newQianwenBindingManagementUser(
	t *testing.T,
	fixture *qianwenServiceDBFixture,
) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var userID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Qianwen Binding Management Other User', $1)
		RETURNING id
	`, "qianwen-binding-management-"+uuid.NewString()+"@multica.test").Scan(&userID); err != nil {
		t.Fatalf("insert other binding-management user: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, fixture.workspaceID, userID); err != nil {
		t.Fatalf("add other binding-management user to workspace: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, fixture.agentID); err != nil {
		t.Fatalf("enable shared binding-management agent: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'workspace', $2, $3)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, fixture.agentID, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("grant other binding-management user invocation access: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(
			context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
			fixture.workspaceID,
			userID,
		)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func pairQianwenBindingManagementUser(
	t *testing.T,
	ctx context.Context,
	fixture *qianwenServiceDBFixture,
	userID pgtype.UUID,
	now time.Time,
	sequence int,
) {
	t.Helper()

	minted, err := fixture.service.MintPairingCode(
		ctx,
		fixture.workspaceID,
		fixture.installation.Installation.ID,
		userID,
	)
	if err != nil {
		t.Fatalf("MintPairingCode() for user %s: %v", util.UUIDToString(userID), err)
	}
	request := PairingRedeemRequest{
		Code: minted.Code,
		Identity: InvocationMetadata{
			OpenUserID: fmt.Sprintf("opaque-binding-management-user-%d", sequence),
			OpenUUID:   fmt.Sprintf("opaque-binding-management-device-%d", sequence),
			Timestamp:  fmt.Sprint(now.Add(time.Duration(sequence) * time.Millisecond).UnixMilli()),
			Nonce:      fmt.Sprintf("binding-management-nonce-%07d", sequence),
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)
	result, err := fixture.service.RedeemPairingCode(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		request,
	)
	if err != nil {
		t.Fatalf("RedeemPairingCode() for user %s: %v", util.UUIDToString(userID), err)
	}
	if result.MulticaUserID != userID || result.InstallationID != fixture.installation.Installation.ID {
		t.Fatalf("paired result = %+v, want user %s and installation %s", result, util.UUIDToString(userID), util.UUIDToString(fixture.installation.Installation.ID))
	}
}

func cleanupQianwenBindingManagementState(
	t *testing.T,
	fixture *qianwenServiceDBFixture,
) {
	t.Helper()

	clear := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM channel_user_binding WHERE installation_id = $1`,
			`DELETE FROM qianwen_pairing_code WHERE installation_id = $1`,
			`DELETE FROM qianwen_invocation_nonce WHERE installation_id = $1`,
			`DELETE FROM qianwen_pairing_attempt WHERE installation_id = $1`,
		} {
			if _, err := fixture.pool.Exec(ctx, statement, fixture.installation.Installation.ID); err != nil {
				t.Errorf("clean binding-management state with %q: %v", statement, err)
			}
		}
	}

	// The shared fixture may seed the installer's default binding so unrelated
	// submit/status tests start from an authorized identity. Binding-management
	// tests own their exact preconditions, so clear that seed immediately and
	// register the identical cleanup for rows created by the test itself.
	clear()
	t.Cleanup(clear)
}

func assertQianwenBindingManagementCounts(
	t *testing.T,
	ctx context.Context,
	fixture *qianwenServiceDBFixture,
	userID pgtype.UUID,
	want qianwenBindingManagementCounts,
) {
	t.Helper()

	var got qianwenBindingManagementCounts
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM channel_user_binding
			 WHERE installation_id = $1 AND multica_user_id = $2),
			(SELECT count(*) FROM qianwen_pairing_code
			 WHERE installation_id = $1 AND multica_user_id = $2),
			(SELECT count(*) FROM qianwen_invocation_nonce
			 WHERE installation_id = $1 AND multica_user_id = $2 AND outcome = 'paired')
	`, fixture.installation.Installation.ID, userID).Scan(
		&got.bindings,
		&got.pendingCodes,
		&got.pairedReplays,
	); err != nil {
		t.Fatalf("count binding-management state for user %s: %v", util.UUIDToString(userID), err)
	}
	if got != want {
		t.Fatalf("binding-management state for user %s = %+v, want %+v", util.UUIDToString(userID), got, want)
	}
}

func waitForQianwenBindingManagementBlockedQuery(
	t *testing.T,
	fixture *qianwenServiceDBFixture,
	queryName string,
) int32 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid int32
		err := fixture.pool.QueryRow(ctx, `
			SELECT pid
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%' || $1 || '%'
			ORDER BY query_start DESC
			LIMIT 1
		`, queryName).Scan(&pid)
		if err == nil {
			return pid
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("Qianwen binding-management query %q did not block: %v", queryName, ctx.Err())
		}
	}
}

func waitForQianwenBindingManagementWaiterBlockedBy(
	t *testing.T,
	fixture *qianwenServiceDBFixture,
	blockerPID int32,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := fixture.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND state = 'active'
				  AND wait_event_type = 'Lock'
				  AND $1::integer = ANY(pg_blocking_pids(pid))
			)
		`, blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("observe Qianwen unbind waiter: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("Qianwen unbind did not serialize behind redeem backend %d", blockerPID)
		}
	}
}

func awaitQianwenBindingManagementResult(
	t *testing.T,
	name string,
	done <-chan error,
) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", name)
		return nil
	}
}
