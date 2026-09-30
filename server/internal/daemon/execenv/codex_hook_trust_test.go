package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestRehomeCodexHookTrust(t *testing.T) {
	t.Parallel()

	const shared = "/home/u/.codex"
	const task = "/ws/task/codex-home"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no hook state — returned unchanged",
			in:   "model = \"o3\"\n",
			want: "model = \"o3\"\n",
		},
		{
			name: "shared-home key is re-keyed with hash and surroundings kept",
			in: `model = "o3"

[hooks.state]

[hooks.state."/home/u/.codex/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"
# END managed

[tui]
x = 1
`,
			want: `model = "o3"

[hooks.state]

[hooks.state."/ws/task/codex-home/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"
# END managed

[tui]
x = 1
`,
		},
		{
			name: "hooks.json keys follow the same rule",
			in: `[hooks.state."/home/u/.codex/hooks.json:session_start:0:0"]
trusted_hash = "sha256:bbb"
`,
			want: `[hooks.state."/ws/task/codex-home/hooks.json:session_start:0:0"]
trusted_hash = "sha256:bbb"
`,
		},
		{
			name: "keys outside the shared home are left alone",
			in: `[hooks.state."/repo/.codex/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:ccc"

[hooks.state."/home/u/.codex-other/config.toml:stop:0:0"]
trusted_hash = "sha256:ddd"
`,
			want: `[hooks.state."/repo/.codex/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:ccc"

[hooks.state."/home/u/.codex-other/config.toml:stop:0:0"]
trusted_hash = "sha256:ddd"
`,
		},
		{
			name: "a key already naming the task home is not duplicated",
			in: `[hooks.state."/home/u/.codex/config.toml:stop:0:0"]
trusted_hash = "sha256:old"

[hooks.state."/ws/task/codex-home/config.toml:stop:0:0"]
trusted_hash = "sha256:eee"
`,
			want: `
[hooks.state."/ws/task/codex-home/config.toml:stop:0:0"]
trusted_hash = "sha256:eee"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rehomeCodexHookTrust(tt.in, shared, task); got != tt.want {
				t.Errorf("rehomeCodexHookTrust mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestRehomeCodexHookTrustSymlinkedHomes mirrors a shared home that is itself
// a symlink (~/.codex -> repo/.codex): trust written under either the raw or
// resolved shared path lands under both the raw and resolved task path, each
// target exactly once.
func TestRehomeCodexHookTrustSymlinkedHomes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sharedReal := filepath.Join(root, "repo", ".codex")
	sharedLink := filepath.Join(root, "home", ".codex")
	taskReal := filepath.Join(root, "disk", "codex-home")
	taskLink := filepath.Join(root, "ws", "codex-home")
	for _, d := range []string{sharedReal, taskReal, filepath.Dir(sharedLink), filepath.Dir(taskLink)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(sharedReal, sharedLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(taskReal, taskLink); err != nil {
		t.Fatal(err)
	}

	in := `[features]
hooks = true

# BEGIN managed
[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "route --hook"

[hooks.state."` + sharedLink + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:fff"

[hooks.state."` + sharedReal + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:fff"
# END managed
`
	want := `[features]
hooks = true

# BEGIN managed
[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "route --hook"

[hooks.state."` + taskLink + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:fff"
[hooks.state."` + taskReal + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:fff"

# END managed
`
	if got := rehomeCodexHookTrust(in, sharedLink, taskLink); got != want {
		t.Errorf("rehomeCodexHookTrust mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestSanitizeCopiedCodexConfigRehomesHookTrust(t *testing.T) {
	t.Parallel()

	taskHome := t.TempDir()
	configPath := filepath.Join(taskHome, "config.toml")
	original := `[hooks.state."/home/u/.codex/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sanitizeCopiedCodexConfig(configPath, "/home/u/.codex", taskHome); err != nil {
		t.Fatalf("sanitizeCopiedCodexConfig failed: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for _, form := range codexHomePathForms(taskHome) {
		want += `[hooks.state."` + form + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"
`
	}
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

// Re-applying the rewrite must be a no-op: a reused home whose config.toml
// sync failed keeps the previous, already re-keyed copy, and a task home that
// lives under the shared home must never match and grow a longer prefix.
func TestRehomeCodexHookTrustIdempotentAndBounded(t *testing.T) {
	t.Parallel()

	const shared = "/home/u/.codex"
	in := `[hooks.state."/home/u/.codex/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"

[hooks.state."plugin@local:hooks.json:stop:0:0"]
trusted_hash = "sha256:ppp"

[hooks.state."/home/u/.codex/plugins/x/hooks.json:stop:0:0"]
trusted_hash = "sha256:nnn"
`
	for _, task := range []string{"/ws/task/codex-home", "/home/u/.codex/multica/codex-home"} {
		once := rehomeCodexHookTrust(in, shared, task)
		if twice := rehomeCodexHookTrust(once, shared, task); twice != once {
			t.Errorf("task %s: second pass changed the config\nonce:  %q\ntwice: %q", task, once, twice)
		}
		want := strings.Replace(in, "/home/u/.codex/config.toml:", task+"/config.toml:", 1)
		if once != want {
			t.Errorf("task %s: only the home-level key should move\n got: %q\nwant: %q", task, once, want)
		}
	}

	if got := rehomeCodexHookTrust(in, shared, shared); got != in {
		t.Errorf("task home == shared home should leave the config unchanged, got %q", got)
	}
}

// End to end through prepareCodexHome, twice on the same home (task reuse):
// the per-task config must stay valid TOML, keep exactly one hook definition,
// and carry each trust key exactly once — never an extra hook per prepare.
func TestPrepareCodexHomeRehomesHookTrustOnReuse(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	shared := `model = "o3"

[features]
hooks = true

[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "route --hook"

[hooks.state."` + sharedHome + `/config.toml:user_prompt_submit:0:0"]
trusted_hash = "sha256:aaa"
`
	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(shared), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	for run := 1; run <= 2; run++ {
		if err := prepareCodexHome(codexHome, testLogger()); err != nil {
			t.Fatalf("prepare #%d: %v", run, err)
		}
		data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Hooks struct {
				UserPromptSubmit []map[string]any          `toml:"UserPromptSubmit"`
				State            map[string]map[string]any `toml:"state"`
			} `toml:"hooks"`
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("prepare #%d: per-task config.toml is not valid TOML: %v\n%s", run, err, data)
		}
		if n := len(cfg.Hooks.UserPromptSubmit); n != 1 {
			t.Errorf("prepare #%d: %d UserPromptSubmit groups, want 1", run, n)
		}
		wantKeys := map[string]bool{}
		for _, form := range codexHomePathForms(codexHome) {
			wantKeys[form+"/config.toml:user_prompt_submit:0:0"] = true
		}
		if len(cfg.Hooks.State) != len(wantKeys) {
			t.Errorf("prepare #%d: hooks.state keys = %v, want %v", run, cfg.Hooks.State, wantKeys)
		}
		for key, entry := range cfg.Hooks.State {
			if !wantKeys[key] || entry["trusted_hash"] != "sha256:aaa" {
				t.Errorf("prepare #%d: unexpected hooks.state entry %q = %v", run, key, entry)
			}
		}
	}
}

func TestSanitizeCopiedCodexConfigRefusesInvalidRehome(t *testing.T) {
	t.Parallel()

	taskHome := t.TempDir()
	configPath := filepath.Join(taskHome, "config.toml")
	// A dotted key already defines the task key's table, so appending it as a
	// header would redefine it; the original must survive untouched.
	original := `hooks.state."` + taskHome + `/config.toml:stop:0:0".trusted_hash = "sha256:x"

[hooks.state."/home/u/.codex/config.toml:stop:0:0"]
trusted_hash = "sha256:y"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sanitizeCopiedCodexConfig(configPath, "/home/u/.codex", taskHome); err == nil {
		t.Fatal("expected an error for a re-key that breaks the TOML document")
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != original {
		t.Errorf("config.toml must be left as-is on failure, got %q", data)
	}
}
