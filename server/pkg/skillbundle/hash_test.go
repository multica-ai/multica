package skillbundle

import "testing"

func TestBuildManifestStableAcrossFileOrder(t *testing.T) {
	a := BuildManifest(Skill{
		ID:      "skill-1",
		Source:  SourceWorkspace,
		Name:    "deploy",
		Content: "main",
		Files: []File{
			{Path: "b.md", Content: "b"},
			{Path: "a.md", Content: "a"},
		},
	})
	b := BuildManifest(Skill{
		ID:      "skill-1",
		Source:  SourceWorkspace,
		Name:    "deploy",
		Content: "main",
		Files: []File{
			{Path: "a.md", Content: "a"},
			{Path: "b.md", Content: "b"},
		},
	})
	if a.Hash != b.Hash {
		t.Fatalf("hash depends on file order: %s != %s", a.Hash, b.Hash)
	}
}

func TestBuildManifestChangesWhenContentChanges(t *testing.T) {
	a := BuildManifest(Skill{ID: "skill-1", Source: SourceWorkspace, Name: "deploy", Content: "main"})
	b := BuildManifest(Skill{ID: "skill-1", Source: SourceWorkspace, Name: "deploy", Content: "changed"})
	if a.Hash == b.Hash {
		t.Fatal("hash did not change when content changed")
	}
}
