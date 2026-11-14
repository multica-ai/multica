package daemon

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenclawFinalEnvironment(t *testing.T) {
	for _, tc := range []struct {
		provider, mode string
		isolated       bool
	}{
		{"openclaw", "isolated-local", true}, {"openclaw", "local", false},
		{"openclaw", "gateway", false}, {"hermes", "isolated-local", false},
	} {
		t.Run(tc.provider+tc.mode, func(t *testing.T) {
			root := t.TempDir()
			env := map[string]string{"OPENCLAW_STATE_DIR": "inherited", "OPENCLAW_AGENT_DIR": "inherited", "PI_CODING_AGENT_DIR": "inherited"}
			custom := map[string]string{"OPENCLAW_STATE_DIR": "custom", "OPENCLAW_AGENT_DIR": "custom", "PI_CODING_AGENT_DIR": "custom", "UNRELATED": "retained"}
			layerTaskEnvironment(env, custom, "", tc.provider, tc.mode, root, nil)
			for _, key := range []string{"OPENCLAW_STATE_DIR", "OPENCLAW_AGENT_DIR", "PI_CODING_AGENT_DIR"} {
				want := "custom"
				if tc.isolated {
					want = ""
					if key == "OPENCLAW_STATE_DIR" {
						want = filepath.Join(root, "openclaw-state")
					}
				}
				if env[key] != want {
					t.Errorf("%s=%q want %q", key, env[key], want)
				}
			}
			// Exercise the same assembled map at a real child environment boundary.
			if path, err := exec.LookPath("env"); err == nil {
				cmd := exec.Command(path)
				for k, v := range env {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
				out, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(out), "UNRELATED=retained") {
					t.Fatal("lost custom env")
				}
				if tc.isolated && strings.Contains(string(out), "=custom") {
					t.Fatalf("isolation overridden: %s", out)
				}
			}
		})
	}
}
