package wecom

// media_guard.go — the media fetcher is pointed at an address by somebody else.
//
// A callback carries a pre-signed COS URL and we GET it. The URL is a string
// that arrived over the socket: WeCom put it there, but the adapter cannot
// prove that, and the fetch runs from inside the deployment's network with
// whatever reach that network has. On this machine alone that reach includes
// a Tailscale tailnet (100.64.0.0/10), a proxy's fake-IP range
// (198.18.0.0/15), Docker's bridges, and the loopback the backend's own
// admin endpoints listen on.
//
// So the guard is not on the URL, it is on the CONNECTION. Checking the
// hostname is worth nothing on its own: the URL can redirect (a 302 to
// http://169.254.169.254/ is one line of attacker-controlled response), and
// a hostname that passed a check can resolve to something else a moment
// later. A DialContext that resolves the destination and refuses a
// non-public address runs on every hop of every redirect, against the answer
// actually being connected to, which is the only place both of those are
// covered at once.
//
// The shape is @leroy-chen's, from the other WeCom media PR (#6524,
// newSecureMediaClient / isAllowedMediaIP) — that PR raised this first and
// its fix is the right one. Two things are added here: the reserved ranges
// below (a predicate list built on netip's own helpers misses CGNAT-adjacent
// and IETF-reserved space, and CGNAT is where a Tailscale tailnet lives), and
// an injectable resolver so the rebinding case can be tested rather than
// argued about.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ErrMediaAddrBlocked is returned instead of a connection when every address
// a media host resolves to is one the deployment must not be pointed at. It
// is deliberately distinct from a dial failure: the caller logs the two
// differently, and only this one means somebody sent us a URL they should
// not have.
var ErrMediaAddrBlocked = errors.New("wecom: media host resolves to a non-public address")

// mediaDialTimeout bounds one TCP connect. It sits inside
// mediaDownloadTimeout, which bounds the whole fetch.
const mediaDialTimeout = 10 * time.Second

// The reserved ranges come in two groups, and the difference is not
// taxonomy — it is whether an operator is allowed to open them.
//
// Both lists are the IANA IPv4 and IPv6 Special-Purpose Address Registries
// minus what netip's own predicates already catch (IsLoopback / IsPrivate /
// IsLinkLocal* / IsMulticast / IsUnspecified), minus the handful of
// special-purpose blocks that are ordinary globally-routed unicast (the AS112
// delegations 192.31.196.0/24, 192.175.48.0/24 and 2620:4f:8000::/48, and
// AMT's 192.52.193.0/24) — those are "special" in who runs them, not in where
// they point, and refusing them would buy nothing.
//
// reservedMediaPrefixes is space that is not the public internet: a media URL
// resolving there is either a mistake or an attempt, and either way there is
// no object at the end of it. 100.64.0.0/10 is the one that matters most —
// RFC 6598 shared address space, which no standard-library predicate reports
// as private, and which is exactly what Tailscale hands out. A tailnet peer
// is a machine inside the trust boundary reachable by IP with no credential.
//
// This group is what mediaAllowedPrefixes can reopen, because a real
// deployment shape lives in it: a fake-IP proxy hands out 198.18.0.0/15 for
// every public hostname, WeCom's own COS host included. An operator who knows
// their proxy's pool can say so.
var reservedMediaPrefixes = []netip.Prefix{
	// ---- IPv4 ----
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598 CGNAT — Tailscale lives here
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking — and a proxy's fake-IP range
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes 255.255.255.255
	netip.MustParsePrefix("192.88.99.0/24"),  // deprecated 6to4 relay anycast — the v4 end of 2002::/16

	// ---- IPv6 ----
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("100:0:0:1::/64"), // dummy prefix, also discard-only
	// The whole IETF protocol-assignments block, not its dozen sub-entries.
	// It holds Teredo (2001::/32), benchmarking (2001:2::/48 — the twin of
	// 198.18.0.0/15 above), AMT, AS112-v6, ORCHID/ORCHIDv2, DET, and the PCP
	// / TURN / DNS-SD anycast addresses. None of it is somewhere a COS object
	// lives, and one prefix is easier to keep true than eight. Same call the
	// v4 side already makes with 192.0.0.0/24. Documentation space is
	// 2001:db8::/32, which is outside this /23 and listed separately.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), // documentation
	netip.MustParsePrefix("3fff::/20"),     // documentation, RFC 9637
	netip.MustParsePrefix("5f00::/16"),     // SRv6 SIDs, RFC 9602 — routing labels, not hosts
	// Site-local. RFC 3879 deprecated it and IANA delisted it, which is why
	// no predicate and no registry row covers it — but the networks that
	// were numbered out of it before 2004 still route it internally, and it
	// is one line.
	netip.MustParsePrefix("fec0::/10"),
}

// translationMediaPrefixes is TRANSLATION space, and no configuration reopens
// it. These addresses are not destinations, they are IPv4 destinations
// wearing an IPv6 costume: 64:ff9b:1::a9fe:a9fe is 169.254.169.254 the moment
// a NAT64 translator sees it, and 2002:7f00:1::1 is 127.0.0.1 through a 6to4
// relay.
//
// That costume is why the split exists. The early IsLoopback / IsPrivate /
// IsLinkLocalUnicast checks never fire on these — at that point they are
// IPv6 addresses, and every one of them reaches straight past a guard that
// only knows how to recognise an IPv4 address when it is written as one. This
// list is the ONLY thing standing between the guard and the address embedded
// inside. Letting mediaAllowedPrefixes override it would mean
// MULTICA_WECOM_MEDIA_ALLOW_CIDRS=::/0 — or, just as well, =2002::/16 —
// reaching the loopback and the metadata endpoint the guard exists to refuse,
// written in a spelling the operator never thought they were opening.
//
// So it is a hard block, and it costs nothing: no COS object, no proxy pool
// and no deployment lives in translation space, so there is no shape to
// accommodate here the way there is for a fake-IP proxy.
var translationMediaPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64, well-known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"), // NAT64, local-use prefix (RFC 8215)
	netip.MustParsePrefix("2002::/16"),      // 6to4 — the last 112 bits open with an IPv4 address
}

