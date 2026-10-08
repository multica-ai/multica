package plugincontract_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The shipped examples are documentation, and documentation that does not parse
// is worse than none: an author copies it, hits a validation error the docs said
// nothing about, and concludes the contract is undocumented.
//
// This also catches the failure mode a capability flip introduces. An example
// declaring something the host has not shipped yet would install nowhere, and
// nothing else in the build would say so — the manifest is valid, it is the
// gate that refuses it.
func TestShippedExamplesParseAndInstallOnThisHost(t *testing.T) {
	root := filepath.Join("..", "..", "..", "examples", "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read examples directory: %v", err)
	}

	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "multica.plugin.json")
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found++

		t.Run(entry.Name(), func(t *testing.T) {
			manifest, _, err := plugincontract.ParseManifest(raw)
			if err != nil {
				t.Fatalf("example manifest does not parse: %v", err)
			}
			if err := manifest.CheckCapabilities(plugincontract.HostCapabilities()); err != nil {
				t.Fatalf("example declares something this host cannot run, so it could not be installed: %v", err)
			}
			// A surface entry that is not in the example's own directory is a
			// broken example: the reader copies it and gets a blank panel.
			for _, surface := range manifest.Contributes.Surfaces {
				entryPath := filepath.Join(root, entry.Name(), surface.Entry)
				if _, err := os.Stat(entryPath); err != nil {
					t.Fatalf("surface %q points at %s, which is not in the example: %v", surface.Key, surface.Entry, err)
				}
			}
		})
	}

	if found == 0 {
		t.Fatal("no example manifests were found; this test would pass vacuously")
	}
}
