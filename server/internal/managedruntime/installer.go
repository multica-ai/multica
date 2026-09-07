package managedruntime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	defaultInstallTimeout = 3 * time.Minute
	maxManifestBytes      = 1 << 20
	maxArchiveBytes       = 256 << 20
	staleLockAge          = 5 * time.Minute
	lockPollInterval      = 100 * time.Millisecond
)

type InstallResult struct {
	Provider  string `json:"provider"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

// Installer is the shared managed-runtime engine. Production callers use
// DefaultInstaller; tests inject a local HTTP client, platform, root, and
// descriptor without changing process-global state.
type Installer struct {
	RootDir     string
	Client      *http.Client
	GOOS        string
	GOARCH      string
	Descriptors map[string]Descriptor
}

func DefaultInstaller() (*Installer, error) {
	root, err := defaultRootDir()
	if err != nil {
		return nil, err
	}
	return &Installer{
		RootDir:     root,
		Client:      &http.Client{Timeout: defaultInstallTimeout},
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Descriptors: descriptors,
	}, nil
}

func (i *Installer) Install(ctx context.Context, provider string) (InstallResult, error) {
	d, err := i.descriptor(provider)
	if err != nil {
		return InstallResult{}, err
	}
	if d.Source.Kind != SourceGitHubRelease || d.Integrity.Kind != IntegrityManifestSHA256 || d.Layout.Kind != LayoutDirectory {
		return InstallResult{}, fmt.Errorf("managed runtime %q uses an unsupported descriptor shape", d.Provider)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultInstallTimeout)
		defer cancel()
	}

	providerRoot := filepath.Join(i.RootDir, d.Provider)
	if err := os.MkdirAll(filepath.Join(providerRoot, "versions"), 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("create managed runtime root: %w", err)
	}
	release, err := acquireInstallLock(ctx, filepath.Join(providerRoot, ".install.lock"))
	if err != nil {
		return InstallResult{}, err
	}
	defer release()

	execRel, err := renderTemplate(d.Layout.ExecRelPath, i.GOOS, i.GOARCH)
	if err != nil {
		return InstallResult{}, err
	}
	versionDir := filepath.Join(providerRoot, "versions", d.Version)
	versionExec := filepath.Join(versionDir, filepath.FromSlash(execRel))
	if err := smokeExecutable(ctx, versionExec, d.Verify, d.EnvOnLaunch); err == nil {
		if err := switchCurrent(providerRoot, versionDir); err != nil {
			return InstallResult{}, fmt.Errorf("activate existing managed runtime: %w", err)
		}
		return InstallResult{Provider: d.Provider, Version: d.Version, Path: filepath.Join(providerRoot, "current", filepath.FromSlash(execRel))}, nil
	}

	assetName, err := renderTemplate(d.Asset.NameTemplate, i.GOOS, i.GOARCH)
	if err != nil {
		return InstallResult{}, err
	}
	tag := d.Version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	releaseBase := strings.TrimRight(d.Source.ReleaseBaseURL, "/") + "/" + tag
	manifestURL := releaseBase + "/" + d.Integrity.ManifestName
	assetURL := releaseBase + "/" + assetName

	manifest, err := i.downloadBytes(ctx, manifestURL, maxManifestBytes)
	if err != nil {
		return InstallResult{}, fmt.Errorf("download checksum manifest: %w", err)
	}
	expected, err := checksumForAsset(manifest, assetName)
	if err != nil {
		return InstallResult{}, err
	}

	archive, err := os.CreateTemp(providerRoot, ".download-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create archive temp file: %w", err)
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		_ = os.Remove(archivePath)
		return InstallResult{}, fmt.Errorf("close archive temp file: %w", err)
	}
	defer os.Remove(archivePath)
	actual, err := i.downloadFile(ctx, assetURL, archivePath, maxArchiveBytes)
	if err != nil {
		return InstallResult{}, fmt.Errorf("download runtime archive: %w", err)
	}
	if !strings.EqualFold(expected, actual) {
		return InstallResult{}, fmt.Errorf("checksum mismatch for %q: expected %s, got %s", assetName, expected, actual)
	}

	staging, err := os.MkdirTemp(providerRoot, ".staging-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := extractArchive(archivePath, staging, maxArchiveBytes*4); err != nil {
		return InstallResult{}, fmt.Errorf("extract runtime archive: %w", err)
	}

	stagedRoot := filepath.Join(staging, filepath.FromSlash(d.Asset.ArchiveRoot))
	info, err := os.Lstat(stagedRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return InstallResult{}, fmt.Errorf("archive root %q is missing or unsafe", d.Asset.ArchiveRoot)
	}
	stagedExec := filepath.Join(stagedRoot, filepath.FromSlash(execRel))
	if i.GOOS != "windows" {
		if err := os.Chmod(stagedExec, 0o755); err != nil {
			return InstallResult{}, fmt.Errorf("make runtime executable: %w", err)
		}
	}
	if err := smokeExecutable(ctx, stagedExec, d.Verify, d.EnvOnLaunch); err != nil {
		return InstallResult{}, fmt.Errorf("runtime smoke test failed: %w", err)
	}

	if err := os.RemoveAll(versionDir); err != nil {
		return InstallResult{}, fmt.Errorf("remove invalid managed version: %w", err)
	}
	if err := os.Rename(stagedRoot, versionDir); err != nil {
		return InstallResult{}, fmt.Errorf("commit managed version: %w", err)
	}
	if err := switchCurrent(providerRoot, versionDir); err != nil {
		return InstallResult{}, fmt.Errorf("activate managed runtime: %w", err)
	}
	return InstallResult{
		Provider:  d.Provider,
		Version:   d.Version,
		Path:      filepath.Join(providerRoot, "current", filepath.FromSlash(execRel)),
		Installed: true,
	}, nil
}

func (i *Installer) descriptor(provider string) (Descriptor, error) {
	key := strings.ToLower(strings.TrimSpace(provider))
	d, ok := i.Descriptors[key]
	if !ok {
		return Descriptor{}, fmt.Errorf("unsupported managed runtime %q", provider)
	}
	return d, nil
}

func (i *Installer) httpClient() *http.Client {
	if i.Client != nil {
		return i.Client
	}
	return &http.Client{Timeout: defaultInstallTimeout}
}

func (i *Installer) downloadBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := i.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return data, nil
}

func (i *Installer) downloadFile(ctx context.Context, url, path string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := i.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return "", fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > limit {
		return "", fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

var sha256HexPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func checksumForAsset(manifest []byte, assetName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(manifest)))
	for scanner.Scan() {
		fields := strings.Fields(strings.TrimSpace(scanner.Text()))
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		if !sha256HexPattern.MatchString(fields[0]) {
			return "", fmt.Errorf("checksum for %q is not a SHA-256 digest", assetName)
		}
		return strings.ToLower(fields[0]), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksum manifest: %w", err)
	}
	return "", fmt.Errorf("checksum for %q not found in manifest", assetName)
}

func smokeExecutable(ctx context.Context, path string, verify VerifyDescriptor, launchEnv map[string]string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	smokeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(smokeCtx, path, verify.Args...)
	cmd.Env = os.Environ()
	for key, value := range launchEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	if verify.ExpectVersion != "" && !strings.Contains(string(out), verify.ExpectVersion) {
		return fmt.Errorf("unexpected version output %q (want %s)", strings.TrimSpace(string(out)), verify.ExpectVersion)
	}
	return nil
}

func acquireInstallLock(ctx context.Context, path string) (func(), error) {
	for {
		if err := os.Mkdir(path, 0o700); err == nil {
			owner := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
			if err := os.WriteFile(filepath.Join(path, ".owner"), []byte(owner), 0o600); err != nil {
				_ = os.RemoveAll(path)
				return nil, fmt.Errorf("record install lock owner: %w", err)
			}
			// Keep the lock fresh while a slow download is active. The CLI's
			// timeout is configurable, so relying only on a fixed stale age could
			// let a second process steal a healthy long-running installation.
			done := make(chan struct{})
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				ticker := time.NewTicker(staleLockAge / 3)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						if installLockOwnedBy(path, owner) {
							now := time.Now()
							_ = os.Chtimes(path, now, now)
						}
					case <-done:
						return
					}
				}
			}()
			var once sync.Once
			return func() {
				once.Do(func() {
					close(done)
					<-stopped
					if installLockOwnedBy(path, owner) {
						_ = os.RemoveAll(path)
					}
				})
			}, nil
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire install lock: %w", err)
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.RemoveAll(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for managed runtime install lock: %w", ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
}

func installLockOwnedBy(path, owner string) bool {
	data, err := os.ReadFile(filepath.Join(path, ".owner"))
	return err == nil && string(data) == owner
}
