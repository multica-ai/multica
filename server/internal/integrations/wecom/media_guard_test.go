package wecom

// media_guard_test.go — the fetcher must refuse to be pointed inward.
//
// The tests come in two shapes because the guard has two halves. The policy
// is pure and is tested exhaustively by table: it is where a missed range
// hides. The dialer is tested against real sockets, because the parts worth
// doubting — that a redirect is re-checked, that an IPv4-mapped address is
// unmapped first, that a hostname resolving to something internal is refused
// at connect time rather than at parse time — are all properties of the
// connection, not of the URL.
//
// The harness's own server is on loopback, which the production policy
// refuses. So the tests that need a live server inject a policy that permits
// loopback and nothing else extra; what they exercise is still the real
// guard, with the one address the test itself occupies allowed.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// allowLoopbackPolicy is the production policy with loopback permitted, for
// tests that host their own server. Everything else it refuses is refused
// for the production reason.
func allowLoopbackPolicy(a netip.Addr) bool {
	if a.Unmap().IsLoopback() {
		return true
	}
	return publicAddrOnly(a)
}

// testMediaClient is what every media test fetches through, so the whole
// media suite runs against a real guard rather than a bare http.Client.
func testMediaClient() *http.Client {
	return newMediaHTTPClient(mediaGuard{allow: allowLoopbackPolicy})
}

// staticResolver answers every lookup with one address, so a test can make a
// public-looking hostname resolve wherever it likes.
type staticResolver struct{ addr netip.Addr }

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{r.addr}, nil
}

// ---- the policy ----

func TestPublicAddrOnlyRefusesEverythingInward(t *testing.T) {
	blocked := []string{
		// loopback
		"127.0.0.1", "127.0.0.53", "::1",
		// the IPv4-mapped forms of the same — the predicate only sees these
		// after Unmap, which is the trap this exists for
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254",
		// RFC 1918
		"10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.1.1",
		// link-local, and the cloud metadata address that lives in it
		"169.254.169.254", "169.254.1.1", "fe80::1",
		// RFC 6598 shared space — this machine's tailnet
		"100.64.0.1", "100.100.100.100", "100.127.255.254",
		// IPv6 unique local
		"fc00::1", "fd12:3456::1",
		// unspecified, multicast, broadcast
		"0.0.0.0", "::", "224.0.0.1", "ff02::1", "255.255.255.255",
		// IETF reserved, including the proxy fake-IP range on this machine
		"192.0.0.1", "192.0.2.1", "198.18.0.18", "198.19.255.1",
		"198.51.100.1", "203.0.113.1", "240.0.0.1",
		"192.88.99.1", // deprecated 6to4 relay anycast
		"2001:db8::1",
		// Translation space. Each of these is an IPv4 address in an IPv6
		// costume, and the IPv4 it carries is 169.254.169.254 or 127.0.0.1 —
		// a guard that only recognises those written as IPv4 lets all of
		// them straight through.
		"64:ff9b::7f00:1",        // NAT64 well-known → 127.0.0.1
		"64:ff9b::a9fe:a9fe",     // NAT64 well-known → 169.254.169.254
		"64:ff9b:1::7f00:1",      // NAT64 local-use (RFC 8215) → 127.0.0.1
		"64:ff9b:1::a9fe:a9fe",   // NAT64 local-use → 169.254.169.254
		"64:ff9b:1:ffff::c0a8:1", // anywhere else in that /48
		"2002:a9fe:a9fe::1",      // 6to4 → 169.254.169.254 through a relay
		"2002:7f00:1::1",         // 6to4 → 127.0.0.1
		// IETF protocol assignments, 2001::/23 end to end
		"2001::1",     // Teredo, which also carries an IPv4 endpoint
		"2001:2::1",   // benchmarking — the twin of 198.18.0.0/15
		"2001:1::1",   // PCP anycast
		"2001:1ff::1", // the last address in the /23
		"2001:20::1",  // ORCHIDv2
		// Discard-only, documentation, routing labels, deprecated site-local
		"100::1", "100:0:0:1::1", "3fff::1", "5f00::1", "fec0::1",
	}
	for _, s := range blocked {
		a := netip.MustParseAddr(s)
		if publicAddrOnly(a) {
			t.Errorf("publicAddrOnly(%s) allowed a non-public address", s)
		}
	}

	allowed := []string{
		"1.1.1.1", "8.8.8.8", "140.82.121.4", // public v4
		"101.226.0.1",          // a Tencent COS range — the real traffic
		"2001:4860:4860::8888", // public v6
		"2400:3200::1",         // public v6
		// The blocks above end where they say they end. 2001:200:: is one
		// group past 2001::/23 and is ordinary APNIC unicast; 2001:db8:: is
		// blocked by its own entry, not by the /23. 64:ff9c:: is one nibble
		// past both NAT64 prefixes. A guard that swallowed these would refuse
		// real attachments, which fails just as loudly as letting one through.
		"2001:200::1",
		"2003::1",
		"64:ff9c::1",
		"2402:4e00::1", // Tencent's v6 space
	}
	for _, s := range allowed {
		a := netip.MustParseAddr(s)
		if !publicAddrOnly(a) {
			t.Errorf("publicAddrOnly(%s) refused a public address — real media would fail", s)
		}
	}
}

