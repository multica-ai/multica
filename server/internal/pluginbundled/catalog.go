// Package pluginbundled discovers and validates immutable Plugin releases
// shipped with the Multica server binary.
package pluginbundled

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	PublisherTypeOfficial = "official"
	TrustTierOfficial     = "official"
)

//go:embed trust_roots.json releases
var bundledFS embed.FS

// CatalogEntry is a validated bundled release plus the acquisition-boundary
// trust decision used when it is installed into a workspace.
type CatalogEntry struct {
	Release          plugincontract.ValidatedRelease
	PublisherType    string
	TrustTier        string
	Compatible       bool
	CompatibilityWhy string
}

// Diagnostic reports a release that could not be admitted without preventing
// healthy releases, or the server, from loading.
type Diagnostic struct {
	SourceRef string `json:"source_ref"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// Catalog is immutable after construction and safe for concurrent reads.
type Catalog struct {
	entries     map[string]map[string]CatalogEntry
	diagnostics []Diagnostic
}

type trustRootFile struct {
	Keys []struct {
		ID        string `json:"id"`
		PublicKey string `json:"public_key"`
	} `json:"keys"`
}

type releaseMetadata struct {
	SourceRef      string `json:"source_ref"`
	SignatureKeyID string `json:"signature_key_id"`
	SignatureFile  string `json:"signature_file"`
	PublisherType  string `json:"publisher_type"`
	TrustTier      string `json:"trust_tier"`
}

// Load discovers the releases embedded in the production server. A bad trust
// root or release becomes a catalog diagnostic; it never blocks Kernel startup.
func Load() *Catalog {
	return LoadFromFS(bundledFS)
}

// LoadFromFS is the testable discovery boundary for bundled sources.
func LoadFromFS(source fs.FS) *Catalog {
	catalog := &Catalog{entries: make(map[string]map[string]CatalogEntry)}
	verifier, err := loadTrustStore(source)
	if err != nil {
		catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
			SourceRef: "bundled://trust-roots",
			Code:      "trust_roots_invalid",
			Message:   "Bundled Plugin trust roots could not be loaded.",
		})
		return catalog
	}

	var metadataFiles []string
	if err := fs.WalkDir(source, "releases", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				SourceRef: "bundled://" + name,
				Code:      "source_unreadable",
				Message:   "A bundled Plugin source could not be read.",
			})
			return fs.SkipDir
		}
		if !entry.IsDir() && path.Base(name) == "release.json" {
			metadataFiles = append(metadataFiles, name)
		}
		return nil
	}); err != nil {
		catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
			SourceRef: "bundled://releases",
			Code:      "source_unreadable",
			Message:   "Bundled Plugin releases could not be discovered.",
		})
		return catalog
	}
	sort.Strings(metadataFiles)
	for _, metadataFile := range metadataFiles {
		entry, diagnostic := loadRelease(source, verifier, metadataFile)
		if diagnostic != nil {
			catalog.diagnostics = append(catalog.diagnostics, *diagnostic)
			continue
		}
		key := entry.Release.Manifest.Metadata.Key
		version := entry.Release.Manifest.Metadata.Version
		if catalog.entries[key] == nil {
			catalog.entries[key] = make(map[string]CatalogEntry)
		}
		if _, duplicate := catalog.entries[key][version]; duplicate {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				SourceRef: entry.Release.SourceRef,
				Code:      "duplicate_release",
				Message:   "A duplicate bundled Plugin release was ignored.",
			})
			continue
		}
		catalog.entries[key][version] = entry
	}
	return catalog
}

func loadTrustStore(source fs.FS) (*plugincontract.Ed25519TrustStore, error) {
	raw, err := fs.ReadFile(source, "trust_roots.json")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var roots trustRootFile
	if err := decoder.Decode(&roots); err != nil {
		return nil, err
	}
	keys := make(map[string]ed25519.PublicKey, len(roots.Keys))
	for _, root := range roots.Keys {
		decoded, err := base64.StdEncoding.DecodeString(root.PublicKey)
		if err != nil {
			return nil, err
		}
		keys[root.ID] = ed25519.PublicKey(decoded)
	}
	if len(keys) == 0 {
		return nil, errors.New("bundled Plugin trust store is empty")
	}
	return plugincontract.NewEd25519TrustStore(keys)
}

func loadRelease(source fs.FS, verifier plugincontract.ReleaseVerifier, metadataFile string) (CatalogEntry, *Diagnostic) {
	releaseDir := path.Dir(metadataFile)
	metadataRaw, err := fs.ReadFile(source, metadataFile)
	if err != nil {
		return CatalogEntry{}, releaseDiagnostic(releaseDir, "release_metadata_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataRaw))
	decoder.DisallowUnknownFields()
	var metadata releaseMetadata
	if err := decoder.Decode(&metadata); err != nil || metadata.SourceRef == "" || metadata.SignatureKeyID == "" || metadata.SignatureFile == "" {
		return CatalogEntry{}, releaseDiagnostic(releaseDir, "release_metadata_invalid")
	}
	if metadata.PublisherType != PublisherTypeOfficial || metadata.TrustTier != TrustTierOfficial {
		return CatalogEntry{}, releaseDiagnostic(metadata.SourceRef, "publisher_not_official")
	}
	archive, err := buildArchive(source, releaseDir, metadata.SignatureFile)
	if err != nil {
		return CatalogEntry{}, releaseDiagnostic(metadata.SourceRef, "artifact_invalid")
	}
	signatureRaw, err := fs.ReadFile(source, path.Join(releaseDir, metadata.SignatureFile))
	if err != nil {
		return CatalogEntry{}, releaseDiagnostic(metadata.SourceRef, "signature_missing")
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureRaw)))
	if err != nil {
		return CatalogEntry{}, releaseDiagnostic(metadata.SourceRef, "signature_invalid")
	}
	release, err := plugincontract.ValidateReleaseCandidate(plugincontract.ReleaseCandidate{
		Archive:        archive,
		SourceKind:     plugincontract.SourceBundled,
		SourceRef:      metadata.SourceRef,
		SignatureKeyID: metadata.SignatureKeyID,
		Signature:      signature,
	}, verifier)
	if err != nil {
		code := "artifact_invalid"
		if errors.Is(err, plugincontract.ErrInvalidSignature) || errors.Is(err, plugincontract.ErrUnknownSigningKey) {
			code = "signature_invalid"
		}
		return CatalogEntry{}, releaseDiagnostic(metadata.SourceRef, code)
	}
	compatible, why := CompatibleWithV1Host(release.Manifest)
	return CatalogEntry{
		Release:          release,
		PublisherType:    metadata.PublisherType,
		TrustTier:        metadata.TrustTier,
		Compatible:       compatible,
		CompatibilityWhy: why,
	}, nil
}

func releaseDiagnostic(sourceRef, code string) *Diagnostic {
	if !strings.HasPrefix(sourceRef, "bundled://") {
		sourceRef = "bundled://" + strings.TrimPrefix(sourceRef, "releases/")
	}
	return &Diagnostic{
		SourceRef: sourceRef,
		Code:      code,
		Message:   "A bundled Plugin release failed validation and was isolated.",
	}
}

func buildArchive(source fs.FS, releaseDir, signatureFile string) ([]byte, error) {
	var files []string
	if err := fs.WalkDir(source, releaseDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(name, releaseDir+"/")
		if relative == "release.json" || relative == signatureFile {
			return nil
		}
		files = append(files, name)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("bundled Plugin release has no artifact files")
	}
	sort.Strings(files)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range files {
		content, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		header := &zip.FileHeader{
			Name:   strings.TrimPrefix(name, releaseDir+"/"),
			Method: zip.Store,
		}
		header.SetMode(0o600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// CompatibleWithV1Host applies the V1 host and daemon compatibility contract
// to both bundled and workspace-private releases.
func CompatibleWithV1Host(manifest plugincontract.Manifest) (bool, string) {
	if manifest.Compatibility.HostAPI != ">=1.0.0 <2.0.0" {
		return false, "host_api_unsupported"
	}
	supported := map[string]bool{
		plugincontract.DaemonFeatureExecutionManifestV1: true,
		plugincontract.DaemonFeatureAgentSkillV1:        true,
		plugincontract.DaemonFeatureRemoteMCPV1:         true,
	}
	for _, feature := range manifest.Compatibility.RequiredDaemonFeatures {
		if !supported[feature] {
			return false, "daemon_feature_unsupported"
		}
	}
	return true, ""
}

// List returns every admitted release in stable plugin/version order.
func (c *Catalog) List() []CatalogEntry {
	if c == nil {
		return nil
	}
	var entries []CatalogEntry
	for _, versions := range c.entries {
		for _, entry := range versions {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].Release.Manifest.Metadata
		right := entries[j].Release.Manifest.Metadata
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return compareSemver(left.Version, right.Version) > 0
	})
	return entries
}

// Find resolves an exact immutable bundled release.
func (c *Catalog) Find(pluginKey, version string) (CatalogEntry, bool) {
	if c == nil || c.entries[pluginKey] == nil {
		return CatalogEntry{}, false
	}
	entry, ok := c.entries[pluginKey][version]
	return entry, ok
}

// Latest resolves the highest semantic version for a Plugin.
func (c *Catalog) Latest(pluginKey string) (CatalogEntry, bool) {
	versions := c.entries[pluginKey]
	var latest CatalogEntry
	found := false
	for _, entry := range versions {
		if !found || compareSemver(entry.Release.Manifest.Metadata.Version, latest.Release.Manifest.Metadata.Version) > 0 {
			latest = entry
			found = true
		}
	}
	return latest, found
}

func (c *Catalog) Diagnostics() []Diagnostic {
	if c == nil {
		return nil
	}
	return append([]Diagnostic(nil), c.diagnostics...)
}

// IsNewerVersion reports whether candidate is strictly newer than current.
// Both inputs have already passed the Plugin manifest's semantic-version
// validation before reaching the bundled catalog.
func IsNewerVersion(candidate, current string) bool {
	return compareSemver(candidate, current) > 0
}

func compareSemver(left, right string) int {
	parse := func(value string) ([3]string, []string) {
		var numeric [3]string
		withoutBuild, _, _ := strings.Cut(value, "+")
		main, prerelease, _ := strings.Cut(withoutBuild, "-")
		copy(numeric[:], strings.Split(main, "."))
		if prerelease == "" {
			return numeric, nil
		}
		return numeric, strings.Split(prerelease, ".")
	}
	leftNumeric, leftPrerelease := parse(left)
	rightNumeric, rightPrerelease := parse(right)
	for index := range leftNumeric {
		if comparison := compareNumericIdentifier(leftNumeric[index], rightNumeric[index]); comparison != 0 {
			return comparison
		}
	}
	if len(leftPrerelease) == 0 && len(rightPrerelease) == 0 {
		return 0
	}
	if len(leftPrerelease) == 0 {
		return 1
	}
	if len(rightPrerelease) == 0 {
		return -1
	}
	for index := 0; index < len(leftPrerelease) && index < len(rightPrerelease); index++ {
		leftIdentifier := leftPrerelease[index]
		rightIdentifier := rightPrerelease[index]
		leftNumber := isNumericIdentifier(leftIdentifier)
		rightNumber := isNumericIdentifier(rightIdentifier)
		switch {
		case leftNumber && rightNumber:
			if comparison := compareNumericIdentifier(leftIdentifier, rightIdentifier); comparison != 0 {
				return comparison
			}
		case leftNumber:
			return -1
		case rightNumber:
			return 1
		default:
			if comparison := strings.Compare(leftIdentifier, rightIdentifier); comparison != 0 {
				return comparison
			}
		}
	}
	return len(leftPrerelease) - len(rightPrerelease)
}

func compareNumericIdentifier(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) != len(right) {
		return len(left) - len(right)
	}
	return strings.Compare(left, right)
}

func isNumericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// VerifyReproducibleRelease rebuilds a source release and returns the exact
// immutable identities used by release automation and repository tests.
func VerifyReproducibleRelease(source fs.FS, releaseDir, signatureFile string) (plugincontract.Artifact, error) {
	archive, err := buildArchive(source, releaseDir, signatureFile)
	if err != nil {
		return plugincontract.Artifact{}, err
	}
	artifact, err := plugincontract.ValidateArtifact(archive)
	if err != nil {
		return plugincontract.Artifact{}, fmt.Errorf("validate reproducible bundled artifact: %w", err)
	}
	return artifact, nil
}
