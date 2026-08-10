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
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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

// Translation space is the other half of that promise, and it is the half the
// IPv4 literals above cannot see.
//
// Loopback and the private ranges are refused above the allow-list loop by
// IsLoopback / IsPrivate / IsLinkLocalUnicast. Those predicates never fire on
// a NAT64 or 6to4 address, because at that point it IS an IPv6 address — the
// IPv4 destination is embedded in the low bits and only the translator on the
// other end unwraps it. So for these prefixes the deny list was the only thing
// in the way, and letting an operator's CIDR override it put
// 64:ff9b:1::a9fe:a9fe (→ 169.254.169.254) and 2002:7f00:1::1 (→ 127.0.0.1)
// back within reach of a URL WeCom supplies — the exact addresses the entry
// above proves are unreachable when they are written as IPv4.
//
// ::/0 is the widest an operator can paste, and 2002::/16 is the narrow,
// plausible version of the same mistake: someone widening the range until
// images load. Neither may open translation space.
//
// Driven through the real dialer, like the loopback case, and for the same
// reason: WHICH error comes back is the whole assertion. ErrMediaAddrBlocked
// means the address never got past the policy; a connect timeout or "no route
// to host" means a socket was opened toward an address that reaches inside.
func TestAWideOpenAllowListStillCannotReachTranslationSpace(t *testing.T) {
	// nat64 carries 169.254.169.254, sixToFour carries 127.0.0.1.
	const (
		nat64     = "64:ff9b:1::a9fe:a9fe"
		sixToFour = "2002:7f00:1::1"
	)

	for _, allow := range [][]string{
		{"0.0.0.0/0", "::/0"}, // the widest thing there is
		{"2002::/16"},         // widened until images loaded
		{"64:ff9b:1::/48"},    // the same mistake, NAT64 side
	} {
		t.Run(strings.Join(allow, ","), func(t *testing.T) {
			withAllowed(t, allow...)

			for _, addr := range []string{nat64, sixToFour} {
				if publicAddrOnly(netip.MustParseAddr(addr)) {
					t.Errorf("%s became reachable under an allow-list of %v — it is %s in an IPv6 spelling",
						addr, allow, map[string]string{nat64: "169.254.169.254", sixToFour: "127.0.0.1"}[addr])
				}
			}

			// A literal in the signed URL, straight through the client every
			// media fetch goes through.
			for _, target := range []string{
				"http://[" + nat64 + "]/latest/meta-data/",
				"http://[" + sixToFour + "]:9/object",
			} {
				_, err := downloadMedia(context.Background(), newMediaHTTPClient(mediaGuard{}), target)
				if !errors.Is(err, ErrMediaAddrBlocked) {
					t.Errorf("%s under an allow-list of %v: err = %v, want the guard's refusal — anything else means a socket was opened", target, allow, err)
				}
			}

			// And an innocent hostname whose answer is not, which is the shape
			// a rebinding attempt takes.
			for _, resolved := range []string{nat64, sixToFour} {
				var refused []netip.Addr
				client := newMediaHTTPClient(mediaGuard{
					resolve:  staticResolver{addr: netip.MustParseAddr(resolved)},
					onRefuse: func(_ string, a netip.Addr) { refused = append(refused, a) },
				})
				_, err := downloadMedia(context.Background(), client, "https://cos.example.com/object")
				if !errors.Is(err, ErrMediaAddrBlocked) {
					t.Errorf("a host resolving to %s under an allow-list of %v: err = %v, want the guard's refusal", resolved, allow, err)
				}
				if len(refused) != 1 || refused[0].String() != resolved {
					t.Errorf("refused = %v, want exactly %s — the refusal has to come from the policy, not from a failed connect", refused, resolved)
				}
			}
		})
	}
}

// The escape hatch still has to work, or the split above would have been paid
// for by breaking the deployment it exists for.
func TestTheProxyEscapeHatchSurvivesTheTranslationBlock(t *testing.T) {
	withAllowed(t, "198.18.0.0/15")
	if !publicAddrOnly(netip.MustParseAddr("198.18.0.80")) {
		t.Fatal("a declared proxy range is refused — media stays broken on a fake-IP machine")
	}
}

