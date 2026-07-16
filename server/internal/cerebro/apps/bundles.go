package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxBundleFiles     = 100
	maxBundleFileBytes = 512 << 10
	maxBundleBytes     = 5 << 20
)

type BundleFile struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type,omitempty"`
	Content   []byte `json:"content_base64"`
	SHA256    string `json:"sha256"`
	Symlink   bool   `json:"-"`
}

type bundleExec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func StoreVersionBundle(ctx context.Context, tx bundleExec, appID uuid.UUID, version string, bundle ValidatedBundle) error {
	for _, file := range bundle.Files {
		if _, err := tx.Exec(ctx, `INSERT INTO cerebro_app_version_file (app_id,version,path,media_type,content,sha256,size_bytes) VALUES ($1,$2,$3,$4,$5,$6,$7)`, appID, version, file.Path, file.MediaType, file.Content, file.SHA256, len(file.Content)); err != nil {
			return fmt.Errorf("store app bundle file: %w", err)
		}
	}
	return nil
}

type ValidatedBundle struct {
	Files  []BundleFile
	SHA256 string
}

type bundleManifest struct {
	Manifest struct {
		SchemaVersion string `json:"schema_version"`
		Name          string `json:"name"`
		Version       string `json:"version"`
		Frontend      *struct {
			Entry string `json:"entry"`
		} `json:"frontend"`
		Backend *struct {
			Entry string `json:"entry"`
		} `json:"backend"`
	} `json:"manifest"`
}

func ValidateBundle(appName, version string, files []BundleFile) (ValidatedBundle, error) {
	if len(files) == 0 {
		return ValidatedBundle{}, errors.New("bundle requires app.json")
	}
	if len(files) > maxBundleFiles {
		return ValidatedBundle{}, fmt.Errorf("bundle exceeds %d files", maxBundleFiles)
	}

	validated := make([]BundleFile, len(files))
	byPath := make(map[string]BundleFile, len(files))
	total := 0
	for i, file := range files {
		if err := validateBundlePath(file.Path); err != nil {
			return ValidatedBundle{}, err
		}
		if file.Symlink {
			return ValidatedBundle{}, fmt.Errorf("bundle file %q cannot be a symlink", file.Path)
		}
		if _, exists := byPath[file.Path]; exists {
			return ValidatedBundle{}, fmt.Errorf("bundle contains duplicate path %q", file.Path)
		}
		if len(file.Content) > maxBundleFileBytes {
			return ValidatedBundle{}, fmt.Errorf("bundle file %q exceeds %d bytes", file.Path, maxBundleFileBytes)
		}
		total += len(file.Content)
		if total > maxBundleBytes {
			return ValidatedBundle{}, fmt.Errorf("bundle exceeds %d bytes", maxBundleBytes)
		}
		sum := sha256.Sum256(file.Content)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.SHA256) {
			return ValidatedBundle{}, fmt.Errorf("bundle file %q has an invalid sha256", file.Path)
		}
		file.SHA256 = actual
		if file.MediaType == "" {
			file.MediaType = "application/octet-stream"
		}
		validated[i] = file
		byPath[file.Path] = file
	}

	manifestFile, ok := byPath["app.json"]
	if !ok {
		return ValidatedBundle{}, errors.New("bundle requires app.json")
	}
	var document bundleManifest
	if err := json.Unmarshal(manifestFile.Content, &document); err != nil {
		return ValidatedBundle{}, errors.New("app.json is not valid JSON")
	}
	manifest := document.Manifest
	if manifest.SchemaVersion != "1" || manifest.Name != appName || manifest.Version != version {
		return ValidatedBundle{}, errors.New("app.json manifest identity does not match the app version")
	}
	if manifest.Frontend == nil || manifest.Frontend.Entry == "" {
		return ValidatedBundle{}, errors.New("app.json requires a frontend entrypoint")
	}
	if _, ok := byPath[manifest.Frontend.Entry]; !ok {
		return ValidatedBundle{}, errors.New("frontend entrypoint is missing from the bundle")
	}
	if manifest.Backend != nil {
		if manifest.Backend.Entry == "" {
			return ValidatedBundle{}, errors.New("backend entrypoint cannot be empty")
		}
		if _, ok := byPath[manifest.Backend.Entry]; !ok {
			return ValidatedBundle{}, errors.New("backend entrypoint is missing from the bundle")
		}
	}

	sort.Slice(validated, func(i, j int) bool { return validated[i].Path < validated[j].Path })
	bundleHash := sha256.New()
	for _, file := range validated {
		bundleHash.Write([]byte(file.Path))
		bundleHash.Write([]byte{0})
		bundleHash.Write([]byte(file.SHA256))
		bundleHash.Write([]byte{0})
	}
	return ValidatedBundle{Files: validated, SHA256: hex.EncodeToString(bundleHash.Sum(nil))}, nil
}

func validateBundlePath(filePath string) error {
	if filePath == "" || strings.HasPrefix(filePath, "/") || strings.Contains(filePath, "\\") || strings.Contains(filePath, "//") || path.Clean(filePath) != filePath {
		return fmt.Errorf("bundle path %q is unsafe", filePath)
	}
	for _, segment := range strings.Split(filePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("bundle path %q is unsafe", filePath)
		}
	}
	if filePath != "app.json" && !strings.HasPrefix(filePath, "frontend/") && !strings.HasPrefix(filePath, "backend/") {
		return fmt.Errorf("bundle path %q is outside the app package", filePath)
	}
	return nil
}
