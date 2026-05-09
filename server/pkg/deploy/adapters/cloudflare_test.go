package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

const cfTestSecret = "cf-shared-secret"

func cloudflareEnv(secret string) *deploy.Environment {
	return &deploy.Environment{
		AdapterKind:   "cloudflare",
		Config:        json.RawMessage(`{"account_id":"acc1","project_name":"my-pages-project","api_token":"tok"}`),
		WebhookSecret: secret,
		TargetBranch:  "main",
	}
}

func TestCloudflare_VerifySignature_AcceptsSharedSecret(t *testing.T) {
	a := &cloudflareAdapter{}
	headers := http.Header{}
	headers.Set("cf-webhook-auth", cfTestSecret)
	if err := a.VerifySignature(cloudflareEnv(cfTestSecret), headers, []byte("ignored")); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestCloudflare_VerifySignature_RejectsBad(t *testing.T) {
	a := &cloudflareAdapter{}
	headers := http.Header{}
	headers.Set("cf-webhook-auth", "wrong")
	if err := a.VerifySignature(cloudflareEnv(cfTestSecret), headers, nil); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestCloudflare_OnWebhook_ExtractsSHA(t *testing.T) {
	a := &cloudflareAdapter{}
	body := loadFixture(t, "cloudflare_succeeded.json")
	event, err := a.OnWebhook(context.Background(), cloudflareEnv(cfTestSecret), body)
	if err != nil {
		t.Fatalf("OnWebhook: %v", err)
	}
	if event.Status != "succeeded" {
		t.Errorf("status: want succeeded, got %q", event.Status)
	}
	if event.SHA != "cafebabe1234cafebabe1234cafebabe1234cafe" {
		t.Errorf("sha mismatch: %q", event.SHA)
	}
}

func TestCloudflare_OnWebhook_IrrelevantProject(t *testing.T) {
	a := &cloudflareAdapter{}
	env := cloudflareEnv(cfTestSecret)
	env.Config = json.RawMessage(`{"project_name":"different-project","account_id":"a","api_token":"t"}`)
	body := loadFixture(t, "cloudflare_succeeded.json")
	if _, err := a.OnWebhook(context.Background(), env, body); !errors.Is(err, deploy.ErrIrrelevantPayload) {
		t.Fatalf("expected ErrIrrelevantPayload, got %v", err)
	}
}

func TestCloudflare_PollCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/accounts/acc1/pages/projects/my-pages-project/deployments") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"result": [
				{
					"id": "abc",
					"url": "https://abc.my-pages-project.pages.dev",
					"created_on": "2024-01-15T12:00:00Z",
					"source": {"config": {"commit_hash": "cafebabe1234cafebabe1234cafebabe1234cafe"}},
					"latest_stage": {"status": "success"}
				}
			]
		}`))
	}))
	defer srv.Close()
	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &cloudflareAdapter{}
	state, err := a.PollCurrent(context.Background(), cloudflareEnv(cfTestSecret))
	if err != nil {
		t.Fatalf("PollCurrent: %v", err)
	}
	if state.CurrentSHA != "cafebabe1234cafebabe1234cafebabe1234cafe" {
		t.Errorf("sha mismatch: %q", state.CurrentSHA)
	}
}

func TestCloudflare_Rollback(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/deployments"):
			_, _ = w.Write([]byte(`{
				"result": [
					{"id": "old-id", "source": {"config": {"commit_hash": "deadbeef"}}}
				]
			}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/deployments/old-id/retry"):
			_, _ = w.Write([]byte(`{"ok": true}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &cloudflareAdapter{}
	if err := a.Rollback(context.Background(), cloudflareEnv(cfTestSecret), "deadbeef"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if hits != 2 {
		t.Errorf("expected 2 hits, got %d", hits)
	}
}
