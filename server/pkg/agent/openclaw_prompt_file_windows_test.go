//go:build windows

package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenclawWindowsPromptFile(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_OPENCLAW_PROMPT_TEST_HELPER", "1")
	for _, shim := range []bool{false, true} {
		name := "native"
		if shim {
			name = "cmd_shim"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "openclaw.exe")
			writeTestExecutable(t, executable, binary)
			if shim {
				executable = filepath.Join(dir, "openclaw.cmd")
				if err := os.WriteFile(executable, []byte("@echo off\r\n\"%~dp0openclaw.exe\" %*\r\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			recordPath := filepath.Join(dir, "record.json")
			t.Setenv("MULTICA_OPENCLAW_PROMPT_TEST_RECORD", recordPath)
			prompt := strings.Repeat("quoted \"input\" & %PATH%\r\n", 4000) + "Привет"
			system := "Literal system instructions"
			config := Config{ExecutablePath: executable, Logger: slog.Default(), Env: map[string]string{
				"MULTICA_OPENCLAW_PROMPT_TEST_HELPER": "1",
				"MULTICA_OPENCLAW_PROMPT_TEST_RECORD": recordPath,
			}}
			if shim {
				config.ExecutablePath = os.Getenv("ComSpec")
				if config.ExecutablePath == "" {
					t.Fatal("ComSpec is required for the cmd shim regression")
				}
				config.LaunchPrefix = []string{"/d", "/c", executable}
			}
			backend, err := New("openclaw", config)
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(t.Context(), prompt, ExecOptions{SystemPrompt: system, OpenclawMode: "gateway", Timeout: 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := <-session.Result
			if result.Status != "completed" || result.Output != "ok" {
				t.Fatalf("result = %+v", result)
			}
			var record struct {
				Args   []string
				Path   string
				Prompt string
			}
			raw, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if record.Prompt != system+"\n\n"+prompt {
				t.Fatal("prompt bytes changed")
			}
			for _, arg := range record.Args {
				if arg == "--message" || arg == "--local" || strings.Contains(arg, "quoted") {
					t.Fatal("prompt or local mode leaked into Gateway argv")
				}
			}
			if _, err := os.Stat(record.Path); !os.IsNotExist(err) {
				t.Fatalf("prompt file remains after result: %v", err)
			}
		})
	}
}
