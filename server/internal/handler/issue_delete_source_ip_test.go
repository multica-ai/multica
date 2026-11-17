package handler

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRequestSourceIPAuditContext(t *testing.T) {
	trustedProxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		realIP     string
		trusted    []netip.Prefix
		want       string
	}{
		{
			name:       "default trusts only the direct peer",
			remoteAddr: "203.0.113.10:5678",
			forwarded:  "198.51.100.7",
			realIP:     "198.51.100.8",
			want:       "203.0.113.10",
		},
		{
			name:       "trusted proxy first forwarded hop",
			remoteAddr: "10.0.0.2:5678",
			forwarded:  "198.51.100.7,203.0.113.99",
			trusted:    trustedProxy,
			want:       "198.51.100.7",
		},
		{
			name:       "trusted proxy real IP fallback",
			remoteAddr: "10.0.0.2:5678",
			realIP:     "198.51.100.8",
			trusted:    trustedProxy,
			want:       "198.51.100.8",
		},
		{
			name:       "untrusted proxy cannot set forwarded identity",
			remoteAddr: "192.0.2.9:5678",
			forwarded:  "198.51.100.7",
			realIP:     "198.51.100.8",
			trusted:    trustedProxy,
			want:       "192.0.2.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/issues/00000000-0000-0000-0000-000000000000", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}

			got := requestSourceIP(req, tt.trusted)
			if got == nil {
				t.Fatalf("requestSourceIP() = nil, want %s", tt.want)
			}
			if got.String() != tt.want {
				t.Fatalf("requestSourceIP() = %s, want %s", got, tt.want)
			}
		})
	}
}
