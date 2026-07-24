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
