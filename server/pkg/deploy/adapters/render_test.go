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

const renderTestSecret = "render-shared"

func renderEnv(secret string) *deploy.Environment {
	return &deploy.Environment{
		AdapterKind:   "render",
		Config:        json.RawMessage(`{"service_id":"srv-abcdefg","api_key":"rnd_key"}`),
		WebhookSecret: secret,
		TargetBranch:  "main",
	}
}

func TestRender_VerifySignature_AcceptsValid(t *testing.T) {
	a := &renderAdapter{}
	body := loadFixture(t, "render_succeeded.json")
	headers := http.Header{}
	headers.Set("x-webhook-signature", hmacSHA256Hex(body, renderTestSecret))
	if err := a.VerifySignature(renderEnv(renderTestSecret), headers, body); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestRender_VerifySignature_RejectsBad(t *testing.T) {
	a := &renderAdapter{}
	body := loadFixture(t, "render_succeeded.json")
	headers := http.Header{}
	headers.Set("x-webhook-signature", "00")
	if err := a.VerifySignature(renderEnv(renderTestSecret), headers, body); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestRender_OnWebhook_HitsAPIForSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/services/srv-abcdefg/deploys/dep-xxx") {
			_, _ = w.Write([]byte(`{"commit": {"id": "feedface1234feedface1234feedface1234feed"}}`))
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer srv.Close()
	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &renderAdapter{}
	body := loadFixture(t, "render_succeeded.json")
	event, err := a.OnWebhook(context.Background(), renderEnv(renderTestSecret), body)
	if err != nil {
		t.Fatalf("OnWebhook: %v", err)
	}
	if event.Status != "succeeded" {
		t.Errorf("status: %q", event.Status)
	}
	if event.SHA != "feedface1234feedface1234feedface1234feed" {
		t.Errorf("sha: %q", event.SHA)
	}
}

func TestRender_RollbackUnsupported(t *testing.T) {
	a := &renderAdapter{}
	if a.SupportsRollback() {
		t.Error("expected render adapter to not support rollback")
	}
	if err := a.Rollback(context.Background(), renderEnv(""), "abc"); !errors.Is(err, deploy.ErrRollbackNotSupported) {
		t.Errorf("expected ErrRollbackNotSupported, got %v", err)
	}
}
