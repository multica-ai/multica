package plugincontract_test

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const bundleManifest = `{
  "manifest_version": 1,
  "key": "com.example.hello",
  "name": "Hello Panel",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "surfaces": [{ "key": "hello", "type": "issue_panel", "name": "Hello", "entry": "ui/main.js" }],
    "resources": [{ "type": "skill", "key": "pr-review", "entry": "skills/pr-review/SKILL.md" }]
  }
}`

func zipBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func TestParseBundleKeepsOnlyDeclaredFiles(t *testing.T) {
	archive := zipBundle(t, map[string]string{
		plugincontract.ManifestFilename: bundleManifest,
		"ui/main.js":                    "console.log('hi');\n",
		"skills/pr-review/SKILL.md":     "---\nname: pr-review\n---\n\nRead the diff.\n",
		// Never referenced by the manifest, so nothing could ever serve it.
		"README.md": "# hello\n",
	})

	bundle, err := plugincontract.ParseBundle(archive)
	if err != nil {
		t.Fatalf("ParseBundle: %v", err)
	}
	if bundle.Manifest.Key != "com.example.hello" || len(bundle.Canonical) == 0 {
		t.Fatalf("unexpected manifest: %+v", bundle.Manifest)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("files = %d, want the two the manifest declares: %+v", len(bundle.Files), bundle.Files)
	}
	if _, ok := bundle.File("README.md"); ok {
		t.Fatal("an undeclared file was stored; nothing can ever serve it")
	}
	if content, ok := bundle.File("ui/main.js"); !ok || !strings.Contains(string(content), "console.log") {
		t.Fatalf("surface entry was not stored: %q", content)
	}
}

// Zipping a folder is what a person does, and it produces
// `my-plugin/multica.plugin.json`. One leading directory is accepted; the
// manifest's own entries stay relative to it.
func TestParseBundleAcceptsOneWrappingDirectory(t *testing.T) {
	archive := zipBundle(t, map[string]string{
		"hello-panel/" + plugincontract.ManifestFilename: bundleManifest,
		"hello-panel/ui/main.js":                         "console.log('hi');\n",
		"hello-panel/skills/pr-review/SKILL.md":          "Read the diff.\n",
	})

	bundle, err := plugincontract.ParseBundle(archive)
	if err != nil {
		t.Fatalf("ParseBundle: %v", err)
	}
	if _, ok := bundle.File("ui/main.js"); !ok {
		t.Fatalf("entries must stay relative to the manifest: %+v", bundle.Files)
	}
}

func TestParseBundleRejections(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "no manifest",
			files: map[string]string{"ui/main.js": "console.log('hi');"},
			want:  plugincontract.ManifestFilename,
		},
		{
			name: "declared surface entry is missing",
			files: map[string]string{
				plugincontract.ManifestFilename: bundleManifest,
				"skills/pr-review/SKILL.md":     "Read the diff.\n",
			},
			want: "ui/main.js",
		},
		{
			name: "declared skill is missing",
			files: map[string]string{
				plugincontract.ManifestFilename: bundleManifest,
				"ui/main.js":                    "console.log('hi');",
			},
			want: "skills/pr-review/SKILL.md",
		},
		{
			name: "surface entry is empty",
			files: map[string]string{
				plugincontract.ManifestFilename: bundleManifest,
				"ui/main.js":                    "   \n",
				"skills/pr-review/SKILL.md":     "Read the diff.\n",
			},
			want: "empty",
		},
		{
			// The failure the repository's own deploy-sentinel example shipped
			// with: an entry that cannot load, caught at publish instead of in a
			// reader's browser.
			name: "surface entry has a top-level import",
			files: map[string]string{
				plugincontract.ManifestFilename: bundleManifest,
				"ui/main.js":                    "import { multica } from \"https://esm.sh/@multica/plugin-sdk@1\";\n",
				"skills/pr-review/SKILL.md":     "Read the diff.\n",
			},
			want: "top-level import",
		},
		{
			name: "surface entry is not valid JavaScript",
			files: map[string]string{
				plugincontract.ManifestFilename: bundleManifest,
				"ui/main.js":                    "const = 1;\n",
				"skills/pr-review/SKILL.md":     "Read the diff.\n",
			},
			want: "not valid JavaScript",
		},
		{
			name: "two manifests",
			files: map[string]string{
				"a/" + plugincontract.ManifestFilename: bundleManifest,
				"b/" + plugincontract.ManifestFilename: bundleManifest,
			},
			want: "more than one",
		},
		{
			name: "manifest is buried too deep",
			files: map[string]string{
				"a/b/" + plugincontract.ManifestFilename: bundleManifest,
			},
			want: "root of the package",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := plugincontract.ParseBundle(zipBundle(t, tc.files))
			if err == nil {
				t.Fatal("ParseBundle accepted an invalid package")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q; a publish failure the author cannot act on is a bug", err, tc.want)
			}
		})
	}
}

func TestParseBundleRejectsTraversingEntries(t *testing.T) {
	for _, name := range []string{"../escape.js", "/absolute.js", "ui/../../escape.js"} {
		archive := zipBundle(t, map[string]string{
			plugincontract.ManifestFilename: bundleManifest,
			name:                            "console.log('hi');",
		})
		if _, err := plugincontract.ParseBundle(archive); err == nil {
			t.Fatalf("ParseBundle accepted entry %q", name)
		}
	}
}

