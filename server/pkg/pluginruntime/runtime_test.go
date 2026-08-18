package pluginruntime

import "testing"

func TestSnapshotDigestCanonicalizesEntryOrder(t *testing.T) {
	left := []CompiledEntry{
		{PluginKey: "z.plugin", ContributionKey: "b", Ordinal: 1, RequiredDaemonFeatures: []string{"b", "a"}},
		{PluginKey: "a.plugin", ContributionKey: "a", Ordinal: 0},
	}
	right := []CompiledEntry{left[1], left[0]}
	first, err := SnapshotDigest(map[string]int64{"b": 2, "a": 1}, left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SnapshotDigest(map[string]int64{"a": 1, "b": 2}, right)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("snapshot digest is not canonical: %s != %s", first, second)
	}
}

func TestSnapshotDigestCanonicalizesSkillFileOrder(t *testing.T) {
	left := []CompiledEntry{{
		PluginKey:       "example.plugin",
		ContributionKey: "community-skill",
		SkillFiles: []SkillFile{
			{ArtifactFileID: "file-b", Path: "references/b.md", Digest: "sha256:b", SizeBytes: 2},
			{ArtifactFileID: "file-a", Path: "agents/openai.yaml", Digest: "sha256:a", SizeBytes: 1},
		},
	}}
	right := []CompiledEntry{{
		PluginKey:       "example.plugin",
		ContributionKey: "community-skill",
		SkillFiles: []SkillFile{
			{ArtifactFileID: "file-a", Path: "agents/openai.yaml", Digest: "sha256:a", SizeBytes: 1},
			{ArtifactFileID: "file-b", Path: "references/b.md", Digest: "sha256:b", SizeBytes: 2},
		},
	}}
	first, err := SnapshotDigest(map[string]int64{"installation": 1}, left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SnapshotDigest(map[string]int64{"installation": 1}, right)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("snapshot digest depends on Skill file order: %s != %s", first, second)
	}
}
