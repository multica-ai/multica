package costmeasure

import "testing"

func TestSnapshotTokensEstimate_UsesTypicalWhenNoBody(t *testing.T) {
	baseline, effective := SnapshotTokensEstimate(0)
	if baseline != SnapshotTypicalTokens() || effective != 0 {
		t.Fatalf("got baseline=%d effective=%d, want %d/0", baseline, effective, SnapshotTypicalTokens())
	}
}

func TestBundledTokensEstimate_BaselineExceedsEffective(t *testing.T) {
	baseline, effective := BundledTokensEstimate()
	if baseline <= effective {
		t.Fatalf("expected baseline > effective, got %d/%d", baseline, effective)
	}
}