func TestPublicAddrOnlyRefusesAnInvalidAddress(t *testing.T) {
	if publicAddrOnly(netip.Addr{}) {
		t.Error("the zero address was allowed")
	}
}

// ---- the dialer ----

// The whole point: a URL naming an internal address never opens a socket.
func TestTheGuardRefusesAnInternalAddressOutright(t *testing.T) {
	client := newMediaHTTPClient(mediaGuard{}) // production policy
	for _, target := range []string{
		"http://127.0.0.1:9/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://[::1]:9/",
		"http://100.100.100.100/", // the tailnet resolver
		"http://198.18.0.18/",     // a fake-IP address
	} {
		_, err := downloadMedia(context.Background(), client, target)
		if err == nil {
			t.Errorf("%s was fetched", target)
			continue
		}
		if !errors.Is(err, ErrMediaAddrBlocked) {
			t.Errorf("%s failed with %v, want the guard's refusal (a connection error means it tried)", target, err)
		}
	}
}

// TestTheGuardRefusesBothNAT64Prefixes: a NAT64 address is not a destination,
// it is an IPv4 destination the translator will unwrap. 64:ff9b:1::a9fe:a9fe
// arrives at 169.254.169.254 — the cloud metadata endpoint — on any deployment
// that runs the RFC 8215 local-use prefix, which is the prefix an operator
// picks when the well-known one is already in use. The guard knew the
// well-known prefix and not that one, so the whole "we only dial public
// addresses" promise came apart on a deployment that had done nothing wrong.
//
// Driven through the real dialer, not through publicAddrOnly. The distinction
// is the same one TestAWideOpenAllowListStillCannotReachLoopback rests on: a
// predicate that answers "no" while the transport connects anyway is a guard
// that does not guard, and only a test that watches what the transport does
// can tell those apart. Here that shows up in WHICH error comes back —
// ErrMediaAddrBlocked means the address never left the policy, and anything
// else (a connect timeout, "no route to host") means a socket was attempted
// against an address that reaches inside.
func TestTheGuardRefusesBothNAT64Prefixes(t *testing.T) {
	client := newMediaHTTPClient(mediaGuard{}) // production policy

	// Written as a literal, which is how it arrives in a signed URL.
	for _, target := range []string{
		"http://[64:ff9b::a9fe:a9fe]/latest/meta-data/",   // well-known → 169.254.169.254
		"http://[64:ff9b:1::a9fe:a9fe]/latest/meta-data/", // local-use → 169.254.169.254
		"http://[64:ff9b:1::7f00:1]:9/",                   // local-use → 127.0.0.1
		"http://[64:ff9b:1::a00:1]/",                      // local-use → 10.0.0.1
		"http://[2002:a9fe:a9fe::1]/latest/meta-data/",    // 6to4 → 169.254.169.254
	} {
		_, err := downloadMedia(context.Background(), client, target)
		if err == nil {
			t.Errorf("%s was fetched", target)
			continue
		}
		if !errors.Is(err, ErrMediaAddrBlocked) {
			t.Errorf("%s failed with %v, want the guard's refusal (any other error means a socket was opened toward an address that reaches IPv4 private space through the translator)", target, err)
		}
	}

	// And resolved from a hostname, which is how it arrives when the name is
	// innocent and the answer is not.
	for _, resolved := range []string{"64:ff9b:1::a9fe:a9fe", "64:ff9b:1::7f00:1", "2002:7f00:1::1"} {
		c := newMediaHTTPClient(mediaGuard{
			resolve: staticResolver{addr: netip.MustParseAddr(resolved)},
		})
		_, err := downloadMedia(context.Background(), c, "https://cos.example.com/object")
		if !errors.Is(err, ErrMediaAddrBlocked) {
			t.Errorf("a host resolving to %s: err = %v, want the guard's refusal", resolved, err)
		}
	}
}

