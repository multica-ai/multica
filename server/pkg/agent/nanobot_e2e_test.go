package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestNanobotE2E(t *testing.T) {
	// Skip if nanobot gateway is not reachable.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8765/auth/token", nil)
	if err != nil {
		t.Skip("cannot build request:", err)
	}
	req.Header.Set("Authorization", "Bearer nb-ws-test-secret-2026")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skip("nanobot gateway not running:", err)
	}
	resp.Body.Close()

	// Create backend and execute a prompt.
	b := &nanobotBackend{cfg: Config{
		Env: map[string]string{
			"NANOBOT_GATEWAY_URL": "ws://127.0.0.1:8765/ws?token=" + getGatewayToken(t),
		},
		Logger: slog.Default(),
	}}

	runCtx, runCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer runCancel()

	session, err := b.Execute(runCtx, "Reply with exactly one word: OK", ExecOptions{
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Drain messages.
	msgCount := 0
	for msg := range session.Messages {
		msgCount++
		t.Logf("message: type=%s content=%q tool=%s", msg.Type, msg.Content[:min(len(msg.Content), 100)], msg.Tool)
	}

	// Wait for result.
	result := <-session.Result
	t.Logf("result: status=%s output=%q error=%q session=%s duration=%dms",
		result.Status, result.Output[:min(len(result.Output), 200)], result.Error, result.SessionID, result.DurationMs)

	if result.Status != "completed" {
		t.Errorf("expected status completed, got %s (error: %s)", result.Status, result.Error)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
	if result.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if msgCount == 0 {
		t.Error("expected at least one streaming message")
	}
	t.Logf("PASS: %d messages, %d chars output", msgCount, len(result.Output))
}

func getGatewayToken(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8765/auth/token", nil)
	req.Header.Set("Authorization", "Bearer nb-ws-test-secret-2026")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	defer resp.Body.Close()
	var tr struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	return tr.Token
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
