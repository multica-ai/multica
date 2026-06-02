package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDecodeOpenclawRuntimeConfigEmpty(t *testing.T) {
	t.Parallel()

	mode, gw := decodeOpenclawRuntimeConfig(nil, quietLogger())
	if mode != "" {
		t.Errorf("mode for nil payload: got %q, want \"\"", mode)
	}
	if !gw.IsZero() {
		t.Errorf("gateway for nil payload: got %+v, want zero", gw)
	}
}

func TestDecodeOpenclawRuntimeConfigGatewayMode(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"mode": "gateway",
		"gateway": {
			"host": "gw.internal",
			"port": 18789,
			"token": "secret",
			"tls": true
		}
	}`)
	mode, gw := decodeOpenclawRuntimeConfig(raw, quietLogger())
	if mode != "gateway" {
		t.Errorf("mode: got %q, want %q", mode, "gateway")
	}
	want := execenv.OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "secret",
		TLS:   true,
	}
	if gw != want {
		t.Errorf("gateway: got %+v, want %+v", gw, want)
	}
}

func TestDecodeOpenclawRuntimeConfigMalformedFailsSoftToLocal(t *testing.T) {
	t.Parallel()

	// A broken JSON blob must never block dispatch — the agent runs in the
	// historical embedded mode until the user fixes the config.
	mode, gw := decodeOpenclawRuntimeConfig(json.RawMessage(`{"mode": "gateway"`), quietLogger())
	if mode != "" {
		t.Errorf("mode for malformed payload: got %q, want \"\"", mode)
	}
	if !gw.IsZero() {
		t.Errorf("gateway for malformed payload: got %+v, want zero", gw)
	}
}

func TestDecodeOpenclawRuntimeConfigModeOnly(t *testing.T) {
	t.Parallel()

	// Users may switch to gateway mode and rely on the daemon host's local
	// ~/.openclaw/openclaw.json for the endpoint — gateway block stays zero.
	mode, gw := decodeOpenclawRuntimeConfig(json.RawMessage(`{"mode": "gateway"}`), quietLogger())
	if mode != "gateway" {
		t.Errorf("mode: got %q, want %q", mode, "gateway")
	}
	if !gw.IsZero() {
		t.Errorf("gateway: got %+v, want zero", gw)
	}
}

// TestOpenclawGatewayPinDefaultFormattingMasksToken — a stray `%v` /
// `%+v` / json.Marshal of an OpenclawGatewayPin must NOT print the bearer
// token verbatim. The wrapper-config writer still gets the real value
// directly off the Token field; only default formatters get redacted.
// Guards against the secondary leak path called out in the issue #3260 CR.
func TestOpenclawGatewayPinDefaultFormattingMasksToken(t *testing.T) {
	t.Parallel()

	pin := execenv.OpenclawGatewayPin{
		Host:  "gw.internal",
		Port:  18789,
		Token: "real-secret",
		TLS:   true,
	}

	if got := pin.String(); strings.Contains(got, "real-secret") {
		t.Errorf("String() leaks token: %q", got)
	}
	if got := fmt.Sprintf("%+v", pin); strings.Contains(got, "real-secret") {
		t.Errorf("%%+v leaks token: %q", got)
	}
	raw, err := json.Marshal(pin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "real-secret") {
		t.Errorf("MarshalJSON leaks token: %s", raw)
	}
	// Sanity: the host stays visible so the masked payload is still
	// useful for debugging the non-secret half of the pin.
	if !strings.Contains(string(raw), "gw.internal") {
		t.Errorf("MarshalJSON dropped host along with token: %s", raw)
	}
}
