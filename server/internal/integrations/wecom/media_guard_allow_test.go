package wecom

// media_guard_allow_test.go — a machine behind a fake-IP proxy resolves every
// public hostname into 198.18.0.0/15, so WeCom's own COS host is
// indistinguishable from a metadata endpoint by address alone and the guard
// refuses every attachment. The allow-list is how an operator says "that range
// is my proxy". It must not become a way to reach loopback or the private
// ranges, which is what the guard is actually for.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func withAllowed(t *testing.T, cidrs ...string) {
	t.Helper()
	prev := mediaAllowedPrefixes
	t.Cleanup(func() { mediaAllowedPrefixes = prev })
	if errs := SetMediaAllowedPrefixes(cidrs); len(errs) > 0 {
		t.Fatalf("SetMediaAllowedPrefixes: %v", errs)
	}
}

func TestAnEmptyAllowListLeavesTheGuardUnchanged(t *testing.T) {
	withAllowed(t)
	for _, addr := range []string{
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "100.100.100.100", "198.18.0.80", "::1",
	} {
		if publicAddrOnly(netip.MustParseAddr(addr)) {
			t.Errorf("%s was allowed with an empty allow-list", addr)
		}
	}
	for _, addr := range []string{"1.1.1.1", "8.8.8.8", "2606:4700::1111"} {
		if !publicAddrOnly(netip.MustParseAddr(addr)) {
			t.Errorf("%s was refused — ordinary public addresses must still work", addr)
		}
	}
}

func TestADeclaredProxyRangeBecomesReachable(t *testing.T) {
	withAllowed(t, "198.18.0.0/15")
	if !publicAddrOnly(netip.MustParseAddr("198.18.0.80")) {
		t.Fatal("a declared proxy range is still refused — media stays broken on a fake-IP machine")
	}
}

// The whole point of the guard. Declaring one range must not open the others.
func TestDeclaringOneRangeDoesNotOpenTheRest(t *testing.T) {
	withAllowed(t, "198.18.0.0/15")
	for _, addr := range []string{
		"127.0.0.1",       // the backend's own admin endpoints
		"169.254.169.254", // cloud metadata
		"100.100.100.100", // Alibaba cloud metadata
		"10.0.0.1", "192.168.1.1", "172.16.0.1",
		"::1", "::ffff:127.0.0.1", // the IPv4-mapped trick
	} {
		if publicAddrOnly(netip.MustParseAddr(addr)) {
			t.Errorf("declaring 198.18.0.0/15 also opened %s", addr)
		}
	}
}

// Loopback and the private ranges are refused before the allow-list is even
// consulted, so an operator cannot open them by mistake or by pasting a
// too-wide CIDR.
func TestLoopbackAndPrivateCannotBeAllowedAtAll(t *testing.T) {
	withAllowed(t, "0.0.0.0/0", "::/0")
	for _, addr := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "::1"} {
		if publicAddrOnly(netip.MustParseAddr(addr)) {
			t.Errorf("%s became reachable via a 0.0.0.0/0 allow-list — the guard's core promise is broken", addr)
		}
	}
}

func TestAnUnparseableCidrIsReportedNotIgnored(t *testing.T) {
	prev := mediaAllowedPrefixes
	t.Cleanup(func() { mediaAllowedPrefixes = prev })
	errs := SetMediaAllowedPrefixes([]string{"198.18.0.0/15", "not-a-cidr", "  "})
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want exactly one for the malformed entry", len(errs))
	}
	if !publicAddrOnly(netip.MustParseAddr("198.18.0.80")) {
		t.Error("the valid entry was dropped because a sibling was malformed")
	}
}

// The dangerous shape of this switch is an operator pasting the widest CIDR
// there is. `0.0.0.0/0` must still not reach the loopback the backend's own
// admin endpoints listen on: the allow-list is consulted only inside the
// reservedMediaPrefixes loop, and loopback, private and link-local are refused
// before that loop is reached.
//
// Driven through the real dialer against a real listening server, not through
// publicAddrOnly alone. A policy that answers "no" while the transport
// connects anyway is exactly the failure a predicate-level test cannot see, so
// the assertion that matters is that the server was never reached.
func TestAWideOpenAllowListStillCannotReachLoopback(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Write([]byte("the backend's own admin endpoint"))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}

	withAllowed(t, "0.0.0.0/0", "::/0")

	// Both spellings of the same loopback: the IPv4-mapped form reports none
	// of the IPv4 predicates until it is unmapped, which is the trick the
	// guard has to survive whether or not an allow-list is in play.
	for _, resolved := range []string{"127.0.0.1", "::ffff:127.0.0.1"} {
		reached = false
		client := newMediaHTTPClient(mediaGuard{
			resolve: staticResolver{addr: netip.MustParseAddr(resolved)},
		})
		_, err := downloadMedia(context.Background(),
			client, "http://cos.example.com:"+u.Port()+"/object")
		if !errors.Is(err, ErrMediaAddrBlocked) {
			t.Errorf("resolving to %s under a 0.0.0.0/0 allow-list: err = %v, want the guard's refusal", resolved, err)
		}
		if reached {
			t.Errorf("resolving to %s under a 0.0.0.0/0 allow-list opened a socket to loopback — the guard's core promise is broken", resolved)
		}
	}
}
