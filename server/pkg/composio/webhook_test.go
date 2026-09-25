package composio_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/composio"
)

// helper: produce a valid signature for the given inputs.
func sign(secret, id, ts, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id + "." + ts + "." + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook_Success(t *testing.T) {
	secret := "shh"
	body := `{"id":"evt_1","type":"composio.connected_account.expired"}`
	id := "msg_abc"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, id, ts, body)

	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: "v1," + sig,
	}, []byte(body), composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
}

func TestVerifyWebhook_AcceptsBareSignature(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_b"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, id, ts, body)
	// No version prefix: just the raw base64
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: sig,
	}, []byte(body), composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyWebhook bare: %v", err)
	}
}

func TestVerifyWebhook_AcceptsMultipleVersions(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_c"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	good := sign(secret, id, ts, body)
	bad := "AAAA" + good[4:]
	// One bad sig, one good sig — verify should still pass.
	hdr := "v2," + bad + " v1," + good
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: hdr,
	}, []byte(body), composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyWebhook multi: %v", err)
	}
}

func TestVerifyWebhook_RejectsTamperedBody(t *testing.T) {
	secret := "shh"
	body := `{"data":"original"}`
	id := "msg_d"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, id, ts, body)

	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: "v1," + sig,
	}, []byte(`{"data":"tampered"}`), composio.VerifyOptions{})
	if !errors.Is(err, composio.ErrInvalidWebhookSignature) {
		t.Fatalf("expected ErrInvalidWebhookSignature, got %v", err)
	}
}

func TestVerifyWebhook_RejectsStaleTimestamp(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_e"
	old := time.Now().Add(-10 * time.Minute).Unix()
	ts := strconv.FormatInt(old, 10)
	sig := sign(secret, id, ts, body)
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: "v1," + sig,
	}, []byte(body), composio.VerifyOptions{Tolerance: 5 * time.Minute})
	if !errors.Is(err, composio.ErrWebhookTimestampStale) {
		t.Fatalf("expected ErrWebhookTimestampStale, got %v", err)
	}
}

func TestVerifyWebhook_NegativeToleranceDisablesCheck(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_f"
	ts := "1" // ancient
	sig := sign(secret, id, ts, body)
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: "v1," + sig,
	}, []byte(body), composio.VerifyOptions{Tolerance: -1})
	if err != nil {
		t.Fatalf("VerifyWebhook negative tolerance: %v", err)
	}
}

func TestVerifyWebhook_HonorsCustomNow(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_g"
	ts := "1700000000"
	sig := sign(secret, id, ts, body)
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: ts, Signature: "v1," + sig,
	}, []byte(body), composio.VerifyOptions{
		Tolerance: 5 * time.Second,
		Now:       func() time.Time { return time.Unix(1700000003, 0) },
	})
	if err != nil {
		t.Fatalf("expected fresh timestamp, got %v", err)
	}
}

func TestVerifyWebhook_MissingHeaders(t *testing.T) {
	err := composio.VerifyWebhook("shh", composio.WebhookHeaders{}, []byte(`{}`), composio.VerifyOptions{})
	if !errors.Is(err, composio.ErrMissingWebhookHeaders) {
		t.Fatalf("expected ErrMissingWebhookHeaders, got %v", err)
	}
}

func TestVerifyWebhook_EmptySecret(t *testing.T) {
	err := composio.VerifyWebhook("", composio.WebhookHeaders{
		ID: "x", Timestamp: "1", Signature: "v1,xyz",
	}, []byte(`{}`), composio.VerifyOptions{})
	if !errors.Is(err, composio.ErrWebhookSecretMissing) {
		t.Fatalf("expected ErrWebhookSecretMissing, got %v", err)
	}
}

func TestVerifyWebhook_AcceptsRFC3339Timestamp(t *testing.T) {
	secret := "shh"
	body := `{}`
	id := "msg_h"
	now := time.Now().UTC().Format(time.RFC3339)
	sig := sign(secret, id, now, body)
	err := composio.VerifyWebhook(secret, composio.WebhookHeaders{
		ID: id, Timestamp: now, Signature: "v1," + sig,
	}, []byte(body), composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyWebhook rfc3339: %v", err)
	}
}

func TestVerifyHTTPRequest_HappyPath(t *testing.T) {
	secret := "shh"
	body := `{"x":1}`
	id := "msg_req"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, id, ts, body)

	r := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(body)))
	r.Header.Set(composio.HeaderWebhookID, id)
	r.Header.Set(composio.HeaderWebhookTimestamp, ts)
	r.Header.Set(composio.HeaderWebhookSignature, "v1,"+sig)

	got, err := composio.VerifyHTTPRequest(secret, r, composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyHTTPRequest: %v", err)
	}
	if string(got) != body {
		t.Errorf("body roundtrip mismatch: %q vs %q", got, body)
	}
}

func TestVerifyHTTPRequest_ReturnsBodyOnFailure(t *testing.T) {
	body := `{"x":1}`
	r := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(body)))
	r.Header.Set(composio.HeaderWebhookID, "id")
	r.Header.Set(composio.HeaderWebhookTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	r.Header.Set(composio.HeaderWebhookSignature, "v1,deadbeef")

	got, err := composio.VerifyHTTPRequest("shh", r, composio.VerifyOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if string(got) != body {
		t.Errorf("expected body returned for logging, got %q", got)
	}
}

func TestVerifyHTTPRequest_NilBody(t *testing.T) {
	r := &http.Request{}
	_, err := composio.VerifyHTTPRequest("shh", r, composio.VerifyOptions{})
	if err == nil {
		t.Fatal("expected error for nil body")
	}
}

// Sanity check: io.ReadAll still gets the same body bytes via our helper.
func TestVerifyHTTPRequest_BodyReadFully(t *testing.T) {
	body := "{}"
	r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(bytes.NewReader([]byte(body))))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r.Header.Set(composio.HeaderWebhookID, "id")
	r.Header.Set(composio.HeaderWebhookTimestamp, ts)
	r.Header.Set(composio.HeaderWebhookSignature, "v1,"+sign("shh", "id", ts, body))
	got, err := composio.VerifyHTTPRequest("shh", r, composio.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyHTTPRequest: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

// ---------------------------------------------------------------------------
// Event envelope
// ---------------------------------------------------------------------------

func TestParseEvent_V3Envelope(t *testing.T) {
	raw := []byte(`{
		"id": "evt_1",
		"type": "composio.connected_account.expired",
		"metadata": {"project_id":"pr_a","user_id":"u_1"},
		"data":     {"id":"ca_1","status":"EXPIRED"},
		"timestamp": "2026-02-06T12:00:00Z"
	}`)
	ev, err := composio.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if ev.ID != "evt_1" || ev.Type != "composio.connected_account.expired" {
		t.Errorf("unexpected envelope: %+v", ev)
	}
	if !bytes.Contains(ev.Data, []byte(`"EXPIRED"`)) {
		t.Errorf("data lost: %s", ev.Data)
	}
}

func TestParseEvent_RejectsGarbage(t *testing.T) {
	if _, err := composio.ParseEvent([]byte(`not-json`)); err == nil {
		t.Error("expected error for non-JSON body")
	}
}