// And on the redirect hop, against a real socket: the first fetch is allowed
// and answered, the Location header points into the local-use NAT64 prefix,
// and the second hop has to be refused by the same policy the first passed.
func TestARedirectIntoTheLocalUseNAT64PrefixIsRefused(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta" {
			reached = true
			w.Write([]byte("secrets"))
			return
		}
		http.Redirect(w, r, "http://[64:ff9b:1::a9fe:a9fe]/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	_, err := downloadMedia(context.Background(), testMediaClient(), srv.URL+"/object")
	if err == nil {
		t.Fatal("the redirect into NAT64 space was followed")
	}
	if !strings.Contains(err.Error(), ErrMediaAddrBlocked.Error()) {
		t.Fatalf("err = %v, want the guard's refusal on the second hop", err)
	}
	if reached {
		t.Error("the redirect target was actually fetched")
	}
}

// A public hostname is not a promise about where it points. The check has to
// happen against the resolved address at connect time, which is also what
// makes a rebinding answer useless.
func TestAPublicHostnameResolvingInwardIsRefused(t *testing.T) {
	client := newMediaHTTPClient(mediaGuard{
		resolve: staticResolver{addr: netip.MustParseAddr("169.254.169.254")},
	})
	_, err := downloadMedia(context.Background(), client, "https://cos.example.com/object")
	if !errors.Is(err, ErrMediaAddrBlocked) {
		t.Fatalf("err = %v, want the guard's refusal", err)
	}
}

// The redirect is the cheap attack: one Location header turns an allowed
// fetch into an internal one, and a check on the original URL never sees it.
func TestARedirectToAnInternalAddressIsRefused(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta" {
			reached = true
			w.Write([]byte("secrets"))
			return
		}
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	_, err := downloadMedia(context.Background(), testMediaClient(), srv.URL+"/object")
	if err == nil {
		t.Fatal("the redirect was followed")
	}
	if !strings.Contains(err.Error(), ErrMediaAddrBlocked.Error()) {
		t.Fatalf("err = %v, want the guard's refusal on the second hop", err)
	}
	if reached {
		t.Error("the redirect target was actually fetched")
	}
}

// A redirect chain that stays public still has to terminate.
func TestARedirectLoopIsCutOff(t *testing.T) {
	var hops int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer srv.Close()

	_, err := downloadMedia(context.Background(), testMediaClient(), srv.URL+"/loop")
	if err == nil {
		t.Fatal("the loop was followed to completion")
	}
	if hops > 8 {
		t.Errorf("followed %d hops before giving up", hops)
	}
}

// A redirect out of HTTP entirely never reaches a dialer, so the scheme guard
// is a separate half.
func TestARedirectToANonHTTPSchemeIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer srv.Close()

	_, err := downloadMedia(context.Background(), testMediaClient(), srv.URL+"/object")
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("err = %v, want the scheme refusal", err)
	}
}

// The guard must not have broken the thing it guards.
func TestAnAllowedFetchStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		w.Write([]byte("the bytes"))
	}))
	defer srv.Close()

	got, err := downloadMedia(context.Background(), testMediaClient(), srv.URL+"/object")
	if err != nil {
		t.Fatalf("a permitted fetch failed: %v", err)
	}
	if string(got.Body) != "the bytes" {
		t.Errorf("body = %q", got.Body)
	}
	if got.Filename != "report.pdf" {
		t.Errorf("filename = %q", got.Filename)
	}
}

// A host with one internal and one public answer must be reached on the
// public one rather than refused wholesale or allowed wholesale.
func TestAMixedAnswerDialsOnlyThePermittedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	var refused []netip.Addr
	g := mediaGuard{
		allow: allowLoopbackPolicy,
		resolve: multiResolver{addrs: []netip.Addr{
			netip.MustParseAddr("10.1.2.3"),  // refused
			netip.MustParseAddr("127.0.0.1"), // permitted by the test policy
		}},
		onRefuse: func(_ string, a netip.Addr) { refused = append(refused, a) },
	}
	got, err := downloadMedia(context.Background(), newMediaHTTPClient(g),
		fmt.Sprintf("http://cos.example.com:%s/object", port))
	if err != nil {
		t.Fatalf("the public answer was not used: %v", err)
	}
	if string(got.Body) != "ok" {
		t.Errorf("body = %q", got.Body)
	}
	if len(refused) != 1 || refused[0].String() != "10.1.2.3" {
		t.Errorf("refused = %v, want exactly the internal answer", refused)
	}
}

