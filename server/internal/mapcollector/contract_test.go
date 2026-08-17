package mapcollector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContractRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(`{"mapping_version":"NEW31-MAP-v1","unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadContract(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestLoadContractRejectsTrailingJSONValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.json")
	valid, err := os.ReadFile(filepath.Join("testdata", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid = append(valid, []byte(` {}`)...)
	if err := os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadContract(path); err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("expected trailing value rejection, got %v", err)
	}
}

func TestReadAndDestroyKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.key")
	key := []byte("0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAndDestroyKeyFile(path)
	if err != nil {
		t.Fatalf("ReadAndDestroyKeyFile: %v", err)
	}
	defer clear(got)
	if string(got) != string(key) {
		t.Fatalf("key changed while reading")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key file still exists: %v", err)
	}
}

func TestReadAndDestroyKeyFileRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.key")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndDestroyKeyFile(path); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("expected permission rejection, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rejected key file should remain for operator handling: %v", err)
	}
}

func TestReadAndDestroyKeyFileRejectsSymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target.key")
	if err := os.WriteFile(target, []byte("0123456789abcdef0123456789abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndDestroyKeyFile(link); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was modified: %v", err)
	}
}

func TestCycleMembers(t *testing.T) {
	got := cycleMembers(map[string]string{"a": "b", "b": "c", "c": "b", "d": ""})
	if strings.Join(got, ",") != "b,c" {
		t.Fatalf("cycleMembers = %v, want [b c]", got)
	}
}

func TestLoadContractUsesRFC8785CanonicalJSON(t *testing.T) {
	path := filepath.Join("testdata", "contract.json")
	_, canonical, err := LoadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonical), "\n") || strings.Contains(string(canonical), "  ") {
		t.Fatalf("canonical contract contains presentation whitespace")
	}
	if !strings.HasPrefix(string(canonical), `{"attachments":`) {
		t.Fatalf("canonical contract keys are not lexicographically ordered: %.40s", canonical)
	}
}
