//go:build !windows

// Process-based coverage for hermes model discovery. Split from the
// cross-platform half because every test here writes a `#!/bin/sh` fixture and
// executes it: on Windows, writeTestExecutable can only drop the bytes on disk
// (there is no shebang to honour and no .exe extension), so `go test
// ./pkg/agent` would fail to run rather than fail to build. Same posture as
// codebuddy_discovery_fallback_test.go, which fakes the same handshake.
//
// The assertions that need no subprocess — hint wording, classification
// inertness, the timeout invariant — live in hermes_model_discovery_test.go and
// run everywhere.

package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hermesACPProviderUnconfiguredScript is a `hermes acp` stand-in that completes
// initialize and then rejects session/new exactly as hermes 0.20.0 does when it
// resolves no LLM provider — verified against the real binary by pointing it at
// an empty HERMES_HOME. The error frame carries its message in a `data` OBJECT,
// not a string, which is why the transport renders it as raw JSON and why
// ProviderUnconfigured matches on a substring.
func hermesACPProviderUnconfiguredScript() string {
	// hermes' message quotes its own commands in backticks, which cannot
	// appear inside a Go raw string literal — hence the concatenation. Inside
	// the shell script they sit in single quotes, so sh treats them as text
	// rather than command substitution.
	bt := "`"
	details := "No LLM provider configured. Run " + bt + "hermes model" + bt +
		" to select a provider, or run " + bt + "hermes setup" + bt +
		" for first-time configuration."
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"hermes-agent","version":"0.20.0"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":{"details":"` + details + `"}}}\n' "$id"
      ;;
  esac
done
`
}

// hermesACPHandshakeErrorScript fails session/new for a reason that has nothing
// to do with configuration.
func hermesACPHandshakeErrorScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":{"details":"401 invalid api key"}}}\n' "$id"
      ;;
  esac
done
`
}

func hermesACPCatalogScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_ok","models":{"currentModelId":"openai-codex:gpt-5.6-terra","availableModels":[{"modelId":"openai-codex:gpt-5.6-terra","name":"OpenAI Codex · gpt-5.6-terra"},{"modelId":"openai-codex:gpt-5.5","name":"OpenAI Codex · gpt-5.5"}]}}}\n' "$id"
      ;;
  esac
done
`
}

// TestDiscoverHermesModelsSurfacesSessionNewFailure is the regression this file
// exists for (MUL-6606). Hermes ships no static catalog, so swallowing a
// session/new failure into an empty list reports "discovery succeeded and found
// nothing" — which the picker renders as an authoritative empty dropdown with
// no error, no reason, and no prompt to type a model in by hand. The failure
// must reach the caller so the daemon reports status=failed.
func TestDiscoverHermesModelsSurfacesSessionNewFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		// wantHint is true only for the provider-unconfigured failure; every
		// other failure must pass through with its own text and nothing added.
		wantHint bool
	}{
		{name: "provider unconfigured", script: hermesACPProviderUnconfiguredScript(), wantHint: true},
		{name: "rejected credential", script: hermesACPHandshakeErrorScript(), wantHint: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakePath := filepath.Join(t.TempDir(), "hermes")
			writeTestExecutable(t, fakePath, []byte(tc.script))

			models, err := discoverHermesModels(context.Background(), Command{Path: fakePath})
			if err == nil {
				t.Fatalf("session/new failed but discovery reported success with %d models", len(models))
			}
			if len(models) != 0 {
				t.Errorf("a failed discovery must return no models, got %+v", models)
			}
			// The runtime's own words are the actionable part of the message;
			// the wrapper must not replace them.
			if !strings.Contains(err.Error(), "session/new") {
				t.Errorf("error must name the stage that failed, got: %v", err)
			}
			if got := strings.Contains(err.Error(), hermesDiscoveryUnconfiguredHint); got != tc.wantHint {
				t.Errorf("hint present=%v, want %v; error: %v", got, tc.wantHint, err)
			}
		})
	}
}

// TestDiscoverHermesModelsStillReturnsACatalogOnSuccess guards the other half:
// strictErrors must not have turned a working hermes into a failing one.
func TestDiscoverHermesModelsStillReturnsACatalogOnSuccess(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(hermesACPCatalogScript()))

	models, err := discoverHermesModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discover hermes models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "openai-codex:gpt-5.6-terra" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if !models[0].Default {
		t.Error("currentModelId must still be badged as the default pick")
	}
}

// TestACPDiscoveryTimeoutIsPerProvider covers the knob hermes needs: a provider
// that sets no timeout keeps the shared default, and one that sets a short
// timeout is actually cut off by it.
func TestACPDiscoveryTimeoutIsPerProvider(t *testing.T) {
	t.Parallel()

	// A binary that consumes initialize but never answers it. Keep the stall in
	// the shell's built-in read: spawning sleep here would leave a descendant
	// holding stdout open after CommandContext kills the shell, so Scanner would
	// wait for the descendant instead of measuring the provider deadline.
	stalled := `#!/bin/sh
while IFS= read -r _; do
  :
done
`
	fakePath := filepath.Join(t.TempDir(), "stalled")
	writeTestExecutable(t, fakePath, []byte(stalled))

	start := time.Now()
	_, err := discoverACPModels(context.Background(), Command{Path: fakePath}, acpDiscoveryProvider{
		defaultBin:   "stalled",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-timeout-test-",
		strictErrors: true,
		timeout:      300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the per-provider deadline to abort the handshake")
	}
	// The override has to be what fired, not the 15s default.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("provider timeout was ignored; handshake ran for %s", elapsed)
	}
}
