package codegraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Parser fixtures exercise structure and file boundaries, not agent acceptance.
func TestDocumentStructureAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		file := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Agent guide\n\n[config](.agents/agent.yaml)\n\n```go\nfunc main() {}\n```\n\n[[README]]\n")
	write(".agents/agent.yaml", "defaults: &base\n  model: actual-model-secret\nagent:\n  config: *base\n  key: ${MODEL_KEY}\n  instructions: ../README.md\n")
	write(".github/workflows/check.yml", "name: Verify\non: push\n")
	write("broken.yaml", "key: [\n")
	write(".env", "PRIVATE=secret\n")
	write(".credentials/leak.yaml", "token: hidden\n")
	if err := os.Symlink(".agents/agent.yaml", filepath.Join(dir, "alias.yaml")); err != nil {
		t.Fatal(err)
	}
	index, err := IndexDocuments(context.Background(), dir, "build", "commit")
	if err != nil {
		t.Fatal(err)
	}
	if index.Coverage.Files != 3 || index.Coverage.Skipped != 1 || !index.Coverage.Incomplete {
		t.Fatalf("coverage: %+v", index.Coverage)
	}
	find := func(kind, name string) DocumentNode {
		t.Helper()
		for _, n := range index.Nodes {
			if n.Kind == kind && n.Name == name {
				return n
			}
		}
		t.Fatalf("missing %s %s", kind, name)
		return DocumentNode{}
	}
	if find("heading", "Agent guide").StartLine != 1 {
		t.Fatal("heading line")
	}
	if find("link", ".agents/agent.yaml").StartLine != 3 {
		t.Fatal("link line")
	}
	if find("code_block", "go").StartLine != 6 {
		t.Fatal("code body line")
	}
	if find("wiki_link", "README.md").StartLine != 9 {
		t.Fatal("wiki line")
	}
	if find("yaml_key", "$1.agent.key").StartLine != 5 {
		t.Fatal("yaml line")
	}
	alias, environment, reference := false, false, false
	for _, edge := range index.Edges {
		if edge.Kind == "alias" && edge.Resolved {
			alias = true
		}
		if edge.Kind == "environment" && edge.Reference == "MODEL_KEY" {
			environment = true
		}
		if edge.Kind == "references" && edge.Reference == ".agents/agent.yaml" && edge.Resolved {
			reference = true
		}
	}
	if !alias || !environment || !reference {
		t.Fatalf("missing relationships: %v %v %v", alias, environment, reference)
	}
	encoded, _ := json.Marshal(index)
	if strings.Contains(string(encoded), "actual-model-secret") || strings.Contains(string(encoded), "leak.yaml") || strings.Contains(string(encoded), "alias.yaml") {
		t.Fatal("private data or scalar leaked")
	}
	file := filepath.Join(t.TempDir(), "documents.json")
	if err = index.Save(file); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadDocuments(file, "build", "commit"); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadDocuments(file, "other", "commit"); err == nil {
		t.Fatal("mixed snapshot accepted")
	}
	if _, err = index.References("missing"); err == nil {
		t.Fatal("missing node accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = IndexDocuments(ctx, dir, "b", "c"); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestSnapshotPolicy(t *testing.T) {
	for _, file := range []string{".agents/agent.yaml", ".github/workflows/build.yml", "docs/readme.md"} {
		if PrivatePath(file, false) {
			t.Fatalf("blocked %s", file)
		}
	}
	for _, file := range []string{".agents", ".env", ".git/config", "../outside", "a/../outside", "node_modules/test.md", "foo.pem", "id_rsa", ".credentials/file.yaml", "a\\b.yaml"} {
		if !PrivatePath(file, false) {
			t.Fatalf("allowed %s", file)
		}
	}
	for _, ref := range []string{"https://host/a.md?token=private", "../../secret.md", "/.env", "../.env", "foo.md?key=secret"} {
		if localDocumentReference("README.md", ref) != "" {
			t.Fatalf("unsafe reference %s", ref)
		}
	}
}