// classifyMediaFailure has to tell the guard's refusal apart from an ordinary
// failed download, because those two put an operator in completely different
// places: one is a URL that should not have been sent (or a proxy range that
// needs declaring in MULTICA_WECOM_MEDIA_ALLOW_CIDRS), the other is WeCom or
// the network.
//
// Driven from real errors rather than hand-built ones. The refusal travels
// from the dialer through http.Client (which wraps it in a *url.Error) and
// through stripURL before anything classifies it, so a test that constructs
// ErrMediaAddrBlocked directly would pass even if that chain stopped
// preserving errors.Is — which is exactly how this went unnoticed.
func TestAGuardRefusalIsClassifiedApartFromAnOrdinaryFailure(t *testing.T) {
	withAllowed(t) // the guard as it ships

	// Blocked: the production policy, pointed inward.
	_, err := downloadMedia(context.Background(), newMediaHTTPClient(mediaGuard{}), "http://127.0.0.1:9/object")
	if !errors.Is(err, ErrMediaAddrBlocked) {
		t.Fatalf("err = %v, want the guard's refusal — this test is not exercising the blocked path", err)
	}
	if got := classifyMediaFailure(err); got != mediaFailureBlocked {
		t.Errorf("classifyMediaFailure(guard refusal) = %d, want mediaFailureBlocked (%d)", got, mediaFailureBlocked)
	}

	// Too large: still its own kind, not swallowed by the new branch.
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "209715200")
		w.WriteHeader(http.StatusOK)
	}))
	defer oversize.Close()
	_, err = downloadMedia(context.Background(), testMediaClient(), oversize.URL)
	if !errors.Is(err, errMediaTooLarge) {
		t.Fatalf("err = %v, want the size refusal", err)
	}
	if got := classifyMediaFailure(err); got != mediaFailureTooLarge {
		t.Errorf("classifyMediaFailure(oversize) = %d, want mediaFailureTooLarge (%d)", got, mediaFailureTooLarge)
	}

	// An expired link is neither.
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()
	_, err = downloadMedia(context.Background(), testMediaClient(), expired.URL)
	if got := classifyMediaFailure(err); got != mediaFailureUnreadable {
		t.Errorf("classifyMediaFailure(http 403) = %d, want mediaFailureUnreadable (%d)", got, mediaFailureUnreadable)
	}
}

// The refusal reaches an operator through the log and stops there. Whatever
// the sender is told must not name the address the host resolved to, or the
// signed url — a chat message is the one place neither belongs.
//
// The message carries one attachment the guard refuses and one it never tries
// (an unfetchable scheme, rejected before any client is involved). Those are
// two different mediaFailure kinds that deliberately render to the SAME
// sentence, because neither is anything the sender can act on. So the sender
// must still hear it once: two lines here would be the adapter repeating
// itself at the one person who can do nothing about either.
func TestAGuardRefusalNeverPutsAnAddressOrUrlInTheChat(t *testing.T) {
	withAllowed(t)

	// A real server on loopback, so the refusal is about a genuine address
	// the guard declined rather than a name that failed to resolve.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("should never be fetched"))
	}))
	defer srv.Close()

	storage := &fakeMediaStorage{}
	senders, conn := notifierWithLiveSocket(uuidOf(1))
	// NOT newTestResolver: that one swaps in a client which permits loopback,
	// which is the very thing under test here. This keeps the production
	// guard.
	r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders, testLogger()).(*wecomMediaResolver)

	msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
		map[string]any{"msgtype": "image", "image": map[string]any{"url": srv.URL, "aeskey": testAESKey}},
		map[string]any{"msgtype": "image", "image": map[string]any{"url": "ftp://cos.example.com/object", "aeskey": testAESKey}},
	}}})
	out := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

	if len(out.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 — the guard was supposed to refuse this fetch", len(out.MediaRefs))
	}
	if len(storage.stored()) != 0 {
		t.Fatal("an object was stored from an address the guard refuses")
	}
	if len(conn.frames) != 1 {
		t.Fatalf("notices = %d, want exactly 1", len(conn.frames))
	}
	body := conn.sendBody(t, 0)
	md, _ := body["markdown"].(map[string]any)
	content, _ := md["content"].(string)
	if content != mediaUnreadableNotice {
		t.Errorf("notice = %q, want the plain did-not-arrive wording exactly once", content)
	}
	for _, secret := range []string{"127.0.0.1", "::1", srv.URL, testAESKey} {
		if strings.Contains(content, secret) {
			t.Errorf("the chat notice leaks %q: %q", secret, content)
		}
	}
}
