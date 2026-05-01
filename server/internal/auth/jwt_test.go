package auth

import (
	"strings"
	"testing"
)

func TestResolveJWTSecret(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		wantErr  string
	}{
		{
			name:     "empty value fails",
			envValue: "",
			wantErr:  "JWT_SECRET is not set",
		},
		{
			name:     "31 byte secret fails",
			envValue: strings.Repeat("a", 31),
			wantErr:  "must be at least 32 bytes",
		},
		{
			name:     "exactly 32 bytes succeeds",
			envValue: strings.Repeat("a", 32),
			wantErr:  "",
		},
		{
			name:     "64 byte hex secret succeeds",
			envValue: strings.Repeat("0123456789abcdef", 4), // 64 bytes
			wantErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", tt.envValue)

			got, err := resolveJWTSecret()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
				if string(got) != tt.envValue {
					t.Fatalf("expected secret %q, got %q", tt.envValue, string(got))
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestMinJWTSecretBytes(t *testing.T) {
	if MinJWTSecretBytes != 32 {
		t.Fatalf("MinJWTSecretBytes must be 32 (HS256 output size), got %d", MinJWTSecretBytes)
	}
}
