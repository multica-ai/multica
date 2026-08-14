package qianwen

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestBoundQianwenSubmitAttributesSessionAndTaskToBoundUser(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent SET permission_mode = 'public_to' WHERE id = $1
	`, fixture.agentID); err != nil {
		t.Fatalf("enable explicit Qianwen agent invocation grants: %v", err)
	}

	const (
		openUserID = "opaque-bound-submit-user"
		openUUID   = "opaque-bound-submit-device"
	)
	boundUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Bound Submit", openUserID, openUUID)
	registerBoundQianwenUsersCleanup(t, fixture, boundUserID)

	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }
	requestID := uuid.NewString()
	invocation := signedSubmitInvocationFixture(
		fixture.installation.AccessToken,
		requestID,
		"inspect the bound user's workspace",
		openUserID,
		openUUID,
		now,
	)

	result, err := fixture.service.Submit(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		invocation,
	)
	if err != nil {
		t.Fatalf("bound Submit() error = %v", err)
	}
	if result.RequestID != requestID || result.Status != "accepted" {
		t.Fatalf("bound Submit() result = %+v, want accepted request %s", result, requestID)
	}

	var ledgerUserID, sessionCreatorID pgtype.UUID
	var initiatorUserID, originatorUserID, accountableUserID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			request.multica_user_id,
			session.creator_id,
			task.initiator_user_id,
			task.originator_user_id,
			task.accountable_user_id
		FROM qianwen_skill_request AS request
		JOIN chat_session AS session ON session.id = request.chat_session_id
		JOIN agent_task_queue AS task ON task.id = request.task_id
		WHERE request.installation_id = $1
		  AND request.request_id = $2
	`, fixture.installation.Installation.ID, util.MustParseUUID(requestID)).Scan(
		&ledgerUserID,
		&sessionCreatorID,
		&initiatorUserID,
		&originatorUserID,
		&accountableUserID,
	); err != nil {
		t.Fatalf("load bound Qianwen request attribution: %v", err)
	}

	for name, got := range map[string]pgtype.UUID{
		"request.multica_user_id":  ledgerUserID,
		"session.creator_id":       sessionCreatorID,
		"task.initiator_user_id":   initiatorUserID,
		"task.originator_user_id":  originatorUserID,
		"task.accountable_user_id": accountableUserID,
	} {
		if got != boundUserID {
			t.Errorf("%s = %s, want bound user %s (installer is %s)",
				name,
				util.UUIDToString(got),
				util.UUIDToString(boundUserID),
				util.UUIDToString(fixture.userID),
			)
		}
	}
}

func TestBoundQianwenSubmitRejectsCrossUserRequestIDReuse(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent SET permission_mode = 'public_to' WHERE id = $1
	`, fixture.agentID); err != nil {
		t.Fatalf("enable explicit Qianwen agent invocation grants: %v", err)
	}

	const (
		boundOpenUserID = "opaque-cross-user-bound"
		boundOpenUUID   = "opaque-cross-user-bound-device"
		otherOpenUserID = "opaque-cross-user-other"
		otherOpenUUID   = "opaque-cross-user-other-device"
	)
	boundUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Bound", boundOpenUserID, boundOpenUUID)
	otherUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Other", otherOpenUserID, otherOpenUUID)
	registerBoundQianwenUsersCleanup(t, fixture, boundUserID, otherUserID)

	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }
	requestID := uuid.NewString()
	query := "inspect one durable request"

	first, err := fixture.service.Submit(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedSubmitInvocationFixture(
			fixture.installation.AccessToken,
			requestID,
			query,
			boundOpenUserID,
			boundOpenUUID,
			now,
		),
	)
	if err != nil {
		t.Fatalf("bound user's Submit() error = %v", err)
	}
	if first.RequestID != requestID || first.Status != "accepted" {
		t.Fatalf("bound user's Submit() result = %+v, want accepted request %s", first, requestID)
	}

	_, err = fixture.service.Submit(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedSubmitInvocationFixture(
			fixture.installation.AccessToken,
			requestID,
			query,
			otherOpenUserID,
			otherOpenUUID,
			now,
		),
	)
	if !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("other user's same-request Submit() error = %v, want ErrRequestConflict", err)
	}

	var requestCount, taskCount, sessionCount, otherSessionCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM qianwen_skill_request WHERE installation_id = $1),
			(SELECT count(*) FROM agent_task_queue WHERE agent_id = $2),
			(SELECT count(*) FROM chat_session WHERE workspace_id = $3),
			(SELECT count(*) FROM chat_session WHERE workspace_id = $3 AND creator_id = $4)
	`, fixture.installation.Installation.ID, fixture.agentID, fixture.workspaceID, otherUserID).Scan(
		&requestCount,
		&taskCount,
		&sessionCount,
		&otherSessionCount,
	); err != nil {
		t.Fatalf("count rows after cross-user request conflict: %v", err)
	}
	if requestCount != 1 || taskCount != 1 || sessionCount != 1 || otherSessionCount != 0 {
		t.Fatalf(
			"rows after cross-user request conflict: requests=%d tasks=%d sessions=%d other_user_sessions=%d, want 1/1/1/0",
			requestCount,
			taskCount,
			sessionCount,
			otherSessionCount,
		)
	}
}

