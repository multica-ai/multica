package servicetoken

import "testing"

func TestNormalizeScopes(t *testing.T) {
	t.Run("dedupes and sorts known scopes", func(t *testing.T) {
		got, err := NormalizeScopes([]string{"issues:read", "skills:read", "issues:read", "  ", "skills:read"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"issues:read", "skills:read"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("rejects unknown scope", func(t *testing.T) {
		if _, err := NormalizeScopes([]string{"skills:read", "billing:write"}); err == nil {
			t.Fatal("expected error for unknown scope, got nil")
		}
	})

	t.Run("rejects every write scope", func(t *testing.T) {
		for _, scope := range []string{"skills:write", "agents:write", "issues:write"} {
			if _, err := NormalizeScopes([]string{scope}); err == nil {
				t.Fatalf("expected %s to be rejected", scope)
			}
		}
	})

	t.Run("rejects empty set", func(t *testing.T) {
		if _, err := NormalizeScopes([]string{"  ", ""}); err == nil {
			t.Fatal("expected error for empty scope set, got nil")
		}
	})
}

func TestGenerateServiceTokenShape(t *testing.T) {
	raw, err := GenerateServiceToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(raw) != len(Prefix)+40 {
		t.Fatalf("token length: got %d, want %d", len(raw), len(Prefix)+40)
	}
	if raw[:len(Prefix)] != Prefix {
		t.Fatalf("token prefix: got %q, want %q", raw[:len(Prefix)], Prefix)
	}
}
