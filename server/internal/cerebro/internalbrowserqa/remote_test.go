package internalbrowserqa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteRunnerMovesBrowserExecutionAcrossServerBoundaryWithoutLeakingCredential(t *testing.T) {
	const username = "registry-test@example.com"
	const password = "must-never-appear-in-response"
	var gotAuthorization string
	var gotRequest remoteVerifyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(Result{
			App: "registry", InternalHost: "firtal-data-registry-private.internal:3000",
			FinalURL: "http://firtal-data-registry-private.internal:3000/", Markers: []string{"Dashboard"},
			ScreenshotPNG: append([]byte(nil), pngSignature...),
		})
	}))
	defer server.Close()

	runner, err := NewRemoteRunner(server.URL, "runner-token", server.Client())
	if err != nil {
		t.Fatalf("NewRemoteRunner: %v", err)
	}
	result, err := runner.Verify(context.Background(), "registry", Credential{Username: username, Password: password})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if gotAuthorization != "Bearer runner-token" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
	if gotRequest.App != "registry" || gotRequest.Credential.Username != username || gotRequest.Credential.Password != password {
		t.Fatalf("runner request did not carry the authorized job")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), username) || strings.Contains(string(encoded), password) {
		t.Fatalf("credential leaked in result: %s", encoded)
	}
}

func TestRemoteRunnerRequiresHTTPSOutsideTests(t *testing.T) {
	if _, err := NewRemoteRunner("http://verifier.example.com", "token", http.DefaultClient); err == nil {
		t.Fatal("public plaintext verifier URL was accepted")
	}
}

func TestRunnerHTTPHandlerRejectsWrongTokenBeforeBrowserExecution(t *testing.T) {
	handler := NewRunnerHTTPHandler("correct-token", NewRunner(failingCommander{}))
	req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(`{"app":"registry","credential":{"username":"u","password":"p"}}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "wrong-token") {
		t.Fatal("response leaked authorization token")
	}
}

func TestRunnerHTTPHandlerKeepsBrowserTargetOnPrivateInternalAllowlist(t *testing.T) {
	handler := NewRunnerHTTPHandler("runner-token", NewRunner(&recordingCommander{}))
	req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(`{"app":"https://registry.firtal.com","credential":{}}`))
	req.Header.Set("Authorization", "Bearer runner-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
