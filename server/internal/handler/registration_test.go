package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRegistrationModeOpen(t *testing.T) {
	os.Unsetenv("MULTICA_REGISTRATION_MODE")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	const email = "reg-open-test@multica.ai"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode in open mode: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegistrationModeInviteOnly(t *testing.T) {
	os.Setenv("MULTICA_REGISTRATION_MODE", "invite_only")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	const email = "reg-invite-new@multica.ai"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	// SendCode itself should reject — no email wasted
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SendCode invite_only (new user): expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegistrationModeInviteOnlyAllowsExistingMember(t *testing.T) {
	os.Setenv("MULTICA_REGISTRATION_MODE", "invite_only")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	// The handler test fixture already created a user (handlerTestEmail) who is a
	// member of a workspace. Use that email to verify invite_only allows existing members.
	const email = handlerTestEmail
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read code from DB
	ctx := context.Background()
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// VerifyCode should succeed because user exists and has a workspace
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode for existing member in invite_only: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Token == "" {
		t.Fatal("VerifyCode: expected non-empty token")
	}
}

func TestRegistrationModeClosed(t *testing.T) {
	os.Setenv("MULTICA_REGISTRATION_MODE", "closed")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": "reg-closed@multica.ai"})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SendCode in closed mode: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "registration is closed" {
		t.Fatalf("expected error 'registration is closed', got %q", resp["error"])
	}
}

func TestAllowedDomainsAccepted(t *testing.T) {
	os.Unsetenv("MULTICA_REGISTRATION_MODE")
	os.Setenv("MULTICA_ALLOWED_DOMAINS", "multica.ai,example.com")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	const email = "domain-ok@multica.ai"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode with allowed domain: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAllowedDomainsRejected(t *testing.T) {
	os.Unsetenv("MULTICA_REGISTRATION_MODE")
	os.Setenv("MULTICA_ALLOWED_DOMAINS", "multica.ai,example.com")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": "blocked@evil.com"})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SendCode with blocked domain: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "email domain is not allowed" {
		t.Fatalf("expected error 'email domain is not allowed', got %q", resp["error"])
	}
}

func TestAllowedDomainsEmptyMeansAll(t *testing.T) {
	os.Unsetenv("MULTICA_REGISTRATION_MODE")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	const email = "anyone@anydomain.org"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode with no domain restriction: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegistrationModeInviteOnlyNoAutoWorkspace(t *testing.T) {
	os.Setenv("MULTICA_REGISTRATION_MODE", "invite_only")
	os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	t.Cleanup(func() {
		os.Unsetenv("MULTICA_REGISTRATION_MODE")
		os.Unsetenv("MULTICA_ALLOWED_DOMAINS")
	})

	// Use the existing fixture user who already has a workspace.
	const email = handlerTestEmail
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Count workspaces before
	user, err := testHandler.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	wsBefore, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces before: %v", err)
	}

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read code from DB
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Count workspaces after — should be unchanged (no auto-creation)
	wsAfter, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces after: %v", err)
	}
	if len(wsAfter) != len(wsBefore) {
		t.Fatalf("invite_only should not auto-create workspace: had %d before, got %d after", len(wsBefore), len(wsAfter))
	}
}
