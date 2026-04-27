package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultUpdateManifestURL = "https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json"
)

type UpdateManifest struct {
	Version             string                `json:"version"`
	PublishedAt         string                `json:"published_at,omitempty"`
	MinSupportedVersion string                `json:"min_supported_version,omitempty"`
	ReleaseNotes        string                `json:"release_notes,omitempty"`
	Assets              []UpdateManifestAsset `json:"assets"`
}

type UpdateManifestAsset struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Channel     string `json:"channel,omitempty"`
	URL         string `json:"download_url"`
	Checksum    string `json:"checksum"`
	ArchiveName string `json:"archive_name,omitempty"`
}

type semver struct {
	Major int
	Minor int
	Patch int
}

var semverRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

func parseSemver(raw string) (semver, error) {
	m := semverRe.FindStringSubmatch(raw)
	if len(m) != 4 {
		return semver{}, fmt.Errorf("cannot parse version %q", raw)
	}

	var v semver
	if _, err := fmt.Sscanf(m[0], "v%d.%d.%d", &v.Major, &v.Minor, &v.Patch); err == nil {
		return v, nil
	}
	if _, err := fmt.Sscanf(m[0], "%d.%d.%d", &v.Major, &v.Minor, &v.Patch); err == nil {
		return v, nil
	}
	return semver{}, fmt.Errorf("cannot parse version %q", raw)
}

func (v semver) lessThan(other semver) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

func normalizeReleaseTag(targetVersion string) string {
	tag := strings.TrimSpace(targetVersion)
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return tag
}

func releaseArchiveExtension(goos string) string {
	if goos == "windows" {
		return "zip"
	}
	return "tar.gz"
}

func releaseAssetCandidates(targetVersion, goos, goarch string) []string {
	tag := normalizeReleaseTag(targetVersion)
	version := strings.TrimPrefix(tag, "v")
	ext := releaseArchiveExtension(goos)
	return []string{
		fmt.Sprintf("multica-cli-%s-%s-%s.%s", version, goos, goarch, ext),
		fmt.Sprintf("multica_%s_%s.%s", goos, goarch, ext),
	}
}

func resolveUpdateManifestURL() string {
	if env := strings.TrimSpace(os.Getenv("MULTICA_UPDATE_MANIFEST_URL")); env != "" {
		return env
	}
	cfg, err := LoadCLIConfig()
	if err == nil && strings.TrimSpace(cfg.UpdateManifestURL) != "" {
		return strings.TrimSpace(cfg.UpdateManifestURL)
	}
	return defaultUpdateManifestURL
}

func FetchLatestRelease() (*UpdateManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, resolveUpdateManifestURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update manifest returned %d", resp.StatusCode)
	}

	var manifest UpdateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return nil, fmt.Errorf("update manifest missing version")
	}
	return &manifest, nil
}

func resolveManagedInstallPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	binaryName := "multica"
	if runtime.GOOS == "windows" {
		binaryName = "multica.exe"
	}
	return filepath.Join(home, ".multica", "bin", binaryName), nil
}

func ResolveInstalledBinaryPath() (string, error) {
	managedPath, err := resolveManagedInstallPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(managedPath); err == nil {
		return managedPath, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err == nil {
		return resolved, nil
	}
	return exePath, nil
}

func IsManagedInstall() bool {
	managedPath, err := resolveManagedInstallPath()
	if err != nil {
		return false
	}
	current, err := ResolveInstalledBinaryPath()
	if err != nil {
		return false
	}
	return current == managedPath
}

func findManifestAsset(manifest *UpdateManifest, goos, goarch string) (*UpdateManifestAsset, error) {
	candidates := releaseAssetCandidates(manifest.Version, goos, goarch)
	var fallback *UpdateManifestAsset

	for _, candidate := range candidates {
		for i := range manifest.Assets {
			asset := &manifest.Assets[i]
			if asset.OS != goos || asset.Arch != goarch {
				continue
			}
			if asset.ArchiveName == candidate {
				return asset, nil
			}
			if asset.ArchiveName == "" && fallback == nil {
				fallback = asset
			}
		}
	}

	if fallback != nil {
		return fallback, nil
	}

	return nil, fmt.Errorf("no matching manifest asset for %s/%s", goos, goarch)
}

func downloadAsset(url string) ([]byte, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func verifyChecksum(data []byte, expected string) error {
	want := strings.ToLower(strings.TrimSpace(expected))
	if want == "" {
		return fmt.Errorf("missing checksum")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", want, got)
	}
	return nil
}

func UpdateViaDownload(targetVersion string) (string, error) {
	manifest, err := FetchLatestRelease()
	if err != nil {
		return "", fmt.Errorf("fetch update manifest: %w", err)
	}

	targetTag := normalizeReleaseTag(targetVersion)
	manifestTag := normalizeReleaseTag(manifest.Version)
	if targetTag != manifestTag {
		return "", fmt.Errorf("requested version %s does not match manifest version %s", targetTag, manifestTag)
	}

	asset, err := findManifestAsset(manifest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}

	archiveData, err := downloadAsset(asset.URL)
	if err != nil {
		return "", err
	}
	if err := verifyChecksum(archiveData, asset.Checksum); err != nil {
		return "", err
	}

	binaryName := "multica"
	if runtime.GOOS == "windows" {
		binaryName = "multica.exe"
	}

	var binaryData []byte
	if runtime.GOOS == "windows" {
		binaryData, err = extractBinaryFromZip(bytes.NewReader(archiveData), binaryName)
	} else {
		binaryData, err = extractBinaryFromTarGz(bytes.NewReader(archiveData), binaryName)
	}
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}

	exePath, err := resolveManagedInstallPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		return "", fmt.Errorf("create install dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(exePath), "multica-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(binaryData); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod temp file: %w", err)
	}

	if err := replaceBinary(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("replace binary: %w", err)
	}

	return fmt.Sprintf("Downloaded %s and installed %s", targetTag, exePath), nil
}

func ShouldUpdate(currentVersion string, latest *UpdateManifest) (bool, error) {
	currentSemver, err := parseSemver(currentVersion)
	if err != nil {
		return false, err
	}
	latestSemver, err := parseSemver(latest.Version)
	if err != nil {
		return false, err
	}
	return currentSemver.lessThan(latestSemver), nil
}

func extractBinaryFromTarGz(r io.Reader, name string) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("binary %q not found in archive", name)
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if filepath.Base(hdr.Name) == name && hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read binary: %w", err)
			}
			return data, nil
		}
	}
}

func extractBinaryFromZip(r io.Reader, name string) ([]byte, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read zip data: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return nil, fmt.Errorf("zip reader: %w", err)
	}

	for _, f := range zr.File {
		if filepath.Base(f.Name) == name && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open zip entry: %w", err)
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("read binary: %w", err)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", name)
}

func GetBrewPrefix() string {
	out, err := exec.Command("brew", "--prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func IsBrewInstall() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		resolved = exePath
	}

	brewPrefix := GetBrewPrefix()
	if brewPrefix != "" && strings.HasPrefix(resolved, brewPrefix) {
		return true
	}

	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		if strings.HasPrefix(resolved, prefix+"/Cellar/") {
			return true
		}
	}
	return false
}

func UpdateViaBrew() (string, error) {
	cmd := exec.Command("brew", "upgrade", "multica-ai/tap/multica")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("brew upgrade failed: %w", err)
	}
	return string(out), nil
}