type multiResolver struct{ addrs []netip.Addr }

func (r multiResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addrs, nil
}

// A nil client used to fall through to http.DefaultClient, which is the
// unguarded fetch this whole file exists to prevent.
func TestANilClientRefusesRatherThanFallingBackToTheDefault(t *testing.T) {
	_, err := downloadMedia(context.Background(), nil, "https://cos.example.com/object")
	if err == nil || !strings.Contains(err.Error(), "guarded http client") {
		t.Fatalf("err = %v, want a refusal naming the missing guarded client", err)
	}
}

// The proxy environment must not become a way around the dial guard.
func TestTheMediaClientIgnoresProxyEnvironment(t *testing.T) {
	tr, ok := newMediaHTTPClient(mediaGuard{}).Transport.(*http.Transport)
	if !ok {
		t.Fatal("the media client's transport is not an *http.Transport")
	}
	if tr.Proxy != nil {
		t.Error("the media transport honours a proxy; a proxied fetch never reaches the dial guard")
	}
	// Not "DialContext is non-nil" — Transport.Clone() carries the default
	// dialer over, so that assertion passes on a transport with no guard at
	// all. Drive the installed dialer instead and require it to refuse.
	if tr.DialContext == nil {
		t.Fatal("the media transport has no dialer at all")
	}
	if _, err := tr.DialContext(context.Background(), "tcp", "169.254.169.254:80"); !errors.Is(err, ErrMediaAddrBlocked) {
		t.Errorf("the installed dialer answered %v for the metadata address, want the guard's refusal", err)
	}
}

// A transport failure must not write the pre-signed link into the error.
//
// The COS URL is a five-minute bearer credential for someone's private
// attachment: whoever holds the string can GET the file with no credential of
// their own. net/http wraps every failure in a *url.Error that prints the URL,
// and these errors go straight to r.logger.Warn — so a DNS hiccup, a reset or
// a timeout used to put a live link in the application log, and from there
// into whatever ships those logs onward.
func TestATransportFailureNeverCarriesTheSignedURL(t *testing.T) {
	const secret = "sig=AKIDSUPERSECRETSIGNATURE&t=1"

	// A server that accepts the connection and drops it mid-response, so the
	// failure is a genuine transport error rather than a status code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("test server cannot hijack")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close() // no status line at all
	}))
	defer srv.Close()

	target := srv.URL + "/object?" + secret

	t.Run("buffered", func(t *testing.T) {
		_, err := downloadMedia(context.Background(), testMediaClient(), target)
		if err == nil {
			t.Fatal("download succeeded, want a transport failure")
		}
		assertNoSignedURL(t, err, secret, srv.URL)
	})

	t.Run("streaming", func(t *testing.T) {
		_, _, err := openMedia(context.Background(), testMediaClient(), target)
		if err == nil {
			t.Fatal("open succeeded, want a transport failure")
		}
		assertNoSignedURL(t, err, secret, srv.URL)
	})
}

// A URL too malformed to parse is still a URL somebody signed.
func TestAnUnparseableURLIsNotEchoedBack(t *testing.T) {
	err := checkMediaURL("https://cos.example.com/o\x7f?sig=AKIDSECRET")
	if err == nil {
		t.Fatal("checkMediaURL accepted a control character, want a refusal")
	}
	if strings.Contains(err.Error(), "AKIDSECRET") {
		t.Fatalf("the refusal quoted the link back: %v", err)
	}
}

func assertNoSignedURL(t *testing.T, err error, secret, host string) {
	t.Helper()
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Fatalf("the error carries the signature: %v", err)
	}
	if strings.Contains(msg, host) {
		t.Fatalf("the error carries the link: %v", err)
	}
	// It still has to say what went wrong.
	if !strings.Contains(msg, "wecom: media") {
		t.Fatalf("the error lost its diagnosis: %v", err)
	}
}
