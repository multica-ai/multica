package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/pkg/deploy"
)

const genericTestSecret = "shh"

func genericEnv(cfg string, secret string) *deploy.Environment {
	return &deploy.Environment{
		AdapterKind:   "generic_webhook",
		Config:        json.RawMessage(cfg),
		WebhookSecret: secret,
		TargetBranch:  "main",
	}
}

func TestGeneric_VerifySignature_HMACSHA256(t *testing.T) {
	a := &genericWebhookAdapter{}
	body := loadFixture(t, "generic_succeeded.json")
	cfg := `{
		"status_path":"build.status",
		"sha_path":"build.commit.sha",
		"signature_header":"X-CI-Signature",
		"signature_algo":"hmac-sha256"
	}`
	headers := http.Header{}
	headers.Set("X-CI-Signature", hmacSHA256Hex(body, genericTestSecret))
	if err := a.VerifySignature(genericEnv(cfg, genericTestSecret), headers, body); err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
}

func TestGeneric_VerifySignature_SharedSecretFallback(t *testing.T) {
	a := &genericWebhookAdapter{}
	cfg := `{"status_path":"a","sha_path":"b"}`
	headers := http.Header{}
	headers.Set("X-Webhook-Secret", genericTestSecret)
	if err := a.VerifySignature(genericEnv(cfg, genericTestSecret), headers, nil); err != nil {
		t.Fatalf("expected default-shared-secret accept, got %v", err)
	}
}

func TestGeneric_VerifySignature_RejectsBad(t *testing.T) {
	a := &genericWebhookAdapter{}
	cfg := `{"status_path":"a","sha_path":"b","signature_header":"X","signature_algo":"hmac-sha256"}`
	headers := http.Header{}
	headers.Set("X", "00")
	if err := a.VerifySignature(genericEnv(cfg, genericTestSecret), headers, []byte(`{}`)); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Errorf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestGeneric_OnWebhook_GjsonExtraction(t *testing.T) {
	a := &genericWebhookAdapter{}
	body := loadFixture(t, "generic_succeeded.json")
	cfg := `{
		"status_path":"build.status",
		"sha_path":"build.commit.sha",
		"ref_path":"build.commit.branch",
		"log_url_path":"build.url",
		"status_map":{"PASSED":"succeeded","FAILED":"failed"}
	}`
	event, err := a.OnWebhook(context.Background(), genericEnv(cfg, genericTestSecret), body)
	if err != nil {
		t.Fatalf("OnWebhook: %v", err)
	}
	if event.Status != "succeeded" {
		t.Errorf("status: want succeeded, got %q", event.Status)
	}
	if event.SHA != "feedface1234feedface1234feedface1234feed" {
		t.Errorf("sha: %q", event.SHA)
	}
	if event.Ref != "main" {
		t.Errorf("ref: want main, got %q", event.Ref)
	}
	if event.LogURL == "" {
		t.Error("expected non-empty log_url")
	}
}

func TestGeneric_OnWebhook_IdentityFilter(t *testing.T) {
	a := &genericWebhookAdapter{}
	body := loadFixture(t, "generic_succeeded.json")
	cfg := `{
		"status_path":"build.status",
		"sha_path":"build.commit.sha",
		"identity_path":"project",
		"identity_value":"different-project"
	}`
	if _, err := a.OnWebhook(context.Background(), genericEnv(cfg, genericTestSecret), body); !errors.Is(err, deploy.ErrIrrelevantPayload) {
		t.Errorf("expected ErrIrrelevantPayload when identity doesn't match, got %v", err)
	}
}

func TestGeneric_PollAndRollbackUnsupported(t *testing.T) {
	a := &genericWebhookAdapter{}
	if a.SupportsPoll() || a.SupportsRollback() {
		t.Error("generic_webhook should not support poll or rollback")
	}
	if _, err := a.PollCurrent(context.Background(), genericEnv(`{}`, "")); !errors.Is(err, deploy.ErrPollNotSupported) {
		t.Errorf("expected ErrPollNotSupported, got %v", err)
	}
	if err := a.Rollback(context.Background(), genericEnv(`{}`, ""), "x"); !errors.Is(err, deploy.ErrRollbackNotSupported) {
		t.Errorf("expected ErrRollbackNotSupported, got %v", err)
	}
}
