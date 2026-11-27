package vcs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const instanceRequestTimeout = 15 * time.Second

// ErrForbiddenInstance is returned when an instance URL points at an address
// the VCS client must not fetch: loopback, link-local (including cloud
// metadata), unspecified, multicast, or a well-known metadata hostname.
// Private (RFC1918 / ULA) addresses are allowed — connecting a self-hosted
// Gitea, Forgejo, or GitLab on a LAN or compose network is the feature.
var ErrForbiddenInstance = errors.New("vcs: instance URL is not a permitted provider address")

// Resolver looks up a hostname. Tests inject a fake; production uses the
// default resolver.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

var (
	httpClient = newInstanceHTTPClient()

	policyMu           sync.RWMutex
	testAddressAllowed func(netip.Addr) bool
)

// OverrideInstanceAddressPolicyForTest replaces the address predicate used by
// ValidateInstanceURL and the instance HTTP client. The returned function
// restores the previous policy. Production code must not call this.
func OverrideInstanceAddressPolicyForTest(fn func(netip.Addr) bool) func() {
	policyMu.Lock()
	prev := testAddressAllowed
	testAddressAllowed = fn
	policyMu.Unlock()
	return func() {
		policyMu.Lock()
		testAddressAllowed = prev
		policyMu.Unlock()
	}
}

// ValidateInstanceURL checks that a ConnectVCS instance URL is an absolute
// http(s) URL whose host is safe to request from the Multica server (GH #9439).
// userinfo is rejected so a token cannot be smuggled in the URL.
func ValidateInstanceURL(ctx context.Context, raw string, resolver Resolver) error {
	parsed, err := url.Parse(NormalizeInstanceURL(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("instance URL must be an absolute http(s) URL")
	}
	if parsed.User != nil {
		return ErrForbiddenInstance
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || isForbiddenInstanceHostname(host) {
		return ErrForbiddenInstance
	}
	addresses, err := lookupInstanceAddresses(ctx, host, resolver)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return fmt.Errorf("resolve instance host: no addresses")
	}
	for _, address := range addresses {
		if !instanceAddressPermitted(address) {
			return ErrForbiddenInstance
		}
	}
	return nil
}

func lookupInstanceAddresses(ctx context.Context, host string, resolver Resolver) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	if resolver == nil {
		resolver = currentInstanceResolver()
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve instance host: %w", err)
	}
	out := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.Unmap())
	}
	return out, nil
}

func currentInstanceResolver() Resolver {
	return net.DefaultResolver
}

func instanceAddressPermitted(address netip.Addr) bool {
	address = address.Unmap()
	policyMu.RLock()
	fn := testAddressAllowed
	policyMu.RUnlock()
	if fn != nil {
		return fn(address)
	}
	return !isForbiddenInstanceAddress(address)
}

func isForbiddenInstanceHostname(host string) bool {
	switch host {
	case "localhost", "metadata", "metadata.google.internal", "metadata.goog", "instance-data":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}

func isForbiddenInstanceAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	// Cloud metadata and other unicast ranges that are never a Git instance.
	// RFC1918 and general ULA are intentionally not listed.
	blocked := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.100.100.200/32"), // Alibaba metadata
		netip.MustParsePrefix("fd00:ec2::/32"),      // AWS IPv6 metadata
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func newInstanceHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: instanceRequestTimeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := lookupInstanceAddresses(ctx, strings.TrimSuffix(host, "."), nil)
		if err != nil {
			return nil, err
		}
		var last error
		for _, candidate := range addresses {
			if !instanceAddressPermitted(candidate) {
				last = ErrForbiddenInstance
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			last = dialErr
		}
		if last == nil {
			last = ErrForbiddenInstance
		}
		return nil, last
	}
	return &http.Client{
		Timeout:       instanceRequestTimeout,
		Transport:     transport,
		CheckRedirect: checkInstanceRedirect,
	}
}

func checkInstanceRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return ValidateInstanceURL(req.Context(), req.URL.String(), nil)
}
