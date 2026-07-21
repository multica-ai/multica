package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

func createTestSetupToken(t *testing.T) SetupTokenResponse {
	t.Helper()
	req := withURLParam(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/setup-tokens", nil),
		"id",
		testWorkspaceID,
	)
	w := httptest.NewRecorder()
	testHandler.CreateSetupToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create setup token: got %d: %s", w.Code, w.Body.String())
	}
	var response SetupTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode setup token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM setup_token WHERE id = $1`, parseUUID(response.ID))
	})
	return response
}

func redeemTestSetupToken(t *testing.T, token string) (*httptest.ResponseRecorder, RedeemSetupTokenResponse) {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/setup-tokens/redeem", RedeemSetupTokenRequest{
		Token:      token,
		DeviceName: "test-host",
		CLIVersion: "test",
	})
	w := httptest.NewRecorder()
	testHandler.RedeemSetupToken(w, req)
	var response RedeemSetupTokenResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode redeem response: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE token_hash = $1`, auth.HashToken(response.Token))
		})
	}
	return w, response
}

func TestSetupToken_CreateStoresOnlyHashAndRedeemsOnce(t *testing.T) {
	created := createTestSetupToken(t)
	if created.Token == "" || created.Token[:4] != "mst_" {
		t.Fatalf("expected mst_ token, got %q", created.Token)
	}

	var storedHash, storedPrefix string
	if err := testPool.QueryRow(context.Background(), `
		SELECT token_hash, token_prefix FROM setup_token WHERE id = $1
	`, parseUUID(created.ID)).Scan(&storedHash, &storedPrefix); err != nil {
		t.Fatalf("read setup token row: %v", err)
	}
	if storedHash != auth.HashToken(created.Token) || storedHash == created.Token {
		t.Fatalf("setup token was not hash-only: hash=%q", storedHash)
	}
	if storedPrefix != created.Token[:12] {
		t.Fatalf("prefix = %q, want %q", storedPrefix, created.Token[:12])
	}

	w, redeemed := redeemTestSetupToken(t, created.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem: got %d: %s", w.Code, w.Body.String())
	}
	if redeemed.SetupSessionID != created.ID || redeemed.WorkspaceID != testWorkspaceID {
		t.Fatalf("unexpected redeem scope: %+v", redeemed)
	}
	if len(redeemed.Token) < 4 || redeemed.Token[:4] != "mul_" {
		t.Fatalf("expected normal PAT, got %q", redeemed.Token)
	}
	if _, err := testHandler.Queries.GetPersonalAccessTokenByHash(context.Background(), auth.HashToken(redeemed.Token)); err != nil {
		t.Fatalf("redeemed PAT not usable: %v", err)
	}

	replay, _ := redeemTestSetupToken(t, created.Token)
	if replay.Code != http.StatusGone {
		t.Fatalf("replay: got %d, want 410: %s", replay.Code, replay.Body.String())
	}
}

func TestSetupToken_ConcurrentRedeemHasSingleWinner(t *testing.T) {
	created := createTestSetupToken(t)
	const attempts = 8
	var winners atomic.Int32
	var wg sync.WaitGroup
	wg.Add(attempts)
	for range attempts {
		go func() {
			defer wg.Done()
			req := newRequest(http.MethodPost, "/api/setup-tokens/redeem", RedeemSetupTokenRequest{
				Token:      created.Token,
				DeviceName: "race-host",
			})
			w := httptest.NewRecorder()
			testHandler.RedeemSetupToken(w, req)
			if w.Code == http.StatusOK {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("redeem winners = %d, want 1", winners.Load())
	}
	_, _ = testPool.Exec(context.Background(), `
		DELETE FROM personal_access_token
		WHERE user_id = $1 AND name = 'CLI (race-host)' AND created_at > now() - interval '1 minute'
	`, parseUUID(testUserID))
}

func TestSetupToken_ExpiredCannotRedeem(t *testing.T) {
	created := createTestSetupToken(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE setup_token SET expires_at = $2 WHERE id = $1
	`, parseUUID(created.ID), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("expire setup token: %v", err)
	}
	w, _ := redeemTestSetupToken(t, created.Token)
	if w.Code != http.StatusGone {
		t.Fatalf("expired redeem: got %d, want 410: %s", w.Code, w.Body.String())
	}
}

func TestDaemonRegister_SetupSessionReportsConnectedWithZeroRuntimes(t *testing.T) {
	created := createTestSetupToken(t)
	w, redeemed := redeemTestSetupToken(t, created.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem: got %d: %s", w.Code, w.Body.String())
	}

	req := newRequest(http.MethodPost, "/api/daemon/register", DaemonRegisterRequest{
		WorkspaceID:    testWorkspaceID,
		DaemonID:       "setup-zero-runtime-daemon",
		SetupSessionID: redeemed.SetupSessionID,
		DeviceName:     "empty-host",
		Runtimes:       nil,
		FailedProfiles: nil,
	})
	register := httptest.NewRecorder()
	testHandler.DaemonRegister(register, req)
	if register.Code != http.StatusOK {
		t.Fatalf("zero-runtime register: got %d: %s", register.Code, register.Body.String())
	}

	var connectedAt *time.Time
	var daemonID *string
	var runtimeCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT daemon_connected_at, daemon_id, runtime_count
		FROM setup_token WHERE id = $1
	`, parseUUID(created.ID)).Scan(&connectedAt, &daemonID, &runtimeCount); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if connectedAt == nil || daemonID == nil || *daemonID != "setup-zero-runtime-daemon" || runtimeCount != 0 {
		t.Fatalf("unexpected zero-runtime progress: connected=%v daemon=%v count=%d", connectedAt, daemonID, runtimeCount)
	}
}