func TestBoundQianwenStatusHidesAnotherUsersRequest(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent SET permission_mode = 'public_to' WHERE id = $1
	`, fixture.agentID); err != nil {
		t.Fatalf("enable explicit Qianwen agent invocation grants: %v", err)
	}

	const (
		boundOpenUserID = "opaque-status-bound"
		boundOpenUUID   = "opaque-status-bound-device"
		otherOpenUserID = "opaque-status-other"
		otherOpenUUID   = "opaque-status-other-device"
	)
	boundUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Status Bound", boundOpenUserID, boundOpenUUID)
	otherUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Status Other", otherOpenUserID, otherOpenUUID)
	registerBoundQianwenUsersCleanup(t, fixture, boundUserID, otherUserID)

	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }
	requestID := uuid.NewString()
	if _, err := fixture.service.Submit(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedSubmitInvocationFixture(
			fixture.installation.AccessToken,
			requestID,
			"create a request visible only to its bound user",
			boundOpenUserID,
			boundOpenUUID,
			now,
		),
	); err != nil {
		t.Fatalf("bound user's Submit() error = %v", err)
	}

	ownerStatus, err := fixture.service.Status(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedStatusInvocationFixture(
			fixture.installation.AccessToken,
			requestID,
			boundOpenUserID,
			boundOpenUUID,
			now,
		),
	)
	if err != nil {
		t.Fatalf("bound user's Status() error = %v", err)
	}
	if ownerStatus.RequestID != requestID {
		t.Fatalf("bound user's Status() request_id = %q, want %q", ownerStatus.RequestID, requestID)
	}

	_, err = fixture.service.Status(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedStatusInvocationFixture(
			fixture.installation.AccessToken,
			requestID,
			otherOpenUserID,
			otherOpenUUID,
			now,
		),
	)
	if !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("other user's Status() error = %v, want ErrRequestNotFound", err)
	}
}

func TestBoundQianwenSubmitRequiresExactOpaqueIdentity(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent SET permission_mode = 'public_to' WHERE id = $1
	`, fixture.agentID); err != nil {
		t.Fatalf("enable explicit Qianwen agent invocation grants: %v", err)
	}

	const (
		openUserID = "opaque-exact-case-user"
		openUUID   = "opaque-exact-case-device"
	)
	boundUserID := seedBoundQianwenDBUser(t, ctx, fixture, "Exact Identity", openUserID, openUUID)
	registerBoundQianwenUsersCleanup(t, fixture, boundUserID)

	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }
	if _, err := fixture.service.Submit(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		signedSubmitInvocationFixture(
			fixture.installation.AccessToken,
			uuid.NewString(),
			"prove the exact bound identity works",
			openUserID,
			openUUID,
			now,
		),
	); err != nil {
		t.Fatalf("exact bound identity Submit() error = %v", err)
	}

	identityVariants := []struct {
		name       string
		openUserID string
		openUUID   string
	}{
		{
			name:       "wrong open UUID",
			openUserID: openUserID,
			openUUID:   openUUID + "-wrong",
		},
		{
			name:       "open user ID case changed",
			openUserID: strings.ToUpper(openUserID),
			openUUID:   openUUID,
		},
		{
			name:       "open UUID case changed",
			openUserID: openUserID,
			openUUID:   strings.ToUpper(openUUID),
		},
	}
	for _, variant := range identityVariants {
		t.Run(variant.name, func(t *testing.T) {
			requestID := uuid.NewString()
			_, err := fixture.service.Submit(
				ctx,
				fixture.installation.ConnectionID,
				fixture.installation.AccessToken,
				signedSubmitInvocationFixture(
					fixture.installation.AccessToken,
					requestID,
					"this altered opaque identity must not submit",
					variant.openUserID,
					variant.openUUID,
					now,
				),
			)
			if !errors.Is(err, ErrPairingAccessDenied) {
				t.Fatalf("altered identity Submit() error = %v, want ErrPairingAccessDenied", err)
			}

			var alteredRequestRows int
			if err := fixture.pool.QueryRow(ctx, `
				SELECT count(*)
				FROM qianwen_skill_request
				WHERE installation_id = $1 AND request_id = $2
			`, fixture.installation.Installation.ID, util.MustParseUUID(requestID)).Scan(&alteredRequestRows); err != nil {
				t.Fatalf("count altered identity request rows: %v", err)
			}
			if alteredRequestRows != 0 {
				t.Fatalf("altered identity request rows = %d, want 0", alteredRequestRows)
			}
		})
	}

	var requestCount, taskCount, sessionCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM qianwen_skill_request WHERE installation_id = $1),
			(SELECT count(*) FROM agent_task_queue WHERE agent_id = $2),
			(SELECT count(*) FROM chat_session WHERE workspace_id = $3)
	`, fixture.installation.Installation.ID, fixture.agentID, fixture.workspaceID).Scan(
		&requestCount,
		&taskCount,
		&sessionCount,
	); err != nil {
		t.Fatalf("count rows after altered opaque identities: %v", err)
	}
	if requestCount != 1 || taskCount != 1 || sessionCount != 1 {
		t.Fatalf(
			"rows after altered opaque identities: requests=%d tasks=%d sessions=%d, want exact baseline 1/1/1",
			requestCount,
			taskCount,
			sessionCount,
		)
	}
}

func seedBoundQianwenDBUser(
	t *testing.T,
	ctx context.Context,
	fixture *qianwenServiceDBFixture,
	label string,
	openUserID string,
	openUUID string,
) pgtype.UUID {
	t.Helper()

	userID := util.MustParseUUID(uuid.NewString())
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO "user" (id, name, email)
		VALUES ($1, $2, $3)
	`, userID, "Qianwen "+label+" User", "qianwen-bound-"+uuid.NewString()+"@multica.test"); err != nil {
		t.Fatalf("seed %s Qianwen user: %v", label, err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, fixture.workspaceID, userID); err != nil {
		t.Fatalf("seed %s Qianwen member: %v", label, err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'member', $2, $3)
	`, fixture.agentID, userID, fixture.userID); err != nil {
		t.Fatalf("grant %s Qianwen user invocation access: %v", label, err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO channel_user_binding (
			workspace_id, multica_user_id, installation_id,
			channel_type, channel_user_id, config
		) VALUES (
			$1, $2, $3, 'qianwen', $4,
			jsonb_build_object('open_uuid', $5::text, 'identity_scope', 'skill')
		)
	`, fixture.workspaceID, userID, fixture.installation.Installation.ID, openUserID, openUUID); err != nil {
		t.Fatalf("seed %s Qianwen identity: %v", label, err)
	}
	return userID
}

