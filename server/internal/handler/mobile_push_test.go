package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

func TestMobilePushSessionRequiresMatchingExpiringHumanJWT(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sub    string
		exp    time.Time
		method jwt.SigningMethod
		want   bool
	}{
		{"valid", "user", time.Now().Add(time.Hour), jwt.SigningMethodHS256, true},
		{"expired", "user", time.Now().Add(-time.Hour), jwt.SigningMethodHS256, false},
		{"other user", "other", time.Now().Add(time.Hour), jwt.SigningMethodHS256, false},
		{"other algorithm", "user", time.Now().Add(time.Hour), jwt.SigningMethodHS384, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(tc.method, jwt.MapClaims{"sub": tc.sub, "exp": tc.exp.Unix()}).SignedString(auth.JWTSecret())
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPut, "/api/mobile-push", nil)
			req.Header.Set("Authorization", "Bearer "+raw)
			req.Header.Set("X-User-ID", "user")
			_, _, ok := mobilePushSession(req)
			if ok != tc.want {
				t.Fatalf("got %v want %v", ok, tc.want)
			}
		})
	}
}

func TestMobilePushSubscriptionRejectsDisabledAndMalformedRequests(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": testUserID, "exp": time.Now().Add(time.Hour).Unix()}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatal(err)
	}
	previous := testHandler.MobilePushEnabled
	t.Cleanup(func() { testHandler.MobilePushEnabled = previous })
	for _, tc := range []struct {
		enabled bool
		body    string
		want    int
	}{
		{false, `{"token":"ExpoPushToken[test]"}`, http.StatusServiceUnavailable},
		{true, `{"token":"bad"}`, http.StatusBadRequest},
		{true, `{`, http.StatusBadRequest},
	} {
		testHandler.MobilePushEnabled = tc.enabled
		req := httptest.NewRequest(http.MethodPut, "/api/mobile-push", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("X-User-ID", testUserID)
		recorder := httptest.NewRecorder()
		testHandler.MobilePushSubscription(recorder, req)
		if recorder.Code != tc.want {
			t.Fatalf("status=%d want=%d", recorder.Code, tc.want)
		}
	}
}

func TestMobilePushSubscriptionLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	previous := testHandler.MobilePushEnabled
	testHandler.MobilePushEnabled = true
	t.Cleanup(func() { testHandler.MobilePushEnabled = previous })
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": testUserID, "exp": expiry.Unix()}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatal(err)
	}
	token := "ExpoPushToken[lifecycle]"
	dbfx.Cleanup(t, "DELETE FROM mobile_push_subscription WHERE token=$1", token)
	request := func(method string) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/mobile-push", strings.NewReader(`{"token":"`+token+`"}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("X-User-ID", testUserID)
		recorder := httptest.NewRecorder()
		testHandler.MobilePushSubscription(recorder, req)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	request(http.MethodPut)
	var user string
	var expires time.Time
	dbfx.QueryRow(t, "SELECT user_id, expires_at FROM mobile_push_subscription WHERE token=$1", token).Scan(&user, &expires)
	if user != testUserID || !expires.Equal(expiry) {
		t.Fatalf("incorrect user or expiry: %s %s", user, expires)
	}
	request(http.MethodDelete)
	var count int
	dbfx.QueryRow(t, "SELECT count(*) FROM mobile_push_subscription WHERE token=$1", token).Scan(&count)
	if count != 0 {
		t.Fatal("subscription survived logout")
	}
}
