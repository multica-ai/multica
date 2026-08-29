//go:build unix

package agent

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const nativeDiagnosticCanary = "native-diagnostic-canary-3f73c06a"

func TestSanitizeClaudeNativeDiagnostics(t *testing.T) {
	script := `#!/bin/sh
IFS= read -r _
printf '%s' "$ANTHROPIC_API_KEY" >&2
printf '{"type":"result","subtype":"error","is_error":true,"result":"%s"}\n' "$ANTHROPIC_API_KEY"
exit 1
`
	assertNativeDiagnosticCanaryRedacted(t, "Claude", "claude", "ANTHROPIC_API_KEY", script)
}

func TestSanitizeAntigravityNativeDiagnostics(t *testing.T) {
	script := `#!/bin/sh
printf '%s' "$GOOGLE_API_KEY" >&2
exit 1
`
	assertNativeDiagnosticCanaryRedacted(t, "Antigravity", "antigravity", "GOOGLE_API_KEY", script)
}

func TestSanitizeCursorNativeDiagnostics(t *testing.T) {
	script := `#!/bin/sh
cat > /dev/null
printf '%s' "$CURSOR_API_KEY" >&2
printf '{"type":"result","subtype":"error","is_error":true,"result":"%s","session_id":"sess-diagnostic"}\n' "$CURSOR_API_KEY"
exit 1
`
	assertNativeDiagnosticCanaryRedacted(t, "Cursor", "cursor", "CURSOR_API_KEY", script)
}

func TestSanitizeGrokNativeDiagnostics(t *testing.T) {
	script := `#!/bin/sh
printf '%s' "$XAI_API_KEY" >&2
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"%s"}}\n' "$id" "$XAI_API_KEY"
      exit 0
      ;;
  esac
done
`
	assertNativeDiagnosticCanaryRedacted(t, "Grok", "grok", "XAI_API_KEY", script)
}

func assertNativeDiagnosticCanaryRedacted(t *testing.T, displayName, provider, credentialEnv, script string) {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), provider)
	writeTestExecutable(t, fakePath, []byte(script))

	var logOutput nativeDiagnosticLogBuffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelDebug}))
	backend, err := New(provider, Config{
		ExecutablePath: fakePath,
		Env: map[string]string{
			credentialEnv: nativeDiagnosticCanary,
			"IS_SANDBOX":  "1",
		},
		Logger: logger,
	})
	if err != nil {
		t.Fatal("new native diagnostic fixture backend failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "diagnostic fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal("native diagnostic fixture execution failed")
	}

	messagesDrained := make(chan struct{})
	go func() {
		defer close(messagesDrained)
		for range session.Messages {
		}
	}()

	var result Result
	select {
	case received, ok := <-session.Result:
		if !ok {
			t.Fatal("native diagnostic fixture result channel closed without a value")
		}
		result = received
	case <-ctx.Done():
		t.Fatal("native diagnostic fixture result timed out")
	}
	select {
	case <-messagesDrained:
	case <-ctx.Done():
		t.Fatal("native diagnostic fixture message drain timed out")
	}

	if strings.Contains(logOutput.String(), nativeDiagnosticCanary) {
		t.Errorf("%s daemon log retained the configured diagnostic canary", displayName)
	}
	if !strings.Contains(logOutput.String(), "[REDACTED]") {
		t.Errorf("%s daemon log did not flush the sanitized final diagnostic", displayName)
	}
	resultText := result.Status + result.Output + result.Error + result.SessionID
	if strings.Contains(resultText, nativeDiagnosticCanary) {
		t.Errorf("%s Result retained the configured diagnostic canary", displayName)
	}
}

type nativeDiagnosticLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *nativeDiagnosticLogBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *nativeDiagnosticLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
