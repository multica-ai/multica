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

func flyEnv() *deploy.Environment {
	return &deploy.Environment{
		AdapterKind:  "fly",
		Config:       json.RawMessage(`{"app_name":"my-app","api_token":"fly_token"}`),
		TargetBranch: "main",
	}
}

func TestFly_WebhookRejected(t *testing.T) {
	a := &flyAdapter{}
	if err := a.VerifySignature(flyEnv(), http.Header{}, nil); !errors.Is(err, deploy.ErrSignatureInvalid) {
		t.Errorf("expected ErrSignatureInvalid (fly has no webhooks), got %v", err)
	}
}

func TestFly_PollCurrent_FromMachines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/apps/my-app/machines") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fly_token" {
			t.Errorf("missing auth header")
		}
		_, _ = w.Write([]byte(`[
			{
				"id": "m1",
				"updated_at": "2024-01-15T12:00:00Z",
				"config": {"image": "registry.fly.io/my-app:latest", "env": {"COMMIT_SHA": "abc123abc123abc123abc123abc123abc123abcd"}}
			}
		]`))
	}))
	defer srv.Close()
	restore := overrideClient(&http.Client{Transport: rewriteTransport(srv.URL)})
	defer restore()

	a := &flyAdapter{}
	state, err := a.PollCurrent(context.Background(), flyEnv())
	if err != nil {
		t.Fatalf("PollCurrent: %v", err)
	}
	if state.CurrentSHA != "abc123abc123abc123abc123abc123abc123abcd" {
		t.Errorf("sha: %q", state.CurrentSHA)
	}
}

func TestFly_RollbackUnsupported(t *testing.T) {
	a := &flyAdapter{}
	if err := a.Rollback(context.Background(), flyEnv(), "abc"); !errors.Is(err, deploy.ErrRollbackNotSupported) {
		t.Errorf("expected ErrRollbackNotSupported, got %v", err)
	}
}
