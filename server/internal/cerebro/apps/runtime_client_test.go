package apps

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeClientSignsDeploymentRequest(t *testing.T) {
	const secret = "runtime-service-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyHash := sha256.Sum256(body)
		canonical := strings.Join([]string{r.Method, r.URL.EscapedPath(), hex.EncodeToString(bodyHash[:]), r.Header.Get("X-Multica-Timestamp")}, "\n")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(canonical))
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(want), []byte(r.Header.Get("X-Multica-Signature"))) {
			t.Errorf("invalid signature: got %q want %q", r.Header.Get("X-Multica-Signature"), want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"provisioning"}`))
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, secret)
	err := client.Deploy(context.Background(), RuntimeDeploymentRequest{
		AppID:        "f1540000-0000-4154-8154-000000000001",
		Version:      "1.0.0",
		BundleSHA256: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
}

func TestRuntimeClientInvokesOneAppVersionWithSignedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workers/f1540000-0000-4154-8154-000000000001/1.0.0/invoke" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		bodyHash := sha256.Sum256(body)
		canonical := strings.Join([]string{r.Method, r.URL.EscapedPath(), hex.EncodeToString(bodyHash[:]), r.Header.Get("X-Multica-Timestamp")}, "\n")
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(canonical))
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(want), []byte(r.Header.Get("X-Multica-Signature"))) {
			t.Fatal("invoke signature mismatch")
		}
		_, _ = w.Write([]byte(`{"formatted":"MILK"}`))
	}))
	defer server.Close()

	output, err := NewRuntimeClient(server.URL, "secret").Invoke(context.Background(), "f1540000-0000-4154-8154-000000000001", "1.0.0", json.RawMessage(`{"value":"milk"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if string(output) != `{"formatted":"MILK"}` {
		t.Fatalf("unexpected output %s", output)
	}
}

func TestRuntimeClientSignsLifecycleRequests(t *testing.T) {
	const secret = "runtime-service-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lifecycle/pause" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		bodyHash := sha256.Sum256(body)
		canonical := strings.Join([]string{r.Method, r.URL.EscapedPath(), hex.EncodeToString(bodyHash[:]), r.Header.Get("X-Multica-Timestamp")}, "\n")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(canonical))
		if got, want := r.Header.Get("X-Multica-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Fatalf("invalid signature: got %q want %q", got, want)
		}
		if string(body) != `{"service_id":"service-123"}` {
			t.Fatalf("unexpected body %s", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := NewRuntimeClient(server.URL, secret).Lifecycle(context.Background(), "pause", "service-123"); err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
}

func TestRuntimeClientMasksRemoteFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "/srv/runtime/private.js: secret=do-not-expose", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := NewRuntimeClient(server.URL, "secret").Deploy(context.Background(), RuntimeDeploymentRequest{})
	if err == nil {
		t.Fatal("remote failure was accepted")
	}
	if strings.Contains(err.Error(), "/srv/") || strings.Contains(err.Error(), "do-not-expose") {
		t.Fatalf("runtime detail escaped to caller: %v", err)
	}
}

func TestRuntimeClientHonorsRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, "secret")
	client.httpClient.Timeout = 10 * time.Millisecond
	if err := client.Deploy(context.Background(), RuntimeDeploymentRequest{}); err == nil {
		t.Fatal("timed out deployment was accepted")
	}
}

func TestRuntimeSignatureRejectsReplayAndTampering(t *testing.T) {
	now := time.Date(2026, time.July, 15, 18, 0, 0, 0, time.UTC)
	body := []byte(`{"status":"ready"}`)
	timestamp := now.Format(time.RFC3339)
	signature := signRuntimeRequest("secret", http.MethodPost, "/internal/apps/a/1/callback", body, timestamp)
	if err := verifyRuntimeSignature("secret", http.MethodPost, "/internal/apps/a/1/callback", body, timestamp, signature, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := verifyRuntimeSignature("secret", http.MethodPost, "/internal/apps/b/1/callback", body, timestamp, signature, now); err == nil {
		t.Fatal("signature was reusable for another app path")
	}
	stale := now.Add(-3 * time.Minute).Format(time.RFC3339)
	staleSignature := signRuntimeRequest("secret", http.MethodPost, "/internal/apps/a/1/callback", body, stale)
	if err := verifyRuntimeSignature("secret", http.MethodPost, "/internal/apps/a/1/callback", body, stale, staleSignature, now); err == nil {
		t.Fatal("stale signature was accepted")
	}
}

func TestBundleTokenIsBoundToAppVersionAndExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 15, 18, 0, 0, 0, time.UTC)
	appID := "f1540000-0000-4154-8154-000000000001"
	token := mintBundleToken("secret", appID, "1.0.0", now.Add(30*time.Minute))
	if err := verifyBundleToken("secret", token, appID, "1.0.0", now); err != nil {
		t.Fatalf("valid bundle token rejected: %v", err)
	}
	if err := verifyBundleToken("secret", token, appID, "2.0.0", now); err == nil {
		t.Fatal("bundle token escaped its version")
	}
	if err := verifyBundleToken("secret", token, appID, "1.0.0", now.Add(31*time.Minute)); err == nil {
		t.Fatal("expired bundle token was accepted")
	}
}

func TestInvocationGrantIsBoundToHumanAppAndExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 15, 18, 0, 0, 0, time.UTC)
	grant := invocationGrant{AppID: "f1540000-0000-4154-8154-000000000001", Version: "1.0.0", WorkspaceID: "11111111-1111-4111-8111-111111111111", MemberID: "22222222-2222-4222-8222-222222222222"}
	token := mintInvocationGrant("secret", grant, now.Add(2*time.Minute))
	verified, err := verifyInvocationGrant("secret", token, now)
	if err != nil || verified.AppID != grant.AppID || verified.MemberID != grant.MemberID {
		t.Fatalf("valid invocation grant rejected: %+v %v", verified, err)
	}
	if _, err := verifyInvocationGrant("other", token, now); err == nil {
		t.Fatal("invocation grant was accepted by another signer")
	}
	if _, err := verifyInvocationGrant("secret", token, now.Add(3*time.Minute)); err == nil {
		t.Fatal("expired invocation grant was accepted")
	}
}
