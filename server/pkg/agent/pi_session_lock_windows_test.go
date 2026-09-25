//go:build windows

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPiSessionFileLockDoesNotBlockTranscriptIO(t *testing.T) {
	t.Parallel()

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	content := []byte("{\"type\":\"session\"}\n")
	if err := os.WriteFile(sessionPath, content, 0o644); err != nil {
		t.Fatalf("create session file: %v", err)
	}

	claim, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		t.Fatalf("lock session file: %v", err)
	}
	if !locked {
		t.Fatal("session-file lock was unexpectedly busy")
	}
	defer releasePiSessionFileLock(claim)

	transcript, err := os.OpenFile(sessionPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open transcript while session is running: %v", err)
	}
	defer transcript.Close()

	got := make([]byte, len(content))
	if _, err := transcript.ReadAt(got, 0); err != nil {
		t.Fatalf("read transcript while session is running: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("transcript = %q, want %q", got, content)
	}
	if _, err := transcript.WriteAt([]byte("X"), 0); err != nil {
		t.Fatalf("write transcript while session is running: %v", err)
	}
}

func TestPiSessionFileLockSerializesWriters(t *testing.T) {
	t.Parallel()

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("create session file: %v", err)
	}

	first, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	if !locked {
		t.Fatal("first lock was unexpectedly busy")
	}

	second, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		releasePiSessionFileLock(first)
		t.Fatalf("attempt competing lock: %v", err)
	}
	if locked {
		releasePiSessionFileLock(second)
		releasePiSessionFileLock(first)
		t.Fatal("competing lock unexpectedly succeeded")
	}

	releasePiSessionFileLock(first)

	third, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		t.Fatalf("reacquire lock after release: %v", err)
	}
	if !locked {
		t.Fatal("lock remained busy after release")
	}
	releasePiSessionFileLock(third)
}
