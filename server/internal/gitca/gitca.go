// Package gitca extracts the private CA certificates an operator added to the
// backend's trust store through SSL_CERT_DIR, so the server can hand them to
// daemons. Daemons clone with the system git binary, which never reads the
// backend's SSL_CERT_DIR; without these certificates a repository on a host
// with an internal CA cannot sync.
package gitca

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// MaxBundleSize caps the PEM sent to daemons. A handful of internal CAs is a
// few KB; anything near this limit is a misconfigured directory (for example
// the whole system store mounted under another path).
const MaxBundleSize = 256 << 10

// Load returns the PEM of every certificate in the non-system directories of
// SSL_CERT_DIR that crypto/x509 also trusts, or nil when there are none.
// Problems with individual files or certificates are logged and skipped so a
// bad file never blocks the server from starting.
func Load(logger *slog.Logger) []byte {
	certs, warnings := collect(os.Getenv(certDirEnv), systemCertDirectories)
	for _, w := range warnings {
		logger.Warn("git CA: skipping certificate", "detail", w)
	}
	if len(certs) == 0 {
		return nil
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		logger.Warn("git CA: system cert pool unavailable; not sharing SSL_CERT_DIR certificates with daemons", "error", err)
		return nil
	}

	var bundle bytes.Buffer
	fingerprints := make([]string, 0, len(certs))
	for _, cert := range certs {
		if err := trustedBy(cert, roots); err != nil {
			logger.Warn("git CA: certificate is not trusted by the backend; skipping", "subject", cert.Subject.String(), "error", err)
			continue
		}
		block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		if bundle.Len()+len(block) > MaxBundleSize {
			logger.Warn("git CA: bundle size limit reached; skipping remaining certificates", "limit_bytes", MaxBundleSize)
			break
		}
		bundle.Write(block)
		fingerprints = append(fingerprints, fingerprint(cert))
	}
	if bundle.Len() == 0 {
		return nil
	}
	logger.Info("git CA: sharing SSL_CERT_DIR certificates with daemons", "count", len(fingerprints), "sha256", fingerprints)
	return bundle.Bytes()
}

// trustedBy reports whether crypto/x509 accepts cert as a trust anchor, so
// daemons receive exactly what the backend itself trusts even if collect ever
// drifts from the standard library's loader.
func trustedBy(cert *x509.Certificate, roots *x509.CertPool) error {
	_, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	return err
}

// collect mirrors the SSL_CERT_DIR half of loadSystemRoots in
// crypto/x509/root_unix.go (Go 1.26): split on ":", read each directory while
// skipping symlinks that point within it, and parse every file as PEM. Unlike
// the standard library it skips the system directories, keeps the parsed
// certificates instead of a CertPool, and drops duplicates.
func collect(certDir string, systemDirs []string) ([]*x509.Certificate, []string) {
	if certDir == "" {
		return nil, nil
	}
	system := make(map[string]struct{}, len(systemDirs))
	for _, d := range systemDirs {
		system[filepath.Clean(d)] = struct{}{}
	}

	var (
		certs    []*x509.Certificate
		warnings []string
		seen     = map[[sha256.Size]byte]struct{}{}
	)
	for _, dir := range strings.Split(certDir, ":") {
		if dir == "" {
			continue
		}
		if _, ok := system[filepath.Clean(dir)]; ok {
			continue
		}
		entries, err := readUniqueDirectoryEntries(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("read %s: %v", dir, err))
			}
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				// Subdirectories land here too; crypto/x509 ignores them the same way.
				continue
			}
			for _, cert := range parseCertificates(path, data, &warnings) {
				sum := sha256.Sum256(cert.Raw)
				if _, dup := seen[sum]; dup {
					continue
				}
				seen[sum] = struct{}{}
				certs = append(certs, cert)
			}
		}
	}
	return certs, warnings
}

// parseCertificates matches CertPool.AppendCertsFromPEM: non-CERTIFICATE
// blocks and certificates that fail to parse are skipped, not fatal.
func parseCertificates(path string, data []byte, warnings *[]string) []*x509.Certificate {
	var certs []*x509.Certificate
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("parse %s: %v", path, err))
			continue
		}
		certs = append(certs, cert)
	}
	return certs
}

func fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}
