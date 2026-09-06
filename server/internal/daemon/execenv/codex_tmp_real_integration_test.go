//go:build agentintegration

package execenv

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCodexSharedTemporaryCacheRealTask verifies the real Codex CLI can start
// with the daemon's shared .tmp layout and does not replace the task link with
// a private marketplace/plugin clone. It is opt-in because it uses the user's
// authenticated Codex account and may consume quota.
func TestCodexSharedTemporaryCacheRealTask(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real Codex CLI and account access")
	}
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not on PATH; skipping real-binary smoke test")
	}
	sharedHome := strings.TrimSpace(os.Getenv("MULTICA_REAL_CODEX_HOME"))
	if sharedHome == "" {
		userHome, homeErr := os.UserHomeDir()
		if homeErr != nil {
			t.Fatalf("resolve user home: %v", homeErr)
		}
		sharedHome = filepath.Join(userHome, ".codex")
	}
	sharedHome, err = filepath.Abs(sharedHome)
	if err != nil {
		t.Fatalf("resolve shared Codex home: %v", err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	environment, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "00000000-0000-4000-8000-000000000011",
		TaskID:         "00000000-0000-4000-8000-000000000012",
		AgentName:      "codex-real-cache-smoke",
		Provider:       "codex",
		Task: TaskContextForEnv{
			IssueID: "codex-real-cache-smoke",
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare daemon-equivalent Codex environment: %v", err)
	}
	defer environment.Cleanup(true)

	taskTmp := filepath.Join(environment.CodexHome, ".tmp")
	sharedTmp := filepath.Join(sharedHome, ".tmp")
	assertSameCodexTemporaryCache(t, taskTmp, sharedTmp)
	before := localBytesWithoutLinks(t, environment.CodexHome)

	lastMessage := filepath.Join(t.TempDir(), "last-message.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"exec",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--output-last-message", lastMessage,
		"Reply with exactly: MULTICA_CODEX_TMP_OK",
	)
	cmd.Dir = environment.WorkDir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CODEX_HOME=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+environment.CodexHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real Codex task failed: %v\n%s", err, output)
	}
	message, err := os.ReadFile(lastMessage)
	if err != nil {
		t.Fatalf("read real Codex final message: %v", err)
	}
	if strings.TrimSpace(string(message)) != "MULTICA_CODEX_TMP_OK" {
		t.Fatalf("real Codex final message = %q", message)
	}

	assertSameCodexTemporaryCache(t, taskTmp, sharedTmp)
	after := localBytesWithoutLinks(t, environment.CodexHome)
	t.Logf("real Codex cache smoke passed; task-local CODEX_HOME bytes before=%d after=%d", before, after)
}

func assertSameCodexTemporaryCache(t *testing.T, taskTmp, sharedTmp string) {
	t.Helper()
	info, err := os.Lstat(taskTmp)
	if err != nil {
		t.Fatalf("stat task .tmp: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("task .tmp is not a symlink: mode=%v", info.Mode())
	}
	taskInfo, err := os.Stat(taskTmp)
	if err != nil {
		t.Fatalf("resolve task .tmp: %v", err)
	}
	sharedInfo, err := os.Stat(sharedTmp)
	if err != nil {
		t.Fatalf("stat shared .tmp: %v", err)
	}
	if !os.SameFile(taskInfo, sharedInfo) {
		t.Fatal("task .tmp no longer resolves to the shared Codex cache")
	}
}

func localBytesWithoutLinks(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	if err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatalf("measure task-local Codex home: %v", err)
	}
	return total
}
