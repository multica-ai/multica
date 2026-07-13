package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillBundleCacheLoadStore(t *testing.T) {
	cache := NewSkillBundleCache(t.TempDir())
	bundle := testSkillBundle()
	ref := skillRefFromBundle(bundle)

	if err := cache.Store("ws-1", bundle); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(cache.bundleDir("ws-1", ref), "SKILL.md")); err != nil {
		t.Fatalf("bundle dir missing SKILL.md: %v", err)
	} else if string(data) != "---\nname: deploy\n---\n\nmain" {
		t.Fatalf("bundle dir SKILL.md = %q, want normalized frontmatter", data)
	}
	if data, err := os.ReadFile(filepath.Join(cache.bundleDir("ws-1", ref), "rules.md")); err != nil {
		t.Fatalf("bundle dir missing supporting file: %v", err)
	} else if string(data) != "rules" {
		t.Fatalf("bundle dir rules.md = %q, want %q", data, "rules")
	}
	got, ok := cache.Load("ws-1", ref)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Content != bundle.Content || len(got.Files) != 1 || got.Files[0].Content != "rules" {
		t.Fatalf("unexpected bundle: %+v", got)
	}
}

func TestSkillBundleCacheRejectsCorruptBundle(t *testing.T) {
	cache := NewSkillBundleCache(t.TempDir())
	bundle := testSkillBundle()
	ref := skillRefFromBundle(bundle)
	if err := cache.Store("ws-1", bundle); err != nil {
		t.Fatalf("Store: %v", err)
	}

	path := cache.bundlePath("ws-1", ref)
	if err := os.WriteFile(path, []byte(`{"id":"skill-1","source":"workspace","hash":"sha256:bad","content":"tampered"}`), 0o644); err != nil {
		t.Fatalf("tamper cache: %v", err)
	}
	if _, ok := cache.Load("ws-1", ref); ok {
		t.Fatal("expected corrupt cache miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected corrupt cache file to be removed, stat err=%v", err)
	}
}

func TestSkillBundleCacheMigratesLegacyLayout(t *testing.T) {
	cache := NewSkillBundleCache(filepath.Join(t.TempDir(), "v2"))
	bundle := testSkillBundle()
	ref := skillRefFromBundle(bundle)

	legacyDir := bundleDirForRoot(cache.legacyRoot, "ws-1", ref)
	if err := os.MkdirAll(filepath.Join(legacyDir, "nested"), 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, ".skill-bundle.json"), raw, 0o644); err != nil {
		t.Fatalf("seed legacy meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), []byte("main"), 0o644); err != nil {
		t.Fatalf("seed legacy SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "rules.md"), []byte("rules"), 0o644); err != nil {
		t.Fatalf("seed legacy support file: %v", err)
	}

	got, ok := cache.Load("ws-1", ref)
	if !ok {
		t.Fatal("expected cache hit from legacy layout")
	}
	if got.Content != bundle.Content {
		t.Fatalf("loaded legacy bundle = %+v, want %+v", got, bundle)
	}
	if _, err := os.Stat(filepath.Join(cache.bundleDir("ws-1", ref), "SKILL.md")); err != nil {
		t.Fatalf("migrated bundle missing SKILL.md: %v", err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("expected legacy bundle to be reclaimed after successful migration, stat err=%v", err)
	}
}

func TestSkillBundleCacheKeepsLegacyLayoutWhenMigrationWriteFails(t *testing.T) {
	base := t.TempDir()
	v2Root := filepath.Join(base, "v2")
	if err := os.WriteFile(v2Root, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed blocking v2 root: %v", err)
	}
	cache := NewSkillBundleCache(v2Root)
	bundle := testSkillBundle()
	ref := skillRefFromBundle(bundle)

	legacyDir := bundleDirForRoot(cache.legacyRoot, "ws-1", ref)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, ".skill-bundle.json"), raw, 0o644); err != nil {
		t.Fatalf("seed legacy meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "SKILL.md"), []byte("main"), 0o644); err != nil {
		t.Fatalf("seed legacy SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "rules.md"), []byte("rules"), 0o644); err != nil {
		t.Fatalf("seed legacy support file: %v", err)
	}

	got, ok := cache.Load("ws-1", ref)
	if !ok {
		t.Fatal("expected cache hit from legacy layout")
	}
	if got.Content != bundle.Content {
		t.Fatalf("loaded legacy bundle = %+v, want %+v", got, bundle)
	}
	if _, err := os.Stat(filepath.Join(cache.bundleDir("ws-1", ref), "SKILL.md")); err == nil {
		t.Fatal("expected v2 migration write to fail, but bundle dir exists")
	}
	if _, err := os.Stat(legacyDir); err != nil {
		t.Fatalf("expected legacy bundle to remain after failed migration, stat err=%v", err)
	}
}

func TestConvertSkillsForEnvSkipsMissingCacheDir(t *testing.T) {
	cache := NewSkillBundleCache(filepath.Join(t.TempDir(), "v2"))
	skills := []SkillData{testSkillBundle()}

	got := convertSkillsForEnv("ws-1", skills, cache)
	if len(got) != 1 {
		t.Fatalf("convertSkillsForEnv returned %d skills, want 1", len(got))
	}
	if got[0].CacheDir != "" {
		t.Fatalf("CacheDir = %q, want empty when bundle dir is missing", got[0].CacheDir)
	}
}

func testSkillBundle() SkillData {
	bundle := SkillData{
		ID:      "skill-1",
		Source:  "workspace",
		Name:    "deploy",
		Content: "main",
		Files:   []SkillFileData{{Path: "rules.md", Content: "rules"}},
	}
	ref := skillRefFromBundle(bundle)
	bundle.Hash = ref.Hash
	bundle.SizeBytes = ref.SizeBytes
	bundle.Files[0].SHA256 = ref.Files[0].SHA256
	bundle.Files[0].SizeBytes = ref.Files[0].SizeBytes
	return bundle
}
