// Package webhook provides outbound HTTP webhook delivery for Multica events.
// See RFC #1964 (multica-ai/multica issue 1964) for the full design.
//
// The SSRF helper here is the security boundary: a workspace-configurable
// URL the server POSTs to is the textbook SSRF gadget. Block private ranges
// at DNS-resolution time, re-check on each redirect, and only allow loopback
// when explicitly opted in via env-var (intended for self-hosted dev).
package webhook

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// AllowPrivateEnvVar is the env-level escape hatch for self-hosted deployments
// that intentionally need to reach internal services (e.g. an internal n8n on
// the same network). When set to "true", IsAllowedHost returns nil even for
// private IPs. NEVER set this on a multi-tenant cloud deployment.
const AllowPrivateEnvVar = "MULTICA_WEBHOOK_ALLOW_PRIVATE"

// MaxRedirects is the cap on HTTP redirects per delivery attempt. Each redirect
// re-runs IsAllowedHost on the new target so an attacker can't escape via a
// 302 to an internal address.
const MaxRedirects = 3

// ErrSSRFBlocked is returned by IsAllowedHost when the destination is in a
// blocked range. The dispatcher should mark the delivery as failed/dead and
// surface the reason in last_error.
var ErrSSRFBlocked = errors.New("destination address is in a blocked range")

// blockedNets is the IPv4 + IPv6 deny list applied at DNS resolution time.
// Listed mostly per RFC1918 + RFC4193 + RFC4291 + cloud-metadata norms.
//
// "Blocked" means: a webhook subscription targeting a domain that resolves
// to one of these will refuse to send. The receive side never sees the
// request; the dispatcher records a permanent error.
var blockedCIDRs = []string{
	// IPv4
	"10.0.0.0/8",       // RFC1918
	"172.16.0.0/12",    // RFC1918
	"192.168.0.0/16",   // RFC1918
	"127.0.0.0/8",      // loopback
	"169.254.0.0/16",   // link-local + cloud-metadata (169.254.169.254)
	"100.64.0.0/10",    // RFC6598 carrier-grade NAT — internal-facing in practice
	"0.0.0.0/8",        // "this network" reserved
	"224.0.0.0/4",      // multicast
	"240.0.0.0/4",      // future-use reserved
	// IPv6
	"::1/128",  // loopback
	"fe80::/10", // link-local
	"fc00::/7",  // ULA
	"::/128",    // unspecified
	"ff00::/8",  // multicast
}

// CompileBlockedNets parses the deny list once at process start. Tests can
// call it; production callers go through IsAllowedHost.
func CompileBlockedNets() ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(blockedCIDRs))
	for _, s := range blockedCIDRs {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// IsAllowedHost resolves the URL's host via the system resolver and checks
// every returned A/AAAA record against the deny list. Returns ErrSSRFBlocked
// (wrapped with the offending IP) on any block, nil on success.
//
// allowPrivate bypasses the deny check; intended for the env-var override path.
// allowHTTP permits http:// targets; otherwise only https:// is accepted.
func IsAllowedHost(target *url.URL, allowPrivate, allowHTTP bool) error {
	if target == nil {
		return errors.New("nil target url")
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "https" && !(scheme == "http" && allowHTTP) {
		return fmt.Errorf("scheme %q not permitted (use https; set allow_http=true to permit http)", scheme)
	}

	host := target.Hostname()
	if host == "" {
		return errors.New("missing host in url")
	}

	if allowPrivate {
		return nil
	}

	addrs, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("dns lookup for %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("dns lookup for %s returned no records", host)
	}

	nets, err := CompileBlockedNets()
	if err != nil {
		return fmt.Errorf("compile blocked cidrs: %w", err)
	}

	for _, ip := range addrs {
		for _, n := range nets {
			if n.Contains(ip) {
				return fmt.Errorf("%w: %s resolves to %s", ErrSSRFBlocked, host, ip)
			}
		}
	}
	return nil
}

// SSRFAwareCheckRedirect is the http.Client.CheckRedirect function the
// webhook dispatcher uses. Caps redirects at MaxRedirects and re-runs
// IsAllowedHost on every redirect target — preventing an attacker from
// escaping the deny list via a 302 from an allowed host to an internal one.
//
// Bind via closure so allowPrivate / allowHTTP travel with the per-request
// configuration of the subscription:
//
//	client := &http.Client{
//	    CheckRedirect: webhook.SSRFAwareCheckRedirect(allowPrivate, allowHTTP),
//	}
func SSRFAwareCheckRedirect(allowPrivate, allowHTTP bool) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("too many redirects (cap=%d)", MaxRedirects)
		}
		if err := IsAllowedHost(req.URL, allowPrivate, allowHTTP); err != nil {
			return err
		}
		return nil
	}
}
