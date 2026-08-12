package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/piagent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeProber struct {
	result piagent.ProbeResult
	calls  int
	gotCfg piagent.Config
	gotKey string
}

func (f *fakeProber) Probe(_ context.Context, cfg piagent.Config, apiKey string) piagent.ProbeResult {
	f.calls++
	f.gotCfg = cfg
	f.gotKey = apiKey
	return f.result
}

type denyingLimiter struct{}

func (denyingLimiter) Allow(context.Context, string) bool { return false }

const probeWorkspaceID = "11111111-1111-1111-1111-111111111111"

func validateRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/model-connection/validate", bytes.NewReader(encoded))
	member := db.Member{UserID: parseUUID("22222222-2222-2222-2222-222222222222")}
	req = req.WithContext(middleware.SetMemberContext(req.Context(), probeWorkspaceID, member))
	return req
}

func validBody() map[string]any {
	return map[string]any{
		"provider": "deepseek",
		"api":      "openai-completions",
		"base_url": "https://api.deepseek.com",
		"model":    "deepseek-v4-flash",
		"api_key":  "sk-test-key-value",
	}
}

func TestValidateModelConnectionReportsSuccess(t *testing.T) {
	prober := &fakeProber{result: piagent.ProbeResult{Outcome: piagent.OutcomeOK, Status: 200}}
	h := &Handler{PiProber: prober}
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, validBody()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp ValidateRuntimeModelConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Valid {
		t.Errorf("valid = false, want true")
	}
	if resp.Outcome != "" {
		t.Errorf("outcome = %q, want empty on success", resp.Outcome)
	}
	if prober.gotKey != "sk-test-key-value" {
		t.Errorf("prober received key %q", prober.gotKey)
	}
	if prober.gotCfg.Model != "deepseek-v4-flash" {
		t.Errorf("prober received model %q", prober.gotCfg.Model)
	}
}

func TestValidateModelConnectionReturns200ForRejectedKey(t *testing.T) {
	h := &Handler{PiProber: &fakeProber{result: piagent.ProbeResult{
		Outcome: piagent.OutcomeInvalidKey,
		Status:  401,
		Detail:  "invalid api key",
	}}}
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, validBody()))

	// A wrong key is a successful verification, not a failed request: an HTTP
	// error here would be indistinguishable from a broken endpoint.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a rejected key", rec.Code)
	}
	var resp ValidateRuntimeModelConnectionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Valid {
		t.Error("valid = true, want false")
	}
	if resp.Outcome != string(piagent.OutcomeInvalidKey) {
		t.Errorf("outcome = %q, want invalid_key", resp.Outcome)
	}
	if resp.Detail == "" {
		t.Error("detail must reach the UI so the user can act on it")
	}
}

func TestValidateModelConnectionRejectsInternalTargets(t *testing.T) {
	cases := []struct{ name, baseURL string }{
		{"loopback http", "http://127.0.0.1:8080/v1"},
		{"loopback https", "https://localhost:8443/v1"},
		{"private ip", "https://10.1.2.3/v1"},
		{"metadata", "https://169.254.169.254/latest"},
		{"plain http public", "http://api.deepseek.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prober := &fakeProber{result: piagent.ProbeResult{Outcome: piagent.OutcomeOK}}
			h := &Handler{PiProber: prober}
			body := validBody()
			body["base_url"] = tc.baseURL
			rec := httptest.NewRecorder()

			h.ValidateRuntimeModelConnection(rec, validateRequest(t, body))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if prober.calls != 0 {
				t.Error("the server must not dial an internal address on a user's behalf")
			}
		})
	}
}

func TestValidateModelConnectionRequiresKey(t *testing.T) {
	prober := &fakeProber{}
	h := &Handler{PiProber: prober}
	body := validBody()
	body["api_key"] = "   "
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if prober.calls != 0 {
		t.Error("no probe should run without a key")
	}
}

func TestValidateModelConnectionRejectsIncompleteConfig(t *testing.T) {
	prober := &fakeProber{}
	h := &Handler{PiProber: prober}
	body := validBody()
	delete(body, "model")
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if prober.calls != 0 {
		t.Error("no probe should run for an incomplete config")
	}
}

func TestValidateModelConnectionForbidsAgentActors(t *testing.T) {
	prober := &fakeProber{}
	h := &Handler{PiProber: prober}
	req := validateRequest(t, validBody())
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", "33333333-3333-3333-3333-333333333333")
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if prober.calls != 0 {
		t.Error("an agent must not be able to drive server-side egress")
	}
}

func TestValidateModelConnectionHonorsRateLimit(t *testing.T) {
	prober := &fakeProber{}
	h := &Handler{PiProber: prober, ModelProbeRateLimiter: denyingLimiter{}}
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, validBody()))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if prober.calls != 0 {
		t.Error("rate-limited requests must not reach the provider")
	}
}

func TestValidateModelConnectionNeverEchoesTheKey(t *testing.T) {
	const key = "sk-test-key-value"
	h := &Handler{PiProber: &fakeProber{result: piagent.ProbeResult{
		Outcome: piagent.OutcomeInvalidKey,
		Status:  401,
		Detail:  "rejected",
	}}}
	rec := httptest.NewRecorder()

	h.ValidateRuntimeModelConnection(rec, validateRequest(t, validBody()))

	if strings.Contains(rec.Body.String(), key) {
		t.Fatalf("response echoed the API key: %s", rec.Body.String())
	}
}
