package auth

import (
	"testing"
	"time"
)

func TestValidateJWTSecretConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		appEnv    string
		secret    string
		wantError bool
	}{
		{"production missing secret", "production", "", true},
		{"production default fallback", "production", defaultJWTSecret, true},
		{"production placeholder", "production", placeholderJWTSecret, true},
		{"production configured", "production", "persistent-random-secret", false},
		{"development missing secret", "development", "", false},
		{"local missing app env", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tt.appEnv)
			t.Setenv("JWT_SECRET", tt.secret)

			err := ValidateJWTSecretConfiguration()
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateJWTSecretConfiguration() err=%v, wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestSessionDuration(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("MULTICA_SESSION_TTL", "")
		if got := SessionDuration(); got != 30*24*time.Hour {
			t.Fatalf("SessionDuration() = %s, want 720h", got)
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Setenv("MULTICA_SESSION_TTL", "24h")
		if got := SessionDuration(); got != 24*time.Hour {
			t.Fatalf("SessionDuration() = %s, want 24h", got)
		}
		if got := SessionMaxAgeSeconds(); got != 24*60*60 {
			t.Fatalf("SessionMaxAgeSeconds() = %d, want 86400", got)
		}
	})

	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv("MULTICA_SESSION_TTL", "not-a-duration")
		if got := SessionDuration(); got != defaultSessionDuration {
			t.Fatalf("SessionDuration() = %s, want %s", got, defaultSessionDuration)
		}
	})
}
