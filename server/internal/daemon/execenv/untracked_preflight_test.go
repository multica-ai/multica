package execenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntrackedPreflightBytesIgnoresAndRecovery(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "debug", "large.bin"), "")
	if err := os.Truncate(filepath.Join(repo, "debug", "large.bin"), maxUntrackedBytes+1); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "services", "api", "notes"), "ok")
	check, err := InspectUntrackedReplay(context.Background(), filepath.Join(repo, "services", "api"))
	if err != nil {
		t.Fatal(err)
	}
	if check.Files != 2 || check.Bytes != maxUntrackedBytes+3 || check.Err() == nil {
		t.Fatalf("incorrect measurement: %+v", check)
	}
	if check.Largest[0].Path != "debug" || !strings.Contains(check.Err().Error(), "200 MiB") {
		t.Fatalf("missing actionable diagnostic: %v", check.Err())
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "debug/\n")
	check, err = InspectUntrackedReplay(context.Background(), repo)
	if err != nil || check.Err() != nil {
		t.Fatalf("ignore did not recover: %+v, %v", check, err)
	}
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "debug", "large.bin")); err != nil {
		t.Fatal(err)
	}
	check, err = InspectUntrackedReplay(context.Background(), repo)
	if err != nil || check.Err() != nil {
		t.Fatalf("remove did not recover: %+v, %v", check, err)
	}
}

func TestUntrackedPreflightCountAndSymlinks(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	for i := range maxUntrackedFiles + 1 {
		writeFile(t, filepath.Join(repo, "debug", fmt.Sprint(i)), "")
	}
	check, err := InspectUntrackedReplay(context.Background(), repo)
	if err != nil || check.Files != maxUntrackedFiles+1 || check.Bytes != 0 || check.Err() == nil {
		t.Fatalf("count limit not enforced: %+v, %v", check, err)
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "debug/\n")
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside, "")
	if err := os.Truncate(outside, maxUntrackedBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Skip(err)
	}
	check, err = InspectUntrackedReplay(context.Background(), repo)
	if err != nil || check.Symlinks != 1 || check.Bytes >= maxUntrackedBytes || check.Err() == nil {
		t.Fatalf("symlink followed or allowed: %+v, %v", check, err)
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "debug/\nlink\n")
	check, err = InspectUntrackedReplay(context.Background(), repo)
	if err != nil || check.Err() != nil {
		t.Fatalf("ignored symlink blocks replay: %+v, %v", check, err)
	}
}
