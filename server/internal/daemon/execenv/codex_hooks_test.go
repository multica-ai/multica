package execenv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSyncCodexHooksAndTrustCopiesAndRewritesTrustedPath(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	task := filepath.Join(root, "task")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := `{"hooks": []}`
	if err := os.WriteFile(filepath.Join(shared, "hooks.json"), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	sharedHooks := filepath.Join(shared, "hooks.json")
	trust := `trusted_hash = "sha256:keep-this-hash"`
	config := "[hooks.state." + strconv.Quote(sharedHooks+":pre_tool_use:0:0") + "]\n" + trust + "\n" +
		"[hooks.state." + strconv.Quote(sharedHooks+":user_prompt_submit:0:0") + "]\n" + trust + "\n" +
		"[hooks.state." + strconv.Quote(sharedHooks+":stop:0:0") + "]\n" + trust + "\n"
	if err := os.WriteFile(filepath.Join(shared, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncCodexHooksAndTrust(shared, task); err != nil {
		t.Fatal(err)
	}
	gotHooks, err := os.ReadFile(filepath.Join(task, "hooks.json"))
	if err != nil || string(gotHooks) != hooks {
		t.Fatalf("hooks.json = %q, err = %v", gotHooks, err)
	}
	gotConfig, err := os.ReadFile(filepath.Join(task, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"pre_tool_use", "user_prompt_submit", "stop"} {
		wantKey := strconv.Quote(filepath.Join(task, "hooks.json") + ":" + event + ":0:0")
		if !strings.Contains(string(gotConfig), wantKey) {
			t.Fatalf("task config did not preserve %s trust at rewritten path:\n%s", event, gotConfig)
		}
	}
	if strings.Count(string(gotConfig), trust) != 3 {
		t.Fatalf("task config did not preserve all trust hashes:\n%s", gotConfig)
	}
	sharedConfig, err := os.ReadFile(filepath.Join(shared, "config.toml"))
	if err != nil || string(sharedConfig) != config {
		t.Fatalf("shared config changed: %q, err = %v", sharedConfig, err)
	}
}

func TestSyncCodexHooksAndTrustDoesNotInventTrust(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	task := filepath.Join(root, "task")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "hooks.json"), []byte(`{"hooks": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config := "model = \"gpt-6\"\n"
	if err := os.WriteFile(filepath.Join(task, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncCodexHooksAndTrust(shared, task); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(task, "config.toml"))
	if err != nil || string(got) != config {
		t.Fatalf("config = %q, err = %v; expected no synthesized trust", got, err)
	}
}
