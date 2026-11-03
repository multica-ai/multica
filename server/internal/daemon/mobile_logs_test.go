package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/p2p"
)

func TestMobileLogsReportsALinkThatIsNotRunning(t *testing.T) {
	logs := &mobileLogs{link: func() *p2p.Service { return nil }, dir: t.TempDir(), now: time.Now}
	if _, err := logs.Phones(); !errors.Is(err, errDeviceLinkNotRunning) {
		t.Fatalf("phones err = %v", err)
	}
	if _, err := logs.Read(context.Background(), "ios-a", 60); !errors.Is(err, errDeviceLinkNotRunning) {
		t.Fatalf("read err = %v", err)
	}
}

func TestMobileExportsAreBounded(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	logs := &mobileLogs{dir: dir, now: func() time.Time { return now }}
	write := func(name string, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	id := "00000000-0000-0000-0000-"
	write("multica-mobile-logs-2026-01-01T00-00-00Z-"+id+"000000000000.jsonl", 8*24*time.Hour)
	for i := range maxMobileExports + 2 {
		write("multica-mobile-logs-2026-01-02T00-00-00Z-"+id+strings.Repeat("a", 11)+string(rune('a'+i%6))+string(rune('a'+i/6))+".jsonl", time.Duration(i)*time.Minute)
	}
	write("keep-me.txt", 30*24*time.Hour)
	logs.prune(now)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept := 0
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "2026-01-01") {
			t.Fatal("kept an expired export")
		}
		if mobileExportName.MatchString(entry.Name()) {
			kept++
		}
	}
	if kept != maxMobileExports {
		t.Fatalf("kept %d exports", kept)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep-me.txt")); err != nil {
		t.Fatal("removed a file that is not an export")
	}
}
