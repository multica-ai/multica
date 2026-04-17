package handler

import "testing"

func TestIsSignupAllowed(t *testing.T) {
	tests := []struct {
		name          string
		allowSignup   string
		allowedEmails string
		allowedDomain string
		email         string
		want          bool
	}{
		{"default allows signup", "", "", "", "alice@example.com", true},
		{"ALLOW_SIGNUP=false blocks everyone", "false", "", "", "alice@example.com", false},
		{"ALLOW_SIGNUP=0 blocks everyone", "0", "", "", "alice@example.com", false},
		{"domain whitelist allows matching", "false", "", "company.com,example.com", "bob@company.com", true},
		{"domain whitelist blocks non-matching", "false", "", "company.com", "mallory@evil.com", false},
		{"domain whitelist (signup enabled) still blocks non-match", "true", "", "company.com", "mallory@evil.com", false},
		{"email whitelist allows listed", "false", "bob@company.com, carol@x.io", "", "carol@x.io", true},
		{"email whitelist is case-insensitive", "false", "Bob@Company.com", "", "bob@company.com", true},
		{"email whitelist blocks unlisted", "false", "bob@company.com", "", "alice@company.com", false},
		{"domain + email whitelists both apply", "false", "vip@other.com", "company.com", "vip@other.com", true},
		{"empty email never allowed", "true", "", "", "", false},
		{"malformed ALLOW_SIGNUP falls back to default", "maybe", "", "", "alice@example.com", true},
		{"whitespace tolerant in lists", "false", "", "  company.com ,  example.com ", "a@example.com", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ALLOW_SIGNUP", tc.allowSignup)
			t.Setenv("ALLOWED_EMAILS", tc.allowedEmails)
			t.Setenv("ALLOWED_EMAIL_DOMAINS", tc.allowedDomain)
			if got := isSignupAllowed(tc.email); got != tc.want {
				t.Errorf("isSignupAllowed(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}
