// CEREBRO-PATCH(daemon-cerebro-account-test): cerebro-only tests for the
// JEH-997 identity probe. Net-new fork file in the upstream-zone path.
package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestHeartbeatAccountFor_NoCacheNoRuntime exercises the safe-fallback path:
// asking for an unknown runtime's account must return nil rather than
// crashing or producing a half-built payload. This is the JEH-997
// equivalent of the heartbeat code's "no identity yet" branch.
func TestHeartbeatAccountFor_NoCacheNoRuntime(t *testing.T) {
	d := &Daemon{
		runtimeIndex:      map[string]Runtime{},
		accountIdentities: newAccountIdentityCache(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if got := d.heartbeatAccountFor("nope"); got != nil {
		t.Fatalf("heartbeatAccountFor on unknown runtime = %+v, want nil", got)
	}
}

// TestHeartbeatAccountFor_CachesAfterProbe pins the cache semantics: a
// freshly seeded ~/.claude.json produces an Account payload, and the
// second call short-circuits via the cache. The second-read assertion is
// what makes the heartbeat loop's "every beat" cost acceptable.
func TestHeartbeatAccountFor_CachesAfterProbe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "cache@firtal.dk"},
	})
	d := &Daemon{
		runtimeIndex: map[string]Runtime{
			"rt-1": {ID: "rt-1", Provider: "claude"},
		},
		accountIdentities: newAccountIdentityCache(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	got := d.heartbeatAccountFor("rt-1")
	if got == nil {
		t.Fatalf("heartbeatAccountFor: expected Account, got nil")
	}
	if got.Provider != "claude" || got.LoginIdentity != "cache@firtal.dk" {
		t.Fatalf("Account mismatch: %+v", got)
	}

	// Delete the file — the cache must still return the previously probed
	// value until the next forced refresh.
	if err := os.Remove(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("remove claude.json: %v", err)
	}
	got2 := d.heartbeatAccountFor("rt-1")
	if got2 == nil || got2.LoginIdentity != "cache@firtal.dk" {
		t.Fatalf("expected cached account after file removal, got %+v", got2)
	}
}

// TestRefreshHeartbeatAccount_PicksUpFreshLogin pins the "login change
// propagates within one beat" property: refreshHeartbeatAccount is what
// the heartbeat loop calls every tick to absorb a freshly written
// auth-state file.
func TestRefreshHeartbeatAccount_PicksUpFreshLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := &Daemon{
		runtimeIndex: map[string]Runtime{
			"rt-1": {ID: "rt-1", Provider: "claude"},
		},
		accountIdentities: newAccountIdentityCache(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// First refresh: no file → cache has an empty-identity entry, payload
	// returns nil so the heartbeat omits Account.
	d.refreshHeartbeatAccount("rt-1")
	if got := d.heartbeatAccountFor("rt-1"); got != nil {
		t.Fatalf("expected nil payload before login, got %+v", got)
	}

	// User logs in → file appears → next refresh picks up the identity.
	writeJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "fresh@firtal.dk"},
	})
	d.refreshHeartbeatAccount("rt-1")
	got := d.heartbeatAccountFor("rt-1")
	if got == nil || got.LoginIdentity != "fresh@firtal.dk" {
		t.Fatalf("expected fresh-login pickup, got %+v", got)
	}
}

// TestDetectClaudeIdentity_EmailFirst verifies that the Claude probe prefers
// oauthAccount.emailAddress and falls back to accountUuid only when the
// email is empty. This pins JEH-997's "human-readable identity beats opaque
// UUID" contract.
func TestDetectClaudeIdentity_EmailFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": "sara@firtal.dk",
			"accountUuid":  "should-not-be-used",
		},
	})

	id, source, err := DetectAccountIdentity("claude")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "sara@firtal.dk" {
		t.Errorf("identity = %q, want sara@firtal.dk", id)
	}
	if filepath.Base(source) != ".claude.json" {
		t.Errorf("source = %q, want path ending in .claude.json", source)
	}
}

func TestDetectClaudeIdentity_FallsBackToAccountUUID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{
			"accountUuid": "abc-123",
		},
	})

	id, _, err := DetectAccountIdentity("claude")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("identity = %q, want abc-123", id)
	}
}

