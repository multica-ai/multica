package toolpolicy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredCredentialPolicyHasNoQueryReader(t *testing.T) {
	serverRoot := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(serverRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "migrations" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "retired_credential_policy_source_test.go" ||
			(filepath.Ext(path) != ".go" && filepath.Ext(path) != ".sql") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(strings.ToLower(string(raw)), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if strings.Contains(trimmed, "cerebro_credential_policy") {
				t.Errorf("retired credential-policy reader reintroduced in %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
