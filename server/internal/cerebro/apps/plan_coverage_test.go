package apps

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMiniAppDocumentationAndSDKSurface(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	for _, name := range []string{"README.md", "sdk.md", "manifest.md", "workflows.md", "when-to-build-a-mini-app.md", "plan-coverage.md"} {
		if _, err := os.Stat(filepath.Join(root, "docs", "mini-apps", name)); err != nil {
			t.Errorf("missing docs/mini-apps/%s", name)
		}
	}
	sdk, _ := os.ReadFile(filepath.Join(root, "docs", "mini-apps", "sdk.md"))
	for _, method := range []string{"registry.token", "storage.get", "storage.set", "storage.delete", "connections.call", "workers.invoke", "views.submit", "views.onInput"} {
		if !strings.Contains(string(sdk), method) {
			t.Errorf("sdk.md does not document %s", method)
		}
	}
}

func TestMiniAppPlanCoverageReferencesExist(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "docs", "mini-apps", "plan-coverage.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| ([0-9]+) \\|.*`([^#`]+)#([^`]+)` \\|$").FindAllStringSubmatch(string(raw), -1)
	if len(rows) != 12 {
		t.Fatalf("plan coverage has %d packages, want 12", len(rows))
	}
	for index, row := range rows {
		if row[1] != string(rune('0'+index)) && !(index >= 10 && row[1] == []string{"10", "11"}[index-10]) {
			t.Errorf("package row %d is numbered %s", index, row[1])
		}
		path := filepath.Join(root, filepath.FromSlash(row[2]))
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("package %s references missing test file %s", row[1], row[2])
			continue
		}
		if !strings.Contains(string(source), row[3]) {
			t.Errorf("package %s references missing test %q", row[1], row[3])
		}
	}
}