func TestParseBundleRejectsOversizedInput(t *testing.T) {
	if _, err := plugincontract.ParseBundle(nil); err == nil {
		t.Fatal("ParseBundle accepted an empty package")
	}
	if _, err := plugincontract.ParseBundle(bytes.Repeat([]byte{0}, plugincontract.MaxBundleSize+1)); err == nil {
		t.Fatal("ParseBundle accepted a package over the size limit")
	}
	// Compresses to almost nothing, so the archive is small while the file it
	// carries is not. The limit has to be enforced on what is read out.
	archive := zipBundle(t, map[string]string{
		plugincontract.ManifestFilename: bundleManifest,
		"ui/main.js":                    strings.Repeat("a", plugincontract.MaxBundleFileSize+1),
		"skills/pr-review/SKILL.md":     "Read the diff.\n",
	})
	if _, err := plugincontract.ParseBundle(archive); err == nil {
		t.Fatal("ParseBundle accepted a file that expands past the per-file limit")
	}
}

// The local development channel and an upload must be validated to the same
// standard, or "is this code frozen?" would depend on how it was published.
func TestParseBundleFromDirMatchesTheUploadPath(t *testing.T) {
	files := map[string]string{
		plugincontract.ManifestFilename: bundleManifest,
		"ui/main.js":                    "console.log('hi');\n",
		"skills/pr-review/SKILL.md":     "Read the diff.\n",
	}
	read := func(entry string) ([]byte, bool, error) {
		content, ok := files[entry]
		return []byte(content), ok, nil
	}
	bundle, err := plugincontract.ParseBundleFromDir(read)
	if err != nil {
		t.Fatalf("ParseBundleFromDir: %v", err)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("files = %+v", bundle.Files)
	}

	delete(files, "ui/main.js")
	if _, err := plugincontract.ParseBundleFromDir(read); err == nil {
		t.Fatal("a missing surface entry must fail the local publish too")
	}
}

// The publish-time module check refuses a publish, so it is asymmetric: missing
// an import costs the author a readable runtime error, while a false positive
// blocks a legitimate publish outright. This pins both directions.
func TestSurfaceEntryModuleDetection(t *testing.T) {
	refused := map[string]string{
		"bare import":                    "import { multica } from \"./sdk.js\";\n",
		"indented import":                "  import { multica } from \"./sdk.js\";\n",
		"import after a line comment":    "// set up\nimport { multica } from \"./sdk.js\";\n",
		"import after a block comment":   "/* set up */ import { multica } from \"./sdk.js\";\n",
		"namespace import":               "import * as sdk from \"./sdk.js\";\n",
		"named export":                   "export { render };\n",
		"import after another statement": "const x = 1; import y from \"./y.js\";\n",
		"export after another statement": "const x = 1; export { x };\n",
		"import inside a function":       "function boot() {\n  import { x } from \"./x.js\";\n}\n",
		"top-level await":                "await 1;\n",
		"import meta":                    "console.log(import.meta.url);\n",
	}
	for name, source := range refused {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDirBundle(t, source); err == nil {
				t.Fatal("a module-only entry was published; it cannot load as a classic script")
			}
		})
	}

	accepted := map[string]string{
		// The case the line-prefix version got wrong: a surface that renders a
		// code sample is ordinary, and refusing it leaves the author stuck.
		"import inside a template literal": "const help = `\nimport { multica } from \"@multica/plugin-sdk\";\n`;\nconsole.log(help);\n",
		"import inside a string":           "const help = \"import x from 'y'\";\nconsole.log(help);\n",
		"import inside a comment":          "// import { x } from \"./x.js\";\nconsole.log(1);\n",
		// Dynamic import is legal in a classic script.
		"dynamic import":              "const load = () => import(\"./late.js\");\nload();\n",
		"spaced dynamic import":       "const load = () => import (\"./late.js\");\nload();\n",
		"commented dynamic import":    "const load = () => import /* webpackIgnore */ (\"https://example.com/late.js\");\nload();\n",
		"regexp containing import":    "const matcher = /import y from ['\"]x['\"]/;\nconsole.log(matcher);\n",
		"a property named import":     "const registry = {};\nregistry.import = 1;\n",
		"a word starting with export": "const exports = {};\nexports.value = 1;\n",
		"nested template expression":  "const x = `a${ `b` }c`;\nconsole.log(x);\n",
		"await in an async function":  "async function boot() { await Promise.resolve(); }\nboot();\n",
		"await in an async arrow":     "const boot = async () => { await Promise.resolve(); };\nboot();\n",
	}
	for name, source := range accepted {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDirBundle(t, source); err != nil {
				t.Fatalf("a valid surface entry was refused: %v", err)
			}
		})
	}
}

func parseDirBundle(t *testing.T, entry string) (plugincontract.Bundle, error) {
	t.Helper()
	files := map[string]string{
		plugincontract.ManifestFilename: bundleManifest,
		"ui/main.js":                    entry,
		"skills/pr-review/SKILL.md":     "Read the diff.\n",
	}
	return plugincontract.ParseBundleFromDir(func(name string) ([]byte, bool, error) {
		content, ok := files[name]
		return []byte(content), ok, nil
	})
}
