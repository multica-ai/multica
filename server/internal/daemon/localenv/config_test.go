package localenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
[environments.web]
label = "Web app"
start = "pnpm dev --port $MULTICA_PORT"

[environments.worker]
start = "pnpm worker"
url = ""
ttl = "2h"
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	web := cfg.Environments["web"]
	if web.URL != "http://localhost:$MULTICA_PORT" || web.Ready != web.URL || web.TTL != DefaultTTL || web.Label != "Web app" {
		t.Fatalf("web = %+v", web)
	}
	worker := cfg.Environments["worker"]
	if worker.URL != "" || worker.Ready != "" || worker.TTL != 2*time.Hour {
		t.Fatalf("worker = %+v, want no URL and a 2h TTL", worker)
	}
	if got := strings.Join(cfg.Names(), ","); got != "web,worker" {
		t.Fatalf("Names = %s", got)
	}
	if got := expandPort(web.URL, 41000); got != "http://localhost:41000" {
		t.Fatalf("expandPort = %s", got)
	}
}

func TestParseConfigRejects(t *testing.T) {
	cases := map[string]string{
		"no environments": ``,
		"bad name":        "[environments.Web]\nstart = \"x\"",
		"no start":        "[environments.web]\nlabel = \"x\"",
		"ttl too long":    "[environments.web]\nstart = \"x\"\nttl = \"720h\"",
		"ttl unparsable":  "[environments.web]\nstart = \"x\"\nttl = \"soon\"",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(body)); err == nil {
				t.Fatal("ParseConfig accepted an invalid file")
			}
		})
	}
}

func writeConfig(t *testing.T, repo, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".multica"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(ConfigPath)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindConfigWalksUpToTheBoundary(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "workdir", "app")
	sub := filepath.Join(repo, "packages", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, repo, "[environments.web]\nstart = \"x\"")

	cfg, err := FindConfig(sub, root)
	if err != nil {
		t.Fatalf("FindConfig: %v", err)
	}
	if cfg.RepoRoot != repo {
		t.Fatalf("RepoRoot = %s, want %s", cfg.RepoRoot, repo)
	}

	// A config above the task directory belongs to someone else.
	outside := t.TempDir()
	task := filepath.Join(outside, "task")
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, outside, "[environments.web]\nstart = \"x\"")
	if _, err := FindConfig(task, task); !errors.Is(err, ErrNoConfig) {
		t.Fatalf("FindConfig past the boundary = %v, want ErrNoConfig", err)
	}
	if _, err := FindConfig(outside, task); err == nil {
		t.Fatal("FindConfig accepted a directory outside the task")
	}
}

// The repository's own environments file is what agents working on Multica
// start, so a typo in it should fail here rather than in an agent's run.
func TestRepositoryConfigParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", filepath.FromSlash(ConfigPath)))
	if err != nil {
		t.Fatalf("read %s: %v", ConfigPath, err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got := strings.Join(cfg.Names(), ","); got != "desktop,web" {
		t.Fatalf("declared = %s, want desktop and web", got)
	}
	// Desktop has nothing to open, but still waits for its app to be up.
	if desktop := cfg.Environments["desktop"]; desktop.URL != "" || desktop.Ready == "" {
		t.Fatalf("desktop = %+v, want no URL and a ready check", desktop)
	}
}