func registerBoundQianwenUsersCleanup(t *testing.T, fixture *qianwenServiceDBFixture, userIDs ...pgtype.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		statements := []struct {
			query string
			args  []any
		}{
			{`DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, []any{fixture.workspaceID}},
			{`DELETE FROM agent_task_queue WHERE agent_id = $1`, []any{fixture.agentID}},
			{`DELETE FROM qianwen_skill_request WHERE installation_id = $1`, []any{fixture.installation.Installation.ID}},
			{`DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, []any{fixture.installation.Installation.ID}},
			{`DELETE FROM chat_session WHERE workspace_id = $1`, []any{fixture.workspaceID}},
			{`DELETE FROM channel_user_binding WHERE installation_id = $1`, []any{fixture.installation.Installation.ID}},
			{`DELETE FROM agent_invocation_target WHERE agent_id = $1`, []any{fixture.agentID}},
		}
		for index, statement := range statements {
			if _, err := fixture.pool.Exec(ctx, statement.query, statement.args...); err != nil {
				t.Errorf("cleanup bound users fixture step %d: %v", index, err)
			}
		}
		for _, userID := range userIDs {
			if _, err := fixture.pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fixture.workspaceID, userID); err != nil {
				t.Errorf("cleanup bound user membership %s: %v", util.UUIDToString(userID), err)
			}
			if _, err := fixture.pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID); err != nil {
				t.Errorf("cleanup bound user %s: %v", util.UUIDToString(userID), err)
			}
		}
	})
}

func signedSubmitInvocationFixture(token, requestID, query, openUserID, openUUID string, now time.Time) SubmitInvocation {
	identity := InvocationMetadata{
		OpenUserID: openUserID,
		OpenUUID:   openUUID,
		Timestamp:  fmt.Sprint(now.UnixMilli()),
		Nonce:      "0123456789abcdef0123456789abcdef",
	}
	queryDigest := sha256.Sum256([]byte(query))
	canonical := strings.Join([]string{
		"QIANWEN-HMAC-SHA256-V1",
		"request_submit",
		identity.Timestamp,
		identity.Nonce,
		identity.OpenUserID,
		identity.OpenUUID,
		requestID,
		hex.EncodeToString(queryDigest[:]),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return SubmitInvocation{
		Request:  SubmitRequest{RequestID: requestID, Query: query},
		Identity: identity,
	}
}

func signedStatusInvocationFixture(token, requestID, openUserID, openUUID string, now time.Time) StatusInvocation {
	identity := InvocationMetadata{
		OpenUserID: openUserID,
		OpenUUID:   openUUID,
		Timestamp:  fmt.Sprint(now.UnixMilli()),
		Nonce:      "fedcba9876543210fedcba9876543210",
	}
	canonical := strings.Join([]string{
		"QIANWEN-HMAC-SHA256-V1",
		"request_status",
		identity.Timestamp,
		identity.Nonce,
		identity.OpenUserID,
		identity.OpenUUID,
		requestID,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return StatusInvocation{
		RequestID: requestID,
		Identity:  identity,
	}
}
