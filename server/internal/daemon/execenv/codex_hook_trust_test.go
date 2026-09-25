package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
