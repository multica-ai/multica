package infoflow

import (
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// The guarded HTTP client used for the one outbound fetch InfoFlow performs: an
// object key turned into a presigned URL by im.File.GetURL.
//
// The FID route does not come through here — im.File.Download performs its own
// authenticated fetch inside the SDK, against the open platform's own gateway.
// This client exists for the presigned-URL route, where the URL is a bearer
// value that came off the wire and can point anywhere. The policy is the one
// dingtalk/media.go and wecom/media_guard.go established:
//
//   - HTTPS only, so the presigned signature is never sent in cleartext.
//   - No ambient proxy: a per-process HTTP proxy would silently carry the
//     download somewhere the SSRF check below cannot see.
//   - The hostname is resolved, every address must be publicly routable, and
//     the dial goes straight to a validated address — so DNS rebinding between
//     the check and the connect cannot re-point the request.
//   - Redirects are re-checked against the same rules, drop the Referer (which
//     would otherwise carry a signed URL to the redirect target), and are capped.
//   - The signed query string is stripped before the error travels anywhere.

const maxMediaRedirects = 3

// newMediaHTTPClient builds the client described above.
func newMediaHTTPClient() *http.Client {
	dialer := &publicDownloadDialer{
		dialer:   &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
		resolver: net.DefaultResolver,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{
		Transport: transport,
		// The per-request context carries the deadline; this is the backstop for
		// a stalled TLS handshake that never yields to the context.
		Timeout: mediaFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxMediaRedirects {
				return errors.New("stopped after media redirects")
			}
			if err := validateDownloadURL(req.URL); err != nil {
				return err
			}
			// A signed URL must not follow a hop to another origin with our
			// credentials attached.
			if len(via) > 0 && !sameDownloadOrigin(via[0].URL, req.URL) {
				return errors.New("refusing cross-origin media redirect")
			}
			req.Header.Del("Referer")
			return nil
		},
	}
}

// validateDownloadURL accepts only a public HTTPS target.
func validateDownloadURL(u *url.URL) error {
	if u == nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return errors.New("invalid download target")
	}
	if u.User != nil {
		return errors.New("invalid download target credentials")
	}
	return nil
}

func sameDownloadOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// publicDownloadDialer resolves the host itself, refuses a non-public address,
// then dials one of the addresses it validated.
type publicDownloadDialer struct {
	dialer   *net.Dialer
	resolver *net.Resolver
}

func (d *publicDownloadDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid download target address")
	}
	addrs, err := d.lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, errors.New("resolve download target failed")
	}
	for _, addr := range addrs {
		if !isPublicDownloadAddress(addr) {
			return nil, errors.New("blocked non-public download target")
		}
	}
	for _, addr := range addrs {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("connect to download target failed")
}

func (d *publicDownloadDialer) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	return addrs, nil
}

// nonPublicDownloadPrefixes are ranges netip still calls global unicast that a
// presigned URL has no business pointing at.
var nonPublicDownloadPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv6 transition mechanisms can encapsulate an otherwise-blocked IPv4
	// destination in an address netip classifies as global unicast. Fail closed
	// rather than decode every deployment-specific mapping.
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var wellKnownNAT64Prefix = netip.MustParsePrefix("64:ff9b::/96")

// isPublicDownloadAddress is the same predicate dingtalk and wecom apply: a
// presigned URL may only ever point at a public host.
func isPublicDownloadAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if wellKnownNAT64Prefix.Contains(addr) {
		raw := addr.As16()
		return isPublicDownloadAddress(netip.AddrFrom4([4]byte(raw[12:16])))
	}
	for _, prefix := range nonPublicDownloadPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// fileNameFromDisposition takes the file name out of a Content-Disposition when
// it has one, falling back to whatever the platform called the attachment.
// The result is a bare name: inboundMediaName strips any directory component
// before it reaches storage.
func fileNameFromDisposition(disposition, fallback string) string {
	if strings.TrimSpace(disposition) == "" {
		return fallback
	}
	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return fallback
	}
	if name := strings.TrimSpace(params["filename"]); name != "" {
		return name
	}
	return fallback
}
