package managedruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SourceKind identifies a release metadata and asset transport supported by
// the managed runtime installer. Descriptors are compiled into the CLI; they
// are never accepted from an unauthenticated remote source.
type SourceKind string

const SourceGitHubRelease SourceKind = "github-release"

// IntegrityKind identifies how an asset's expected digest is published.
type IntegrityKind string

const IntegrityManifestSHA256 IntegrityKind = "manifest-sha256"

// LayoutKind identifies how an archive should be installed.
type LayoutKind string

const LayoutDirectory LayoutKind = "directory"

// Descriptor contains the provider-specific data consumed by the shared
// download, verification, extraction, smoke-test, and activation engine.
type Descriptor struct {
	Provider    string
	Command     string
	Version     string
	Source      SourceDescriptor
	Asset       AssetDescriptor
	Integrity   IntegrityDescriptor
	Layout      LayoutDescriptor
	Verify      VerifyDescriptor
	EnvOnLaunch map[string]string
}

type SourceDescriptor struct {
	Kind           SourceKind
	ReleaseBaseURL string
}

type AssetDescriptor struct {
	NameTemplate string
	ArchiveRoot  string
}

type IntegrityDescriptor struct {
	Kind         IntegrityKind
	ManifestName string
}

type LayoutDescriptor struct {
	Kind        LayoutKind
	ExecRelPath string
}

type VerifyDescriptor struct {
	Args          []string
	ExpectVersion string
}

var descriptors = map[string]Descriptor{
	"pi": {
		Provider: "pi",
		Command:  "pi",
		Version:  "0.83.0",
		Source: SourceDescriptor{
			Kind:           SourceGitHubRelease,
			ReleaseBaseURL: "https://github.com/earendil-works/pi/releases/download",
		},
		Asset: AssetDescriptor{
			NameTemplate: "pi-{os}-{arch}.{ext}",
			ArchiveRoot:  "pi",
		},
		Integrity: IntegrityDescriptor{
			Kind:         IntegrityManifestSHA256,
			ManifestName: "SHA256SUMS",
		},
		Layout: LayoutDescriptor{
			Kind:        LayoutDirectory,
			ExecRelPath: "pi{exe_ext}",
		},
		Verify: VerifyDescriptor{
			Args:          []string{"--version"},
			ExpectVersion: "0.83.0",
		},
		EnvOnLaunch: map[string]string{
			"PI_SKIP_VERSION_CHECK": "1",
			"PI_TELEMETRY":          "0",
		},
	},
}

func DescriptorFor(provider string) (Descriptor, bool) {
	d, ok := descriptors[strings.ToLower(strings.TrimSpace(provider))]
	return d, ok
}

func SupportedProviders() []string {
	out := make([]string, 0, len(descriptors))
	for provider := range descriptors {
		out = append(out, provider)
	}
	return out
}

func defaultRootDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MULTICA_MANAGED_RUNTIME_DIR")); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("MULTICA_MANAGED_RUNTIME_DIR must be absolute")
		}
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".multica", "runtimes"), nil
}

func platformTokens(goos, goarch string) (assetOS, assetArch, ext, exeExt string, err error) {
	switch goos {
	case "darwin", "linux", "windows":
		assetOS = goos
	default:
		return "", "", "", "", fmt.Errorf("unsupported OS %q", goos)
	}
	switch goarch {
	case "amd64":
		assetArch = "x64"
	case "arm64":
		assetArch = "arm64"
	default:
		return "", "", "", "", fmt.Errorf("unsupported architecture %q", goarch)
	}
	if goos == "windows" {
		return assetOS, assetArch, "zip", ".exe", nil
	}
	return assetOS, assetArch, "tar.gz", "", nil
}

func renderTemplate(template, goos, goarch string) (string, error) {
	assetOS, assetArch, ext, exeExt, err := platformTokens(goos, goarch)
	if err != nil {
		return "", err
	}
	replacer := strings.NewReplacer(
		"{os}", assetOS,
		"{arch}", assetArch,
		"{ext}", ext,
		"{exe_ext}", exeExt,
	)
	return replacer.Replace(template), nil
}

// ManagedExecutablePath returns the stable executable path for a provider.
// The target need not exist yet; the daemon probes this path on every discovery
// round so an install completed after daemon startup is still discoverable.
func ManagedExecutablePath(provider string) (string, error) {
	d, ok := DescriptorFor(provider)
	if !ok {
		return "", fmt.Errorf("unsupported managed runtime %q", provider)
	}
	root, err := defaultRootDir()
	if err != nil {
		return "", err
	}
	execRel, err := renderTemplate(d.Layout.ExecRelPath, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, d.Provider, "current", filepath.FromSlash(execRel)), nil
}

// IsManagedExecutable reports whether path belongs to the provider's managed
// runtime root. It uses lexical containment because a stable current symlink
// intentionally resolves outside the current path but remains inside the same
// provider root.
func IsManagedExecutable(provider, path string) bool {
	d, ok := DescriptorFor(provider)
	if !ok || strings.TrimSpace(path) == "" {
		return false
	}
	root, err := defaultRootDir()
	if err != nil {
		return false
	}
	providerRoot := filepath.Join(root, d.Provider)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(providerRoot, absPath)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func LaunchEnv(provider string) map[string]string {
	d, ok := DescriptorFor(provider)
	if !ok || len(d.EnvOnLaunch) == 0 {
		return nil
	}
	out := make(map[string]string, len(d.EnvOnLaunch))
	for key, value := range d.EnvOnLaunch {
		out[key] = value
	}
	return out
}
