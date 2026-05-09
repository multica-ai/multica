package auth

import (
	"net/http/httptest"
	"testing"
)

func TestIsSecureCookie(t *testing.T) {
	cases := []struct {
		name           string
		frontendOrigin string
		want           bool
	}{
		{"https origin → Secure", "https://app.example.com", true},
		{"https with port", "https://app.example.com:8443", true},
		{"http origin → not Secure", "http://192.168.5.5:13000", false},
		{"http localhost → not Secure", "http://localhost:3000", false},
		{"empty → not Secure", "", false},
		{"malformed → not Secure", "::not-a-url", false},
		{"uppercase scheme still matches", "HTTPS://app.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FRONTEND_ORIGIN", tc.frontendOrigin)
			if got := isSecureCookie(); got != tc.want {
				t.Errorf("isSecureCookie() = %v, want %v (FRONTEND_ORIGIN=%q)", got, tc.want, tc.frontendOrigin)
			}
		})
	}
}

func TestCookieDomain(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"real domain", ".example.com", ".example.com"},
		{"bare domain", "example.com", "example.com"},
		{"IPv4 rejected", "192.168.5.5", ""},
		{"IPv4 with leading dot rejected", ".192.168.5.5", ""},
		{"IPv6 rejected", "::1", ""},
		{"IPv6 bracketed is not a valid IP literal → passthrough", "[::1]", "[::1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COOKIE_DOMAIN", tc.env)
			if got := cookieDomain(); got != tc.want {
				t.Errorf("cookieDomain() = %q, want %q (COOKIE_DOMAIN=%q)", got, tc.want, tc.env)
			}
		})
	}
}

// TestSetAuthCookies_HTTPSelfHost covers the exact misconfiguration that
// shipped to users on LAN self-host: COOKIE_DOMAIN=<ip> + HTTP FRONTEND_ORIGIN.
// The cookie must land with no Domain attribute and Secure=false so browsers
// actually store it.
func TestSetAuthCookies_HTTPSelfHost(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "http://192.168.5.5:13000")
	t.Setenv("COOKIE_DOMAIN", "192.168.5.5")

	rec := httptest.NewRecorder()
	if err := SetAuthCookies(rec, "test-token"); err != nil {
		t.Fatalf("SetAuthCookies: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies (auth + csrf), got %d", len(cookies))
	}
	for _, c := range cookies {
		if c.Secure {
			t.Errorf("cookie %q has Secure=true on HTTP origin; browser would reject it", c.Name)
		}
		if c.Domain != "" {
			t.Errorf("cookie %q has Domain=%q; IP-address Domain would be rejected by the browser (RFC 6265)", c.Name, c.Domain)
		}
	}
}

func TestSetAuthCookies_HTTPSProduction(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.com")
	t.Setenv("COOKIE_DOMAIN", "app.example.com")

	rec := httptest.NewRecorder()
	if err := SetAuthCookies(rec, "test-token"); err != nil {
		t.Fatalf("SetAuthCookies: %v", err)
	}

	for _, c := range rec.Result().Cookies() {
		if !c.Secure {
			t.Errorf("cookie %q missing Secure flag on HTTPS origin", c.Name)
		}
		if c.Domain != "app.example.com" {
			t.Errorf("cookie %q Domain = %q, want %q", c.Name, c.Domain, "app.example.com")
		}
	}
}
