package remotemcp

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"strings"
)

// The operator opt-in that lets a Plugin author point an `mcp` hook at a server
// running on their own machine.
//
// The `http` transport already has this: MULTICA_PLUGIN_DEV_ORIGINS names exact
// origins whose endpoints skip the public-internet requirement. The `mcp`
// transport did not, so an author had to deploy to the public internet before
// they could try anything — and, for the same reason, the startup path that
// pins approved tools by schema digest could not be exercised by a test at all.
// One asymmetry, both symptoms.
//
// This changes nothing in a deployment that does not set the variable, which is
// every production deployment: with no entries every call below returns false
// and the original checks run unmodified.

// DevOriginsEnv is read by both the server and the daemon. They are separate
// processes, so the value is looked up per call rather than cached at init.
const DevOriginsEnv = "MULTICA_PLUGIN_DEV_ORIGINS"

// DevCAEnv is the CA bundle to trust for those origins. A locally-run MCP
// server still has to speak HTTPS — the manifest validator requires it, and
// relaxing that would mean the URL an administrator consented to no longer says
// whether the connection is encrypted. So the dev allowance is "trust this
// extra CA", never "skip verification".
const DevCAEnv = "MULTICA_PLUGIN_DEV_CA"

// isDevOrigin reports whether the operator named this exact origin.
//
// Exact match on scheme + host + port. A prefix or suffix match would let
// "http://127.0.0.1:9000" authorise "http://127.0.0.1:9000.example.com".
// devTLSConfig returns the TLS config for a dev origin, or nil to leave the
// transport's default alone.
//
// Nil is also what an unreadable or empty CA file yields: failing closed here
// means a mistyped path produces a certificate error, not a silently
// unverified connection.
func devTLSConfig() *tls.Config {
	path := strings.TrimSpace(os.Getenv(DevCAEnv))
	if path == "" {
		return nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func isDevOrigin(endpoint *url.URL) bool {
	if endpoint == nil || endpoint.Host == "" {
		return false
	}
	configured := strings.TrimSpace(os.Getenv(DevOriginsEnv))
	if configured == "" {
		return false
	}
	origin := endpoint.Scheme + "://" + endpoint.Host
	for _, entry := range strings.Split(configured, ",") {
		if strings.TrimSpace(entry) == origin {
			return true
		}
	}
	return false
}
