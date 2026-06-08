package skillbundle

import "testing"

func TestShouldSkipDir(t *testing.T) {
	for _, name := range []string{
		".git",
		".venv",
		"__pycache__",
		"node_modules",
		"dist",
		"coverage",
	} {
		if !ShouldSkipDir(name) {
			t.Fatalf("ShouldSkipDir(%q) = false, want true", name)
		}
	}

	for _, name := range []string{"references", "scripts", "examples"} {
		if ShouldSkipDir(name) {
			t.Fatalf("ShouldSkipDir(%q) = true, want false", name)
		}
	}
}

func TestShouldSkipFile(t *testing.T) {
	for _, path := range []string{
		"SKILL.md",
		"LICENSE",
		".DS_Store",
		"scripts/__pycache__/tool.cpython-312.pyc",
		"node_modules/pkg/index.js",
		"assets/logo.png",
		"data/cache.sqlite",
	} {
		if !ShouldSkipFile(path) {
			t.Fatalf("ShouldSkipFile(%q) = false, want true", path)
		}
	}

	for _, path := range []string{
		"references/guide.md",
		"scripts/tool.py",
		"agents/openai.yaml",
		"examples/sample.json",
	} {
		if ShouldSkipFile(path) {
			t.Fatalf("ShouldSkipFile(%q) = true, want false", path)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	if got, ok := NormalizePath("scripts/../references/guide.md"); !ok || got != "references/guide.md" {
		t.Fatalf("NormalizePath = %q, %v; want references/guide.md, true", got, ok)
	}
	if got, ok := NormalizePath(`scripts\tool.py`); !ok || got != "scripts/tool.py" {
		t.Fatalf("NormalizePath backslash = %q, %v; want scripts/tool.py, true", got, ok)
	}
	if got, ok := NormalizePath("notes/..foo.md"); !ok || got != "notes/..foo.md" {
		t.Fatalf("NormalizePath dot-prefixed basename = %q, %v; want notes/..foo.md, true", got, ok)
	}

	for _, path := range []string{"", "../secret.md", "..", "/tmp/secret.md", `..\secret.md`} {
		if got, ok := NormalizePath(path); ok {
			t.Fatalf("NormalizePath(%q) = %q, true; want false", path, got)
		}
	}
}
