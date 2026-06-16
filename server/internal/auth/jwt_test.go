package auth

import (
	"testing"
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

