package plugincontract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExamplePluginsSatisfyArtifactContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "examples", "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read example plugins: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(source, ManifestFilename)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("inspect %s manifest: %v", entry.Name(), err)
		}
		t.Run(entry.Name(), func(t *testing.T) {
			if _, _, err := PackDirectory(source); err != nil {
				t.Fatalf("pack example plugin: %v", err)
			}
		})
	}
}
