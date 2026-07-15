package apps

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductionMiniAppsContainNoWorkflowFakes(t *testing.T) {
	root := "."
	forbidden := regexp.MustCompile(`(?i)\b(workflowTest\w*|fake\w*)\b`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if forbidden.Match(raw) {
			t.Errorf("production fake found in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
