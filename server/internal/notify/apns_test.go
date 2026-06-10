package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPNSSendPush_SendsHeadersAndPayload(t *testing.T) {
	var pushCalls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushCalls++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/3/device/device-token-1" {
			t.Fatalf("path = %q, want device path", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); !strings.HasPrefix(got, "bearer ") {
			t.Fatalf("authorization should be bearer token, got %q", got)
		}
		if got := r.Header.Get("apns-topic"); got != "com.wujieai.multica" {
			t.Fatalf("apns-topic = %q, want bundle id", got)
		}
		if got := r.Header.Get("apns-push-type"); got != "alert" {
			t.Fatalf("apns-push-type = %q, want alert", got)
		}
		if got := r.Header.Get("apns-id"); got != "11111111-1111-1111-1111-111111111111" {
			t.Fatalf("apns-id = %q, want delivery id", got)
		}
		if got := r.Header.Get("apns-collapse-id"); got != "issue-00000000-0000-0000-0000-000000000001" {
			t.Fatalf("apns-collapse-id = %q, want issue collapse id", got)
		}

		var body struct {
			APS struct {
				Alert struct {
					Title string `json:"title"`
					Body  string `json:"body"`
				} `json:"alert"`
				Sound string `json:"sound"`
			} `json:"aps"`
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode apns body: %v", err)
		}
		if body.APS.Alert.Title != "OPE-1" || body.APS.Alert.Body != "hello" || body.APS.Sound != "default" {
			t.Fatalf("unexpected aps payload: %#v", body.APS)
		}
		if body.URL != "wujieai-multicam://issues/issue-1" {
			t.Fatalf("url = %q, want deep link", body.URL)
		}

		w.Header().Set("apns-id", "apns-response-id")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(apiServer.Close)

	cfg := APNSConfig{
		TeamID:     "TEAMID1234",
		KeyID:      "KEYID1234",
		BundleID:   "com.wujieai.multica",
		AuthKeyP8:  testAPNSPrivateKeyPEM(t),
		BaseURL:    apiServer.URL,
		HTTPClient: apiServer.Client(),
	}

	result, err := cfg.SendPush(context.Background(), APNSPushMessage{
		DeviceToken: "device-token-1",
		RequestID:   "11111111-1111-1111-1111-111111111111",
		Title:       "OPE-1",
		Body:        "hello",
		ClickURL:    "wujieai-multicam://issues/issue-1",
		CollapseID:  "issue-00000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatalf("SendPush: %v", err)
	}
	if result.APNSID != "apns-response-id" {
		t.Fatalf("APNSID = %q, want response id", result.APNSID)
	}
	if pushCalls != 1 {
		t.Fatalf("expected one push call, got %d", pushCalls)
	}
}

func TestAPNSSendPush_ClassifiesInvalidDeviceToken(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	t.Cleanup(apiServer.Close)

	cfg := APNSConfig{
		TeamID:     "TEAMID1234",
		KeyID:      "KEYID1234",
		BundleID:   "com.wujieai.multica",
		AuthKeyP8:  testAPNSPrivateKeyPEM(t),
		BaseURL:    apiServer.URL,
		HTTPClient: apiServer.Client(),
	}

	_, err := cfg.SendPush(context.Background(), APNSPushMessage{
		DeviceToken: "device-token-invalid",
		Title:       "OPE-1",
		Body:        "hello",
	})
	if !errors.Is(err, ErrAPNSDeviceTokenInvalid) {
		t.Fatalf("expected ErrAPNSDeviceTokenInvalid, got %v", err)
	}
}

func TestAPNSSendPush_TemporaryErrorDoesNotClassifyInvalidToken(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"reason":"InternalServerError"}`))
	}))
	t.Cleanup(apiServer.Close)

	cfg := APNSConfig{
		TeamID:     "TEAMID1234",
		KeyID:      "KEYID1234",
		BundleID:   "com.wujieai.multica",
		AuthKeyP8:  testAPNSPrivateKeyPEM(t),
		BaseURL:    apiServer.URL,
		HTTPClient: apiServer.Client(),
	}

	_, err := cfg.SendPush(context.Background(), APNSPushMessage{
		DeviceToken: "device-token-retry",
		Title:       "OPE-1",
		Body:        "hello",
	})
	if err == nil {
		t.Fatal("expected temporary apns error")
	}
	if errors.Is(err, ErrAPNSDeviceTokenInvalid) {
		t.Fatalf("temporary error should not be invalid token: %v", err)
	}
}

func testAPNSPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