// TestDetectClaudeIdentity_NoFile is the "not logged in yet" branch — the
// probe must return ("", "", nil) so the caller stays quiet and retries on
// the next heartbeat tick. A non-nil error would spam the daemon log.
func TestDetectClaudeIdentity_NoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	id, _, err := DetectAccountIdentity("claude")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "" {
		t.Errorf("identity = %q, want empty (file missing)", id)
	}
}

// TestDetectCodexIdentity_FromJWT covers the happy path: the codex CLI
// stored an id_token JWT, and we extract the email claim without verifying
// the signature.
func TestDetectCodexIdentity_FromJWT(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}

	payload := map[string]any{"email": "codex-user@firtal.dk"}
	jwt := fakeJWT(t, payload)
	writeJSON(t, filepath.Join(codexDir, "auth.json"), map[string]any{
		"tokens": map[string]any{
			"id_token":   jwt,
			"account_id": "should-not-be-used",
		},
	})

	id, _, err := DetectAccountIdentity("codex")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "codex-user@firtal.dk" {
		t.Errorf("identity = %q, want codex-user@firtal.dk", id)
	}
}

// TestDetectCodexIdentity_FallsBackToAccountID covers the case where the
// auth file lacks a parseable id_token (e.g. opaque token format). We
// should still report SOMETHING stable so the runtime can be bound.
func TestDetectCodexIdentity_FallsBackToAccountID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	writeJSON(t, filepath.Join(codexDir, "auth.json"), map[string]any{
		"tokens": map[string]any{
			"account_id": "codex-account-1",
		},
	})

	id, _, err := DetectAccountIdentity("codex")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "codex-account-1" {
		t.Errorf("identity = %q, want codex-account-1", id)
	}
}

// TestDetectCodexIdentity_HonoursCodexHomeEnv pins that CODEX_HOME overrides
// the default ~/.codex location; matches how the rest of the daemon's codex
// support (skills, sandbox, execenv) resolves the codex home.
func TestDetectCodexIdentity_HonoursCodexHomeEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	override := t.TempDir()
	t.Setenv("CODEX_HOME", override)

	payload := map[string]any{"email": "override@firtal.dk"}
	writeJSON(t, filepath.Join(override, "auth.json"), map[string]any{
		"tokens": map[string]any{"id_token": fakeJWT(t, payload)},
	})

	id, source, err := DetectAccountIdentity("codex")
	if err != nil {
		t.Fatalf("DetectAccountIdentity: %v", err)
	}
	if id != "override@firtal.dk" {
		t.Errorf("identity = %q, want override@firtal.dk", id)
	}
	if filepath.Dir(source) != override {
		t.Errorf("source dir = %q, want %q", filepath.Dir(source), override)
	}
}

// TestDetectAccountIdentity_UnknownProvider must NOT error — we want adding
// a new provider to be a pure additive change, never a regression for the
// providers already covered.
func TestDetectAccountIdentity_UnknownProvider(t *testing.T) {
	id, source, err := DetectAccountIdentity("copilot")
	if err != nil {
		t.Fatalf("unexpected error for unknown provider: %v", err)
	}
	if id != "" || source != "" {
		t.Errorf("expected empty result for unknown provider, got id=%q source=%q", id, source)
	}
}

// TestEmailFromJWT_Tolerant covers a handful of well-formed and malformed
// JWT payloads. We only test the unsigned payload-extraction path because
// the daemon never trusts the token for auth — it's just an identity tag.
func TestEmailFromJWT_Tolerant(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		want    string
	}{
		{"empty", "", ""},
		{"not-a-jwt", "garbage", ""},
		{"missing-payload", "header.", ""},
		{"valid-email", fakeJWT(t, map[string]any{"email": "x@y.z"}), "x@y.z"},
		{"falls-back-to-preferred-username", fakeJWT(t, map[string]any{"preferred_username": "user42"}), "user42"},
		{"falls-back-to-sub", fakeJWT(t, map[string]any{"sub": "subject-id"}), "subject-id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := emailFromJWT(tc.token); got != tc.want {
				t.Errorf("emailFromJWT(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

// fakeJWT builds a header.payload.signature token with the given claims
// payload. The header and signature are placeholders — we exercise the
// daemon's unverified payload extraction path.
func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return fmt.Sprintf("header.%s.signature", base64.RawURLEncoding.EncodeToString(payload))
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
