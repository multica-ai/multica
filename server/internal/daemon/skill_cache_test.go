package daemon

import (
	"os"
	"testing"
)

func TestSkillBundleCacheLoadStore(t *testing.T) {
	cache := NewSkillBundleCache(t.TempDir())
	bundle := testSkillBundle()
	ref := skillRefFromBundle(bundle)

	if err := cache.Store("ws-1", bundle); err != nil {
		t.Fatalf("Store: %v", err)
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
