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
