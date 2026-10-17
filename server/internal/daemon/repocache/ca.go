package repocache

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// caBundleConfigKey is the git setting that points at the CA bundle.
const caBundleConfigKey = "http.sslCAInfo"

// activeCABundle is the bundle path git must use, or "" when the server sent
// no extra CAs. It is package state because gitEnv, which every git command
// here goes through, is a package-level function.
var activeCABundle atomic.Pointer[string]

func currentCABundle() string {
	if p := activeCABundle.Load(); p != nil {
		return *p
	}
	return ""
}

// systemCABundleFiles mirrors crypto/x509's certFiles on Linux plus the
// macOS/BSD location: the bundle libcurl uses by default on these systems.
var systemCABundleFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora/RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS/RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine Linux, macOS
}

func (c *Cache) caBundlePath() string {
	return filepath.Join(c.root, ".ca", "bundle.pem")
}

// SetExtraCACerts makes git trust pemCerts in addition to the system roots.
//
// git accepts a CA only as a file, and http.sslCAInfo replaces its default
// trust store, so the bundle written here is the system bundle followed by
// pemCerts; public hosts stay trusted. GIT_SSL_CAINFO and URL-scoped
// http.<url>.sslCAInfo settings the operator configured still win over it.
// An empty pemCerts removes the bundle.
func (c *Cache) SetExtraCACerts(pemCerts string) error {
	c.caMu.Lock()
	defer c.caMu.Unlock()

	path := c.caBundlePath()
	extra, err := normalizeCACerts(pemCerts)
	if err != nil {
		return err
	}
	if len(extra) == 0 {
		activeCABundle.Store(nil)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		// Git for Windows defaults to Schannel, which ignores sslCAInfo, and
		// its OpenSSL backend ships its own bundle we would have to locate.
		return errors.New("private CA certificates from the server are not applied on Windows; add them to the Windows certificate store")
	}

	system := readSystemCABundle()
	if len(system) == 0 {
		return errors.New("no system CA bundle found; not overriding git's trust store")
	}
	var bundle bytes.Buffer
	bundle.Write(system)
	if !bytes.HasSuffix(system, []byte("\n")) {
		bundle.WriteByte('\n')
	}
	bundle.Write(extra)

	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, bundle.Bytes()) {
		if err := writeFileAtomic(path, bundle.Bytes()); err != nil {
			return fmt.Errorf("write CA bundle: %w", err)
		}
	}
	activeCABundle.Store(&path)
	return nil
}

// normalizeCACerts keeps the parseable CERTIFICATE blocks of raw. An input
// with content but no usable certificate is an error so a broken server value
// does not silently drop a bundle that was working.
func normalizeCACerts(raw string) ([]byte, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data := []byte(raw)
	var out bytes.Buffer
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			continue
		}
		_ = pem.Encode(&out, block)
	}
	if out.Len() == 0 {
		return nil, errors.New("server sent CA certificates without a valid PEM certificate")
	}
	return out.Bytes(), nil
}

func readSystemCABundle() []byte {
	files := systemCABundleFiles
	if f := os.Getenv("SSL_CERT_FILE"); f != "" {
		files = append([]string{f}, files...)
	}
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil && len(bytes.TrimSpace(data)) > 0 {
			return data
		}
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".bundle-*.pem")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// configureRepoCABundleLogged is configureRepoCABundleContext for paths where
// a failure must not block the clone, fetch, or checkout around it.
func (c *Cache) configureRepoCABundleLogged(ctx context.Context, repoPath string) {
	if err := c.configureRepoCABundleContext(ctx, repoPath); err != nil {
		c.logger.Warn("repo cache: could not record CA bundle in repo config", "path", repoPath, "error", err)
	}
}

// configureRepoCABundleContext records the bundle in a repository's own
// config. The daemon's git already gets it through gitEnv; this is for the
// git commands agents run in worktrees (which share the bare cache's config)
// and isolated checkouts, which do not go through gitEnv. When no bundle is
// active it removes only a value this cache wrote, never one set by hand.
func (c *Cache) configureRepoCABundleContext(ctx context.Context, repoPath string) error {
	want := currentCABundle()
	out, _ := runGitOutputContext(ctx, "-C", repoPath, "config", "--local", "--get", caBundleConfigKey)
	have := strings.TrimSpace(string(out))
	switch {
	case want != "" && have != want:
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "config", "--local", caBundleConfigKey, want); err != nil {
			return fmt.Errorf("set %s: %s: %w", caBundleConfigKey, strings.TrimSpace(string(out)), err)
		}
	case want == "" && have != "" && have == c.caBundlePath():
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "config", "--local", "--unset", caBundleConfigKey); err != nil {
			return fmt.Errorf("unset %s: %s: %w", caBundleConfigKey, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}
