package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

const vercelTestSecret = "whsec_vercel_test"

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return b
}

func vercelEnv(secret string) *deploy.Environment {
	return &deploy.Environment{
		AdapterKind:   "vercel",
		Config:        json.RawMessage(`{"team_id":"team_xyz","project_id":"prj_abc123","token":"vc_token"}`),
		WebhookSecret: secret,
		TargetBranch:  "main",
	}
}

func TestVercel_VerifySignature_AcceptsValid(t *testing.T) {
	a := &vercelAdapter{}
	body := loadFixture(t, "vercel_succeeded.json")
	sig := hmacSHA1Hex(body, vercelTestSecret)
	headers := http.Header{}
	headers.Set("x-vercel-signature", sig)
	if err := a.VerifySignature(vercelEnv(vercelTestSecret), headers, body); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestVercel_VerifySignature_RejectsBad(t *testing.T) {
	a := &vercelAdapter{}
	body := loadFixture(t, "vercel_succeeded.json")
	headers := http.Header{}
	headers.Set("x-vercel-signature", "deadbeef")
	if err := a.VerifySignature(vercelEnv(vercelTestSecret), headers, body); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestVercel_VerifySignature_RejectsMissingHeader(t *testing.T) {
	a := &vercelAdapter{}
	body := loadFixture(t, "vercel_succeeded.json")
	if err := a.VerifySignature(vercelEnv(vercelTestSecret), http.Header{}, body); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid for missing header, got %v", err)
	}
}

func TestVercel_OnWebhook_ExtractsSHAAndStatus(t *testing.T) {
	a := &vercelAdapter{}
	body := loadFixture(t, "vercel_succeeded.json")
	event, err := a.OnWebhook(context.Background(), vercelEnv(vercelTestSecret), body)
	if err != nil {
		t.Fatalf("OnWebhook failed: %v", err)
	}
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Status != "succeeded" {
		t.Errorf("status: want 'succeeded', got %q", event.Status)
	}
	if event.SHA != "deadbeef000000000000000000000000deadbeef" {
		t.Errorf("sha: got %q", event.SHA)
	}
	if event.Ref != "main" {
		t.Errorf("ref: want 'main', got %q", event.Ref)
	}
}

func TestVercel_OnWebhook_IrrelevantProject(t *testing.T) {
	a := &vercelAdapter{}
	env := vercelEnv(vercelTestSecret)
	env.Config = json.RawMessage(`{"project_id":"prj_someoneelse","token":"x"}`)
	body := loadFixture(t, "vercel_succeeded.json")
	_, err := a.OnWebhook(context.Background(), env, body)
	if !errors.Is(err, deploy.ErrIrrelevantPayload) {
		t.Fatalf("expected ErrIrrelevantPayload, got %v", err)
	}
}

func TestVercel_PollCurrent_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vc_token" {
			t.Errorf("missing/incorrect Authorization, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deployments": [
				{
					"uid": "dpl_xyz",
					"url": "myapp-xyz.vercel.app",
					"created": 1700000000000,
					"meta": {"githubCommitSha": "deadbeef000000000000000000000000deadbeef"}
				}
			]
		}`))
	}))
	defer srv.Close()

	// Override the Vercel base URL by intercepting via the http client's
	// Transport instead — we wire the adapter to dial our test server.
	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &vercelAdapter{}
	state, err := a.PollCurrent(context.Background(), vercelEnv(vercelTestSecret))
	if err != nil {
		t.Fatalf("PollCurrent: %v", err)
	}
	if state == nil || state.CurrentSHA != "deadbeef000000000000000000000000deadbeef" {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestVercel_Rollback_PromotesMatchingDeployment(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/v6/deployments"):
			_, _ = w.Write([]byte(`{
				"deployments": [
					{"uid": "dpl_old", "meta": {"githubCommitSha": "abc123abc123abc123abc123abc123abc123abcd"}}
				]
			}`))
		case strings.Contains(r.URL.Path, "/v13/deployments/dpl_old/promote"):
			_, _ = w.Write([]byte(`{"ok": true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &vercelAdapter{}
	if err := a.Rollback(context.Background(), vercelEnv(vercelTestSecret), "abc123abc123abc123abc123abc123abc123abcd"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if hits != 2 {
		t.Errorf("expected 2 round-trips (find + promote), got %d", hits)
	}
}
