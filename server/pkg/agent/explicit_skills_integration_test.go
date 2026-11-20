//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
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

// Run the selected subtest after each runtime upgrade. Native providers must
// leave runtime-generated expansion evidence as well as obey the skill; an
// injected fallback alone must not make their smoke check pass.
func TestExplicitSkillRealSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if runtime.GOOS == "windows" {
		t.Skip("wire capture uses a bash wrapper")
	}
	for _, scenario := range []string{"claude", "codex", "codex-fallback", "antigravity", "hermes"} {
		t.Run(scenario, func(t *testing.T) {
			provider := strings.TrimSuffix(scenario, "-fallback")
			command := provider
			if provider == "antigravity" {
				command = "agy"
			}
			if override := os.Getenv(strings.ToUpper(provider) + "_SKILL_SMOKE_BIN"); override != "" {
				command = override
			}
			binary, err := exec.LookPath(command)
			if err != nil {
				t.Skipf("%s unavailable: %v", provider, err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
			defer cancel()
			if version, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput(); err == nil {
				t.Logf("%s", strings.TrimSpace(string(version)))
			}
			root := t.TempDir()
			cwd := filepath.Join(root, "workdir")
			if err := os.MkdirAll(cwd, 0755); err != nil {
				t.Fatal(err)
			}
			config := Config{Logger: slog.Default(), Env: map[string]string{"IS_SANDBOX": "1"}}
			skillParent := filepath.Join(cwd, ".agents", "skills")
			evidenceRoot := root
			switch provider {
			case "claude":
				skillParent = filepath.Join(cwd, ".claude", "skills")
				home := filepath.Join(root, "claude-home")
				copySkillSmokeConfig(t, "CLAUDE_CONFIG_DIR", ".claude", home, []string{"settings.json", ".credentials.json", ".claude.json"})
				if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
					// Without a config-dir override, Claude's global state lives
					// beside ~/.claude, not inside it. Copy into the isolated dir;
					// never update the original account or project configuration.
					copySkillSmokeConfig(t, "HOME", "", home, []string{".claude.json"})
				}
				config.Env["CLAUDE_CONFIG_DIR"] = home
			case "codex":
				home := filepath.Join(root, "codex-home")
				copySkillSmokeConfig(t, "CODEX_HOME", ".codex", home, []string{"config.toml", "auth.json"})
				config.Env["CODEX_HOME"] = home
				skillParent = filepath.Join(home, "skills")
				if scenario == "codex-fallback" {
					// A materialized skill outside Codex's discovery paths must
					// reach the real runtime through the injection fallback.
					skillParent = filepath.Join(root, "materialized-skills")
				}
			}
			dir := filepath.Join(skillParent, "explicit-probe")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			body := "Reply with exactly: EXPLICIT_SKILL followed by the platform token and then the user token, separated by spaces. Do not call any tools."
			skill := "---\nname: explicit-probe\ndescription: Explicit invocation smoke test\ndisable-model-invocation: true\n---\n" + body
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0600); err != nil {
				t.Fatal(err)
			}
			if provider == "codex" {
				if err := os.MkdirAll(filepath.Join(dir, "agents"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "agents", "openai.yaml"), []byte("policy:\n  allow_implicit_invocation: false\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			invocation := &SkillInvocation{PlatformPrompt: "Platform token: PLATFORM_OK", UserMessage: "User token: USER_OK. Apply my selected skill.", Skills: []ExplicitSkill{{Name: "explicit-probe", Directory: dir, Body: body}}}
			invocation.Prompt = invocation.PlatformPrompt + "\nUser message:\n" + invocation.UserMessage
			wrapper := filepath.Join(root, "runtime-wrapper")
			writeTestExecutable(t, wrapper, []byte(`#!/usr/bin/env bash
set -o pipefail
log_path=''
previous=''
for arg in "$@"; do
  if [ "$previous" = '--log-file' ]; then log_path="$arg"; fi
  previous="$arg"
done
tee "$SKILL_SMOKE_INPUT" | "$SKILL_SMOKE_BINARY" "$@" | tee "$SKILL_SMOKE_WIRE"
result=${PIPESTATUS[1]}
if [ -n "$log_path" ] && [ -f "$log_path" ]; then cp "$log_path" "$SKILL_SMOKE_LOG"; fi
exit "$result"
`))
			config.ExecutablePath = wrapper
			config.Env["SKILL_SMOKE_BINARY"] = binary
			config.Env["SKILL_SMOKE_INPUT"] = filepath.Join(root, "input.jsonl")
			config.Env["SKILL_SMOKE_WIRE"] = filepath.Join(root, "wire.jsonl")
			config.Env["SKILL_SMOKE_LOG"] = filepath.Join(root, "runtime.log")
			backend, err := New(provider, config)
			if err != nil {
				t.Fatal(err)
			}
			opts := ExecOptions{Cwd: cwd, Timeout: 120 * time.Second, SkillInvocation: invocation, Model: os.Getenv(strings.ToUpper(provider) + "_SKILL_SMOKE_MODEL")}
			result := runSkillSmokeTurn(t, ctx, backend, invocation.FallbackPrompt(), opts)
			t.Logf("Skill response: %q", result.Output)
			if !strings.Contains(result.Output, "EXPLICIT_SKILL PLATFORM_OK USER_OK") {
				t.Errorf("skill or message context lost: %q", result.Output)
			}
			switch provider {
			case "claude":
				expanded := skillSmokeEvidence(evidenceRoot, "<command-name>/explicit-probe</command-name>")
				t.Logf("Claude native command-name evidence: %t", expanded)
				if !expanded {
					t.Fatal("no Claude command-name evidence; fallback is not native expansion")
				}
			case "codex":
				if scenario == "codex-fallback" {
					input, err := os.ReadFile(config.Env["SKILL_SMOKE_INPUT"])
					if err != nil {
						t.Fatal(err)
					}
					injected := false
					for _, line := range strings.Split(strings.TrimSpace(string(input)), "\n") {
						var request struct {
							Method string `json:"method"`
							Params struct {
								Input []struct {
									Text string `json:"text"`
								} `json:"input"`
							} `json:"params"`
						}
						if err := json.Unmarshal([]byte(line), &request); err != nil {
							t.Fatal(err)
						}
						if request.Method == "turn/start" {
							for _, block := range request.Params.Input {
								injected = injected || (strings.Contains(block.Text, "<selected-skill>") && strings.Contains(block.Text, dir) && strings.Contains(block.Text, body))
							}
						}
					}
					t.Logf("Codex fallback body and directory evidence: %t", injected)
					if !injected || skillSmokeEvidence(filepath.Join(root, "codex-home", "sessions"), "<name>explicit-probe</name>") {
						t.Fatal("expected injected skill body and directory without native skill expansion")
					}
					break
				}
				if !skillSmokeEvidence(filepath.Join(root, "codex-home", "sessions"), "<skill>") || !skillSmokeEvidence(filepath.Join(root, "codex-home", "sessions"), "<name>explicit-probe</name>") {
					t.Fatal("no Codex native skill injection in rollout")
				}
			case "antigravity":
				log, _ := os.ReadFile(config.Env["SKILL_SMOKE_LOG"])
				if !strings.Contains(string(log), `expanded slash command "explicit-probe"`) {
					t.Fatal("no agy slash expansion evidence")
				}
			}
			if !t.Failed() {
				t.Logf("%s selected skill passed; session=%s", scenario, result.SessionID)
			}
		})
	}
}

func runSkillSmokeTurn(t *testing.T, ctx context.Context, backend Backend, prompt string, opts ExecOptions) Result {
	t.Helper()
	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatal(err)
	}
	for message := range session.Messages {
		if message.Type == MessageToolUse {
			t.Errorf("unexpected tool call: %s", message.Tool)
		}
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Errorf("status=%s error=%s", result.Status, result.Error)
	}
	return result
}

func copySkillSmokeConfig(t *testing.T, variable, defaultDir, destination string, names []string) {
	t.Helper()
	source := os.Getenv(variable)
	if source == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		source = filepath.Join(home, defaultDir)
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(source, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func skillSmokeEvidence(root, marker string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), marker) {
			found = true
		}
		return nil
	})
	return found
}
