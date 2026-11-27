package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDeviceAuthorizationExpiryAndAccountBinding(t *testing.T) {
	if testHandler == nil {
		t.Skip("database required")
	}
	t.Setenv("JWT_SECRET", "device-integration-secret")
	a := dbfx.User(t, "Approver A", uuid.NewString()+"@example.com")
	b := dbfx.User(t, "Approver B", uuid.NewString()+"@example.com")
	for _, expiredStatus := range []string{"pending", "approved", "valid"} {
		t.Run(expiredStatus, func(t *testing.T) {
			var start StartDeviceAuthResponse
			testutil.Call(t, testHandler.StartDeviceAuthorization, newRequestAs("", http.MethodPost, "/auth/device/start", map[string]string{"client_name": "  build-runner-01 \t"})).Want(200).JSON(&start)
			dbfx.Cleanup(t, "DELETE FROM device_authorization WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode))
			var stored string
			if err := testPool.QueryRow(context.Background(), "SELECT device_code_hash FROM device_authorization WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode)).Scan(&stored); err != nil || stored == start.DeviceCode {
				t.Fatal("device code not stored as a hash", err)
			}
			if start.ExpiresIn != 600 {
				t.Fatalf("TTL=%d", start.ExpiresIn)
			}
			body := map[string]string{"user_code": start.UserCode}
			var details map[string]string
			testutil.Call(t, testHandler.InspectDeviceAuthorization, newRequestAs(a, http.MethodPost, "/api/device/inspect", body)).Want(200).JSON(&details)
			if details["client_name"] != "build-runner-01" {
				t.Fatal(details)
			}
			// Inspection must leave the code pending, and unauthenticated users cannot inspect/approve.
			testutil.Call(t, testHandler.InspectDeviceAuthorization, newRequestAs("", http.MethodPost, "/api/device/inspect", body)).Want(401)
			testutil.Call(t, testHandler.ApproveDeviceAuthorization, newRequestAs("", http.MethodPost, "/api/device/approve", body)).Want(401)
			poll := func(status int) DeviceTokenResponse {
				var out DeviceTokenResponse
				testutil.Call(t, testHandler.DeviceToken, newRequestAs("", http.MethodPost, "/auth/device/token", map[string]string{"device_code": start.DeviceCode})).Want(status).JSON(&out)
				return out
			}
			if got := poll(400); got.Error != deviceErrPending {
				t.Fatal(got.Error)
			}
			if got := poll(400); got.Error != deviceErrSlow {
				t.Fatal(got.Error)
			}
			if expiredStatus != "pending" {
				testutil.Call(t, testHandler.ApproveDeviceAuthorization, newRequestAs(a, http.MethodPost, "/api/device/approve", body)).Want(200)
				testutil.Call(t, testHandler.ApproveDeviceAuthorization, newRequestAs(b, http.MethodPost, "/api/device/approve", body)).Want(400)
			}
			if expiredStatus != "valid" {
				dbfx.Exec(t, "UPDATE device_authorization SET expires_at=now()-interval '1 second' WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode))
				if got := poll(400); got.Error != deviceErrExpired || got.Token != "" {
					t.Fatal("expired code returned credentials", got.Error)
				}
				testutil.Call(t, testHandler.ApproveDeviceAuthorization, newRequestAs(a, http.MethodPost, "/api/device/approve", body)).Want(400)
			} else {
				got := poll(200)
				if got.Token == "" || decodeJWTSub(t, got.Token) != a {
					t.Fatal("token not bound to first approver")
				}
				var consumed bool
				if err := testPool.QueryRow(context.Background(), "SELECT consumed_at IS NOT NULL FROM device_authorization WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode)).Scan(&consumed); err != nil || !consumed {
					t.Fatal("collected authorization not marked consumed", err)
				}
				if got := poll(400); got.Error != deviceErrInvalid {
					t.Fatalf("replay=%s", got.Error)
				}
			}
		})
	}
}

// The table never holds a login token: approval stores only the approving
// user, and the JWT is minted when the CLI collects the row.
func TestDeviceAuthorizationStoresNoToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database required")
	}
	var n int
	if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.columns WHERE table_name='device_authorization' AND column_name='token'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("device_authorization has a token column (n=%d, err=%v)", n, err)
	}
}

func TestDeviceAuthorizationConcurrentConsumeOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("database required")
	}
	approver := uuid.NewString()
	id := dbfx.Insert(t, "device_authorization", testutil.Cols{"device_code_hash": uuid.NewString(), "user_code_hash": uuid.NewString(), "status": "approved", "user_id": approver, "expires_at": time.Now().Add(time.Minute)})
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userID, err := testHandler.Queries.ConsumeDeviceAuthorization(context.Background(), parseUUID(id))
			if err == nil && uuidToString(userID) == approver {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("successful consumers=%d", success.Load())
	}
}

// Approving hands the approver's login to the requesting machine, so a
// machine credential must not be able to inspect or approve a code, even if
// a route refactor drops RequireHumanActor (MUL-8154 backstop). The row must
// stay pending, and a human session can still approve it afterwards.
func TestDeviceAuthorizationRejectsMachineCredentials(t *testing.T) {
	if testHandler == nil {
		t.Skip("database required")
	}
	t.Setenv("JWT_SECRET", "device-integration-secret")
	var start StartDeviceAuthResponse
	testutil.Call(t, testHandler.StartDeviceAuthorization, newRequestAs("", http.MethodPost, "/auth/device/start", map[string]string{"client_name": "build-runner-01"})).Want(200).JSON(&start)
	dbfx.Cleanup(t, "DELETE FROM device_authorization WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode))
	body := map[string]string{"user_code": start.UserCode}
	status := func() string {
		var s string
		if err := testPool.QueryRow(context.Background(), "SELECT status FROM device_authorization WHERE device_code_hash=$1", auth.HashToken(start.DeviceCode)).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, source := range []string{"task_token", "cloud_pat"} {
		for name, call := range map[string]http.HandlerFunc{"inspect": testHandler.InspectDeviceAuthorization, "approve": testHandler.ApproveDeviceAuthorization} {
			t.Run(source+" "+name, func(t *testing.T) {
				req := newRequest(http.MethodPost, "/api/device/"+name, body)
				req.Header.Set("X-Actor-Source", source)
				w := httptest.NewRecorder()
				call(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
				}
				if got := status(); got != "pending" {
					t.Fatalf("authorization status = %q after rejected %s", got, name)
				}
			})
		}
	}
	testutil.Call(t, testHandler.ApproveDeviceAuthorization, newRequest(http.MethodPost, "/api/device/approve", body)).Want(200)
	if got := status(); got != "approved" {
		t.Fatalf("human approval left status %q", got)
	}
}
