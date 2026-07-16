package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func bundleTestFile(path, content string) BundleFile {
	sum := sha256.Sum256([]byte(content))
	return BundleFile{
		Path:      path,
		MediaType: "application/octet-stream",
		Content:   []byte(content),
		SHA256:    hex.EncodeToString(sum[:]),
	}
}

func validBundleFiles() []BundleFile {
	return []BundleFile{
		bundleTestFile("app.json", `{"manifest":{"schema_version":"1","name":"Allergen Formatter","version":"1.0.0","scopes":[],"frontend":{"entry":"frontend/index.html"},"backend":{"entry":"backend/index.mjs"}}}`),
		bundleTestFile("frontend/index.html", "<!doctype html><title>Allergen Formatter</title>"),
		bundleTestFile("backend/index.mjs", "export default async input => input"),
	}
}

func TestAppBundleAcceptsValidImmutablePackage(t *testing.T) {
	bundle, err := ValidateBundle("Allergen Formatter", "1.0.0", validBundleFiles())
	if err != nil {
		t.Fatalf("validate bundle: %v", err)
	}
	if len(bundle.Files) != 3 || len(bundle.SHA256) != 64 {
		t.Fatalf("unexpected validated bundle: %#v", bundle)
	}
}

func TestAppBundleRejectsUnsafeOrInconsistentFiles(t *testing.T) {
	tests := map[string]func([]BundleFile) []BundleFile{
		"missing manifest":   func(files []BundleFile) []BundleFile { return files[1:] },
		"missing entrypoint": func(files []BundleFile) []BundleFile { return files[:2] },
		"traversal": func(files []BundleFile) []BundleFile {
			return append(files, bundleTestFile("../secret", "no"))
		},
		"backslash": func(files []BundleFile) []BundleFile {
			return append(files, bundleTestFile(`frontend\\secret`, "no"))
		},
		"symlink": func(files []BundleFile) []BundleFile {
			file := bundleTestFile("frontend/link", "target")
			file.Symlink = true
			return append(files, file)
		},
		"duplicate": func(files []BundleFile) []BundleFile { return append(files, files[1]) },
		"wrong hash": func(files []BundleFile) []BundleFile {
			files[1].SHA256 = strings.Repeat("0", 64)
			return files
		},
		"file too large": func(files []BundleFile) []BundleFile {
			return append(files, bundleTestFile("frontend/large.bin", strings.Repeat("x", maxBundleFileBytes+1)))
		},
		"too many files": func(files []BundleFile) []BundleFile {
			for i := len(files); i <= maxBundleFiles; i++ {
				files = append(files, bundleTestFile(fmt.Sprintf("frontend/%03d.txt", i), "x"))
			}
			return files
		},
		"total too large": func(files []BundleFile) []BundleFile {
			chunk := strings.Repeat("x", maxBundleFileBytes)
			for i := 0; i < 11; i++ {
				files = append(files, bundleTestFile(fmt.Sprintf("frontend/chunk-%02d.bin", i), chunk))
			}
			return files
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			files := append([]BundleFile(nil), validBundleFiles()...)
			if _, err := ValidateBundle("Allergen Formatter", "1.0.0", mutate(files)); err == nil {
				t.Fatal("unsafe bundle was accepted")
			}
		})
	}
}

func TestAppBundleRejectsManifestIdentityMismatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "Different App", version: "1.0.0"},
		{name: "Allergen Formatter", version: "2.0.0"},
	} {
		if _, err := ValidateBundle(tc.name, tc.version, validBundleFiles()); err == nil {
			t.Fatalf("manifest mismatch for name=%q version=%q was accepted", tc.name, tc.version)
		}
	}
}
