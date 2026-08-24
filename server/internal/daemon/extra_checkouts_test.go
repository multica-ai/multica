package daemon

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// GAP-11 (fork issue #12): opt-in additional repo checkouts per task.

func TestPrepareExtraCheckoutsCreatesSiblingCheckout(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	d := &Daemon{
		repoCache: repocache.New(t.TempDir(), slog.Default()),
		logger:    slog.Default(),
	}
	envRoot := filepath.Join(t.TempDir(), "ws-1", "taskshort1")

	repos := []RepoData{{URL: sourceRepo, AdditionalCheckout: true}}
	if err := d.prepareExtraCheckouts(repos, "ws-1", envRoot, "agent", "task-1", slog.Default()); err != nil {
		t.Fatalf("prepareExtraCheckouts: %v", err)
	}

	checkout := filepath.Join(envRoot, "extra", repocache.RepoNameFromURL(sourceRepo))
	out, err := exec.Command("git", "-C", checkout, "rev-parse", "--verify", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("expected a working checkout at %s: %v: %s", checkout, err, out)
	}
}

func TestPrepareExtraCheckoutsNoFlagIsNoOp(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	d := &Daemon{
		repoCache: repocache.New(t.TempDir(), slog.Default()),
		logger:    slog.Default(),
	}
	envRoot := filepath.Join(t.TempDir(), "ws-1", "taskshort2")

	// Default (flag unset) must not touch the filesystem at all.
	if err := d.prepareExtraCheckouts([]RepoData{{URL: sourceRepo}}, "ws-1", envRoot, "agent", "task-1", slog.Default()); err != nil {
		t.Fatalf("prepareExtraCheckouts (default): %v", err)
	}
	if _, err := os.Stat(filepath.Join(envRoot, "extra")); !os.IsNotExist(err) {
		t.Fatalf("expected no extra/ directory without opt-in, stat err = %v", err)
	}

	// Nil cache must also stay a no-op rather than panic.
	if err := d.prepareExtraCheckouts([]RepoData{{URL: sourceRepo, AdditionalCheckout: true}}, "ws-1", envRoot, "agent", "task-1", slog.Default()); err != nil {
		t.Fatalf("prepareExtraCheckouts (nil cache): %v", err)
	}
}

func TestConvertReposForEnvNamesExtraCheckoutPath(t *testing.T) {
	t.Parallel()

	repos := []RepoData{
		{URL: "https://github.com/org/main-repo"},
		{URL: "https://github.com/org/side-repo.git", AdditionalCheckout: true},
	}
	got := convertReposForEnv(repos, "/env/root/extra")
	if got[0].ExtraCheckoutPath != "" {
		t.Fatalf("unflagged repo must not name an extra path, got %q", got[0].ExtraCheckoutPath)
	}
	want := filepath.Join("/env/root/extra", "side-repo")
	if got[1].ExtraCheckoutPath != want {
		t.Fatalf("ExtraCheckoutPath = %q, want %q", got[1].ExtraCheckoutPath, want)
	}
}
