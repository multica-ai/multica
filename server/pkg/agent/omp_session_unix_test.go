//go:build unix

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOmpFreshSessionThenResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Model-free reproduction of OMP 18.8.3's contract: --session rejects
	// missing/empty files; --session-dir creates and persists a new transcript.
	fake := filepath.Join(t.TempDir(), "omp")
	writeTestExecutable(t, fake, []byte(`#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    --session) session="$2"; shift ;;
    --session-dir) dir="$2"; shift ;;
  esac
  shift
done
if [ -n "$session" ]; then
  [ -s "$session" ] || exit 1
else
  [ -d "$dir" ] || exit 2
  session="$dir/runtime-chosen.jsonl"
  printf '%s\n' '{"type":"session","id":"test"}' > "$session"
fi
cat >> "$session"
printf '\n' >> "$session"
printf '%s\n' '{"type":"agent_end"}'
`))
	backend, err := ResolveBackend("omp", Config{ExecutablePath: fake, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	execute := func(prompt, resume string) Result {
		t.Helper()
		session, err := backend.Execute(t.Context(), prompt, ExecOptions{ResumeSessionID: resume, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		for range session.Messages {
		}
		return <-session.Result
	}
	var previous string
	for _, prompt := range []string{"fresh chat", "quick-create task"} {
		first := execute(prompt, "")
		if first.Status != "completed" || first.SessionID == "" || first.SessionID == previous {
			t.Fatalf("fresh result: %+v", first)
		}
		previous = first.SessionID
		second := execute("follow-up", first.SessionID)
		if second.Status != "completed" || second.SessionID != first.SessionID {
			t.Fatalf("resume result: %+v", second)
		}
		data, err := os.ReadFile(second.SessionID)
		if err != nil || !strings.Contains(string(data), prompt+"\nfollow-up\n") {
			t.Fatalf("history not preserved: %q, %v", data, err)
		}
	}
	// Old zero-byte transcripts must not be fabricated into valid resumes.
	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := execute("follow-up", empty); result.Status != "failed" {
		t.Fatalf("empty resume unexpectedly succeeded: %+v", result)
	}
	if info, err := os.Stat(empty); err != nil || info.Size() != 0 {
		t.Fatalf("empty resume was modified: %v, %v", info, err)
	}
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	if _, err := backend.Execute(t.Context(), "follow-up", ExecOptions{ResumeSessionID: missing}); err == nil {
		t.Fatal("missing resume unexpectedly started")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing resume was created: %v", err)
	}
}

func TestOmpFreshSessionRequiresPersistedTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := filepath.Join(t.TempDir(), "omp")
	writeTestExecutable(t, fake, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"agent_end\"}'\n"))
	backend, err := ResolveBackend("omp", Config{ExecutablePath: fake, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(t.Context(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || result.SessionID != "" || !strings.Contains(result.Error, "no persisted transcript") {
		t.Fatalf("must not publish a fabricated resume path: %+v", result)
	}
}
