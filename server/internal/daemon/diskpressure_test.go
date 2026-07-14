package daemon

import (
	"context"
	"testing"
	"time"
)

func TestGuardDiskPressureFailsClosedWhenSpaceRemainsLow(t *testing.T) {
	orig := freeDiskBytesFunc
	freeDiskBytesFunc = func(string) (int64, int64, error) {
		return 10, 100, nil
	}
	defer func() { freeDiskBytesFunc = orig }()

	d := &Daemon{
		cfg: Config{
			WorkspacesRoot:            t.TempDir(),
			DiskPressureMinFreeBytes:  20,
			DiskPressureCheckInterval:  time.Minute,
		},
	}

	if err := d.guardDiskPressure(context.Background(), true); err == nil {
		t.Fatal("expected guardDiskPressure to fail closed when free space stays below threshold")
	}
}