// addrPolicy answers whether one resolved address may be dialed. Production
// uses publicAddrOnly; tests substitute a policy that allows the loopback
// their own server is on, so that what is under test is the guard's decision
// rather than the test harness's address.
type addrPolicy func(netip.Addr) bool

// mediaAllowedPrefixes are ranges an operator has declared safe for media
// fetches despite looking reserved. It exists for one real deployment shape:
// a machine behind a fake-IP proxy, where the resolver answers every public
// hostname with an address out of 198.18.0.0/15 and the proxy forwards the
// traffic onward. On such a machine WeCom's own COS host is indistinguishable
// from a link-local metadata endpoint by address alone, so the guard refuses
// every attachment and inbound media stops working entirely.
//
// Empty by default. Widening this is a decision with a cost: whatever range is
// listed here can be reached by a URL somebody else controls, which is exactly
// what the guard exists to prevent. It is opt-in per deployment, and worth
// setting only where the operator knows the range belongs to their proxy.
//
// What it CANNOT open, at any width: loopback, the private ranges and
// link-local, which are refused above the loop this list is consulted in; and
// translationMediaPrefixes, which is refused for the same reason one step
// later. Those are the ranges an over-wide CIDR would otherwise hand to
// whoever supplied the URL.
var mediaAllowedPrefixes []netip.Prefix

// SetMediaAllowedPrefixes declares ranges the media guard may dial. Called at
// boot from MULTICA_WECOM_MEDIA_ALLOW_CIDRS. An unparseable entry is reported
// and skipped rather than silently widening or silently narrowing the guard.
func SetMediaAllowedPrefixes(cidrs []string) []error {
	var (
		out  []netip.Prefix
		errs []error
	)
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("wecom: media allow cidr %q: %w", raw, err))
			continue
		}
		out = append(out, p)
	}
	mediaAllowedPrefixes = out
	return errs
}

// publicAddrOnly is the production policy: everything that is not routable
// public internet is refused.
func publicAddrOnly(a netip.Addr) bool {
	// An IPv4-mapped IPv6 address (::ffff:127.0.0.1) reports none of the IPv4
	// predicates until it is unmapped, which is the whole trick.
	a = a.Unmap()
	if !a.IsValid() {
		return false
	}
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	// Translation space first, and before the allow-list is consulted at all.
	// The address inside one of these is an IPv4 address the checks above
	// would have refused on sight; the only reason they did not fire is the
	// spelling. Nothing an operator configures reopens it.
	for _, p := range translationMediaPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	for _, p := range reservedMediaPrefixes {
		if p.Contains(a) {
			// An operator may have declared this range theirs — a fake-IP
			// proxy's pool is the case this exists for. Checked only for
			// addresses the guard would otherwise refuse, so an empty
			// allow-list leaves the guard exactly as strict as before.
			for _, allowed := range mediaAllowedPrefixes {
				if allowed.Contains(a) {
					return true
				}
			}
			return false
		}
	}
	return true
}

// hostResolver is net.DefaultResolver's one method we need, so a test can
// hand back an address of its choosing for a name of its choosing.
type hostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// mediaGuard is the dialer the media client connects through.
type mediaGuard struct {
	allow    addrPolicy
	resolve  hostResolver
	onRefuse func(host string, addr netip.Addr) // test hook; nil in production
}

// dial resolves the destination itself and connects to a checked address —
// never to the hostname, because resolving twice is the rebinding window.
//
// Every address is checked before any is dialed, and the connection is made
// to the literal that passed. A host that answers with a mix of public and
// internal addresses gets the public ones tried and the internal ones
// refused, rather than the whole name allowed on the strength of one good
// answer.
func (g mediaGuard) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("wecom: media dial: %w", err)
	}
	addrs, err := g.resolver().LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("wecom: media dial: resolve %s: %w", host, err)
	}
	allow := g.allow
	if allow == nil {
		allow = publicAddrOnly
	}

	d := &net.Dialer{Timeout: mediaDialTimeout}
	lastErr := error(ErrMediaAddrBlocked)
	for _, a := range addrs {
		if !allow(a) {
			if g.onRefuse != nil {
				g.onRefuse(host, a)
			}
			continue
		}
		conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func (g mediaGuard) resolver() hostResolver {
	if g.resolve != nil {
		return g.resolve
	}
	return net.DefaultResolver
}

// newMediaHTTPClient builds the client every media download goes through.
//
// Two guards, and they cover different things. The dialer refuses a
// destination; CheckRedirect refuses a SCHEME, because a redirect to
// file:// or gopher:// never reaches a dialer at all and the transport
// would happily hand it to a protocol handler.
func newMediaHTTPClient(g mediaGuard) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = g.dial
	// Nothing about a COS object needs a proxy, and honouring HTTP_PROXY here
	// would send the fetch to an address the dial guard never sees.
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("wecom: media download: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("wecom: media redirect to scheme %q refused", req.URL.Scheme)
			}
			return nil
		},
	}
}
