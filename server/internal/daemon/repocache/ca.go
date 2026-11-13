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
	"time"
)

// caBundleConfigKey is the git setting that points at the CA bundle.
const caBundleConfigKey = "http.sslCAInfo"

// caBundleState describes the bundle file this daemon manages. It is package
// state because the isolated-checkout helpers that must record it are
// package-level functions.
type caBundleState struct {
	path string
	// active is set while the server sends extra CAs: repositories should
	// point at path. Once cleared, path still holds the machine's own trust
	// so checkouts that point at it keep working until they are reconfigured.
	active bool
}

var caState atomic.Pointer[caBundleState]

// activeCABundle returns the bundle path git must use, or "" when the server
// sent no extra CAs.
func activeCABundle() string {
	if st := caState.Load(); st != nil && st.active {
		return st.path
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

// SetExtraCACerts makes git trust pemCerts on top of the trust it already has
// on this machine.
//
// git accepts a CA only as a file, and http.sslCAInfo replaces its default
// trust, so the bundle is the machine's trust (see machineCATrust) followed by
// pemCerts. Repositories point at it through their own config, which a
// repository-local value set by hand, a URL-scoped http.<url>.sslCAInfo, and
// GIT_SSL_CAINFO all override.
//
// An empty pemCerts deactivates the bundle without deleting it: checkouts
// created while it was active still point at the file, so it is rewritten to
// the machine's trust alone and each repository drops the reference the next
// time this cache configures it.
func (c *Cache) SetExtraCACerts(pemCerts string) error {
	c.caMu.Lock()
	defer c.caMu.Unlock()

	path := c.caBundlePath()
	extra, err := normalizeCACerts(pemCerts)
	if err != nil {
		return err
	}

	if len(extra) == 0 {
		caState.Store(&caBundleState{path: path})
		if _, err := os.Stat(path); err != nil {
			return nil
		}
		base, err := machineCATrust(path)
		if err != nil {
			// Keep the previous bundle: stale trust beats a missing file.
			return fmt.Errorf("refresh CA bundle after removal: %w", err)
		}
		return writeBundleIfChanged(path, base, nil)
	}

	if runtime.GOOS == "windows" {
		// Git for Windows defaults to Schannel, which ignores sslCAInfo, and
		// its OpenSSL backend ships its own bundle we would have to locate.
		return errors.New("private CA certificates from the server are not applied on Windows; add them to the Windows certificate store")
	}
	base, err := machineCATrust(path)
	if err != nil {
		return err
	}
	if err := writeBundleIfChanged(path, base, extra); err != nil {
		return err
	}
	caState.Store(&caBundleState{path: path, active: true})
	return nil
}

// machineCATrust returns the CA certificates git on this machine trusts for a
// host without a URL-scoped setting: the file named by a general
// http.sslCAInfo in the system or global config (or GIT_CONFIG_* env), so an
// operator's own internal CAs stay trusted, or else the system bundle.
// ownPath is skipped so the bundle never feeds on itself.
func machineCATrust(ownPath string) ([]byte, error) {
	out, err := runGitOutputWithTimeout(gitConfigReadTimeout, "config", "--type=path", "--get", caBundleConfigKey)
	if configured := strings.TrimSpace(string(out)); err == nil && configured != "" && filepath.Clean(configured) != filepath.Clean(ownPath) {
		data, err := os.ReadFile(configured)
		if err != nil {
			return nil, fmt.Errorf("read %s from git config: %w", caBundleConfigKey, err)
		}
		return data, nil
	}
	if system := readSystemCABundle(); len(system) > 0 {
		return system, nil
	}
	return nil, errors.New("no system CA bundle found; not overriding git's trust store")
}

const gitConfigReadTimeout = 30 * time.Second

func writeBundleIfChanged(path string, base, extra []byte) error {
	var bundle bytes.Buffer
	bundle.Write(base)
	if len(extra) > 0 {
		if !bytes.HasSuffix(base, []byte("\n")) {
			bundle.WriteByte('\n')
		}
		bundle.Write(extra)
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, bundle.Bytes()) {
		return nil
	}
	if err := writeFileAtomic(path, bundle.Bytes()); err != nil {
		return fmt.Errorf("write CA bundle: %w", err)
	}
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

// caCloneArgs returns the git options a clone needs to trust the bundle.
// The new repository has no config yet; configureRepoCABundleContext records
// the setting right after the clone.
func caCloneArgs() []string {
	if bundle := activeCABundle(); bundle != "" {
		return []string{"-c", caBundleConfigKey + "=" + bundle}
	}
	return nil
}

// configureRepoCABundleLogged is configureRepoCABundleContext for paths where
// a failure must not block the clone, fetch, or checkout around it.
func (c *Cache) configureRepoCABundleLogged(ctx context.Context, repoPath string) {
	if err := configureRepoCABundleContext(ctx, repoPath); err != nil {
		c.logger.Warn("repo cache: could not record CA bundle in repo config", "path", repoPath, "error", err)
	}
}

// configureRepoCABundleContext points a repository's own config at the bundle
// while it is active and removes that reference once it is not. The daemon's
// fetches in the bare cache, agents' git in linked worktrees (which share the
// cache's config), and isolated checkouts all read it from there. A value
// that is not this cache's bundle was set by someone else and is left alone.
func configureRepoCABundleContext(ctx context.Context, repoPath string) error {
	st := caState.Load()
	if st == nil {
		return nil
	}
	out, _ := runGitOutputContext(ctx, "-C", repoPath, "config", "--local", "--get", caBundleConfigKey)
	have := strings.TrimSpace(string(out))
	switch {
	case st.active && have == "":
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "config", "--local", caBundleConfigKey, st.path); err != nil {
			return fmt.Errorf("set %s: %s: %w", caBundleConfigKey, strings.TrimSpace(string(out)), err)
		}
	case !st.active && have == st.path:
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "config", "--local", "--unset", caBundleConfigKey); err != nil {
			return fmt.Errorf("unset %s: %s: %w", caBundleConfigKey, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}
