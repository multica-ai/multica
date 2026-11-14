package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenclawPrivateFileReuse(t *testing.T) {
	for _, name := range []string{openclawConfigFile, openclawMcpResetFile} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name)
			if err := os.WriteFile(path, []byte(`{"old":true}`), 0644); err != nil {
				t.Fatal(err)
			}
			// A reader of the old inode must retain complete JSON after replacement.
			old, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			if err := writeOpenclawPrivateFile(root, name, []byte(`{"new":true}`)); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Errorf("mode=%v", info.Mode())
			}
			b := make([]byte, 100)
			n, _ := old.Read(b)
			if string(b[:n]) != `{"old":true}` {
				t.Error("replacement modified existing inode")
			}
		})
	}
}

func TestOpenclawPrivateFileRejectSymlink(t *testing.T) {
	for _, name := range []string{openclawConfigFile, openclawMcpResetFile} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(t.TempDir(), "shared")
			if err := os.WriteFile(target, []byte("unchanged"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Skip(err)
			}
			if err := writeOpenclawPrivateFile(root, name, []byte("new")); err == nil {
				t.Error("accepted symlink")
			}
			b, err := os.ReadFile(target)
			if err != nil || string(b) != "unchanged" {
				t.Fatal("modified shared target")
			}
		})
	}
}

func TestOpenclawIsolationRejectNestedSymlink(t *testing.T) {
	for _, suffix := range []string{"agents", "agents/main", "agents/main/agent"} {
		t.Run(suffix, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "openclaw-state", suffix)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Skip(err)
			}
			rows := []any{map[string]any{openclawEntriesKeyField: "main"}}
			cfg := buildPerTaskOpenclawConfig("", false, "", rows, openclawAgentsSourceEntries, root, nil, false, OpenclawGatewayPin{})
			if err := prepareOpenclawIsolation(cfg, root, rows, openclawAgentsSourceEntries, nil); err == nil {
				t.Fatal("accepted nested state symlink")
			}
		})
	}
}
