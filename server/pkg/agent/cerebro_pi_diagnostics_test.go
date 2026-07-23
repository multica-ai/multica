package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiProviderDiagnosticClassifiesSafeErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stderr string
		want   string
	}{
		"authentication": {stderr: `request failed: 401 invalid_grant`, want: "authentication rejected"},
		"rate limit":     {stderr: `HTTP 429 too many requests`, want: "rate limited"},
		"subscription":   {stderr: `usage limit reached for subscription`, want: "subscription or quota limit"},
		"network":        {stderr: `dial tcp: connection refused`, want: "network unavailable"},
	}

	for name, tc := range tests {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			writer := newPiDiagnosticWriter(slog.New(slog.NewTextHandler(&logs, nil)))
			if _, err := writer.Write([]byte(tc.stderr)); err != nil {
				t.Fatalf("write diagnostic: %v", err)
			}
			got := writer.safeError(errors.New("exit status 1"))
			if !strings.Contains(got, tc.want) {
				t.Fatalf("safe error %q does not contain %q", got, tc.want)
			}
		})
	}
}

func TestPiRetriesOnFirtalGatewayBeforeAnyToolRuns(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
case " $* " in
  *" --provider firtal-gateway "*)
	printf '%s\n' '{"type":"agent_start"}'
	printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"fallback-ok"}}'
    printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"gateway-test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2}}}'
    exit 0
    ;;
esac
printf '%s\n' '{"type":"agent_start"}'
printf '%s\n' '{"type":"error","message":"HTTP 401 invalid_grant"}'
printf '%s\n' 'HTTP 401 invalid_grant' >&2
exit 1
`
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("pi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"FIRTAL_REGISTRY_URL":   "https://registry.example.test",
			"FIRTAL_REGISTRY_KEY":   "synthetic-gateway-key",
			"FIRTAL_REGISTRY_MODEL": "gateway-test",
		},
	})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var statusSessionIDs []string
	var providerErrors []string
	for message := range session.Messages {
		if message.Type == MessageStatus {
			statusSessionIDs = append(statusSessionIDs, message.SessionID)
		}
		if message.Type == MessageError {
			providerErrors = append(providerErrors, message.Content)
		}
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "fallback-ok" {
		t.Fatalf("fallback did not complete: status=%q output=%q error=%q", result.Status, result.Output, result.Error)
	}
	if len(statusSessionIDs) != 1 || statusSessionIDs[0] != result.SessionID {
		t.Fatalf("only the successful fallback session may be pinned: statuses=%v result=%q", statusSessionIDs, result.SessionID)
	}
	if len(providerErrors) != 0 {
		t.Fatalf("recovered primary errors must not be surfaced: %v", providerErrors)
	}
}

func TestPiDoesNotRetryAfterToolExecutionStarts(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
case " $* " in
  *" --provider firtal-gateway "*)
    printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"unsafe-duplicate"}}'
    exit 0
    ;;
esac
printf '%s\n' '{"type":"tool_execution_start","toolCallId":"call-1","toolName":"write","args":{"path":"synthetic"}}'
printf '%s\n' '{"type":"error","message":"HTTP 429 too many requests"}'
printf '%s\n' 'HTTP 429 too many requests' >&2
exit 1
`
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("pi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"FIRTAL_REGISTRY_URL":   "https://registry.example.test",
			"FIRTAL_REGISTRY_KEY":   "synthetic-gateway-key",
			"FIRTAL_REGISTRY_MODEL": "gateway-test",
		},
	})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || strings.Contains(result.Output, "unsafe-duplicate") {
		t.Fatalf("run was retried after a tool started: status=%q output=%q", result.Status, result.Output)
	}
}

func TestPiProviderDiagnosticRedactsRefreshCredentialBeforeLogging(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-provider-refresh-value"
	var logs bytes.Buffer
	writer := newPiDiagnosticWriter(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	_, _ = writer.Write([]byte(`401 invalid_grant {"refresh_token":"` + synthetic + `"}`))

	if strings.Contains(logs.String(), synthetic) {
		t.Fatalf("synthetic credential reached logs: %s", logs.String())
	}
	if got := writer.safeError(errors.New("exit status 1")); strings.Contains(got, synthetic) {
		t.Fatalf("synthetic credential reached result error: %s", got)
	}
}

func TestPiProviderDiagnosticRedactsCredentialSplitAcrossWrites(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-split-refresh-value"
	var logs bytes.Buffer
	writer := newPiDiagnosticWriter(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	_, _ = writer.Write([]byte(`401 invalid_grant refresh_token=synthetic-split-`))
	_, _ = writer.Write([]byte("refresh-value\n"))
	_ = writer.safeError(errors.New("exit status 1"))

	if strings.Contains(logs.String(), synthetic) || strings.Contains(logs.String(), "refresh-value") {
		t.Fatalf("split synthetic credential reached logs: %s", logs.String())
	}
}

func TestPiProviderDiagnosticKeepsGenericExitWhenNoProviderSignal(t *testing.T) {
	t.Parallel()

	writer := newPiDiagnosticWriter(slog.Default())
	if got := writer.safeError(errors.New("exit status 1")); got != "pi exited with error: exit status 1" {
		t.Fatalf("unexpected generic error: %q", got)
	}
}

func TestPiAutoRetryFinalErrorIsSafeForStorage(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-auto-retry-refresh-value"
	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
printf '%s\n' '{"type":"auto_retry_end","success":false,"finalError":"HTTP 401 invalid_grant refresh_token=` + synthetic + `"}'
`
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if strings.Contains(result.Error, synthetic) || result.Error != "pi provider error: authentication rejected" {
		t.Fatalf("unsafe final retry error: %q", result.Error)
	}
}

func TestPiCommandArgsAreSafeForLogging(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-prompt-refresh-value"
	args := safeAgentCommandArgs([]string{"-p", "prompt refresh_token=" + synthetic})
	if strings.Contains(strings.Join(args, " "), synthetic) {
		t.Fatalf("synthetic credential reached logged command args: %v", args)
	}
}

func TestPiGatewayFallbackUsesManagedDefaultModel(t *testing.T) {
	t.Setenv("FIRTAL_REGISTRY_MODEL", "")

	env := map[string]string{
		"FIRTAL_REGISTRY_URL": "https://registry.example.test",
		"FIRTAL_REGISTRY_KEY": "synthetic-gateway-key",
	}
	if !piGatewayFallbackConfigured(env) {
		t.Fatal("gateway URL and key should enable fallback without an explicit model")
	}
	if got := piGatewayFallbackModel(env); got != "claude-sonnet-5" {
		t.Fatalf("unexpected managed default model: %q", got)
	}
}
