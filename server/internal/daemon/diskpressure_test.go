package daemon

import "testing"

func TestDiskFreePercent(t *testing.T) {
	pct := diskFreePercent(t.TempDir())
	if pct == nil || *pct < 0 || *pct > 100 {
		t.Fatalf("diskFreePercent(tmpdir) = %v, want value in [0,100]", pct)
	}
	if pct := diskFreePercent("/definitely/not/a/real/path-xyz"); pct != nil {
		t.Fatalf("diskFreePercent(missing path) = %v, want nil", pct)
	}
}
