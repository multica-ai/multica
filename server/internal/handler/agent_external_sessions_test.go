package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionIDFromCodexResumeCommand(t *testing.T) {
	sessionID := "019d9699-fbec-7f43-a3dd-67b8afd5f5d6"

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "direct codex resume",
			command: "/usr/local/bin/codex resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6",
			want:    sessionID,
		},
		{
			name:    "node wrapper with flags",
			command: "node /opt/codex/bin/codex.js resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6 --workspace /repo",
			want:    sessionID,
		},
		{
			name:    "case insensitive",
			command: "/usr/bin/CODEX ReSuMe 019D9699-FBEC-7F43-A3DD-67B8AFD5F5D6",
			want:    sessionID,
		},
		{
			name:    "no codex",
			command: "python app.py resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6",
			want:    "",
		},
		{
			name:    "no resume subcommand",
			command: "/usr/local/bin/codex run --model gpt-5",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionIDFromCodexResumeCommand(tt.command)
			if got != tt.want {
				t.Fatalf("sessionIDFromCodexResumeCommand(%q): want %q, got %q", tt.command, tt.want, got)
			}
		})
	}
}

func TestParseCodexResumeProcesses(t *testing.T) {
	psOutput := `
  101  1 pts/0 /usr/local/bin/codex resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6
  102  1 pts/1 bash -lc "echo hello"
  103  1 ? node /opt/codex/bin/codex.js resume 019d96b0-288d-7bc3-9488-275af8d26876 --foo
`

	got := parseCodexResumeProcesses(psOutput)
	if len(got) != 2 {
		t.Fatalf("expected 2 codex resume processes, got %d", len(got))
	}

	if got[0].PID != 101 || got[0].PPID != 1 || got[0].TTY != "pts/0" {
		t.Fatalf("unexpected first process metadata: %+v", got[0])
	}
	if got[0].SessionID != "019d9699-fbec-7f43-a3dd-67b8afd5f5d6" {
		t.Fatalf("unexpected first session id: %q", got[0].SessionID)
	}

	if got[1].PID != 103 || got[1].TTY != "?" {
		t.Fatalf("unexpected second process metadata: %+v", got[1])
	}
	if got[1].SessionID != "019d96b0-288d-7bc3-9488-275af8d26876" {
		t.Fatalf("unexpected second session id: %q", got[1].SessionID)
	}
}

func TestMergeExternalSessions(t *testing.T) {
	older := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)

	fileSessions := []ExternalSessionResponse{
		{
			SessionID:  "019d9699-fbec-7f43-a3dd-67b8afd5f5d6",
			WorkDir:    "/workspace/from-file",
			LastSeenAt: older,
			Source:     "session_file",
		},
	}
	processSessions := []ExternalSessionResponse{
		{
			SessionID:  "019d9699-fbec-7f43-a3dd-67b8afd5f5d6",
			WorkDir:    "/workspace/from-process",
			LastSeenAt: newer,
			Source:     "process",
			IsRunning:  true,
			LeaderPID:  2222,
			Command:    "codex resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6",
		},
		{
			SessionID:  "019d96b0-288d-7bc3-9488-275af8d26876",
			WorkDir:    "/workspace/second",
			LastSeenAt: newer,
			Source:     "process",
			IsRunning:  true,
			LeaderPID:  3333,
		},
	}

	merged := mergeExternalSessions(fileSessions, processSessions)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged sessions, got %d", len(merged))
	}

	first := merged[0]
	if first.SessionID != "019d9699-fbec-7f43-a3dd-67b8afd5f5d6" {
		t.Fatalf("expected first session to be merged primary session, got %q", first.SessionID)
	}
	if first.WorkDir != "/workspace/from-process" {
		t.Fatalf("expected process workdir to win for running session, got %q", first.WorkDir)
	}
	if !first.IsRunning {
		t.Fatalf("expected merged session to be running")
	}
	if first.Source != "merged" {
		t.Fatalf("expected merged source, got %q", first.Source)
	}
	if first.LeaderPID != 2222 {
		t.Fatalf("expected leader pid 2222, got %d", first.LeaderPID)
	}
	if first.Command == "" {
		t.Fatalf("expected command to be preserved")
	}
	if first.LastSeenAt != newer {
		t.Fatalf("expected newer timestamp %q, got %q", newer, first.LastSeenAt)
	}
}

func TestReadProcessCommand_ParsesCmdline(t *testing.T) {
	tmp := t.TempDir()
	pid := 4321
	pidDir := filepath.Join(tmp, "4321")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}

	cmdline := []byte("/usr/local/bin/codex\x00resume\x00019d9699-fbec-7f43-a3dd-67b8afd5f5d6\x00--flag\x00")
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), cmdline, 0o644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}

	got := readProcessCommand(tmp, pid)
	want := "/usr/local/bin/codex resume 019d9699-fbec-7f43-a3dd-67b8afd5f5d6 --flag"
	if got != want {
		t.Fatalf("readProcessCommand: want %q, got %q", want, got)
	}
}
