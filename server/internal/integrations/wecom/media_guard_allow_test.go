package wecom

// media_guard_allow_test.go — a machine behind a fake-IP proxy resolves every
// public hostname into 198.18.0.0/15, so WeCom's own COS host is
// indistinguishable from a metadata endpoint by address alone and the guard
// refuses every attachment. The allow-list is how an operator says "that range
// is my proxy". It must not become a way to reach loopback or the private
// ranges, which is what the guard is actually for.

import (
	"net/netip"
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
