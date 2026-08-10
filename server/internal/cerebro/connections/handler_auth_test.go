package connections

import "testing"

func TestMergeStoredTestAuthReusesMaskedAndEmptyCredentials(t *testing.T) {
	t.Parallel()

	stored := AuthConfig{
		BearerToken:    "stored-bearer",
		APIKey:         "stored-api-key",
		APIKeyHeader:   "X-Stored-Key",
		CFAccessID:     "stored-access-id",
		CFAccessSecret: "stored-access-secret",
	}

	got := mergeStoredTestAuth(AuthConfig{
		BearerToken:    "***",
		APIKey:         "",
		APIKeyHeader:   "X-New-Key",
		CFAccessID:     "",
		CFAccessSecret: "***",
	}, stored)

	if got.BearerToken != stored.BearerToken {
		t.Fatalf("bearer token = %q, want stored value", got.BearerToken)
	}
	if got.APIKey != stored.APIKey {
		t.Fatalf("API key = %q, want stored value", got.APIKey)
	}
	if got.CFAccessID != stored.CFAccessID {
		t.Fatalf("Cloudflare Access ID = %q, want stored value", got.CFAccessID)
	}
	if got.CFAccessSecret != stored.CFAccessSecret {
		t.Fatalf("Cloudflare Access secret = %q, want stored value", got.CFAccessSecret)
	}
	if got.APIKeyHeader != "X-New-Key" {
		t.Fatalf("API key header = %q, want explicit form value", got.APIKeyHeader)
	}
}

func TestMergeStoredTestAuthKeepsExplicitReplacementCredentials(t *testing.T) {
	t.Parallel()

	next := AuthConfig{
		BearerToken:    "new-bearer",
		APIKey:         "new-api-key",
		CFAccessID:     "new-access-id",
		CFAccessSecret: "new-access-secret",
	}
	got := mergeStoredTestAuth(next, AuthConfig{
		BearerToken:    "stored-bearer",
		APIKey:         "stored-api-key",
		CFAccessID:     "stored-access-id",
		CFAccessSecret: "stored-access-secret",
	})

	if got.BearerToken != next.BearerToken ||
		got.APIKey != next.APIKey ||
		got.CFAccessID != next.CFAccessID ||
		got.CFAccessSecret != next.CFAccessSecret {
		t.Fatalf("explicit replacement credentials were overwritten: %#v", got)
	}
}

func TestAPIKeyHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty", "", ""},
		{"too short to preview", "rk_abcde", ""},
		{"exactly at the guard", "rk_abcdefg", ""},
		{"registry key", "rk_live_9f2c1b7a4e", "rk_li…"},
		{"multi-byte characters are not split", "æøåæøåæøåæøå", "æøåæø…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := apiKeyHint(tt.key); got != tt.want {
				t.Fatalf("apiKeyHint(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestMaskAuthExposesOnlyTheAPIKeyHint(t *testing.T) {
	t.Parallel()

	got := maskAuth(AuthConfig{
		BearerToken:    "bearer-secret-value",
		APIKey:         "rk_live_9f2c1b7a4e",
		APIKeyHeader:   "x-api-key",
		CFAccessSecret: "cf-secret-value",
	})

	if got.APIKey != "***" {
		t.Fatalf("API key = %q, want it masked", got.APIKey)
	}
	if got.APIKeyHint != "rk_li…" {
		t.Fatalf("API key hint = %q, want the first five characters", got.APIKeyHint)
	}
	if got.BearerToken != "***" || got.CFAccessSecret != "***" {
		t.Fatalf("other secrets leaked: bearer=%q cf=%q", got.BearerToken, got.CFAccessSecret)
	}
}

// The editor round-trips the masked connection straight back on save. The hint
// is derived per response, so it must never reach the stored auth_config —
// otherwise the first five characters of the key end up persisted in plain text.
func TestPreserveMaskedAuthDropsTheAPIKeyHint(t *testing.T) {
	t.Parallel()

	stored := AuthConfig{APIKey: "rk_live_9f2c1b7a4e"}
	got := preserveMaskedAuth(AuthConfig{APIKey: "***", APIKeyHint: "rk_li…"}, stored)

	if got.APIKeyHint != "" {
		t.Fatalf("API key hint = %q, want it dropped before storage", got.APIKeyHint)
	}
	if got.APIKey != stored.APIKey {
		t.Fatalf("API key = %q, want the stored secret preserved", got.APIKey)
	}
}
