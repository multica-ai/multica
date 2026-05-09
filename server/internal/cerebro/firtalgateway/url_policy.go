package firtalgateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const defaultHTTPTimeout = 10 * time.Minute

var disallowedGatewayIPPrefixes = mustParsePrefixes([]string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"255.255.255.255/32",
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
})

func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("gateway URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("gateway URL is invalid")
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("gateway URL must use https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("gateway URL must include a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("gateway URL must not include user info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("gateway URL must not include query or fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimRight(parsed.String(), "/"), nil
}

func ValidateBaseURL(raw string) (string, error) {
	normalized, err := NormalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("gateway URL is invalid")
	}
	if err := validateGatewayHost(parsed.Hostname()); err != nil {
		return "", err
	}
	return normalized, nil
}

func BaseURLHost(raw string) (string, error) {
	normalized, err := NormalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("gateway URL is invalid")
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")), nil
}

func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = guardedDialContext((&net.Dialer{}).DialContext)
	return &http.Client{
		Transport: transport,
		Timeout:   defaultHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func guardedDialContext(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("gateway dial address is invalid: %w", err)
		}
		if err := validateGatewayHost(host); err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve gateway host: %w", err)
		}
		if len(ips) == 0 {
			return nil, errors.New("gateway host resolved no addresses")
		}
		for _, ip := range ips {
			if err := validateGatewayIP(ip.IP); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func validateGatewayHost(host string) error {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "" {
		return fmt.Errorf("gateway URL must include a host")
	}
	if strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateGatewayIP(ip)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("gateway host must not be localhost")
	}
	if !strings.Contains(host, ".") {
		return fmt.Errorf("gateway host must be fully qualified")
	}
	for _, suffix := range []string{".local", ".localdomain", ".internal", ".lan"} {
		if strings.HasSuffix(host, suffix) {
			return fmt.Errorf("gateway host must be public")
		}
	}
	return nil
}

func validateGatewayIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("gateway host resolved invalid address")
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("gateway host must resolve to public addresses")
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("gateway host resolved invalid address")
	}
	addr = addr.Unmap()
	for _, prefix := range disallowedGatewayIPPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("gateway host must resolve to public addresses")
		}
	}
	return nil
}

func mustParsePrefixes(raw []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
