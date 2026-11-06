package repocache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepoLockSharedAcrossCachesAndEviction(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "workspace", "repo.git")
	a, b := New(root, testLogger()), New(root, testLogger())
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.WithRepoLock(bare, func() error {
		ran, err := b.WithRepoMaintenance(context.Background(), bare, func(context.Context) error { t.Error("foreign maintenance acquired busy repo"); return nil })
		if err != nil || ran {
			t.Fatalf("maintenance ran=%v err=%v", ran, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err = b.WithRepoLockContext(ctx, bare, func() error { t.Error("foreign foreground acquired busy repo"); return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wait error=%v", err)
		}
		if err := os.RemoveAll(bare); err != nil {
			return err
		}
		ran, err = b.WithRepoMaintenance(context.Background(), bare, func(context.Context) error { t.Error("eviction removed exclusion lock"); return nil })
		if err != nil || ran {
			t.Fatalf("after removal ran=%v err=%v", ran, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.WithRepoLock(bare, func() error { return os.MkdirAll(bare, 0o755) }); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	ran, err := a.WithRepoMaintenance(context.Background(), bare, func(context.Context) error { return nil })
	if err != nil || !ran {
		t.Fatalf("idle maintenance ran=%v err=%v", ran, err)
	}
}
