package handler

// CEREBRO-PATCH(auth-master-code-handler): cerebro modification of upstream file

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestVerifyCodeRejectsMasterCode verifies that "888888" — previously a
// master backdoor when APP_ENV != "production" — is rejected like any other
// wrong code. The error must be indistinguishable from a normal mismatch:
// no "master code rejected" wording, no hint that master codes ever existed.
func TestVerifyCodeRejectsMasterCode(t *testing.T) {
	const email = "master-code-rejection-test@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Even with APP_ENV unset (the previously-vulnerable case), 888888 must fail.
	t.Setenv("APP_ENV", "")

	// Issue a real verification code so there's a row in the table.
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Submit 888888.
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 888888 master code, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Same response as any other wrong code — no leak about master codes.
	const wantError = `"invalid or expired code"`
	if !bytes.Contains([]byte(body), []byte(wantError)) {
		t.Fatalf("expected body to contain %s, got: %s", wantError, body)
	}
	for _, leak := range []string{"master", "888888", "backdoor", "dev code"} {
		if bytes.Contains(bytes.ToLower([]byte(body)), bytes.ToLower([]byte(leak))) {
			t.Fatalf("response leaks information about master code (%q): %s", leak, body)
		}
	}

	// And no JWT token issued.
	var resp map[string]any
	_ = json.Unmarshal([]byte(body), &resp)
	if _, ok := resp["token"]; ok {
		t.Fatalf("888888 returned a token: %s", body)
	}
}
