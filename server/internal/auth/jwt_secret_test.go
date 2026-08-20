package auth

import (
	"strings"
	"testing"
)

func TestValidateJWTSecret(t *testing.T) {
	strong := "a1b2c3d4e5f60718293a4b5c6d7e8f9012a3b4c5d6e7f8091a2b3c4d5e6f70819"
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"empty_is_rejected", "", true},
		{"code_default_is_rejected", defaultJWTSecret, true},
		{"compose_template_default_is_rejected", "change-me-in-production", true},
		{"whitespace_wrapped_weak_value_is_rejected", "  change-me-in-production \n", true},
		{"strong_random_value_is_accepted", strong, false},
		{"short_weak_value_not_in_denylist_is_accepted", "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJWTSecret(tt.secret)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateJWTSecret(%q) = nil, want error", tt.secret)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateJWTSecret(%q) = %v, want nil", tt.secret, err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "JWT_SECRET") {
				t.Fatalf("ValidateJWTSecret(%q) error = %q, want actionable JWT_SECRET message", tt.secret, err)
			}
		})
	}
}
