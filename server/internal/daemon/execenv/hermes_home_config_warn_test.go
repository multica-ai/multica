package execenv

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLogger returns a logger writing into buf, for asserting on the
// warnings the overlay build emits.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestHermesOverlayWarnsWhenSourceHomeCarriesNoConfig covers the failure shape
// behind GH #6872: the overlay is seeded from a home holding no config.yaml, so
// the task starts with none of the user's provider settings and Hermes refuses
// with "No LLM provider configured" — pointing the user at `hermes model`,
// which edits a home this task never reads.
//
// The build deliberately still SUCCEEDS (an image that ships Hermes and injects
// credentials through the environment is a legitimate setup), so the warning is
// the only signal separating that case from a misresolved source home. It must
// name the source home: that path is the whole answer and appears nowhere else.
func TestHermesOverlayWarnsWhenSourceHomeCarriesNoConfig(t *testing.T) {
	t.Parallel()

	t.Run("source home does not exist", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "never-created")
		var logs bytes.Buffer

		hermesHome := filepath.Join(t.TempDir(), "hermes-home")
		if _, err := prepareHermesHome(hermesHome, missing, false, nil, nil, "", "", captureLogger(&logs)); err != nil {
			t.Fatalf("prepareHermesHome must not fail closed on a missing default home: %v", err)
		}

		out := logs.String()
		if !strings.Contains(out, "hermes source home does not exist") {
			t.Errorf("missing source home must be warned about, got:\n%s", out)
		}
		if !strings.Contains(out, missing) {
			t.Errorf("the warning must name the resolved source home %q, got:\n%s", missing, out)
		}

		// The derived config is what the child actually reads: no provider,
		// which is exactly why Hermes then refuses to start.
		data, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
		if err != nil {
			t.Fatalf("overlay config.yaml unreadable: %v", err)
		}
		if strings.Contains(string(data), "provider:") {
			t.Errorf("nothing to inherit, so the derived config should carry no provider, got:\n%s", data)
		}
	})

	t.Run("source home exists but has no config.yaml", func(t *testing.T) {
		t.Parallel()
		// A real home with auth state but no config.yaml — distinct from the
		// case above for the reader, so it gets its own wording.
		sharedHome := t.TempDir()
		mustWrite(t, filepath.Join(sharedHome, "auth.json"), `{"token":"secret"}`)
		var logs bytes.Buffer

		hermesHome := filepath.Join(t.TempDir(), "hermes-home")
		if _, err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, "", "", captureLogger(&logs)); err != nil {
			t.Fatalf("prepareHermesHome: %v", err)
		}

		out := logs.String()
		if !strings.Contains(out, "has no config.yaml") {
			t.Errorf("a config-less source home must be warned about, got:\n%s", out)
		}
		if strings.Contains(out, "does not exist") {
			t.Errorf("the home DOES exist; the two cases must not be conflated, got:\n%s", out)
		}
		if !strings.Contains(out, sharedHome) {
			t.Errorf("the warning must name the resolved source home %q, got:\n%s", sharedHome, out)
		}
	})
}

// TestHermesOverlayKeepsProviderAndStaysQuiet is the other half of the contract:
// on the ordinary install (config.yaml where the daemon resolves it) the named
// custom provider survives into the overlay untouched and nothing is warned. It
// pins the boundary — without it, the warnings above could be "fixed" by firing
// on every task, which would make them worthless.
func TestHermesOverlayKeepsProviderAndStaysQuiet(t *testing.T) {
	t.Parallel()

	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"model:\n  default: ag/gemini-3.6-flash-high\n  provider: custom:9router\n"+
			"custom_providers:\n  - name: 9router\n    base_url: https://example.invalid/v1\n    api_key: sk-test\n")
	var logs bytes.Buffer

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, "", "", captureLogger(&logs)); err != nil {
		t.Fatalf("prepareHermesHome: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
	if err != nil {
		t.Fatalf("overlay config.yaml unreadable: %v", err)
	}
	for _, want := range []string{"provider: custom:9router", "name: 9router", "api_key: sk-test"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("derived config dropped %q, got:\n%s", want, data)
		}
	}
	if out := logs.String(); strings.Contains(out, "source home") {
		t.Errorf("a healthy source home must not warn, got:\n%s", out)
	}
}
