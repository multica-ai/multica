package taskauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testWorkspaceID = "68a5f501-c34f-417f-a29c-c65199b9ff33"
	testTaskID      = "c6a0dc62-89aa-4fd2-ad29-79d586147f99"
	testAgentID     = "b7eef942-0738-463b-9920-abda697da1c6"
)

func validAuthority() Authority {
	return Authority{
		ManagedBy:   ManagedBy,
		Version:     Version,
		ServerURL:   "https://api.example.test",
		WorkspaceID: testWorkspaceID,
		Token:       "mat_task_token",
		TaskID:      testTaskID,
		AgentID:     testAgentID,
	}
}

func TestWriteAndLoadAuthorityStrictRoundTrip(t *testing.T) {
	root := t.TempDir()
	path, err := Write(root, validAuthority())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if filepath.Dir(path) != root {
		t.Fatalf("authority path %q escaped root %q", path, root)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("authority mode = %v, want regular 0600", info.Mode())
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded != validAuthority() {
		t.Fatalf("loaded authority = %#v", loaded)
	}
}

func TestLoadAuthorityRejectsMalformedAndAmbiguousInputs(t *testing.T) {
	base := `{"managed_by":"multica-daemon-task-authority","version":1,"server_url":"https://api.example.test","workspace_id":"` + testWorkspaceID + `","token":"mat_task_token","task_id":"` + testTaskID + `","agent_id":"` + testAgentID + `"}`
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"managed_by":`},
		{name: "unknown field", body: strings.TrimSuffix(base, "}") + `,"extra":true}`},
		{name: "duplicate field", body: strings.Replace(base, `"version":1`, `"version":1,"version":1`, 1)},
		{name: "trailing JSON", body: base + `{}`},
		{name: "wrong managed by", body: strings.Replace(base, ManagedBy, "attacker", 1)},
		{name: "wrong version", body: strings.Replace(base, `"version":1`, `"version":2`, 1)},
		{name: "bad URL scheme", body: strings.Replace(base, "https://api.example.test", "file:///tmp/socket", 1)},
		{name: "URL credentials", body: strings.Replace(base, "https://api.example.test", "https://user:pass@api.example.test", 1)},
		{name: "bad workspace UUID", body: strings.Replace(base, testWorkspaceID, "workspace", 1)},
		{name: "bad task UUID", body: strings.Replace(base, testTaskID, "task", 1)},
		{name: "bad agent UUID", body: strings.Replace(base, testAgentID, "agent", 1)},
		{name: "member token", body: strings.Replace(base, "mat_task_token", "pat_member", 1)},
		{name: "oversized", body: base + strings.Repeat(" ", MaxBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "authority.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("invalid authority unexpectedly loaded: %s", tt.body)
			}
		})
	}
}

func TestLoadAuthorityRejectsSymlinkAndPermissiveMode(t *testing.T) {
	root := t.TempDir()
	path, err := Write(root, validAuthority())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("world-readable authority unexpectedly loaded")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "authority-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlink authority unexpectedly loaded")
	}
}
