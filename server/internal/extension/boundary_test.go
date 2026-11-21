package extension

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const eePath = "github.com/multica-ai/multica/server/ee"

// The open-source server must build and run without server/ee, so the only
// file outside it that may import it is the build-tagged link in cmd/server.
func TestOpenSourceCodeDoesNotImportEE(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "cmd", "server", "ee.go")
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join(root, "ee") || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || path == allowed {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == eePath || strings.HasPrefix(p, eePath+"/") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports %s; open-source code reaches ee only through package extension", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
