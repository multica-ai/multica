package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteMemberRevokesTheirQianwenInstallationsAndPairingAuthority(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-qianwen-installer-revoke", "daemon-qianwen-installer-revoke")
	ctx := context.Background()
	const appID = "qwc_member_revoke_test"

	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, installer_user_id
		) VALUES (
			$1, $2, 'qianwen',
			jsonb_build_object('app_id', $3::text, 'access_token_hash', repeat('a', 64), 'mode', 'personal_polling'),
			$4
		)
		RETURNING id
	`, fx.WorkspaceID, fx.AgentID, appID, fx.TargetUserID).Scan(&installationID); err != nil {
		t.Fatalf("insert target-owned Qianwen installation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM qianwen_pairing_code WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM qianwen_pairing_attempt WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM qianwen_invocation_nonce WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO qianwen_pairing_code (
			installation_id, workspace_id, multica_user_id, code_digest, expires_at
		) VALUES ($1, $2, $3, decode(repeat('11', 32), 'hex'), now() + interval '10 minutes')
	`, installationID, fx.WorkspaceID, fx.TargetUserID); err != nil {
		t.Fatalf("insert pending pairing code: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO qianwen_pairing_attempt (installation_id, identity_digest)
		VALUES ($1, decode(repeat('22', 32), 'hex'))
	`, installationID); err != nil {
		t.Fatalf("insert pairing failure: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO qianwen_invocation_nonce (
			installation_id, nonce_digest, request_digest, outcome, multica_user_id, expires_at
		) VALUES (
			$1, decode(repeat('33', 32), 'hex'), decode(repeat('44', 32), 'hex'),
			'paired', $2, now() + interval '5 minutes'
		)
	`, installationID, fx.TargetUserID); err != nil {
		t.Fatalf("insert paired invocation outcome: %v", err)
	}
	for openID, userID := range map[string]string{
		"opaque-qianwen-removed-installer": fx.TargetUserID,
		"opaque-qianwen-remaining-member":  testUserID,
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_user_binding (
				workspace_id, multica_user_id, installation_id, channel_type, channel_user_id
			) VALUES ($1, $2, $3, 'qianwen', $4)
		`, fx.WorkspaceID, userID, installationID, openID); err != nil {
			t.Fatalf("insert Qianwen binding %q: %v", openID, err)
		}
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var codes, attempts, nonces, bindings int
	if err := testPool.QueryRow(ctx, `SELECT status FROM channel_installation WHERE id = $1`, installationID).Scan(&status); err != nil {
		t.Fatalf("load Qianwen installation after member removal: %v", err)
	}
	for label, query := range map[string]string{
		"codes":    `SELECT count(*) FROM qianwen_pairing_code WHERE installation_id = $1`,
		"attempts": `SELECT count(*) FROM qianwen_pairing_attempt WHERE installation_id = $1`,
		"nonces":   `SELECT count(*) FROM qianwen_invocation_nonce WHERE installation_id = $1`,
		"bindings": `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`,
	} {
		var target *int
		switch label {
		case "codes":
			target = &codes
		case "attempts":
			target = &attempts
		case "nonces":
			target = &nonces
		default:
			target = &bindings
		}
		if err := testPool.QueryRow(ctx, query, installationID).Scan(target); err != nil {
			t.Fatalf("count %s after member removal: %v", label, err)
		}
	}
	if status != "revoked" || codes != 0 || attempts != 0 || nonces != 0 || bindings != 0 {
		t.Fatalf("member removal left Qianwen authority status=%q codes=%d attempts=%d nonces=%d bindings=%d", status, codes, attempts, nonces, bindings)
	}
}
