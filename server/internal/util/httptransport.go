// Package util provides shared helpers for the Multica server and CLI.
//
// This file implements env-gated TLS configuration for outbound HTTP
// clients (CLI and daemon). It addresses issue #4441: corporate TLS
// inspection middleboxes re-sign leaves with a serverAuth-deficient
// cert, causing x509: OSStatus -26276 (errSecInvalidExtendedKeyUsage)
// on macOS.
//
// Two environment variables are honored (following the existing
// MULTICA_HTTP_TIMEOUT convention):
//
//   - MULTICA_CA_BUNDLE=/path/to/roots.pem
//     Appends a custom CA bundle to the system root pool. Verification
//     stays fully on (chain + hostname + EKU). Use this when the
//     corporate root is missing from the system keychain.
//
//   - MULTICA_TLS_INSECURE=1
//     Emergency-only: sets InsecureSkipVerify on the TLS config. Off by
//     default and loudly warned. Restores continuity as a last resort.
//
//   - MULTICA_TLS_EKU_ANY=1
//     Relaxes ONLY the serverAuth-EKU requirement (sets
//     VerifyOptions.KeyUsages = ExtKeyUsageAny) while keeping hostname
//     and chain trust. Strictly safer than full insecure; targets the
//     exact errSecInvalidExtendedKeyUsage error from issue #4441.
package util

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// NewHTTPClient builds an *http.Client with an env-gated custom Transport.
// The timeout parameter sets the client-level timeout (use 0 for no timeout).
//
// This function is safe to call from both the CLI (cmd/multica) and the
// daemon (internal/daemon). It replaces the previous pattern of
// &http.Client{Timeout: ...} which had no way to supply a CA bundle or
// relax verification for API traffic.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := newHTTPTransport()
	if transport != nil {
		return &http.Client{Timeout: timeout, Transport: transport}
	}
	return &http.Client{Timeout: timeout}
}

// TLSConfig returns a *tls.Config built from MULTICA_CA_BUNDLE /
// MULTICA_TLS_INSECURE / MULTICA_TLS_EKU_ANY, or nil if no TLS
// customization is configured. Used by the WebSocket dialer which
// can't reuse the http.Transport directly.
func TLSConfig() *tls.Config {
	return buildTLSConfig()
}

// newHTTPTransport returns a custom *http.RoundTripper configured from
// environment variables, or nil if no TLS customization is needed (in
// which case the caller should use http.DefaultTransport).
func newHTTPTransport() http.RoundTripper {
	tlsConfig := buildTLSConfig()
	if tlsConfig == nil {
		return nil
	}

	// Clone the default transport so we don't mutate global state.
	base, ok := http.DefaultTransport.(*http.Transport)
	var transport *http.Transport
	if ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.TLSClientConfig = tlsConfig
	return transport
}

// buildTLSConfig returns a *tls.Config from environment variables, or nil
// if no TLS customization is requested.
func buildTLSConfig() *tls.Config {
	caBundle := os.Getenv("MULTICA_CA_BUNDLE")
	insecure := os.Getenv("MULTICA_TLS_INSECURE")
	ekuAny := os.Getenv("MULTICA_TLS_EKU_ANY")

	// Normalize: "0" or empty means disabled, "1" means enabled.
	insecureEnabled := insecure == "1"
	ekuAnyEnabled := ekuAny == "1"

	if caBundle == "" && !insecureEnabled && !ekuAnyEnabled {
		return nil // no customization needed
	}

	tlsConfig := &tls.Config{}

	if caBundle != "" {
		pool, err := loadCABundle(caBundle)
		if err != nil {
			slog.Error("MULTICA_CA_BUNDLE: failed to load, falling back to system roots",
				"path", caBundle, "error", err)
		} else {
			tlsConfig.RootCAs = pool
			slog.Info("MULTICA_CA_BUNDLE: custom CA bundle loaded", "path", caBundle)
		}
	}

	if insecureEnabled {
		tlsConfig.InsecureSkipVerify = true
		slog.Warn("MULTICA_TLS_INSECURE=1: TLS certificate verification is DISABLED. " +
			"Use only as a last resort for corporate TLS inspection middleboxes.")
	}

	if ekuAnyEnabled && !tlsConfig.InsecureSkipVerify {
		// Relax only the ExtendedKeyUsage requirement while keeping chain
		// and hostname verification. This targets errSecInvalidExtendedKeyUsage
		// (the exact error in issue #4441) without the full blast radius
		// of InsecureSkipVerify.
		tlsConfig.MinVersion = tls.VersionTLS12
		// Turn off the default verifier and use a custom VerifyConnection
		// callback that accepts any EKU (ExtKeyUsageAny).
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			opts := x509.VerifyOptions{
				Roots:         tlsConfig.RootCAs,
				DNSName:       state.ServerName,
				Intermediates: x509.NewCertPool(),
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
			}
			for _, cert := range state.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(opts)
			return err
		}
		slog.Info("MULTICA_TLS_EKU_ANY=1: relaxed serverAuth EKU requirement " +
			"(chain + hostname verification remain active)")
	}

	return tlsConfig
}

// loadCABundle reads a PEM-encoded CA bundle from path and returns a
// CertPool that includes both the system roots and the custom CAs.
func loadCABundle(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		// SystemCertPool can fail on some platforms; start fresh.
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, errInvalidCABundle(path)
	}
	return pool, nil
}

// errInvalidCABundle is returned when the PEM file contains no parseable
// certificates.
type invalidCABundleError struct{ path string }

func (e *invalidCABundleError) Error() string {
	return "MULTICA_CA_BUNDLE: no certificates found in " + e.path
}

func errInvalidCABundle(path string) error {
	return &invalidCABundleError{path: path}
}
