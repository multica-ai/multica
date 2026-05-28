package daemon

import (
	"io"
	"log/slog"
	"testing"
)

func TestResolveAuth_PrefersMulticaTokenEnv(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "mul_env_token_xyz")

	d := &Daemon{
		cfg:    Config{Profile: "nonexistent-profile-for-test"},
		client: NewClient("http://example.invalid"),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := d.resolveAuth(); err != nil {
		t.Fatalf("resolveAuth with MULTICA_TOKEN set should succeed, got: %v", err)
	}
	if got := d.client.Token(); got != "mul_env_token_xyz" {
		t.Fatalf("expected client token from env, got %q", got)
	}
}
