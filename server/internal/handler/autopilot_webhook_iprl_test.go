package handler

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

// These tests don't need a database — they exercise the pure header/CIDR
// logic of (h *Handler).clientIPForRateLimit so we construct a minimal
// Handler with just the Config populated.

func newClientIPHandler(t *testing.T, cidrs ...string) *Handler {
	t.Helper()
	var prefixes []netip.Prefix
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatalf("bad test CIDR %q: %v", c, err)
		}
		prefixes = append(prefixes, p)
	}
	return &Handler{cfg: Config{TrustedProxies: prefixes}}
}

func TestClientIPForRateLimit_DefaultIgnoresProxyHeaders(t *testing.T) {
	// The critical assertion: with no TrustedProxies configured, a caller
	// can spam X-Forwarded-For values but the limiter still keys on the
	// real source IP. This is what closes the bypass Bohan flagged.
	h := newClientIPHandler(t /* no CIDRs */)

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")

	if got := h.clientIPForRateLimit(req); got != "203.0.113.5" {
		t.Fatalf("default: got %q, want 203.0.113.5 (RemoteAddr) — headers must be ignored", got)
	}
}

func TestClientIPForRateLimit_HonorsXFFFromTrustedProxy(t *testing.T) {
	h := newClientIPHandler(t, "10.0.0.0/8")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "10.1.2.3:9999" // inside trusted prefix
	req.Header.Set("X-Forwarded-For", "5.5.5.5")

	if got := h.clientIPForRateLimit(req); got != "5.5.5.5" {
		t.Fatalf("trusted: got %q, want 5.5.5.5 (XFF from trusted proxy)", got)
	}
}

func TestClientIPForRateLimit_IgnoresXFFFromUntrustedSource(t *testing.T) {
	// The bypass-closed assertion: even with TrustedProxies set, if the
	// connection didn't come from one of those addresses, XFF must be
	// ignored — otherwise the limiter is still trivially bypassable.
	h := newClientIPHandler(t, "10.0.0.0/8")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234" // NOT inside trusted prefix
	req.Header.Set("X-Forwarded-For", "5.5.5.5")

	if got := h.clientIPForRateLimit(req); got != "203.0.113.5" {
		t.Fatalf("untrusted: got %q, want 203.0.113.5 — XFF must be ignored when source isn't in TrustedProxies", got)
	}
}

func TestClientIPForRateLimit_MultiHopXFFFirstEntryWins(t *testing.T) {
	h := newClientIPHandler(t, "10.0.0.0/8")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "10.1.2.3:9999"
	// Convention: leftmost entry is the original client; the proxy
	// appends its own IP as the next hop.
	req.Header.Set("X-Forwarded-For", "5.5.5.5, 10.0.0.7")

	if got := h.clientIPForRateLimit(req); got != "5.5.5.5" {
		t.Fatalf("multi-hop XFF: got %q, want 5.5.5.5 (leftmost)", got)
	}
}

func TestClientIPForRateLimit_FallsBackToXRealIPWhenXFFEmpty(t *testing.T) {
	h := newClientIPHandler(t, "10.0.0.0/8")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "10.1.2.3:9999"
	req.Header.Set("X-Real-IP", "7.7.7.7")

	if got := h.clientIPForRateLimit(req); got != "7.7.7.7" {
		t.Fatalf("X-Real-IP fallback: got %q, want 7.7.7.7", got)
	}
}

func TestClientIPForRateLimit_IPv6RemoteAddrIsParsed(t *testing.T) {
	// IPv6 RemoteAddr is "[::1]:port" — make sure the host extraction
	// peels off the brackets and the CIDR check works.
	h := newClientIPHandler(t, "::1/128")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "[::1]:5000"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	if got := h.clientIPForRateLimit(req); got != "9.9.9.9" {
		t.Fatalf("IPv6 trusted: got %q, want 9.9.9.9", got)
	}
}

func TestClientIPForRateLimit_IPv6UntrustedKeepsRemoteAddr(t *testing.T) {
	h := newClientIPHandler(t, "10.0.0.0/8")

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "[2001:db8::1]:5000"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	if got := h.clientIPForRateLimit(req); got != "2001:db8::1" {
		t.Fatalf("IPv6 untrusted: got %q, want 2001:db8::1", got)
	}
}

func TestRemoteAddrHost(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"203.0.113.5:1234", "203.0.113.5"},
		{"[::1]:8080", "::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"203.0.113.5", "203.0.113.5"},  // bare, no port
		{"2001:db8::1", "2001:db8::1"},  // bare IPv6
		{"", ""},
	}
	for _, tc := range cases {
		if got := remoteAddrHost(tc.in); got != tc.want {
			t.Errorf("remoteAddrHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
